package vcservice

import (
	"context"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
)

type (
	// transactionPreparer prepares transaction batches for validation and commit.
	transactionPreparer struct {
		ctx context.Context
		// incomingTransactionBatch is an input to the preparer
		incomingTransactionBatch <-chan *protovcservice.TransactionBatch
		// outgoingPreparedTransactions is an output of the preparer and an input to the validator
		outgoingPreparedTransactions chan<- *preparedTransactions
		// metrics is the metrics collector
		metrics *perfMetrics

		wg sync.WaitGroup
	}

	txID string

	// preparedTransactions is a list of transactions that are prepared for validation and commit
	// preparedTransactions is NOT thread safe.
	preparedTransactions struct {
		namespaceToReadEntries       namespaceToReads
		readToTransactionIndices     readToTransactions
		nonBlindWritesPerTransaction transactionToWrites
		blindWritesPerTransaction    transactionToWrites
		newWrites                    transactionToWrites
	}

	// namespaceToReads maps a namespace ID to a list of reads.
	namespaceToReads map[types.NamespaceID]*reads

	// reads is a list of keys and versions.
	reads struct {
		keys     [][]byte
		versions [][]byte
	}

	// readToTransactions maps a read to the index of the transaction that contains it
	// used to find the index of invalid transactions when a read is invalid
	// i.e., the read version is not matching the committed version.
	readToTransactions map[comparableRead][]txID

	// comparableRead is a read that can be used as a map key
	comparableRead struct {
		nsID    types.NamespaceID
		key     string
		version string
	}

	transactionToWrites map[txID]namespaceToWrites

	namespaceToWrites map[types.NamespaceID]*namespaceWrites

	namespaceWrites struct {
		keys     [][]byte
		values   [][]byte
		versions [][]byte
	}
)

// newPreparer creates a new preparer instance with input channel txBatch and output channel preparedTxs
func newPreparer(
	ctx context.Context,
	txBatch <-chan *protovcservice.TransactionBatch,
	preparedTxs chan<- *preparedTransactions,
	metrics *perfMetrics,
) *transactionPreparer {
	logger.Debugf("Creating new preparer")
	return &transactionPreparer{
		ctx:                          ctx,
		incomingTransactionBatch:     txBatch,
		outgoingPreparedTransactions: preparedTxs,
		metrics:                      metrics,
	}
}

func (p *transactionPreparer) start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go p.prepare()
	}
}

// prepare reads transactions from the txBatch channel and prepares them for validation.
// It groups reads by namespace and creates a map of reads to transactions to find
// the index of invalid transactions when a read is invalid.
// It sends the prepared transactions to the preparedTxs channel which is an input
// to the validator.
func (p *transactionPreparer) prepare() {
	p.wg.Add(1)
	defer p.wg.Done()

	var txBatch *protovcservice.TransactionBatch
	for {
		select {
		case <-p.ctx.Done():
			return
		case txBatch = <-p.incomingTransactionBatch:
			if txBatch == nil {
				return
			}
		}
		logger.Debugf("New batch with %d in the preparer.", len(txBatch.Transactions))
		start := time.Now()
		prepTxs := &preparedTransactions{
			namespaceToReadEntries:       make(namespaceToReads),
			readToTransactionIndices:     make(readToTransactions),
			nonBlindWritesPerTransaction: make(transactionToWrites),
			blindWritesPerTransaction:    make(transactionToWrites),
			newWrites:                    make(transactionToWrites),
		}

		for _, tx := range txBatch.Transactions {
			for _, nsOperations := range tx.Namespaces {
				tID := txID(tx.ID)

				prepTxs.addReadsOnly(tID, nsOperations)
				prepTxs.addReadWrites(tID, nsOperations)
				prepTxs.addBlindWrites(tID, nsOperations)
			}
		}

		for ns, read := range prepTxs.namespaceToReadEntries {
			if len(read.keys) == 0 {
				delete(prepTxs.namespaceToReadEntries, ns)
			}
		}

		for _, lst := range []transactionToWrites{
			prepTxs.nonBlindWritesPerTransaction, prepTxs.blindWritesPerTransaction, prepTxs.newWrites,
		} {
			lst.clearEmpty()
		}

		prometheusmetrics.Observe(p.metrics.preparerTxBatchLatencySeconds, time.Since(start))
		select {
		case <-p.ctx.Done():
			return
		case p.outgoingPreparedTransactions <- prepTxs:
		}

		logger.Debugf("Transaction preparing finished.")
	}
}

// addReadsOnly adds reads-only to the prepared transactions
func (p *preparedTransactions) addReadsOnly(id txID, ns *protoblocktx.TxNamespace) {
	if len(ns.ReadsOnly) == 0 {
		return
	}

	nsID := types.NamespaceID(ns.NsId)
	nsReads := p.namespaceToReadEntries.getOrCreate(nsID)

	for _, r := range ns.ReadsOnly {
		// When more than one txs read the same key, we only need to add the key once for validation.
		// If the read is already present in the list, we can skip adding it.
		cr := comparableRead{
			nsID:    nsID,
			key:     string(r.Key),
			version: string(r.Version),
		}
		v, present := p.readToTransactionIndices[cr]
		p.readToTransactionIndices[cr] = append(v, id)
		if !present {
			nsReads.append(r.Key, r.Version)
		}
	}
}

