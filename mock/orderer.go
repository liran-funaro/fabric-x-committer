package mock

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric/common/util"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/decentralized-trust-research/fabricx-config/internaltools/configtxgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

type (
	// OrdererConfig configuration for the mock orderer.
	OrdererConfig struct {
		ServerConfigs    []*connection.ServerConfig `mapstructure:"servers"`
		NumService       int                        `mapstructure:"num-services"`
		BlockSize        uint32                     `mapstructure:"block-size"`
		BlockTimeout     time.Duration              `mapstructure:"block-timeout"`
		OutBlockCapacity uint32                     `mapstructure:"out-block-capacity"`
		ConfigBlockPath  string                     `mapstructure:"config-block-path"`
	}

	// Orderer is a mock of a single ordering GRPC service.
	Orderer struct {
		inEnvs    chan<- *common.Envelope
		outBlocks <-chan *common.Block
		cache     *blockCache
	}

	blockCache struct {
		storage       map[uint64]*common.Block
		freeSlotCount int
		leastBlockNum uint64
		maxBlockNum   uint64
		mu            *sync.Mutex
	}

	// MultiOrderer supports running multiple mock order services which mock a consortium.
	MultiOrderer struct {
		config              *OrdererConfig
		mocks               []*Orderer
		configBlock         *common.Block
		inEnvsPerOrderer    []<-chan *common.Envelope
		outBlocksPerOrderer []chan<- *common.Block
	}
)

