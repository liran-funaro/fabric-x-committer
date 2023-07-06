package vcservice

import (
	"github.com/golang/protobuf/proto"
	"github.ibm.com/decentralized-trust-research/scalable-committer/pkg/types"
)

// transactionPreparer prepares transaction batches for validation and commit.
type transactionPreparer struct {
	// incomingTransactionBatch is an input to the preparer
	incomingTransactionBatch <-chan *TransactionBatch
	// outgoingPreparedTransactions is an output of the preparer and an input to the validator
	outgoingPreparedTransactions chan<- *preparedTransactions
}

// TransactionBatch is a batch of transactions
// TODO: this will be moved to proto as it will be sent by the coordinator
type TransactionBatch struct {
	// we assume no duplicate transaction IDs within a batch
	Transactions []*TransactionWithID
}

// TransactionWithID is a transaction with an ID
// TODO: this will be moved to proto as it will be sent by the coordinator
type TransactionWithID struct {
	ID         TxID
	Namespaces []*types.TxNamespace
}

// TxID is a transaction ID
// TODO: this will be moved to proto as it will be sent by the coordinator
type TxID string

// preparedTransactions is a list of transactions that are prepared for validation and commit
// preparedTransactions is NOT thread safe.
type preparedTransactions struct {
	namespaceToReadEntries       namespaceToReads
	readToTransactionIndices     readToTransactions
	nonBlindWritesPerTransaction transactionToWrites
	blindWritesPerTransaction    transactionToWrites
}

// namespaceToReads maps a namespace ID to a list of reads
type namespaceToReads map[uint32]*reads

// reads is a list of keys and versions
type reads struct {
	keys     []string
	versions [][]byte
}

// readToTransactions maps a read to the index of the transaction that contains it
// used to find the index of invalid transactions when a read is invalid
// i.e., the read version is not matching the committed version
type readToTransactions map[comparableRead][]TxID

// comparableRead is a read that can be used as a map key
type comparableRead struct {
	nsID    uint32
	key     string
	version string
}

type transactionToWrites map[TxID]namespaceToWrites

type namespaceToWrites map[uint32]*namespaceWrites

type namespaceWrites struct {
	keys     []string
	values   [][]byte
	versions [][]byte
}

func (nr namespaceToReads) getOrCreate(nsID uint32) *reads {
	nsRead, ok := nr[nsID]
	if !ok {
		nsRead = &reads{}
		nr[nsID] = nsRead
	}
	return nsRead
}

func (tw transactionToWrites) getOrCreate(id TxID, nsID uint32) *namespaceWrites {
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

func (r *reads) append(key string, version []byte) {
	r.keys = append(r.keys, key)
	r.versions = append(r.versions, version)
}

func (r *reads) appendMany(keys []string, versions [][]byte) {
	r.keys = append(r.keys, keys...)
	r.versions = append(r.versions, versions...)
}

func (nw *namespaceWrites) append(key string, value, version []byte) {
	nw.keys = append(nw.keys, key)
	nw.values = append(nw.values, value)
	nw.versions = append(nw.versions, version)
}

// newPreparer creates a new preparer instance with input channel txBatch and output channel preparedTxs
func newPreparer(txBatch <-chan *TransactionBatch, preparedTxs chan<- *preparedTransactions) *transactionPreparer {
	return &transactionPreparer{
		incomingTransactionBatch:     txBatch,
		outgoingPreparedTransactions: preparedTxs,
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
	for txBatch := range p.incomingTransactionBatch {
		prepTxs := &preparedTransactions{
			namespaceToReadEntries:       make(namespaceToReads),
			readToTransactionIndices:     make(readToTransactions),
			nonBlindWritesPerTransaction: make(transactionToWrites),
			blindWritesPerTransaction:    make(transactionToWrites),
		}

		for _, tx := range txBatch.Transactions {
			for _, ns := range tx.Namespaces {
				if len(ns.ReadsOnly) > 0 {
					p.addReadsOnlyToPreparedTxs(prepTxs, tx.ID, ns.NsId, ns.ReadsOnly)
				}

				if len(ns.ReadWrites) > 0 {
					p.addReadWritesToPreparedTxs(prepTxs, tx.ID, ns.NsId, ns.ReadWrites)
				}

				if len(ns.BlindWrites) > 0 {
					p.addBlindWritesToPreparedTxs(prepTxs, tx.ID, ns.NsId, ns.BlindWrites)
				}
			}
		}

		p.outgoingPreparedTransactions <- prepTxs
	}
}

// addReadsOnlyToPreparedTxs adds reads-only to the prepared transactions
func (p *transactionPreparer) addReadsOnlyToPreparedTxs(prepTxs *preparedTransactions, id TxID, nsID uint32, readsOnly []*types.Read) {
	nsReads := prepTxs.namespaceToReadEntries.getOrCreate(nsID)

	for _, r := range readsOnly {
		// When more than one txs read the same key, we only need to add the key once for validation.
		// If the read is already present in the list, we can skip adding it.
		cr := comparableRead{
			nsID:    nsID,
			key:     r.Key,
			version: string(r.Version),
		}
		v, present := prepTxs.readToTransactionIndices[cr]
		prepTxs.readToTransactionIndices[cr] = append(v, id)
		if !present {
			nsReads.append(r.Key, r.Version)
		}
	}
}

// addReadWritesToPreparedTxs adds read-writes to the prepared transactions
func (p *transactionPreparer) addReadWritesToPreparedTxs(prepTxs *preparedTransactions, id TxID, nsID uint32, readWrites []*types.ReadWrite) {
	nsReads := prepTxs.namespaceToReadEntries.getOrCreate(nsID)
	nsWrites := prepTxs.nonBlindWritesPerTransaction.getOrCreate(id, nsID)

	for _, rw := range readWrites {
		// In read-writes, duplicates are not possible between transactions. This is because
		// read-write and write-write dependency ensures that only one of the transactions is
		// chosen for the validation and commit.
		cr := comparableRead{
			nsID:    nsID,
			key:     rw.Key,
			version: string(rw.Version),
		}
		prepTxs.readToTransactionIndices[cr] = append(prepTxs.readToTransactionIndices[cr], id)
		nsReads.append(rw.Key, rw.Version)

		// A "write" should increase the version by one, or use zero version if it is the fist write.
		var ver versionNumber = 0
		if rw.Version != nil {
			ver = versionNumberFromBytes(rw.Version) + 1
		}
		nsWrites.append(rw.Key, rw.Value, ver.bytes())
	}
}

// addBlindWritesToPreparedTxs adds the blind writes to the prepared transactions
func (p *transactionPreparer) addBlindWritesToPreparedTxs(prepTxs *preparedTransactions, id TxID, nsID uint32, blindWrites []*types.Write) {
	nsWrites := prepTxs.blindWritesPerTransaction.getOrCreate(id, nsID)

	for _, w := range blindWrites {
		nsWrites.append(w.Key, w.Value, nil)
	}
}

type versionNumber uint64

func versionNumberFromBytes(version []byte) versionNumber {
	v, _ := proto.DecodeVarint(version)
	return versionNumber(v)
}

func (v versionNumber) bytes() []byte {
	return proto.EncodeVarint(uint64(v))
}
