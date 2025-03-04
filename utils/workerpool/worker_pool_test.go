package workerpool_test

import (
	"context"
	"testing"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/workerpool"
)

func TestWorkerPool(t *testing.T) {
	workers := workerpool.New(&workerpool.Config{
		Parallelism:     10,
		ChannelCapacity: 5,
	})
	test.RunServiceForTest(t.Context(), t, func(ctx context.Context) error {
		return connection.FilterStreamRPCError(workers.Run(ctx))
	}, nil)

	size := 100
	result := channel.Make[int](t.Context(), size)
	for i := range size {
		i := i
		workers.Submit(t.Context(), func() {
			result.Write(i)
		})
	}
	resultMap := make(map[int]any)
	for range size {
		i, ok := result.Read()
		if !ok {
			t.Fatalf("context ended before worker finished")
		}
		_, ok = resultMap[i]
		if ok {
			t.Error("repeated result")
		}
		resultMap[i] = nil
	}
}