var (
	defaultConfig = OrdererConfig{
		NumService:       1,
		BlockSize:        100,
		BlockTimeout:     100 * time.Millisecond,
		OutBlockCapacity: 128,
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
	defaultBlockCacheSize = 1000
)

// NewMultiOrderer creates multiple orderer instances.
func NewMultiOrderer(config *OrdererConfig) (*MultiOrderer, error) {
	if config.BlockSize == 0 {
		config.BlockSize = defaultConfig.BlockSize
	}
	if config.BlockTimeout.Abs() == 0 {
		config.BlockTimeout = defaultConfig.BlockTimeout
	}
	if config.NumService == 0 {
		config.NumService = defaultConfig.NumService
	}
	if config.OutBlockCapacity == 0 {
		config.OutBlockCapacity = defaultConfig.OutBlockCapacity
	}
	configBlock := defaultConfigBlock
	if config.ConfigBlockPath != "" {
		var err error
		configBlock, err = configtxgen.ReadBlock(config.ConfigBlockPath)
		if err != nil {
			return nil, err
		}
	}

	mocks := make([]*Orderer, config.NumService)
	inEnvsPerOrderer := make([]<-chan *common.Envelope, config.NumService)
	outBlocksPerOrderer := make([]chan<- *common.Block, config.NumService)
	for i := range config.NumService {
		outBlocks := make(chan *common.Block, config.OutBlockCapacity)
		inEnvs := make(chan *common.Envelope, config.BlockSize*config.OutBlockCapacity)
		outBlocksPerOrderer[i] = outBlocks
		inEnvsPerOrderer[i] = inEnvs
		mocks[i] = newMockOrderer(inEnvs, outBlocks)
	}
	return &MultiOrderer{
		config:              config,
		mocks:               mocks,
		configBlock:         configBlock,
		inEnvsPerOrderer:    inEnvsPerOrderer,
		outBlocksPerOrderer: outBlocksPerOrderer,
	}, nil
}

// newMockOrderer creates a new mock ordering GRPC service.
func newMockOrderer(inEnvs chan<- *common.Envelope, outBlocks <-chan *common.Block) *Orderer {
	return &Orderer{
		inEnvs:    inEnvs,
		outBlocks: outBlocks,
		cache: &blockCache{
			storage:       make(map[uint64]*common.Block),
			freeSlotCount: defaultBlockCacheSize,
			mu:            &sync.Mutex{},
		},
	}
}

// Broadcast receives TXs and returns ACKs.
func (o *Orderer) Broadcast(stream ab.AtomicBroadcast_BroadcastServer) error {
	addr := util.ExtractRemoteAddress(stream.Context())
	logger.Infof("Starting broadcast with %s", addr)
	defer logger.Infof("Finished broadcast with %s", addr)

	inEnvs := channel.NewWriter(stream.Context(), o.inEnvs)
	for {
		env, err := stream.Recv()
		if err != nil {
			return err
		}
		inEnvs.Write(env)
		err = stream.Send(&ab.BroadcastResponse{
			Status: common.Status_SUCCESS,
		})
		if err != nil {
			return err
		}
	}
}

// Deliver receives a seek request and returns a stream of the orderered blocks.
func (o *Orderer) Deliver(stream ab.AtomicBroadcast_DeliverServer) error {
	addr := util.ExtractRemoteAddress(stream.Context())
	logger.Infof("Starting delivery with %s", addr)

	sendBlock := func(block *common.Block) error {
		if err := stream.Send(&ab.DeliverResponse{Type: &ab.DeliverResponse_Block{Block: block}}); err != nil {
			return err
		}
		logger.Debugf("Emitted block %d", block.Header.Number)
		return nil
	}

	seekInfo, channelID, err := readSeekEnvelope(stream)
	if err != nil {
		return fmt.Errorf("failed reading seek request: %w", err)
	}

	if start, ok := seekInfo.Start.Type.(*ab.SeekPosition_Specified); ok {
		// for simplicity, we would retrieve from cache only if all blocks in the range (start, end)
		// present in the cache. Otherwise, we send from the newest block.
		var blocks []*common.Block
		blocks, err = o.cache.tryGetBlocks(start.Specified.Number, seekInfo.Stop.GetSpecified().Number)
		if err != nil {
			return err
		}

		if len(blocks) > 0 {
			for _, blk := range blocks {
				if err = sendBlock(blk); err != nil {
					return err
				}
			}
			return nil
		}
	}

	logger.Infof("Received listening request for channel '%s': %v\n "+
		"We ignore all unspecified seek request, specified range which are not in the block cache "+
		"and send a stream anyway.", channelID, seekInfo)

	outBlocks := channel.NewReader(stream.Context(), o.outBlocks)
	for {
		block, ok := outBlocks.Read()
		if !ok {
			return stream.Context().Err()
		}
		o.cache.add(block)
		if err = sendBlock(block); err != nil {
			return err
		}
	}
}

func readSeekEnvelope(stream ab.AtomicBroadcast_DeliverServer) (*ab.SeekInfo, string, error) {
	env, err := stream.Recv()
	if err != nil {
		return nil, "", err
	}
	payload, err := protoutil.UnmarshalPayload(env.Payload)
	if err != nil {
		return nil, "", err
	}
	seekInfo := &ab.SeekInfo{}
	if err = proto.Unmarshal(payload.Data, seekInfo); err != nil {
		return nil, "", err
	}

	chdr := &common.ChannelHeader{}
	if err = proto.Unmarshal(payload.Header.ChannelHeader, chdr); err != nil {
		return nil, "", err
	}
	return seekInfo, chdr.ChannelId, nil
}

// Run creates a specified number of mock ordering service and runs them.
func (o *MultiOrderer) Run(ctx context.Context) error {
	allInEnvs := make(chan *common.Envelope, o.config.OutBlockCapacity)
	allOutBlocks := make(chan *common.Block, o.config.OutBlockCapacity)

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return fanIn(gCtx, o.inEnvsPerOrderer, allInEnvs)
	})
	g.Go(func() error {
		return o.cutBlocks(gCtx, allInEnvs, allOutBlocks)
	})
	g.Go(func() error {
		return fanOut(gCtx, allOutBlocks, o.outBlocksPerOrderer)
	})
	return connection.FilterStreamRPCError(g.Wait())
}

// WaitForReady returns true when we are ready to process GRPC requests.
func (*MultiOrderer) WaitForReady(context.Context) bool {
	return true
}

// Instances returns the ordering GRPC services.
func (o *MultiOrderer) Instances() []*Orderer {
	return o.mocks
}

