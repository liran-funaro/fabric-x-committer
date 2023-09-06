package vcservice

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
)

// transactionCommitter is responsible for committing the transactions
type transactionCommitter struct {
	// db is a handler for the state database holding all the committed states
	db *database
	// incomingValidatedTransactions is the channel from which the committer receives the validated transactions
	// from the validator
	incomingValidatedTransactions <-chan *validatedTransactions
	// outgoingTransactionsStatus is the channel to which the committer sends the status of the transactions
	// so that the client can be notified
	outgoingTransactionsStatus chan<- *protovcservice.TransactionStatus
}

// txIDsStatusNameSpace is the namespace for storing transaction IDs and status
// This namespace would be used to detect duplicate txIDs to avoid replay attacks
// TODO: duplicate txIDs would be handled in issue #202
const txIDsStatusNameSpace = 512

// versionZero is used to indicate that the key is not found in the database and hence,
// the version should be set to 0 for when the key is inserted into the database
var versionZero = versionNumber(0).bytes()

// newCommitter creates a new transactionCommitter
func newCommitter(
	db *database,
	validatedTxs <-chan *validatedTransactions,
	txsStatus chan<- *protovcservice.TransactionStatus,
) *transactionCommitter {
	return &transactionCommitter{
		db:                            db,
		incomingValidatedTransactions: validatedTxs,
		outgoingTransactionsStatus:    txsStatus,
	}
}

func (c *transactionCommitter) start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go c.commit()
	}
}

func (c *transactionCommitter) commit() {
	for vTx := range c.incomingValidatedTransactions {
		nsToWrites, txsStatus, err := c.prepareWritesForCommit(vTx)
		if err != nil {
			panic(err)
		}

		if err := c.db.commit(nsToWrites); err != nil {
			panic(err)
		}

		c.outgoingTransactionsStatus <- txsStatus
	}
}

func (c *transactionCommitter) prepareWritesForCommit(
	vTx *validatedTransactions,
) (namespaceToWrites, *protovcservice.TransactionStatus, error) {
	// Step 1: group the writes by namespace so that we can commit to each table indepdenently
	nsToBlindWrites := groupWritesByNamespace(vTx.validTxBlindWrites)
	nsToNonBlindWrites := groupWritesByNamespace(vTx.validTxNonBlindWrites)

	// Step 2: fill the version for the blind writes
	if err := c.fillVersionForBlindWrites(nsToBlindWrites); err != nil {
		return nil, nil, err
	}

	// Step 3: merge blind and non-blind writes
	mergedWrites := make(namespaceToWrites)
	for ns, writes := range nsToBlindWrites {
		mergedWrites[ns] = writes
	}

	for ns, writes := range nsToNonBlindWrites {
		nsWrites := mergedWrites.getOrCreate(ns)
		nsWrites.appendMany(writes.keys, writes.values, writes.versions)
	}

	// Step 4: construct transaction status
	txCommitStatus := &protovcservice.TransactionStatus{
		Status: map[string]protoblocktx.Status{},
	}

	for txID := range vTx.invalidTxIndices {
		txCommitStatus.Status[string(txID)] = protoblocktx.Status_ABORTED_MVCC_CONFLICT
	}
	for txID := range vTx.validTxNonBlindWrites {
		txCommitStatus.Status[string(txID)] = protoblocktx.Status_COMMITTED
	}
	for txID := range vTx.validTxBlindWrites {
		txCommitStatus.Status[string(txID)] = protoblocktx.Status_COMMITTED
	}

	// Step 5: add the transaction status to the writes
	txIDNsWrites := &namespaceWrites{}
	for txID, status := range txCommitStatus.Status {
		txIDNsWrites.keys = append(txIDNsWrites.keys, []byte(txID))
		txIDNsWrites.values = append(txIDNsWrites.values, []byte{uint8(status)})
	}
	mergedWrites[txIDsStatusNameSpace] = txIDNsWrites

	return mergedWrites, txCommitStatus, nil
}

func (c *transactionCommitter) fillVersionForBlindWrites(nsToWrites namespaceToWrites) error {
	for nsID, writes := range nsToWrites {
		// TODO: Though we could run the following in a goroutine per namespace, we restrain
		// 		 from doing so till we evaluate the performance

		// Step 1: get the committed version for each key in the writes.
		versionOfPresentKeys, err := c.db.queryVersionsIfPresent(nsID, writes.keys)
		if err != nil {
			return err
		}

		// Step 2: if the key is found in the database, use the committed version + 1 as the new version
		//         otherwise, use version 0.
		for i, key := range writes.keys {
			ver, notPresent := versionOfPresentKeys[string(key)]
			if !notPresent {
				writes.versions[i] = versionZero
				continue
			}

			nextVer := versionNumberFromBytes(ver) + 1
			writes.versions[i] = nextVer.bytes()
		}
	}

	return nil
}

func groupWritesByNamespace(txWrites transactionToWrites) namespaceToWrites {
	nsToWrites := make(namespaceToWrites)

	for _, nsWrites := range txWrites {
		for ns, writes := range nsWrites {
			nsWrites := nsToWrites.getOrCreate(ns)
			nsWrites.appendMany(writes.keys, writes.values, writes.versions)
		}
	}

	return nsToWrites
}
