package vcservice

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
)

// transactionValidator validates the reads of transactions against the committed states
type transactionValidator struct {
	ctx context.Context

	// db is the state database holding all the committed states.
	db *database
	// incomingPreparedTransactions is the channel from which the validator receives prepared transactions
	incomingPreparedTransactions <-chan *preparedTransactions
	// outgoingValidatedTransactions is the channel to which the validator sends validated transactions so that
	// the committer can commit them
	outgoingValidatedTransactions chan<- *validatedTransactions

	metrics *perfMetrics

	wg sync.WaitGroup
}

// validatedTransactions contains the writes of valid transactions and the txIDs of invalid transactions
type validatedTransactions struct {
	validTxNonBlindWrites    transactionToWrites
	validTxBlindWrites       transactionToWrites
	newWrites                transactionToWrites
	readToTransactionIndices readToTransactions
	invalidTxStatus          map[txID]protoblocktx.Status
}

func (v *validatedTransactions) Debug() {
	if logger.Level() > zapcore.DebugLevel {
		return
	}
	logger.Debugf("total validated: %d\n\t"+
		"valid non-blind writes: %d\n\t"+
		"valid blind writes: %d\n\t"+
		"new writes: %d\n\treads: %d\n\tinvalid: %d\n",
		len(v.validTxNonBlindWrites)+len(v.validTxBlindWrites)+len(v.newWrites)+
			len(v.readToTransactionIndices)+len(v.invalidTxStatus),
		len(v.validTxNonBlindWrites), len(v.validTxBlindWrites), len(v.newWrites),
		len(v.readToTransactionIndices), len(v.invalidTxStatus))
}

// NewValidator creates a new validator
func newValidator(
	ctx context.Context,
	db *database,
	preparedTxs <-chan *preparedTransactions,
	validatedTxs chan<- *validatedTransactions,
	metrics *perfMetrics,
) *transactionValidator { // nolint:revive
	logger.Debugf("Creating new validator")
	return &transactionValidator{
		ctx:                           ctx,
		db:                            db,
		incomingPreparedTransactions:  preparedTxs,
		outgoingValidatedTransactions: validatedTxs,
		metrics:                       metrics,
	}
}

// start starts the validator with the given number of workers
func (v *transactionValidator) start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go v.validate()
	}
}

func (v *transactionValidator) validate() {
	v.wg.Add(1)
	defer v.wg.Done()

	var prepTx *preparedTransactions
	for {
		select {
		case <-v.ctx.Done():
			return
		case prepTx = <-v.incomingPreparedTransactions:
			if prepTx == nil {
				return
			}
		}
		logger.Debugf("Batch of prepared TXs in the validator.")
		prepTx.Debug()
		start := time.Now()
		// Step 1: we validate reads and collect mismatching reads per namespace.
		// TODO: We can run per namespace validation in parallel. However, we should not
		// 		 over parallelize and make contention among preparer, validator, and committer
		//       goroutines to acquire the CPU. Based on performance evaluation, we can decide
		//       to run per namespace validation in parallel.
		nsToMismatchingReads, valErr := v.validateReads(prepTx.nsToReads)
		if valErr != nil {
			// TODO: we should not panic here. We should handle the error and recover accordingly.
			log.Panic(valErr) // nolint:revive
		}

		// Step 2: we construct validated transactions by removing the writes of invalid transactions
		// and recording the txIDs of invalid transactions.
		validatedTxs := prepTx.makeValidated()
		if matchErr := validatedTxs.updateMismatch(nsToMismatchingReads); matchErr != nil {
			// TODO: we should not panic here. We should handle the error and recover accordingly.
			log.Panic(matchErr) // nolint:revive
		}

		prometheusmetrics.Observe(v.metrics.validatorTxBatchLatencySeconds, time.Since(start))
		select {
		case <-v.ctx.Done():
			return
		case v.outgoingValidatedTransactions <- validatedTxs:
		}

		logger.Debugf("Validator sent batch of validated TXs to the committer")
		validatedTxs.Debug()
	}
}

func (v *transactionValidator) validateReads(
	nsToReads namespaceToReads,
) (namespaceToReads /* mismatched */, error) {
	// nsToMismatchingReads maintains all mismatching reads per namespace.
	// nsID -> mismatchingReads{keys, versions}.
	nsToMismatchingReads := make(namespaceToReads)

	for nsID, r := range nsToReads {
		mismatch, err := v.db.validateNamespaceReads(nsID, r)
		if err != nil {
			return nil, err
		}

		mismatchingReads := nsToMismatchingReads.getOrCreate(nsID)
		mismatchingReads.appendMany(mismatch.keys, mismatch.versions)
	}

	return nsToMismatchingReads, nil
}

func (p *preparedTransactions) makeValidated() *validatedTransactions {
	return &validatedTransactions{
		validTxNonBlindWrites:    p.txIDToNsNonBlindWrites,
		validTxBlindWrites:       p.txIDToNsBlindWrites,
		newWrites:                p.txIDToNsNewWrites,
		readToTransactionIndices: p.readToTxIDs,
		invalidTxStatus:          p.invalidTxIDStatus,
	}
}

func (p *preparedTransactions) Debug() {
	if logger.Level() > zapcore.DebugLevel {
		return
	}
	logger.Debugf("total prepared: %d\n\tvalid non-blind writes: %d\n\t"+
		"valid blind writes: %d\n\tnew writes: %d\n\treads: %d\n",
		len(p.txIDToNsNonBlindWrites)+len(p.txIDToNsBlindWrites)+
			len(p.txIDToNsNewWrites)+len(p.readToTxIDs),
		len(p.txIDToNsNonBlindWrites), len(p.txIDToNsBlindWrites),
		len(p.txIDToNsNewWrites), len(p.readToTxIDs))
}

func (v *validatedTransactions) updateMismatch(nsToMismatchingReads namespaceToReads) error {
	// For each mismatching read, we find the transactions which made the
	// read and mark them as invalid. Further, the writes of those invalid
	// transactions are removed from the valid writes.
	for nsID, mismatchingReads := range nsToMismatchingReads {
		for index, key := range mismatchingReads.keys {
			r := comparableRead{
				nsID:    nsID,
				key:     string(key),
				version: string(mismatchingReads.versions[index]),
			}

			txIDs, ok := v.readToTransactionIndices[r]
			if !ok {
				return fmt.Errorf("read %v not found in readToTransactionIndices", r)
			}

			v.updateInvalidTxs(txIDs, protoblocktx.Status_ABORTED_MVCC_CONFLICT)
		}
	}

	return nil
}

func (v *validatedTransactions) updateInvalidTxs(txIDs []txID, status protoblocktx.Status) {
	for _, tID := range txIDs {
		delete(v.validTxNonBlindWrites, tID)
		delete(v.validTxBlindWrites, tID)
		delete(v.newWrites, tID)
		v.invalidTxStatus[tID] = status
	}
}
