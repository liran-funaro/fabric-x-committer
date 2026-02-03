/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package deliver

import (
	"bytes"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-lib-go/bccsp/factory"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	commontypes "github.com/hyperledger/fabric-x-common/api/types"
	"github.com/hyperledger/fabric-x-common/common/channelconfig"
	"github.com/hyperledger/fabric-x-common/common/configtx"
	"github.com/hyperledger/fabric-x-common/common/policies"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"google.golang.org/protobuf/proto"

	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/ordererconn"
)

type (
	blockProcessingState struct {
		configState
		dataBlockStream     bool
		verifyBlocksContent bool
		nextBlockNum        uint64
		lastBlockHeaderHash []byte
		lastBlock           *common.Block
		updaterID           uint32
		updated             bool
	}

	configState struct {
		configBlockNumber     uint64
		configBlock           *common.Block
		channelHeader         *common.ChannelHeader
		verifierFunc          protoutil.BlockVerifierFunc
		ordererEndpoints      []*commontypes.OrdererEndpoint
		deliverEndpoints      []*connection.Endpoint
		deliverEndpointsPerID map[uint32][]*connection.Endpoint
	}
)

// ErrUnexpectedBlockNumber is returned by the verification step if the blocks are not received in order.
var ErrUnexpectedBlockNumber = errors.New("received unexpected block number")

// verificationStepAndUpdateState returns error if the block number is not what expected,
// or if the blocks header/data hashes does not match the expected value.
func (s *blockProcessingState) verificationStepAndUpdateState(blk *BlockWithSourceID) error {
	// 1. Verify form.
	if !s.verifyBlockForm(blk.Block) {
		return errors.New("received malformed block")
	}

	// 2. Verify correct progress.
	if blk.Block.Header.Number != s.nextBlockNum {
		return errors.Wrapf(ErrUnexpectedBlockNumber, "received [%d] != [%d] expected",
			blk.Block.Header.Number, s.nextBlockNum)
	}

	// 3. Verify content.
	if s.verifyBlocksContent {
		err := s.verifyHashes(blk.Block)
		if err != nil {
			return err
		}
		err = s.verifyBlockPolicy(blk.Block)
		if err != nil {
			return err
		}
	}

	// If it is a valid block, update internal processing state.
	s.update(blk)
	return nil
}

// verifyBlockForm checks whether the block is well-formed.
func (s *blockProcessingState) verifyBlockForm(block *common.Block) bool {
	if block == nil || block.Header == nil || block.Metadata == nil {
		return false
	}
	if len(block.Metadata.Metadata) < len(common.BlockMetadataIndex_name) {
		return false
	}
	// Block's data can be nil only if we only requested headers.
	if s.dataBlockStream && block.Data == nil {
		return false
	}
	return true
}

// verifyHashes checks that the block previous hash matches the previous block,
// and that the internal data hash matches the one in the header.
func (s *blockProcessingState) verifyHashes(block *common.Block) error {
	blockNumber := block.Header.Number

	// Verify header hash if we have the previous block hash.
	if len(s.lastBlockHeaderHash) != 0 {
		if !bytes.Equal(block.Header.PreviousHash, s.lastBlockHeaderHash) {
			return errors.Errorf("previous block header hash mismatch on block [%d]", blockNumber)
		}
	}

	// Block's data can be nil only if we only requested headers.
	// However, the block's data can be non-nil even if we only requested headers in case of config block updates.
	// Thus, we always verify the data hash if data is present.
	if block.Data != nil {
		dataHash := protoutil.ComputeBlockDataHash(block.Data)
		// Verify that Header.DataHash is equal to the hash of block.Data
		// This is to ensure that the header is consistent with the data carried by this block
		if !bytes.Equal(dataHash, block.Header.DataHash) {
			return errors.Errorf("block data hash mismatch on block [%d]", blockNumber)
		}
	}
	return nil
}

// verifyBlockPolicy returns error if the block does not satisfy the config block policy.
func (s *blockProcessingState) verifyBlockPolicy(block *common.Block) error {
	if s.verifierFunc == nil || s.configBlock == nil {
		return nil
	}

	blockNumber := block.Header.Number

	// If the config block is ahead of this block, something wrong has happened.
	// This should never happen as we validate in-order block processing.
	// But if we received a config block from external source, such error may occur.
	if blockNumber < s.configBlockNumber {
		return errors.Errorf("block number [%d] is less than config block number [%d]",
			blockNumber, s.configBlock.Header.Number)
	}

	// We verify that the block points to the correct config block before we verify it.
	// Fabric's orderer verify that the block actually points to the correct config block.
	// so we expect it to be correct.
	lastConfigBlockIdx, err := protoutil.GetLastConfigIndexFromBlock(block)
	if err != nil {
		return errors.Wrap(err, "failed to fetch last config block index from block")
	}
	if s.configBlockNumber != lastConfigBlockIdx {
		// Fabric's config block points to itself. If it is not a config block that
		// points to itself, then return an error.
		// We have a nested verification to ensure we don't run the relatively heavy
		// check [protoutil.IsConfigBlock()] for most cases.
		if !protoutil.IsConfigBlock(block) || lastConfigBlockIdx != blockNumber {
			return errors.Errorf("block's last config block [%d] != [%d] actual",
				s.configBlockNumber, lastConfigBlockIdx)
		}
	}

	// We have a unique handling for the genesis block as Fabric's orderer does not sign it.
	// We verify that it is identical to the genesis block we have.
	// If we got block #0, we must also have the config block #0, as we assert that the config
	// block is never ahead of the next expected block.
	if blockNumber == 0 {
		ok := proto.Equal(block, s.configBlock)
		if !ok {
			return errors.New("delivered genesis block mismatch to known genesis block")
		}
		return nil
	}

	// Verify block according to the config block policy.
	err = s.verifierFunc(block.Header, block.Metadata)
	return errors.Wrapf(err, "block signature verification failed on block [%d]", blockNumber)
}

