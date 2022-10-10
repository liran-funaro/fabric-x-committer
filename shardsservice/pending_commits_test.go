package shardsservice

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/pendingcommits"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

func TestPendingCommits(t *testing.T) {
	pc := NewMutexPendingCommits()

	txIDSN := map[pendingcommits.TxID][]token.SerialNumber{
		pendingcommits.TxID{BlkNum: 1, TxNum: 1}: {[]byte("s1"), []byte("s2"), []byte("s3")},
		pendingcommits.TxID{BlkNum: 1, TxNum: 3}: {[]byte("s4"), []byte("s5"), []byte("s6")},
		pendingcommits.TxID{BlkNum: 2, TxNum: 2}: {[]byte("s7"), []byte("s8")},
	}

	for tID, sn := range txIDSN {
		pc.Add(tID, sn)
	}

	for tID, expectedSN := range txIDSN {
		actualSN := pc.Get(tID)
		require.ElementsMatch(t, expectedSN, actualSN)
	}

	require.Equal(t, 3, pc.CountTxs())

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		pc.WaitTillNotExist([][]byte{[]byte("s7"), []byte("s8"), []byte("s6")})
	}()

	require.Never(t, func() bool { return pc.CountTxs() < 3 }, 2*time.Second, 500*time.Millisecond)
	pc.Delete(pendingcommits.TxID{BlkNum: 2, TxNum: 2})
	require.Eventually(t, func() bool { return pc.CountTxs() == 2 }, 2*time.Second, 500*time.Millisecond)
	pc.Delete(pendingcommits.TxID{BlkNum: 1, TxNum: 3})

	wg.Wait()
	require.Eventually(t, func() bool { return pc.CountTxs() == 1 }, 2*time.Second, 500*time.Millisecond)

	pc.DeleteAll()
	require.Equal(t, 0, pc.CountTxs())
}
