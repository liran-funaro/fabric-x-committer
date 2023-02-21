package main

import (
	"sync/atomic"

	"github.com/filecoin-project/mir/pkg/checkpoint"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	t "github.com/filecoin-project/mir/pkg/types"
	"github.com/filecoin-project/mir/pkg/util/maputil"
	cb "github.com/hyperledger/fabric-protos-go/common"
)

type OrderingService struct {
	membership map[t.NodeID]t.NodeAddress
	blockChan  chan *cb.Block
	blockCnt   uint64
}

func NewOrderingService(initialMembership map[t.NodeID]t.NodeAddress, b chan *cb.Block) *OrderingService {
	return &OrderingService{
		membership: initialMembership,
		blockChan:  b,
	}
}

func (o *OrderingService) ApplyTXs(txs []*requestpb.Request) error {
	// For each request in the batch

	if len(txs) < 1 {
		return nil
	}

	//fmt.Printf("apply %v\n", txs)

	block := &cb.Block{
		Header: &cb.BlockHeader{
			Number:       atomic.AddUint64(&o.blockCnt, 1),
			PreviousHash: nil,
			DataHash:     nil,
		},
		Data: &cb.BlockData{
			Data: make([][]byte, len(txs)),
		},
		Metadata: &cb.BlockMetadata{
			Metadata: nil,
		},
	}

	for i, req := range txs {
		_ = req
		//chatMessage := fmt.Sprintf("Client %s: %s", req.ClientId, string(req.Data))
		//fmt.Printf("> %s\n", chatMessage)

		block.Data.Data[i] = req.Data
	}

	o.blockChan <- block

	return nil
}

func (o *OrderingService) NewEpoch(nr t.EpochNr) (map[t.NodeID]t.NodeAddress, error) {
	return maputil.Copy(o.membership), nil
}

func (o *OrderingService) Snapshot() ([]byte, error) {
	// we do nothing here
	return nil, nil
}

func (o *OrderingService) RestoreState(checkpoint *checkpoint.StableCheckpoint) error {
	// we do noting here
	return nil
}

func (o *OrderingService) Checkpoint(chkp *checkpoint.StableCheckpoint) error {
	return nil
}
