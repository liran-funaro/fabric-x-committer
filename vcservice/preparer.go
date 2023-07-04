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
	keys    []string
	values  [][]byte
	version [][]byte
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
	nsRead, ok := prepTxs.namespaceToReadEntries[nsID]
	if !ok {
		nsRead = &reads{}
		prepTxs.namespaceToReadEntries[nsID] = nsRead
	}

	for _, r := range readsOnly {
		duplicate := false

		cr := comparableRead{
			nsID:    nsID,
			key:     r.Key,
			version: string(r.Version),
		}
		if _, ok := prepTxs.readToTransactionIndices[cr]; ok {
			duplicate = true
		}
		prepTxs.readToTransactionIndices[cr] = append(prepTxs.readToTransactionIndices[cr], id)

		// when more than one txs read the same key, we only need to add the key once for validation
		// if the read is already present in the list, we can skip adding it
		if !duplicate {
			nsRead.keys = append(nsRead.keys, r.Key)
			nsRead.versions = append(nsRead.versions, r.Version)
		}

	}
}

// addReadWritesToPreparedTxs adds read-writes to the prepared transactions
func (p *transactionPreparer) addReadWritesToPreparedTxs(prepTxs *preparedTransactions, id TxID, nsID uint32, readWrites []*types.ReadWrite) {
	nsReads, ok := prepTxs.namespaceToReadEntries[nsID]
	if !ok {
		nsReads = &reads{}
		prepTxs.namespaceToReadEntries[nsID] = nsReads
	}

	nsToWrites, ok := prepTxs.nonBlindWritesPerTransaction[id]
	if !ok {
		nsToWrites = make(namespaceToWrites)
		prepTxs.nonBlindWritesPerTransaction[id] = nsToWrites
	}

	nsWrites, ok := nsToWrites[nsID]
	if !ok {
		nsWrites = &namespaceWrites{}
		nsToWrites[nsID] = nsWrites
	}

	for _, rw := range readWrites {
		// In read-writes, duplicates are not possible between transactions. This is because
		// read-write and write-write dependency ensures that only one of the transactions is
		// chosen for the validation and commit
		cr := comparableRead{
			nsID:    nsID,
			key:     rw.Key,
			version: string(rw.Version),
		}

		prepTxs.readToTransactionIndices[cr] = append(prepTxs.readToTransactionIndices[cr], id)

		nsReads.keys = append(nsReads.keys, rw.Key)
		nsReads.versions = append(nsReads.versions, rw.Version)

		nsWrites.keys = append(nsWrites.keys, rw.Key)
		nsWrites.values = append(nsWrites.values, rw.Value)

		var ver []byte
		if rw.Version == nil {
			ver = versionNumber(0).bytes()
		} else {
			ver = (versionNumberFromBytes(rw.Version) + 1).bytes()
		}
		nsWrites.version = append(nsWrites.version, ver)
	}
}

// addBlindWritesToPreparedTxs adds the blind writes to the prepared transactions
func (p *transactionPreparer) addBlindWritesToPreparedTxs(prepTxs *preparedTransactions, id TxID, nsID uint32, blindWrites []*types.Write) {
	nsWrites, ok := prepTxs.blindWritesPerTransaction[id]
	if !ok {
		nsWrites = make(namespaceToWrites)
		prepTxs.blindWritesPerTransaction[id] = nsWrites
	}

	nsWrite, ok := nsWrites[nsID]
	if !ok {
		nsWrite = &namespaceWrites{}
		nsWrites[nsID] = nsWrite
	}

	for _, w := range blindWrites {
		nsWrite.keys = append(nsWrite.keys, w.Key)
		nsWrite.values = append(nsWrite.values, w.Value)
		nsWrite.version = append(nsWrite.version, nil)
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
