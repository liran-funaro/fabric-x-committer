package deliver

import (
	"flag"
	"math"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/identity"
)

const (
	SeekSinceOldestBlock = -2
	SeekSinceNewestBlock = -1
)

var (
	oldest  = &orderer.SeekPosition{Type: &orderer.SeekPosition_Oldest{Oldest: &orderer.SeekOldest{}}}
	newest  = &orderer.SeekPosition{Type: &orderer.SeekPosition_Newest{Newest: &orderer.SeekNewest{}}}
	maxStop = &orderer.SeekPosition{Type: &orderer.SeekPosition_Specified{Specified: &orderer.SeekSpecified{Number: math.MaxUint64}}}
)

func SeekSince(startBlockNumber int64, channelID string, signer identity.SignerSerializer) *common.Envelope {
	env, err := protoutil.CreateSignedEnvelope(common.HeaderType_DELIVER_SEEK_INFO, channelID, signer, &orderer.SeekInfo{
		Start:    seekPosition(startBlockNumber),
		Stop:     maxStop,
		Behavior: orderer.SeekInfo_BLOCK_UNTIL_READY,
	}, 0, 0)
	if err != nil {
		panic(err)
	}
	return env
}

func seekPosition(startBlockNumber int64) *orderer.SeekPosition {
	var startPosition *orderer.SeekPosition
	if startBlockNumber < -2 {
		flag.PrintDefaults()
		panic(errors.New("wrong seek value"))
	} else if startBlockNumber == SeekSinceOldestBlock {
		startPosition = oldest
	} else if startBlockNumber == SeekSinceNewestBlock {
		startPosition = newest
	} else {
		startPosition = &orderer.SeekPosition{Type: &orderer.SeekPosition_Specified{Specified: &orderer.SeekSpecified{Number: uint64(startBlockNumber)}}}
	}
	return startPosition
}
