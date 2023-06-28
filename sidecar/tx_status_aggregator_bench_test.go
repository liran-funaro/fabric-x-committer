package sidecar_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

var (
	delays     = []time.Duration{0} //, 1 * time.Microsecond, 1 * time.Millisecond}
	blockSizes = []uint64{100}      //, 1000, 10000}
	partsNums  = []uint64{1, 4, 8}
)

func BenchmarkTxStatusAggregatorSerial(b *testing.B) {
	for _, delay := range delays {
		for _, blockSize := range blockSizes {
			for _, parts := range partsNums {
				delayGenerator := test.NewDelayGenerator(test.Constant(int64(delay/time.Duration(parts))), 20)
				partSize := blockSize / parts
				b.Run(fmt.Sprintf("delay=%v, blockSize=%d, partsNums=%d", delay, blockSize, partsNums), func(b *testing.B) {

					t := NewTestInstance()
					t.StartEmptyOutputConsumer()
					b.ResetTimer()
					for n := uint64(0); n < uint64(b.N); n++ {
						b.StopTimer()
						block := createBlock(n, blockSize)
						b.StartTimer()

						t.SubmitToOrderer(ordererRequest{block, []int{}})
						for p := uint64(0); p < parts; p++ {
							b.StopTimer()
							statuses := createValidStatuses(n, p*partSize, uint64(utils.Min(int((p+1)*partSize), int(blockSize))))
							b.StartTimer()
							delayGenerator.Next()
							t.ReturnFromCommitter(statuses...)
						}
					}
				})
			}
		}
	}
}

func BenchmarkTxStatusAggregatorParallel(b *testing.B) {
	for _, delay := range delays {
		for _, blockSize := range blockSizes {
			for _, parts := range partsNums {
				delayGenerator := test.NewDelayGenerator(test.Constant(int64(delay/time.Duration(parts))), 20)
				partSize := blockSize / parts
				b.Run(fmt.Sprintf("delay=%v, blockSize=%d, partsNums=%d", delay, blockSize, partsNums), func(b *testing.B) {
					t := NewTestInstance()
					t.StartEmptyOutputConsumer()
					blocks := uint64(0)
					b.ResetTimer()
					b.RunParallel(func(pb *testing.PB) {
						for pb.Next() {
							n := atomic.AddUint64(&blocks, 1) - 1
							block := createBlock(n, blockSize)

							t.SubmitToOrderer(ordererRequest{block, []int{}})
							for p := uint64(0); p < parts; p++ {
								statuses := createValidStatuses(n, p*partSize, uint64(utils.Min(int((p+1)*partSize), int(blockSize))))
								delayGenerator.Next()
								t.ReturnFromCommitter(statuses...)
							}
						}
					})
				})
			}
		}
	}
}
