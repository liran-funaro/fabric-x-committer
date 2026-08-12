/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package mock

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric-x-common/common/channelconfig"
	"github.com/hyperledger/fabric-x-common/common/util"
	"github.com/hyperledger/fabric-x-common/msp"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"github.com/hyperledger/fabric-x-common/tools/cryptogen"
	"github.com/hyperledger/fabric-x-common/utils/testcrypto"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/proto"

	"github.com/hyperledger/fabric-x-committer/utils"
	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/grpcerror"
	"github.com/hyperledger/fabric-x-committer/utils/ordererdial"
	"github.com/hyperledger/fabric-x-committer/utils/serialization"
	"github.com/hyperledger/fabric-x-committer/utils/serve"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

type (
	// OrdererConfig configuration for the mock orderer.
	OrdererConfig struct {
		Servers                 []*serve.ServerConfig         `mapstructure:"servers"`
		BlockSize               int                           `mapstructure:"block-size"`
		BlockTimeout            time.Duration                 `mapstructure:"block-timeout"`
		OutBlockCapacity        int                           `mapstructure:"out-block-capacity"`
		PayloadCacheSize        int                           `mapstructure:"payload-cache-size"`
		ArtifactsPath           string                        `mapstructure:"artifacts-path"`
		GenesisBlockPath        string                        `mapstructure:"genesis-block-path"`
		ConsentersMSPIdentities []*ordererdial.IdentityConfig `mapstructure:"consenter-msp-identities"`
		SendGenesisBlock        bool                          `mapstructure:"send-genesis-block"`

		// TestServerParameters is only used for internal testing.
		TestServerParameters test.StartServerParameters
	}

	// envelopeEntry wraps an envelope with an optional synchronization signal.
	// Broadcast (gRPC) sends entries without done (async, buffered for throughput).
	// SubmitEnv (test helper) sends entries with done (sync, blocks until collected).
	envelopeEntry struct {
		env  *common.Envelope
		done *channel.Ready
	}

	// Orderer supports running multiple mock-orderer services which mocks a consortium.
	Orderer struct {
		streamStateManager[OrdererStreamState]
		endpointToPartyState utils.SyncMap[string, *PartyState]
		genesisBlock         BlockWithConsenters
		inEnvs               chan envelopeEntry
		inBlocks             chan *BlockWithConsenters
		cutBlock             chan any
		cache                *blockCache
		healthcheck          *health.Server
		tlsUpdater           serve.DynamicTLSUpdater

		// config uses atomic.Pointer to allow safe concurrent reads by the Run() goroutine
		// while supporting runtime updates (e.g., BlockTimeout changes in tests).
		// Always use Load() to read and Store() to update the entire config struct.
		config atomic.Pointer[OrdererConfig]
	}

	// OrdererStreamState holds the streams state.
	OrdererStreamState struct {
		StreamInfo
		*PartyState
		DataBlockStream bool
	}

	// PartyState holds the shared state of all streams of a party.
	// HoldFromBlock will hold the blocks starting from this number.
	// If HoldFromBlock is 0, no blocks will be held.
	// ReplaceBlock holds the blocks to replace, with the block number as the key.
	// This behavior is permanent until the entry is removed from the ReplaceBlock map.
	PartyState struct {
		PartyID       uint32
		HoldFromBlock atomic.Uint64
		ReplaceBlock  utils.SyncMap[uint64, *common.Block]
	}

	// BlockWithConsenters is used to submit a new config block with its consenters.
	BlockWithConsenters struct {
		Block             *common.Block
		ConsenterMetadata []byte
		ConsenterSigners  []msp.SigningIdentity
	}

	blockCache struct {
		storage           []*common.Block
		maxDeliveredBlock int64
		maxAddedBlock     int64
		mu                *sync.Cond
	}
)

var (
	// ErrLostBlock is returned if a block was removed from the cache.
	ErrLostBlock = errors.New("lost block")

	defaultConfig = OrdererConfig{
		BlockSize:        100,
		BlockTimeout:     100 * time.Millisecond,
		OutBlockCapacity: 1024,
		PayloadCacheSize: 1024,
		TestServerParameters: test.StartServerParameters{
			NumService: 1,
		},
	}
	defaultConfigBlock = &common.Block{
		Header: &common.BlockHeader{
			Number: 0,
		},
		Data: &common.BlockData{Data: [][]byte{protoutil.MarshalOrPanic(&common.Envelope{
			Payload: protoutil.MarshalOrPanic(&common.Payload{
				Header: &common.Header{
					ChannelHeader: protoutil.MarshalOrPanic(&common.ChannelHeader{
						Type: int32(common.HeaderType_CONFIG),
					}),
				},
			}),
		})}},
	}
	repsSuccess = ab.BroadcastResponse{
		Status: common.Status_SUCCESS,
	}
)

