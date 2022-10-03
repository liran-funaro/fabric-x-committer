package shardsservice

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPendingCommits(t *testing.T) {
	pc := newPendingCommits()

	txIDSN := map[txID]*SerialNumbers{
		txID{
			blockNum: 1,
			txNum:    1,
		}: &SerialNumbers{
			SerialNumbers: [][]byte{[]byte("s1"), []byte("s2"), []byte("s3")},
		},
		txID{
			blockNum: 1,
			txNum:    3,
		}: &SerialNumbers{
			SerialNumbers: [][]byte{[]byte("s4"), []byte("s5"), []byte("s6")},
		},
		txID{
			blockNum: 2,
			txNum:    2,
		}: &SerialNumbers{
			SerialNumbers: [][]byte{[]byte("s7"), []byte("s8")},
		},
	}

	for tID, sn := range txIDSN {
		pc.add(tID, sn)
	}

	for tID, expectedSN := range txIDSN {
		actualSN := pc.get(tID)
		require.ElementsMatch(t, expectedSN.SerialNumbers, actualSN.SerialNumbers)
	}

	require.Equal(t, 3, pc.count())

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		pc.waitTillNotExist([][]byte{[]byte("s7"), []byte("s8"), []byte("s6")})
	}()

	require.Never(t, func() bool { return pc.count() < 3 }, 2*time.Second, 500*time.Millisecond)
	pc.delete(txID{blockNum: 2, txNum: 2})
	require.Eventually(t, func() bool { return pc.count() == 2 }, 2*time.Second, 500*time.Millisecond)
	pc.delete(txID{blockNum: 1, txNum: 3})

	wg.Wait()
	require.Eventually(t, func() bool { return pc.count() == 1 }, 2*time.Second, 500*time.Millisecond)

	pc.deleteAll()
	require.Equal(t, 0, pc.count())
}