// fanIn reads the envelopes from all in channels to ensure the channels do not fill up.
// However, we only forward the envelopes from the first channel to avoid having to merge the data.
func fanIn(ctx context.Context, in []<-chan *common.Envelope, out chan<- *common.Envelope) error {
	g, gCtx := errgroup.WithContext(ctx)
	for i := range in {
		i := i
		g.Go(func() error {
			cIn := channel.NewReader(gCtx, in[i])
			cOut := channel.NewWriter(gCtx, out)
			for {
				env, ok := cIn.Read()
				if !ok {
					return gCtx.Err()
				}
				if i == 0 {
					cOut.Write(env)
				}
			}
		})
	}
	return g.Wait()
}

func fanOut(ctx context.Context, in <-chan *common.Block, out []chan<- *common.Block) error {
	cIn := channel.NewReader(ctx, in)
	cOut := make([]channel.Writer[*common.Block], len(out))
	for i, o := range out {
		cOut[i] = channel.NewWriter(ctx, o)
	}
	for {
		block, ok := cIn.Read()
		if !ok {
			return ctx.Err()
		}
		logger.Debugf(
			"Block %d, prev: [%x], current: [%x]",
			block.Header.Number,
			block.Header.PreviousHash,
			block.Header.DataHash,
		)
		for _, ch := range cOut {
			ch.Write(block)
		}
	}
}

func (o *MultiOrderer) cutBlocks(
	ctx context.Context,
	inEnvs <-chan *common.Envelope,
	outBlocks chan<- *common.Block,
) error {
	logger.Debugf("Start cutting blocks")

	tick := time.NewTicker(o.config.BlockTimeout)
	cOutBlocks := channel.NewWriter(ctx, outBlocks)

	block := o.configBlock
	blockNum := block.Header.Number
	sendBlock := func() {
		cOutBlocks.Write(block)
		tick.Reset(o.config.BlockTimeout)
		blockNum++
		block = &common.Block{
			Header: &common.BlockHeader{
				Number:       blockNum,
				PreviousHash: protoutil.BlockHeaderHash(block.Header),
			},
			Data: &common.BlockData{
				Data: make([][]byte, 0, o.config.BlockSize),
			},
		}
	}

	// Submit the config block.
	sendBlock()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if len(block.Data.Data) == 0 {
				continue
			}
			logger.Debugf(
				"block [%d] with [%d] txs has been cut due to timeout",
				blockNum,
				len(block.Data.Data),
			)
		case env, ok := <-inEnvs:
			if !ok {
				logger.Infof("Closing block cutter")
				return nil
			}
			block.Data.Data = append(block.Data.Data, protoutil.MarshalOrPanic(env))
			if len(block.Data.Data) < int(o.config.BlockSize) {
				continue
			}
			logger.Debugf(
				"block [%d] has been cut as it reached the required block size [%d]",
				blockNum,
				o.config.BlockSize,
			)
		}
		sendBlock()
	}
}

func (c *blockCache) add(b *common.Block) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.freeSlotCount == 0 {
		delete(c.storage, c.leastBlockNum)
		c.leastBlockNum++
		c.freeSlotCount++
	}

	c.storage[b.Header.Number] = b
	c.freeSlotCount--

	c.leastBlockNum = min(c.leastBlockNum, b.Header.Number)
	c.maxBlockNum = max(c.maxBlockNum, b.Header.Number)
}

func (c *blockCache) tryGetBlocks(startBlkNum, endBlkNum uint64) ([]*common.Block, error) {
	if endBlkNum < startBlkNum {
		return nil, fmt.Errorf("end block number [%d] cannot be lower than the start block number [%d]",
			endBlkNum, startBlkNum)
	}

	var blocks []*common.Block
	c.mu.Lock()
	defer c.mu.Unlock()
	// for simplicity, we would retrieve from cache only if all blocks in the range (start, end)
	// present in the cache. Otherwise, we send from the newest block.
	if len(c.storage) > 0 && c.leastBlockNum <= startBlkNum && c.maxBlockNum >= endBlkNum {
		for i := startBlkNum; i <= endBlkNum; i++ {
			blocks = append(blocks, c.storage[i])
		}
	}

	return blocks, nil
}