// NewMockOrderer creates multiple orderer instances.
func NewMockOrderer(config *OrdererConfig) (*Orderer, error) {
	// The defaults are applied to our own copy: the caller keeps ownership of the struct it
	// passed, and storing it directly would let a later in-place field write bypass the
	// atomic below.
	conf := *config
	if conf.BlockSize <= 0 {
		conf.BlockSize = defaultConfig.BlockSize
	}
	// A non-positive timeout would panic time.NewTicker in Run.
	if conf.BlockTimeout <= 0 {
		conf.BlockTimeout = defaultConfig.BlockTimeout
	}
	if conf.OutBlockCapacity <= 0 {
		conf.OutBlockCapacity = defaultConfig.OutBlockCapacity
	}
	if conf.PayloadCacheSize <= 0 {
		conf.PayloadCacheSize = defaultConfig.PayloadCacheSize
	}
	if conf.TestServerParameters.NumService == 0 && len(conf.Servers) == 0 {
		conf.TestServerParameters.NumService = defaultConfig.TestServerParameters.NumService
	}
	genesisBlock, err := loadGenesisBlockWithConsenters(&conf)
	if err != nil {
		return nil, err
	}
	numServices := max(1, conf.TestServerParameters.NumService, len(conf.Servers))
	o := &Orderer{
		genesisBlock: *genesisBlock,
		// One block's worth of envelopes per server absorbs a broadcast burst; the block
		// queue is bounded by the cache that ultimately holds the blocks. Multiplying the
		// two (as this used to) allocates six-figure buffers for no benefit.
		inEnvs:      make(chan envelopeEntry, numServices*conf.BlockSize),
		inBlocks:    make(chan *BlockWithConsenters, conf.OutBlockCapacity),
		cutBlock:    make(chan any),
		cache:       newBlockCache(conf.OutBlockCapacity),
		healthcheck: serve.DefaultHealthCheckService(),
	}
	o.config.Store(&conf)

	// Initialize TLS with CAs from genesis block if available.
	if protoutil.IsConfigBlock(genesisBlock.Block) {
		logger.Debugf("reading root CAs from genesis block")
		if err := o.updateTLSFromConfigBlock(genesisBlock.Block); err != nil {
			logger.Warnf("Failed to initialize TLS from genesis block: %v", err)
		}
	}

	return o, nil
}

func loadGenesisBlockWithConsenters(config *OrdererConfig) (*BlockWithConsenters, error) {
	ret := &BlockWithConsenters{Block: defaultConfigBlock}
	configBlockPath := config.GenesisBlockPath
	consenterMspDirectories := ordererdial.IdentityConfigToMspDir(config.ConsentersMSPIdentities...)

	// Artifacts path overrides other paths.
	if len(config.ArtifactsPath) > 0 {
		configBlockPath = path.Join(config.ArtifactsPath, cryptogen.ConfigBlockFileName)
		consenterMspDirectories = testcrypto.GetConsenterMspDirs(config.ArtifactsPath)
	}

	if len(configBlockPath) > 0 {
		configMaterial, err := channelconfig.LoadConfigBlockMaterialFromFile(configBlockPath)
		if err != nil {
			return nil, err
		}
		ret.Block = configMaterial.ConfigBlock
	}
	if len(consenterMspDirectories) > 0 {
		consenters, err := testcrypto.GetSigningIdentities(consenterMspDirectories...)
		if err != nil {
			return nil, err
		}
		ret.ConsenterSigners = consenters
	}

	return ret, nil
}

// Broadcast receives TXs and returns ACKs.
func (o *Orderer) Broadcast(stream ab.AtomicBroadcast_BroadcastServer) error {
	streamCtx := stream.Context()
	clientAddr := util.ExtractRemoteAddress(streamCtx)
	serverAddr := utils.ExtractServerAddress(streamCtx)
	logger.Infof("Starting broadcast on server [%s] with client [%s]", serverAddr, clientAddr)
	defer logger.Infof("Finished broadcast on server [%s] with client [%s]", serverAddr, clientAddr)

	inEnvs := channel.NewWriter(streamCtx, o.inEnvs)
	for {
		env, err := stream.Recv()
		if err != nil {
			return err //nolint:wrapcheck // already a GRPC error.
		}
		// No done signal: the ACK below only promises the envelope was accepted, so there is
		// nothing to wait for and per-envelope Ready allocation would be pure overhead.
		inEnvs.Write(envelopeEntry{env: env})
		if err = stream.Send(&repsSuccess); err != nil {
			return err //nolint:wrapcheck // already a GRPC error.
		}
	}
}

