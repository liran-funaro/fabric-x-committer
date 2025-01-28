package coordinatorservice

import (
	"sort"

	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
)

var valid = protoblocktx.Status_COMMITTED

func (c *CoordinatorService) preProcessBlock(
	block *protoblocktx.Block,
) []*protovcservice.Transaction {
	return filterMalformedTxs(block)
}

func filterMalformedTxs(block *protoblocktx.Block) []*protovcservice.Transaction {
	malformedTxs := make([]*protovcservice.Transaction, 0, len(block.Txs))
	badTxIndex := make([]int, 0, len(block.Txs))
	recordInvalidTx := func(txID string, txNum uint32, status protoblocktx.Status, idx int) {
		badTxIndex = append(badTxIndex, idx)
		malformedTxs = append(malformedTxs, &protovcservice.Transaction{
			ID: txID,
			PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
				Code: status,
			},
			BlockNumber: block.Number,
			TxNum:       txNum,
		})
	}

	for idx, tx := range block.Txs {
		txNum := block.TxsNum[idx]
		if tx.Id == "" {
			recordInvalidTx(tx.Id, txNum, protoblocktx.Status_ABORTED_MISSING_TXID, idx)
			continue
		}

		if len(tx.Namespaces) != len(tx.Signatures) {
			recordInvalidTx(tx.Id, txNum, protoblocktx.Status_ABORTED_SIGNATURE_INVALID, idx)
			continue
		}

		nsIDs := make(map[uint32]any)
		for _, ns := range tx.Namespaces {
			if _, ok := nsIDs[ns.NsId]; ok {
				recordInvalidTx(tx.Id, txNum, protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE, idx)
				break
			}

			nsIDs[ns.NsId] = nil

			status := checkNamespaceFormation(ns)
			if status != valid {
				recordInvalidTx(tx.Id, txNum, status, idx)
				break
			}
		}
	}

	removeFromBlock(block, badTxIndex)
	return malformedTxs
}

func checkNamespaceFormation(ns *protoblocktx.TxNamespace) protoblocktx.Status {
	if ns.NsVersion == nil {
		return protoblocktx.Status_ABORTED_MISSING_NAMESPACE_VERSION
	}

	if len(ns.ReadWrites) == 0 && len(ns.BlindWrites) == 0 {
		return protoblocktx.Status_ABORTED_NO_WRITES
	}

	// TODO: need to decide whether we can allow other namespaces
	//       in the transaction when the MetaNamespaceID is present.
	if types.NamespaceID(ns.NsId) != types.MetaNamespaceID {
		return valid
	}

	if len(ns.BlindWrites) > 0 {
		return protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED
	}

	nsKeys := make([][]byte, 0, len(ns.ReadWrites))
	nsPolicy := make([][]byte, 0, len(ns.ReadWrites))

	for _, rw := range ns.ReadWrites {
		nsKeys = append(nsKeys, rw.Key)
		nsPolicy = append(nsPolicy, rw.Value)
	}

	for i, key := range nsKeys {
		status := isValidNamespaceEntry(key, nsPolicy[i])
		if status != valid {
			return status
		}
	}

	return valid
}

func isValidNamespaceEntry(nsID, nsPolicy []byte) protoblocktx.Status {
	if _, err := types.NamespaceIDFromBytes(nsID); err != nil {
		return protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID
	}

	p := &protoblocktx.NamespacePolicy{}
	if err := proto.Unmarshal(nsPolicy, p); err != nil {
		return protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID
	}

	return valid
}

func removeFromBlock(block *protoblocktx.Block, idx []int) {
	// Remove duplicates and sort the indices
	idx = uniqueSortedIndices(idx)

	// Iterate over the indices in reverse order
	for i := len(idx) - 1; i >= 0; i-- {
		index := idx[i]
		block.Txs = append(block.Txs[:index], block.Txs[index+1:]...)
		block.TxsNum = append(block.TxsNum[:index], block.TxsNum[index+1:]...)
	}
}

func uniqueSortedIndices(indices []int) []int {
	sort.Ints(indices)
	unique := indices[:0]
	for _, i := range indices {
		if len(unique) == 0 || unique[len(unique)-1] != i {
			unique = append(unique, i)
		}
	}
	return unique
}
