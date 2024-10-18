package coordinatorservice

import (
	"sort"

	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
)

func (c *CoordinatorService) preProcessBlock(
	block *protoblocktx.Block,
) []*protovcservice.Transaction {
	malformedTxs := filterMalformedTxs(block)
	c.recordMetaNsTxs(block)
	return malformedTxs
}

func filterMalformedTxs(block *protoblocktx.Block) []*protovcservice.Transaction {
	badTxStatus := make([]*protocoordinatorservice.TxValidationStatus, 0, len(block.Txs))
	badTxIndex := make([]int, 0, len(block.Txs))

	for idx, tx := range block.Txs {
		if tx.Id == "" {
			badTxStatus = append(badTxStatus, &protocoordinatorservice.TxValidationStatus{
				TxId:   tx.Id,
				Status: protoblocktx.Status_ABORTED_MISSING_TXID,
			})
			badTxIndex = append(badTxIndex, idx)
			continue
		}

		if len(tx.Namespaces) != len(tx.Signatures) {
			badTxStatus = append(badTxStatus, &protocoordinatorservice.TxValidationStatus{
				TxId:   tx.Id,
				Status: protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
			})
			badTxIndex = append(badTxIndex, idx)
			continue
		}

		nsIDs := make(map[uint32]any)
		for _, ns := range tx.Namespaces {
			if _, ok := nsIDs[ns.NsId]; ok {
				badTxStatus = append(badTxStatus, &protocoordinatorservice.TxValidationStatus{
					TxId:   tx.Id,
					Status: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
				})
				badTxIndex = append(badTxIndex, idx)
				break
			}

			nsIDs[ns.NsId] = nil

			badStatus := checkNamespaceFormation(tx.Id, ns)
			if badStatus != nil {
				badTxStatus = append(badTxStatus, badStatus)
				badTxIndex = append(badTxIndex, idx)
				break
			}
		}
	}

	if len(badTxStatus) == 0 {
		return nil
	}

	malformedTxs := make([]*protovcservice.Transaction, 0, len(badTxStatus))
	for i, t := range badTxStatus {
		malformedTxs = append(malformedTxs, &protovcservice.Transaction{
			ID: t.TxId,
			PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
				Code: t.Status,
			},
			BlockNumber: block.Number,
			TxNum:       block.TxsNum[badTxIndex[i]],
		})
	}

	removeFromBlock(block, badTxIndex)
	return malformedTxs
}

func (c *CoordinatorService) recordMetaNsTxs(block *protoblocktx.Block) {
	for _, tx := range block.Txs {
		for _, ns := range tx.Namespaces {
			if types.NamespaceID(ns.NsId) != types.MetaNamespaceID {
				continue
			}
			c.uncommittedMetaNsTx.Store(tx.Id, ns)
			break
		}
	}
}

func checkNamespaceFormation(txID string, ns *protoblocktx.TxNamespace) *protocoordinatorservice.TxValidationStatus {
	if ns.NsVersion == nil {
		return &protocoordinatorservice.TxValidationStatus{
			TxId:   txID,
			Status: protoblocktx.Status_ABORTED_MISSING_NAMESPACE_VERSION,
		}
	}

	if len(ns.ReadWrites) == 0 && len(ns.BlindWrites) == 0 {
		return &protocoordinatorservice.TxValidationStatus{
			TxId:   txID,
			Status: protoblocktx.Status_ABORTED_NO_WRITES,
		}
	}

	// TODO: need to decide whether we can allow other namespaces
	//       in the transaction when the MetaNamespaceID is present.
	if types.NamespaceID(ns.NsId) != types.MetaNamespaceID {
		return nil
	}

	if len(ns.BlindWrites) > 0 {
		return &protocoordinatorservice.TxValidationStatus{
			TxId:   txID,
			Status: protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED,
		}
	}

	nsKeys := make([][]byte, 0, len(ns.ReadWrites))
	nsPolicy := make([][]byte, 0, len(ns.ReadWrites))

	for _, rw := range ns.ReadWrites {
		nsKeys = append(nsKeys, rw.Key)
		nsPolicy = append(nsPolicy, rw.Value)
	}

	for i, key := range nsKeys {
		badStatus := isValidNamespaceEntry(txID, key, nsPolicy[i])
		if badStatus != nil {
			return badStatus
		}
	}

	return nil
}

func isValidNamespaceEntry(
	txID string,
	nsID []byte,
	nsPolicy []byte,
) *protocoordinatorservice.TxValidationStatus {
	if _, err := types.NamespaceIDFromBytes(nsID); err != nil {
		return &protocoordinatorservice.TxValidationStatus{
			TxId:   txID,
			Status: protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID,
		}
	}

	p := &protoblocktx.NamespacePolicy{}
	if err := proto.Unmarshal(nsPolicy, p); err != nil {
		return &protocoordinatorservice.TxValidationStatus{
			TxId:   txID,
			Status: protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID,
		}
	}

	return nil
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