// Deliver receives a seek request and returns a stream of the orderered blocks.
func (o *Orderer) Deliver(stream ab.AtomicBroadcast_DeliverServer) error {
	env, streamErr := stream.Recv()
	if streamErr != nil {
		return streamErr //nolint:wrapcheck // already a GRPC error.
	}
	seekInfo, seekErr := readSeekInfo(env)
	if seekErr != nil {
		return grpcerror.WrapInvalidArgument(seekErr)
	}
	start, end, seekErr := parseSeekInfoStartStop(seekInfo, o.cache.newestBlockNumber())
	if seekErr != nil {
		return grpcerror.WrapInvalidArgument(seekErr)
	}

	ctx := stream.Context()
	state := o.registerStream(ctx, func(info StreamInfo) *OrdererStreamState {
		partyState, _ := o.endpointToPartyState.LoadOrStore(info.ServerEndpoint, &PartyState{})
		return &OrdererStreamState{
			StreamInfo:      info,
			PartyState:      partyState,
			DataBlockStream: seekInfo.ContentType == ab.SeekInfo_BLOCK,
		}
	})
	logger.Infof("Mock Orderer starting deliver from block [#%d] by %s", start, state)

	// Ensures releasing the waiters when this context ends.
	stop := o.cache.releaseAfter(ctx)
	defer stop()

	var p testcrypto.BlockPrepareParameters
	for i := start; i <= end && ctx.Err() == nil; i++ {
		b, err := o.cache.getBlock(ctx, i)
		if err != nil && !errors.Is(err, ErrLostBlock) {
			return grpcerror.WrapCancelled(err)
		}
		if b == nil {
			logger.Warnf("Lost block [#%d]. Sending an empty block for the sake of the delivery progress.", i)
			b = fillerBlock(i, p)
		}
		msg := state.prepareResponse(ctx, b)
		if msg == nil {
			break
		}
		if err = stream.Send(msg); err != nil {
			return err //nolint:wrapcheck // already a GRPC error.
		}
		logger.Debugf("Emitted block [#%d] by %s", b.Header.Number, state)
		p.PrevBlock = b
	}
	if ctx.Err() != nil {
		return grpcerror.WrapCancelled(ctx.Err())
	}

	// The requested range was fully delivered. A real orderer terminates a bounded seek
	// with a status message rather than just closing the stream.
	return stream.Send(&ab.DeliverResponse{
		Type: &ab.DeliverResponse_Status{Status: common.Status_SUCCESS},
	}) //nolint:wrapcheck // already a GRPC error.
}

// fillerBlock stands in for a block that was evicted from the cache before this stream
// reached it. The requested number is forced via a synthetic predecessor: p.PrevBlock is
// nil until the stream emits its first block, and letting the number default to 0 would
// restart the client's numbering when the very first block of the stream is the lost one.
func fillerBlock(blockNum uint64, p testcrypto.BlockPrepareParameters) *common.Block {
	if p.PrevBlock == nil && blockNum > 0 {
		p.PrevBlock = &common.Block{Header: &common.BlockHeader{Number: blockNum - 1}}
	}
	return testcrypto.PrepareBlockHeaderAndMetadata(&common.Block{Data: &common.BlockData{}}, p)
}

// RegisterPartyState registered a persistent party state for the server.
// It allows configuring a shared, persistent party state between multiple streams
// according to the stream's server endpoint.
// This allows uniform behavior for each mock server.
func (o *Orderer) RegisterPartyState(address string, partyState *PartyState) {
	o.endpointToPartyState.Store(address, partyState)
}

