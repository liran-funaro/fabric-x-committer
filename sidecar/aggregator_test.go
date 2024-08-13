package sidecar

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
)

const (
	defaultTimeout = 2 * time.Second
	defaultTick    = 1 * time.Microsecond
)

func TestNewAggregator(t *testing.T) {
	blockSize := 100
	numOfBlocks := 1000
	var blockCnt uint64

	blockChan := make(chan *common.Block, 100)
	statusChan := make(chan *protocoordinatorservice.TxValidationStatusBatch, 100)
	resultBlockChan := make(chan *common.Block, numOfBlocks)

	var wg sync.WaitGroup
	wg.Add(numOfBlocks)

	sendToCoordinator := func(scBlock *protoblocktx.Block) {
		go func(scBlock *protoblocktx.Block) {
			defer wg.Done()

			// create a new batch for each block
			batch := &protocoordinatorservice.TxValidationStatusBatch{
				TxsValidationStatus: make([]*protocoordinatorservice.TxValidationStatus, len(scBlock.GetTxs())),
			}

			for i, tx := range scBlock.GetTxs() {
				batch.TxsValidationStatus[i] = &protocoordinatorservice.TxValidationStatus{
					TxId:   tx.GetId(),
					Status: protoblocktx.Status_COMMITTED,
				}
			}

			// coordinator sends responses in multiple chunks (parts)
			numParts := 1 + rand.Intn(6)
			perPart := len(scBlock.GetTxs()) / numParts

			wg.Add(numParts)
			for i := 0; i < numParts; i++ {
				go func(i int) {
					defer wg.Done()
					r := 100 + rand.Intn(1000)
					time.Sleep(time.Duration(r) * time.Microsecond)

					lo := i * perPart
					hi := lo + perPart

					total := len(scBlock.GetTxs())
					b := &protocoordinatorservice.TxValidationStatusBatch{}
					// check if we have a rest
					if total-hi > 0 && total-hi < perPart {
						b.TxsValidationStatus = batch.TxsValidationStatus[lo:]
					} else {
						b.TxsValidationStatus = batch.TxsValidationStatus[lo:hi]
					}

					statusChan <- b
				}(i)
			}
		}(scBlock)
	}

	output := func(block *common.Block) {
		// assert that we output committed blocks in the right order
		blockNum := block.Header.Number
		require.True(t, atomic.CompareAndSwapUint64(&blockCnt, blockNum, blockNum+1))
		resultBlockChan <- block
	}

	ctx, stop := context.WithCancel(context.Background())
	agg := NewAggregator(sendToCoordinator, output)
	errChan := agg.Start(ctx, blockChan, statusChan)
	go func() {
		wg.Add(1)
		require.NoError(t, <-errChan)
		wg.Done()
	}()

	// submit blocks
	start := time.Now()
	blockChan <- CreateConfigBlock(uint64(0))
	for i := 1; i < numOfBlocks; i++ {
		blockChan <- createBlock(uint64(i), uint64(blockSize))
	}

	// consume all blocks
	for i := 0; i < numOfBlocks; i++ {
		require.Eventually(t, func() bool {
			return <-resultBlockChan != nil
		}, defaultTimeout, defaultTick)
	}

	// shutdown aggregator
	stop()

	// wait for our little go friends
	wg.Wait()

	// Close channels only after all writers ended.
	close(blockChan)
	close(resultBlockChan)
	close(statusChan)

	fmt.Printf("TPS: %.2f\n", float64(numOfBlocks*blockSize)/time.Since(start).Seconds())

	require.Eventually(t, func() bool {
		return int(blockCnt) == numOfBlocks
	}, defaultTimeout, defaultTick)
}

func createBlock(number, blockSize uint64) *common.Block {
	b := &common.Block{
		Header: &common.BlockHeader{Number: number},
		Data:   &common.BlockData{Data: make([][]byte, blockSize)},
	}

	for i := uint64(0); i < blockSize; i++ {
		tx := &protoblocktx.Tx{
			Id: fmt.Sprintf("%d-%d", number, i),
		}

		chr := &common.ChannelHeader{
			Type: int32(common.HeaderType_ENDORSER_TRANSACTION),
		}

		// note that this is a trick the token sdk uses to wrap the transaction
		value := &common.ConfigValue{
			Value: protoutil.MarshalOrPanic(tx),
		}

		payload := &common.Payload{
			Header: &common.Header{
				ChannelHeader: protoutil.MarshalOrPanic(chr),
			},
			Data: protoutil.MarshalOrPanic(value),
		}

		env := &common.Envelope{
			Payload: protoutil.MarshalOrPanic(payload),
		}

		b.Data.Data[i] = protoutil.MarshalOrPanic(env)
	}

	return b
}

func CreateConfigBlock(number uint64) *common.Block {
	b := &common.Block{
		Header: &common.BlockHeader{Number: number},
		Data:   &common.BlockData{Data: make([][]byte, 1)},
	}

	chr := &common.ChannelHeader{
		Type: int32(common.HeaderType_CONFIG),
	}

	payload := &common.Payload{
		Header: &common.Header{
			ChannelHeader: protoutil.MarshalOrPanic(chr),
		},
	}

	env := &common.Envelope{
		Payload: protoutil.MarshalOrPanic(payload),
	}

	b.Data.Data[0] = protoutil.MarshalOrPanic(env)

	return b
}