// addReadWrites adds read-writes to the prepared transactions
func (p *preparedTransactions) addReadWrites(id txID, ns *protoblocktx.TxNamespace) {
	if len(ns.ReadWrites) == 0 {
		return
	}

	nsID := types.NamespaceID(ns.NsId)
	nsReads := p.namespaceToReadEntries.getOrCreate(nsID)
	nsWrites := p.nonBlindWritesPerTransaction.getOrCreate(id, nsID)
	newWrites := p.newWrites.getOrCreate(id, nsID)

	for _, rw := range ns.ReadWrites {
		// In read-writes, duplicates are not possible between transactions. This is because
		// read-write and write-write dependency ensures that only one of the transactions is
		// chosen for the validation and commit.
		cr := comparableRead{
			nsID:    nsID,
			key:     string(rw.Key),
			version: string(rw.Version),
		}
		p.readToTransactionIndices[cr] = append(p.readToTransactionIndices[cr], id)

		if rw.Version != nil {
			nsReads.append(rw.Key, rw.Version)
			ver := types.VersionNumberFromBytes(rw.Version) + 1
			nsWrites.append(rw.Key, rw.Value, ver.Bytes())
		} else {
			newWrites.append(rw.Key, rw.Value, nil)
		}
	}
}

// addBlindWrites adds the blind writes to the prepared transactions
func (p *preparedTransactions) addBlindWrites(id txID, ns *protoblocktx.TxNamespace) {
	if len(ns.BlindWrites) == 0 {
		return
	}

	nsID := types.NamespaceID(ns.NsId)
	nsWrites := p.blindWritesPerTransaction.getOrCreate(id, nsID)

	for _, w := range ns.BlindWrites {
		nsWrites.append(w.Key, w.Value, nil)
	}
}

func (nw namespaceToWrites) empty() bool {
	if nw == nil {
		return true
	}

	for _, writes := range nw {
		if !writes.empty() {
			return false
		}
	}

	return true
}

func (nw namespaceToWrites) getOrCreate(nsID types.NamespaceID) *namespaceWrites {
	nsWrites, ok := nw[nsID]
	if !ok {
		nsWrites = &namespaceWrites{}
		nw[nsID] = nsWrites
	}
	return nsWrites
}

func (nr namespaceToReads) getOrCreate(nsID types.NamespaceID) *reads {
	nsRead, ok := nr[nsID]
	if !ok {
		nsRead = &reads{}
		nr[nsID] = nsRead
	}
	return nsRead
}

func (tw transactionToWrites) getOrCreate(id txID, nsID types.NamespaceID) *namespaceWrites {
	nsToWrites, ok := tw[id]
	if !ok {
		nsToWrites = make(namespaceToWrites)
		tw[id] = nsToWrites
	}

	nsWrites, ok := nsToWrites[nsID]
	if !ok {
		nsWrites = &namespaceWrites{}
		nsToWrites[nsID] = nsWrites
	}

	return nsWrites
}

func (tw transactionToWrites) clearEmpty() {
	var emptyIDs []txID
	for id, ntw := range tw {
		if ntw.empty() {
			emptyIDs = append(emptyIDs, id)
			continue
		}

		var emptyNsID []types.NamespaceID
		for nsID, nw := range ntw {
			if nw.empty() {
				emptyNsID = append(emptyNsID, nsID)
			}
		}

		for _, nsID := range emptyNsID {
			delete(ntw, nsID)
		}
	}

	for _, id := range emptyIDs {
		delete(tw, id)
	}
}

func (r *reads) append(key, version []byte) {
	r.keys = append(r.keys, key)
	r.versions = append(r.versions, version)
}

func (r *reads) appendMany(keys, versions [][]byte) {
	r.keys = append(r.keys, keys...)
	r.versions = append(r.versions, versions...)
}

func (nw *namespaceWrites) append(key, value, version []byte) {
	nw.keys = append(nw.keys, key)
	nw.values = append(nw.values, value)
	nw.versions = append(nw.versions, version)
}

func (nw *namespaceWrites) appendMany(key, value, version [][]byte) {
	nw.keys = append(nw.keys, key...)
	nw.values = append(nw.values, value...)
	nw.versions = append(nw.versions, version...)
}

func (nw *namespaceWrites) empty() bool {
	return nw == nil || len(nw.keys) == 0
}

func (r *reads) empty() bool {
	return r == nil || len(r.keys) == 0
}

func (nr namespaceToReads) empty() bool {
	if nr == nil {
		return true
	}

	for _, r := range nr {
		if !r.empty() {
			return false
		}
	}

	return true
}