func (s *OrdererStreamState) prepareResponse(ctx context.Context, block *common.Block) *ab.DeliverResponse {
	blockNumber := block.Header.Number

	// Block holding: a simple busy wait loop is sufficient here.
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	logHolding := false
	for s.shouldHold(blockNumber) {
		if !logHolding {
			logger.Infof("Holding block: %d by %s", blockNumber, s)
			logHolding = true
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
	if logHolding {
		logger.Infof("Releasing block: %d (up to %d) by %s",
			blockNumber, s.HoldFromBlock.Load(), s)
	}

	// Block manipulation.
	altBlock, loaded := s.ReplaceBlock.Load(blockNumber)
	if loaded {
		logger.Infof("Replacing block: %d by %s", blockNumber, s)
		block = altBlock
	}

	// Headers only.
	if !s.DataBlockStream && !protoutil.IsConfigBlock(block) {
		block = &common.Block{
			Header:   block.Header,
			Metadata: block.Metadata,
		}
	}

	return &ab.DeliverResponse{Type: &ab.DeliverResponse_Block{Block: block}}
}

func (s *OrdererStreamState) shouldHold(blockNumber uint64) bool {
	holdValue := s.HoldFromBlock.Load()
	if holdValue == 0 {
		// To avoid manual initialization, we assume "0" means not initialized.
		return false
	}
	return blockNumber >= holdValue
}

// String outputs a human-readable identifier for this stream.
func (s *OrdererStreamState) String() string {
	kind := "headers"
	if s.DataBlockStream {
		kind = "data"
	}
	return fmt.Sprintf("{party [%d] stream [%d] (%s)}", s.PartyID, s.Index, kind)
}

func readSeekInfo(env *common.Envelope) (seekInfo *ab.SeekInfo, err error) {
	payload, err := protoutil.UnmarshalPayload(env.Payload)
	if err != nil {
		return nil, errors.Wrap(err, "failed unmarshalling payload")
	}
	seekInfo = &ab.SeekInfo{}
	if err = proto.Unmarshal(payload.Data, seekInfo); err != nil {
		return nil, errors.Wrap(err, "failed unmarshalling seek info")
	}
	return seekInfo, nil
}

func parseSeekInfoStartStop(seekInfo *ab.SeekInfo, newestBlock uint64) (start, end uint64, err error) {
	start = resolveSeekPosition(seekInfo.Start, 0, newestBlock)
	end = resolveSeekPosition(seekInfo.Stop, math.MaxUint64, newestBlock)
	if start > end {
		return start, end, errors.Newf("invalid block range: (start) [%d] > [%d] (end)", start, end)
	}
	return start, end, nil
}

// resolveSeekPosition maps a seek position to a block number, returning unset when the
// position is absent. Treating an OLDEST/NEWEST position as "not specified" would silently
// replay the whole ledger for a client that asked only for the tip.
func resolveSeekPosition(pos *ab.SeekPosition, unset, newestBlock uint64) uint64 {
	if pos == nil {
		return unset
	}
	switch t := pos.Type.(type) {
	case *ab.SeekPosition_Specified:
		return t.Specified.GetNumber()
	case *ab.SeekPosition_Oldest:
		return 0
	case *ab.SeekPosition_Newest:
		return newestBlock
	default:
		// SeekNextCommit is not modelled by the mock.
		return unset
	}
}

// Run collects the envelopes, cuts the blocks, and store them to the block cache.
func (o *Orderer) Run(ctx context.Context) error {
	// Ensures releasing the waiters when this context ends.
	stop := o.cache.releaseAfter(ctx)
	defer stop()

	// We add the signers after submitting the genesis block as Fabric's orderer
	// does not sign the genesis block.
	blockParams := testcrypto.BlockPrepareParameters{}
	tick := time.NewTicker(o.config.Load().BlockTimeout)
	defer tick.Stop()
	sendBlock := func(b *common.Block) {
		if b == nil {
			return
		}
		b = testcrypto.PrepareBlockHeaderAndMetadata(b, blockParams)
		o.cache.addBlock(ctx, b)
		blockParams.PrevBlock = b

		if protoutil.IsConfigBlock(b) {
			blockParams.LastConfigBlockIndex = b.Header.Number
			logger.Debugf("reading root CAs from config block")
			if err := o.updateTLSFromConfigBlock(b); err != nil {
				logger.Warnf("Failed to update TLS from config block %d: %v", b.Header.Number, err)
			}
		}

		tick.Reset(o.config.Load().BlockTimeout)
	}
	sendBlockWithConsenters := func(b *BlockWithConsenters) {
		// The block is signed with OLD signers, then we update the signers.
		sendBlock(b.Block)
		if len(b.ConsenterSigners) > 0 {
			blockParams.ConsenterSigners = b.ConsenterSigners
		}
		if len(b.ConsenterMetadata) > 0 {
			blockParams.ConsenterMetadata = b.ConsenterMetadata
		}
	}

	// Submit the config block.
	if o.config.Load().SendGenesisBlock {
		sendBlockWithConsenters(&o.genesisBlock)
	}

	data := make([][]byte, 0, o.config.Load().BlockSize)
	sendBlockData := func(reason string) {
		if len(data) == 0 {
			return
		}
		logger.Debugf("block with [%d] txs has been cut (%s)", len(data), reason)
		sendBlock(&common.Block{Data: &common.BlockData{Data: data}})
		data = make([][]byte, 0, o.config.Load().BlockSize)
	}
	envCache := newFifoCache[any](o.config.Load().PayloadCacheSize)
	for {
		select {
		case <-ctx.Done():
			return errors.Wrapf(ctx.Err(), "context ended")
		case b := <-o.inBlocks:
			sendBlockWithConsenters(b)
		case <-o.cutBlock:
			sendBlockData("external")
		case <-tick.C:
			sendBlockData("timeout")
		case entry := <-o.inEnvs:
			entry.signal()
			if !addEnvelope(envCache, entry.env) {
				continue
			}
			data = append(data, protoutil.MarshalOrPanic(entry.env))
			if len(data) >= o.config.Load().BlockSize {
				sendBlockData("size")
			}
		}
	}
}

// WaitForReady returns true when we are ready to process GRPC requests.
func (*Orderer) WaitForReady(context.Context) bool {
	return true
}

// RegisterService registers the orderer's gRPC services.
func (o *Orderer) RegisterService(s serve.Servers) {
	ab.RegisterAtomicBroadcastServer(s.GRPC, o)
	healthgrpc.RegisterHealthServer(s.GRPC, o.healthcheck)
	serve.RegisterDynamicTLSUpdater(s.GrpcTLSProvider, &o.tlsUpdater)
}

// SubmitBlock allows submitting blocks directly for testing other packages.
// The block header will be replaced with a generated header.
func (o *Orderer) SubmitBlock(ctx context.Context, b *common.Block) error {
	if !channel.NewWriter(ctx, o.inBlocks).Write(&BlockWithConsenters{Block: b}) {
		return errors.Wrapf(ctx.Err(), "failed to submit block")
	}
	return nil
}

// SubmitBlockWithConsenters allows submitting config-blocks (with crypto) directly for testing other packages.
// The block header will be replaced with a generated header.
func (o *Orderer) SubmitBlockWithConsenters(ctx context.Context, newConfig *BlockWithConsenters) error {
	if !channel.NewWriter(ctx, o.inBlocks).Write(newConfig) {
		return errors.Wrapf(ctx.Err(), "failed to submit new config")
	}
	return nil
}

// SubmitEnv allows submitting envelops directly for testing other packages.
// It blocks until the envelope is collected by the Run goroutine.
func (o *Orderer) SubmitEnv(ctx context.Context, e *common.Envelope) bool {
	env := newSyncEnvelopeEntry(e)
	if !channel.NewWriter(ctx, o.inEnvs).Write(env) {
		return false
	}
	return env.done.WaitForReady(ctx)
}

// GetBlock allows fetching blocks directly for testing other packages.
func (o *Orderer) GetBlock(ctx context.Context, blockNum uint64) (*common.Block, error) {
	// Ensures releasing the waiters when this context ends.
	stop := o.cache.releaseAfter(ctx)
	defer stop()
	return o.cache.getBlock(ctx, blockNum)
}

// CutBlock allows forcing block cut for testing other packages.
func (o *Orderer) CutBlock(ctx context.Context) bool {
	return channel.NewWriter(ctx, o.cutBlock).Write(nil)
}

// newSyncEnvelopeEntry creates an entry whose sender can wait for the Run goroutine to
// collect it.
func newSyncEnvelopeEntry(e *common.Envelope) envelopeEntry {
	return envelopeEntry{
		env:  e,
		done: channel.NewReady(),
	}
}

func (e envelopeEntry) signal() {
	if e.done != nil {
		e.done.SignalReady()
	}
}

func newBlockCache(size int) *blockCache {
	return &blockCache{
		storage:           make([]*common.Block, size),
		maxDeliveredBlock: -1,
		maxAddedBlock:     -1,
		mu:                sync.NewCond(&sync.Mutex{}),
	}
}

// newestBlockNumber returns the highest block number added so far, or 0 if the cache is
// still empty, which is also where a NEWEST seek on an empty ledger has to start.
func (c *blockCache) newestBlockNumber() uint64 {
	c.mu.L.Lock()
	defer c.mu.L.Unlock()
	return uint64(max(0, c.maxAddedBlock))
}

func (c *blockCache) releaseAfter(ctx context.Context) (stop func() bool) {
	return context.AfterFunc(ctx, func() {
		c.mu.L.Lock()
		defer c.mu.L.Unlock()
		c.mu.Broadcast()
	})
}

// addBlock returns true if the block was inserted.
// It may fail if the context ends.
func (c *blockCache) addBlock(ctx context.Context, b *common.Block) bool {
	blockIndex := b.Header.Number % uint64(len(c.storage))

	c.mu.L.Lock()
	defer c.mu.L.Unlock()
	// We ensure that we don't overwrite a block that haven't been delivered yet.
	//nolint:gosec // integer overflow conversion uint64 -> int64
	for c.storage[blockIndex] != nil && int64(c.storage[blockIndex].Header.Number) > c.maxDeliveredBlock {
		if ctx.Err() != nil {
			return false
		}
		c.mu.Wait()
	}
	c.storage[blockIndex] = b
	//nolint:gosec // integer overflow conversion uint64 -> int64
	c.maxAddedBlock = max(c.maxAddedBlock, int64(b.Header.Number))
	// Wakeup all waiting to get blocks.
	c.mu.Broadcast()
	return true
}

func (c *blockCache) getBlock(ctx context.Context, blockNum uint64) (*common.Block, error) {
	blockIndex := blockNum % uint64(len(c.storage))

	c.mu.L.Lock()
	defer c.mu.L.Unlock()
	// Asking for a block declares that everything before it has been consumed. Recording
	// that up front is what frees the older slots for addBlock: otherwise a reader seeking
	// ahead waits for a block whose slot still holds an older undelivered one, while
	// addBlock waits for that same older block to be delivered — a standoff only the
	// context cancellation could break.
	//nolint:gosec // integer overflow conversion uint64 -> int64
	c.markDelivered(int64(blockNum) - 1)
	for ctx.Err() == nil {
		b := c.storage[blockIndex]
		switch {
		case b == nil || b.Header.Number < blockNum:
			// There is still hope for this block.
			c.mu.Wait()
		case b.Header.Number > blockNum:
			// We'll never see this block again.
			return nil, ErrLostBlock
		default: // b.Header.Number == blockNum
			//nolint:gosec // integer overflow conversion uint64 -> int64
			c.markDelivered(int64(b.Header.Number))
			return b, nil
		}
	}
	return nil, errors.Wrapf(ctx.Err(), "context ended")
}

// markDelivered advances the highest delivered block and wakes the writers waiting to
// reuse its slot. The caller must hold the lock.
func (c *blockCache) markDelivered(blockNum int64) {
	if blockNum <= c.maxDeliveredBlock {
		return
	}
	c.maxDeliveredBlock = blockNum
	// Wakeup all waiting to add blocks.
	c.mu.Broadcast()
}

// addEnvelope returns true if the entry is unique.
// It employs a FIFO eviction policy.
func addEnvelope(c *fifoCache[any], e *common.Envelope) bool {
	if e == nil {
		return false
	}
	digestRaw := sha256.Sum256(e.Payload)
	digest := base64.StdEncoding.EncodeToString(digestRaw[:])
	return c.addIfNotExist(digest, nil)
}

// updateTLSFromConfigBlock extracts application TLS CAs from a config block
// and updates the dynamic TLS configuration.
// This is called when new config blocks are processed to enable runtime CA rotation.
func (o *Orderer) updateTLSFromConfigBlock(configBlock *common.Block) error {
	if configBlock == nil || len(configBlock.Data.Data) == 0 {
		return errors.New("config block is nil or has no data")
	}

	certs, err := serialization.ExtractAppTLSCAsFromEnvelope(configBlock.Data.Data[0])
	if err != nil {
		return errors.Wrap(err, "failed to extract TLS CAs from config envelope")
	}

	o.tlsUpdater.UpdateClientRootCAs(certs)

	logger.Infof("Updated dynamic TLS with %d CA certificates from config block %d",
		len(certs), configBlock.Header.Number)
	return nil
}

// OrdererStartAndServe starts the orderer and serves it with the given configuration.
func OrdererStartAndServe(ctx context.Context, service *Orderer) error {
	sc := service.config.Load().Servers
	serveConfs := make([]*serve.Config, len(sc))
	for i, s := range sc {
		serveConfs[i] = &serve.Config{GRPC: *s}
	}
	return serve.StartAndServe(ctx, service, serveConfs...)
}
