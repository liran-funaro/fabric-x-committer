package workerpool_test

import (
	"sync"
	"testing"

	. "github.com/onsi/gomega"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/workerpool"
)

func TestWorkerPool(t *testing.T) {
	test.FailHandler(t)

	workers := workerpool.New(&workerpool.Config{
		Parallelism:     10,
		ChannelCapacity: 5,
	})

	wg := sync.WaitGroup{}
	wg.Add(100)

	result := make([]bool, 100)
	for i := 0; i < 100; i++ {
		workers.Run(func(i int) func() {
			return func() {
				result[i] = true
				wg.Done()
			}
		}(i))
	}

	wg.Wait()

	Expect(result).To(HaveEach(true))
}
