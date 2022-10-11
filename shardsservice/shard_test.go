package shardsservice

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/pendingcommits"
)

type shardForTest struct {
	shard   *shard
	cleanup func()
}

func newShardForTest(t *testing.T, id uint32, path string) *shardForTest {
	s, err := newShard(id, path, 1_000, metrics.New(false))
	require.NoError(t, err)

	return &shardForTest{
		shard: s,
		cleanup: func() {
			s.delete()
		},
	}
}

func TestExecutePhaseOneAndTwoWithSingleShard(t *testing.T) {
	s := newShardForTest(t, 1, "shard_1")
	defer s.cleanup()

	t.Run("only valid txs", func(t *testing.T) {
		phaseOneRequests := txIDToSerialNumbers{
			pendingcommits.TxID{BlkNum: 1, TxNum: 1}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key1"), []byte("key2")},
			},
			pendingcommits.TxID{BlkNum: 1, TxNum: 3}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key3"), []byte("key4")},
			},
			pendingcommits.TxID{BlkNum: 2, TxNum: 13}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key5"), []byte("key6")},
			},
		}

		s.shard.executePhaseOne(phaseOneRequests)

		ensure3PendingCommits := func() bool {
			return s.shard.pendingCommits.CountTxs() == 3
		}
		require.Eventually(t, ensure3PendingCommits, 5*time.Second, 500*time.Millisecond)

		expectedPhaseOneResponses := []*PhaseOneResponse{
			{
				BlockNum: 1,
				TxNum:    1,
				Status:   PhaseOneResponse_CAN_COMMIT,
			},
			{
				BlockNum: 1,
				TxNum:    3,
				Status:   PhaseOneResponse_CAN_COMMIT,
			},
			{
				BlockNum: 2,
				TxNum:    13,
				Status:   PhaseOneResponse_CAN_COMMIT,
			},
		}

		actualPhaseOneResponses := s.shard.accumulatedPhaseOneResponse()
		require.ElementsMatch(t, expectedPhaseOneResponses, actualPhaseOneResponses)

		keys := [][]byte{[]byte("key1"), []byte("key2"), []byte("key3"), []byte("key4"), []byte("key5"), []byte("key6")}
		checkKeysNonExistanceForTest(t, keys, s.shard)

		phaseTwoRequests := txIDToInstruction{
			pendingcommits.TxID{BlkNum: 1, TxNum: 1}:  PhaseTwoRequest_COMMIT,
			pendingcommits.TxID{BlkNum: 1, TxNum: 3}:  PhaseTwoRequest_COMMIT,
			pendingcommits.TxID{BlkNum: 2, TxNum: 13}: PhaseTwoRequest_COMMIT,
		}
		s.shard.executePhaseTwo(phaseTwoRequests)

		checkKeysExistanceForTest(t, keys, s.shard)
		require.Equal(t, 0, s.shard.pendingCommits.CountTxs())
	})

	t.Run("only invalid txs", func(t *testing.T) {
		phaseOneRequests := txIDToSerialNumbers{
			pendingcommits.TxID{BlkNum: 10, TxNum: 11}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key1"), []byte("key2")},
			},
			pendingcommits.TxID{BlkNum: 11, TxNum: 32}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key3"), []byte("key4")},
			},
			pendingcommits.TxID{BlkNum: 12, TxNum: 23}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key5"), []byte("key6")},
			},
		}

		s.shard.executePhaseOne(phaseOneRequests)

		ensure3PendingCommits := func() bool {
			return s.shard.pendingCommits.CountTxs() == 3
		}
		require.Never(t, ensure3PendingCommits, 2*time.Second, 500*time.Millisecond)

		expectedPhaseOneResponses := []*PhaseOneResponse{
			{
				BlockNum: 10,
				TxNum:    11,
				Status:   PhaseOneResponse_CANNOT_COMMITTED,
			},
			{
				BlockNum: 11,
				TxNum:    32,
				Status:   PhaseOneResponse_CANNOT_COMMITTED,
			},
			{
				BlockNum: 12,
				TxNum:    23,
				Status:   PhaseOneResponse_CANNOT_COMMITTED,
			},
		}

		actualPhaseOneResponses := s.shard.accumulatedPhaseOneResponse()
		require.ElementsMatch(t, expectedPhaseOneResponses, actualPhaseOneResponses)
		require.Equal(t, 0, s.shard.pendingCommits.CountTxs())
	})

	t.Run("phase one response is `can_commit` but phase two request is forget", func(t *testing.T) {
		phaseOneRequests := txIDToSerialNumbers{
			pendingcommits.TxID{BlkNum: 21, TxNum: 1}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key7"), []byte("key8")},
			},
			pendingcommits.TxID{BlkNum: 22, TxNum: 3}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key9"), []byte("key10")},
			},
		}

		s.shard.executePhaseOne(phaseOneRequests)

		ensure3PendingCommits := func() bool {
			return s.shard.pendingCommits.CountTxs() == 2
		}
		require.Eventually(t, ensure3PendingCommits, 5*time.Second, 500*time.Millisecond)

		expectedPhaseOneResponses := []*PhaseOneResponse{
			{
				BlockNum: 21,
				TxNum:    1,
				Status:   PhaseOneResponse_CAN_COMMIT,
			},
			{
				BlockNum: 22,
				TxNum:    3,
				Status:   PhaseOneResponse_CAN_COMMIT,
			},
		}

		actualPhaseOneResponses := s.shard.accumulatedPhaseOneResponse()
		require.ElementsMatch(t, expectedPhaseOneResponses, actualPhaseOneResponses)

		keys := [][]byte{[]byte("key7"), []byte("key8"), []byte("key9"), []byte("key10")}
		checkKeysNonExistanceForTest(t, keys, s.shard)

		phaseTwoRequests := txIDToInstruction{
			pendingcommits.TxID{BlkNum: 21, TxNum: 1}: PhaseTwoRequest_COMMIT,
			pendingcommits.TxID{BlkNum: 22, TxNum: 3}: PhaseTwoRequest_FORGET,
		}
		s.shard.executePhaseTwo(phaseTwoRequests)

		checkKeysExistanceForTest(t, keys[:2], s.shard)
		checkKeysNonExistanceForTest(t, keys[2:], s.shard)
		require.Equal(t, 0, s.shard.pendingCommits.CountTxs())
	})

	t.Run("valid txs and invalid tx", func(t *testing.T) {
		phaseOneRequests := txIDToSerialNumbers{
			pendingcommits.TxID{BlkNum: 4, TxNum: 1}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key11"), []byte("key12")},
			},
			pendingcommits.TxID{BlkNum: 4, TxNum: 3}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key13"), []byte("key14")},
			},
		}

		s.shard.executePhaseOne(phaseOneRequests)

		ensure2PendingCommits := func() bool {
			return s.shard.pendingCommits.CountTxs() == 2
		}
		require.Eventually(t, ensure2PendingCommits, 5*time.Second, 500*time.Millisecond)

		expectedPhaseOneResponses := []*PhaseOneResponse{
			{
				BlockNum: 4,
				TxNum:    1,
				Status:   PhaseOneResponse_CAN_COMMIT,
			},
			{
				BlockNum: 4,
				TxNum:    3,
				Status:   PhaseOneResponse_CAN_COMMIT,
			},
		}

		actualPhaseOneResponses := s.shard.accumulatedPhaseOneResponse()
		require.ElementsMatch(t, expectedPhaseOneResponses, actualPhaseOneResponses)

		keys := [][]byte{[]byte("key11"), []byte("key12"), []byte("key13"), []byte("key14")}
		checkKeysNonExistanceForTest(t, keys, s.shard)

		phaseOneRequests = txIDToSerialNumbers{
			pendingcommits.TxID{BlkNum: 5, TxNum: 1}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key11"), []byte("key12")},
			},
			pendingcommits.TxID{BlkNum: 5, TxNum: 3}: &SerialNumbers{
				SerialNumbers: [][]byte{[]byte("key13"), []byte("key14")},
			},
		}

		s.shard.executePhaseOne(phaseOneRequests)

		ensure4PendingCommits := func() bool {
			return s.shard.pendingCommits.CountTxs() == 4
		}
		require.Never(t, ensure4PendingCommits, 2*time.Second, 500*time.Millisecond)

		phaseTwoRequests := txIDToInstruction{
			pendingcommits.TxID{BlkNum: 4, TxNum: 1}: PhaseTwoRequest_COMMIT,
			pendingcommits.TxID{BlkNum: 4, TxNum: 3}: PhaseTwoRequest_FORGET,
		}
		s.shard.executePhaseTwo(phaseTwoRequests)

		checkKeysExistanceForTest(t, keys[:2], s.shard)

		ensure1PendingCommits := func() bool {
			return s.shard.pendingCommits.CountTxs() == 1
		}
		require.Eventually(t, ensure1PendingCommits, 2*time.Second, 500*time.Millisecond)

		expectedPhaseOneResponses = []*PhaseOneResponse{
			{
				BlockNum: 5,
				TxNum:    1,
				Status:   PhaseOneResponse_CANNOT_COMMITTED,
			},
			{
				BlockNum: 5,
				TxNum:    3,
				Status:   PhaseOneResponse_CAN_COMMIT,
			},
		}

		actualPhaseOneResponses = s.shard.accumulatedPhaseOneResponse()
		require.ElementsMatch(t, expectedPhaseOneResponses, actualPhaseOneResponses)

		phaseTwoRequests = txIDToInstruction{
			pendingcommits.TxID{BlkNum: 5, TxNum: 3}: PhaseTwoRequest_COMMIT,
		}
		s.shard.executePhaseTwo(phaseTwoRequests)

		checkKeysExistanceForTest(t, keys, s.shard)
	})
}

func checkKeysExistanceForTest(t *testing.T, keys [][]byte, s *shard) {
	doNotExists, err := s.db.DoNotExist(keys)
	require.NoError(t, err)

	for _, doNotExist := range doNotExists {
		require.False(t, doNotExist)
	}
}

func checkKeysNonExistanceForTest(t *testing.T, keys [][]byte, s *shard) {
	doNotExists, err := s.db.DoNotExist(keys)
	require.NoError(t, err)

	for _, doNotExist := range doNotExists {
		require.True(t, doNotExist)
	}
}
