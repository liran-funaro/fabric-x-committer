package pipeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

func TestDependencyMgr(t *testing.T) {
	setup := func() *dependencyMgr {
		m := newDependencyMgr()
		block0 := &token.Block{
			Number: 0,
			Txs: []*token.Tx{
				{
					SerialNumbers: [][]byte{[]byte("1"), []byte("2")},
				},
				{
					SerialNumbers: [][]byte{[]byte("3"), []byte("4")},
				},
				{
					SerialNumbers: [][]byte{[]byte("5"), []byte("6")},
				},
				{
					SerialNumbers: [][]byte{[]byte("1"), []byte("6")}, // dependent on tx0 and tx2
				},
			},
		}
		m.inputChan <- block0
		time.Sleep(10 * time.Millisecond)
		return m
	}

	t.Run("fetchDependencyFreeTxsThatIntersect", func(t *testing.T) {
		m := setup()
		defer m.stop()
		intersection, extras := m.fetchDependencyFreeTxsThatIntersect([]TxSeqNum{
			{0, 0},
			{0, 1},
			{0, 2},
			{0, 3},
			{1, 1}, // notYetSubmitted
		})

		require.Equal(t,
			map[TxSeqNum][][]byte{
				{0, 0}: {[]byte("1"), []byte("2")},
				{0, 1}: {[]byte("3"), []byte("4")},
				{0, 2}: {[]byte("5"), []byte("6")},
			},
			intersection,
		)

		require.ElementsMatch(t,
			[]TxSeqNum{
				{0, 3}, // dependent tx should be included in the extras
				{1, 1}, // not-yet-seen tx should be included in the extras
			},
			extras,
		)
	})

	t.Run("updateWithValidTx", func(t *testing.T) {
		m := setup()
		m.inputChanStatusUpdate <- []*TxStatus{
			{
				TxSeqNum: TxSeqNum{0, 0}, // dependency tx marked valid
				IsValid:  true,
			},
		}
		time.Sleep(10 * time.Millisecond)

		intersection, extras := m.fetchDependencyFreeTxsThatIntersect([]TxSeqNum{
			{0, 1},
			{0, 2},
			{0, 3},
		})

		require.Equal(t,
			map[TxSeqNum][][]byte{
				{0, 1}: {[]byte("3"), []byte("4")},
				{0, 2}: {[]byte("5"), []byte("6")},
			},
			intersection,
		)

		require.ElementsMatch(t,
			[]TxSeqNum{
				{0, 3}, // dependent tx should have been removed from the graph
			},
			extras,
		)
	})

	t.Run("updateWithInvalidTx", func(t *testing.T) {
		m := setup()
		m.inputChanStatusUpdate <- []*TxStatus{ // both the dependency txs marked invalid
			{
				TxSeqNum: TxSeqNum{0, 0},
				IsValid:  false,
			},
			{
				TxSeqNum: TxSeqNum{0, 2},
				IsValid:  false,
			},
		}
		time.Sleep(10 * time.Millisecond)

		intersection, extras := m.fetchDependencyFreeTxsThatIntersect([]TxSeqNum{
			{0, 1},
			{0, 2},
			{0, 3},
		})

		require.Equal(t,
			map[TxSeqNum][][]byte{
				{0, 1}: {[]byte("3"), []byte("4")}, // now, dependent tx should have been declared as dependency free
				{0, 3}: {[]byte("1"), []byte("6")},
			},
			intersection,
		)

		require.ElementsMatch(t,
			[]TxSeqNum{
				{0, 2}, // invalid tx should have been removed from the graph
			},
			extras,
		)
	})
}
