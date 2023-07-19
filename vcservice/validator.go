package vcservice

import (
	"log"
)

// transactionValidator validates the reads of transactions against the committed states
type transactionValidator struct {
	// db is the state database holding all the committed states.
	db *database
	// incomingPreparedTransactions is the channel from which the validator receives prepared transactions
	incomingPreparedTransactions <-chan *preparedTransactions
	// outgoingValidatedTransactions is the channel to which the validator sends validated transactions so that
	// the committer can commit them
	outgoingValidatedTransactions chan<- *validatedTransactions
}

// validatedTransactions contains the writes of valid transactions and the txIDs of invalid transactions
type validatedTransactions struct {
	validTxNonBlindWrites transactionToWrites
	validTxBlindWrites    transactionToWrites
	invalidTxIndices      map[TxID]bool
}

// NewValidator creates a new validator
func newValidator(
	db *database,
	preparedTxs <-chan *preparedTransactions,
	validatedTxs chan<- *validatedTransactions,
) *transactionValidator {
	return &transactionValidator{
		db:                            db,
		incomingPreparedTransactions:  preparedTxs,
		outgoingValidatedTransactions: validatedTxs,
	}
}

// start starts the validator with the given number of workers
func (v *transactionValidator) start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go v.validate()
	}
}

func (v *transactionValidator) validate() {
	for prepTx := range v.incomingPreparedTransactions {
		// nsToMismatchingReads maintains all mismatching reads per namespace
		// nsID -> mismatchingReads{keys, versions}
		nsToMismatchingReads := make(namespaceToReads)

		// Step 1: we validate reads and collect mismatching reads per namespace
		// TODO: We can run per namespace validation in parallel. However, we should not
		// 		 over parallelize and make contention among preparer, validator, and committer
		//       goroutines to acquire the CPU. Based on performance evaluation, we can decide
		//       to run per namespace validation in parallel.
		for nsID, r := range prepTx.namespaceToReadEntries {
			mismatch, err := v.db.validateNamespaceReads(nsID, r)
			if err != nil {
				// TODO: we should not panic here. We should handle the error and recover accordingly
				log.Panic(err)
			}

			mismatchingReads := nsToMismatchingReads.getOrCreate(nsID)
			mismatchingReads.appendMany(mismatch.keys, mismatch.versions)
		}

		validNonBlindWrites := prepTx.nonBlindWritesPerTransaction
		validBlindWrites := prepTx.blindWritesPerTransaction
		invalidTxID := make(map[TxID]bool)

		// Step 2: for each mismatching read, we find the transactions which made the
		// read and mark them as invalid. Further, the writes of those invalid transactions
		// are removed from the valid writes
		for nsID, mismatchingReads := range nsToMismatchingReads {
			for index, key := range mismatchingReads.keys {
				r := comparableRead{
					nsID:    nsID,
					key:     key,
					version: string(mismatchingReads.versions[index]),
				}

				txIDs, ok := prepTx.readToTransactionIndices[r]
				if !ok {
					// this should never happen if the preparer and validator are
					// implemented correctly
					// TODO: handle this error gracefully
					log.Panicf("read %v not found in readToTx map", r)
				}

				for _, tID := range txIDs {
					delete(validNonBlindWrites, tID)
					delete(validBlindWrites, tID)
					invalidTxID[tID] = true
				}
			}
		}

		v.outgoingValidatedTransactions <- &validatedTransactions{
			validTxNonBlindWrites: validNonBlindWrites,
			validTxBlindWrites:    validBlindWrites,
			invalidTxIndices:      invalidTxID,
		}
	}
}