func (s *blockProcessingState) update(blk *BlockWithSourceID) {
	s.lastBlock = blk.Block
	s.nextBlockNum = blk.Block.Header.Number + 1
	s.lastBlockHeaderHash = protoutil.BlockHeaderHash(blk.Block.Header)
	s.updaterID = blk.SourceID
	s.updated = true
}

// updateIfConfigBlock sets the config by which blocks are verified.
// It is assumed that this config block had already been
// verified using the VerifyBlock method immediately prior to calling this method.
func (cs *configState) updateIfConfigBlock(block *common.Block) (updated bool, err error) {
	chHead, bundle, err := readConfigBlock(block)
	if chHead == nil || bundle == nil || err != nil {
		return false, err
	}

	// This is a config block. Let's start validating.
	if cs.channelHeader != nil && chHead.ChannelId != cs.channelHeader.ChannelId {
		return false, errors.Errorf("config block channel ID [%s] does not match expected: [%s]",
			chHead.ChannelId, cs.channelHeader.ChannelId)
	}

	verifierFunc, err := fetchVerifier(bundle)
	if err != nil {
		return false, err
	}

	endpoints, err := fetchEndpoints(bundle)
	if err != nil {
		return false, err
	}

	cs.configBlockNumber = block.Header.Number
	cs.configBlock = block
	cs.channelHeader = chHead
	cs.verifierFunc = verifierFunc
	cs.ordererEndpoints = endpoints
	cs.deliverEndpoints = ordererconn.GetEndpointsForAPI(endpoints, ordererconn.Deliver)
	cs.deliverEndpointsPerID = ordererconn.GetEndpointsForAPIPerID(endpoints, ordererconn.Deliver)
	return true, nil
}

// readConfigBlock attempts to read a config block from the given block.
// If the block is not a config block, nils are returned.
func readConfigBlock(block *common.Block) (*common.ChannelHeader, *channelconfig.Bundle, error) {
	// We expect config blocks to have exactly one transaction, with a valid payload.
	if block == nil || block.Data == nil {
		return nil, nil, nil
	}
	if len(block.Data.Data) != 1 {
		return nil, nil, nil
	}
	// We expect config blocks to have exactly one transaction, with a valid payload.
	configTx, err := protoutil.GetEnvelopeFromBlock(block.Data.Data[0])
	if err != nil {
		return nil, nil, nil //nolint:nilerr // We don't care if the block content is not a config block.
	}
	payload, err := protoutil.UnmarshalPayload(configTx.Payload)
	if err != nil {
		return nil, nil, nil //nolint:nilerr // We don't care if the block content is not a config block.
	}
	if payload.Header == nil {
		return nil, nil, nil
	}
	chHead, err := protoutil.UnmarshalChannelHeader(payload.Header.ChannelHeader)
	if err != nil {
		return nil, nil, nil //nolint:nilerr // We don't care if the block content is not a config block.
	}
	if chHead.Type != int32(common.HeaderType_CONFIG) {
		return nil, nil, nil
	}

	// This is a config block. Let's parse it.
	configEnvelope, err := configtx.UnmarshalConfigEnvelope(payload.Data)
	if err != nil {
		return nil, nil, errors.WithMessage(err, "error unmarshalling config envelope from payload data")
	}

	bundle, err := channelconfig.NewBundle(chHead.ChannelId, configEnvelope.Config, factory.GetDefault())
	if err != nil {
		return nil, nil, errors.Wrap(err, "error creating channel config bundle")
	}
	return chHead, bundle, nil
}

func fetchVerifier(bundle *channelconfig.Bundle) (protoutil.BlockVerifierFunc, error) {
	policy, exists := bundle.PolicyManager().GetPolicy(policies.BlockValidation)
	if !exists {
		return nil, errors.Errorf("no `%s` policy in config block", policies.BlockValidation)
	}

	oc, ok := bundle.OrdererConfig()
	if !ok {
		return nil, errors.New("no orderer section in config block")
	}

	var consenters []*common.Consenter
	bftEnabled := bundle.ChannelConfig().Capabilities().ConsensusTypeBFT()
	if bftEnabled {
		consenters = oc.Consenters()
	}

	return protoutil.BlockSignatureVerifier(bftEnabled, consenters, policy), nil
}

func fetchEndpoints(bundle *channelconfig.Bundle) ([]*commontypes.OrdererEndpoint, error) {
	oc, ok := bundle.OrdererConfig()
	if !ok {
		return nil, errors.New("no orderer section in config block")
	}

	ordererOrganizations := oc.Organizations()
	var endpoints []*commontypes.OrdererEndpoint
	for orgID, org := range ordererOrganizations {
		endpointsStr := org.Endpoints()
		for _, eStr := range endpointsStr {
			e, parseErr := commontypes.ParseOrdererEndpoint(eStr)
			if parseErr != nil {
				return nil, parseErr
			}
			e.MspID = orgID
			endpoints = append(endpoints, e)
		}
	}

	return endpoints, nil
}
