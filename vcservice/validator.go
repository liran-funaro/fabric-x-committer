package vcservice

import (
	"context"
	"fmt"
	"log"

	"github.com/yugabyte/pgx/v4"
)

// transactionValidator validates the reads of transactions against the committed states
type transactionValidator struct {
	databaseConnection *pgx.Conn
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
	conn *pgx.Conn,
	preparedTxs <-chan *preparedTransactions,
	validatedTxs chan<- *validatedTransactions,
) *transactionValidator {
	return &transactionValidator{
		databaseConnection:            conn,
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
			mismatch, err := v.validateNamespaceReads(nsID, r)
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

func (v *transactionValidator) validateNamespaceReads(nsID namespaceID, r *reads) (*reads, error) {
	// For each namespace nsID, we use the validate_reads_ns_<nsID> function to validate
	// the reads. This function returns the keys and versions of the mismatching reads.
	// Note that we have a table per namespace.
	// We have a validate function per namespace so that we can use the static SQL
	// to avoid parsing, planning and optimizing the query for each invoke. If we use
	// a common function for all namespace, we need to pass the table name as a parameter
	// which makes the query dynamic and hence we lose the benefits of static SQL.
	query := fmt.Sprintf("SELECT * FROM %s($1::varchar[], $2::bytea[])", validateFuncNameForNamespace(nsID))

	mismatch, err := v.databaseConnection.Query(context.Background(), query, r.keys, r.versions)
	if err != nil {
		return nil, err
	}
	defer mismatch.Close()

	keys, values, err := readKeysAndVersions(mismatch)
	if err != nil {
		return nil, err
	}

	mismatchingReads := &reads{}
	mismatchingReads.appendMany(keys, values)

	return mismatchingReads, nil
}

func readKeysAndVersions(r pgx.Rows) ([]string, [][]byte, error) {
	var keys []string
	var versions [][]byte

	for r.Next() {
		var key string
		var version []byte
		if err := r.Scan(&key, &version); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		versions = append(versions, version)
	}

	return keys, versions, r.Err()
}
