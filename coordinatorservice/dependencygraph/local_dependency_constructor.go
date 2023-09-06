package dependencygraph

import (
	"sync"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("dependencygraph")

type (
	// localDependencyConstructor construct dependencies between a given set of transactions
	// without considering other transactions in the dependency graph.
	localDependencyConstructor struct {
		// incomingTransactions is an inputs to the localDependencyConstructor.
		incomingTransactions <-chan *transactionBatch

		// outgoingTransactionsNode is the output of the localDependencyConstructor
		// and input to the globalDependencyManager.
		outgoingTransactionsNode chan<- *transactionNodeBatch

		// lastOutputtedID is the last transaction ID that was outputted by the
		// localDependencyConstructor. It is used to ensure that the transactions
		// are outputted in the same order as they were received by the scalable
		// committer. The id field in the transactionBatch denotes the order.
		lastOutputtedID *atomic.Uint64
		orderEnforcer   *sync.Cond
	}

	transactionBatch struct {
		id  uint64
		txs []*protoblocktx.Tx
	}

	transactionNodeBatch struct {
		txsNode          []*TransactionNode
		localDepDetector *dependencyDetector
	}
)

func newLocalDependencyConstructor(
	incomingTxs <-chan *transactionBatch,
	outgoingTxsNode chan<- *transactionNodeBatch,
) *localDependencyConstructor {
	return &localDependencyConstructor{
		incomingTransactions:     incomingTxs,
		outgoingTransactionsNode: outgoingTxsNode,
		lastOutputtedID:          &atomic.Uint64{},
		orderEnforcer:            sync.NewCond(&sync.Mutex{}),
	}
}

func (p *localDependencyConstructor) start(numWorkers int) {
	for i := 0; i < numWorkers; i++ {
		go p.construct()
	}
}

func (p *localDependencyConstructor) construct() {
	for txs := range p.incomingTransactions {
		logger.Debugf("Constructing dependencies for txs with id %d", txs.id)

		depDetector := newDependencyDetector()
		txsNode := make([]*TransactionNode, len(txs.txs))

		for i, tx := range txs.txs {
			// NOTE: we can parallelize newTransactionNode(), and
			//       addDependenciesAndUpdateDependents() across txs.
			txNode := newTransactionNode(tx)

			// using the dependency detector, we find the transactions that
			// txNode depends on. We then add these transactions as
			// dependencies of txNode and update the dependents of these
			// transactions to include txNode. Finally, we add txNode to
			// the dependency detector's so that reads-writes performed
			// by this transaction can be considered to detect dependencies
			// of the next transaction.
			dependsOnTxs := depDetector.getDependenciesOf(txNode)
			txNode.addDependenciesAndUpdateDependents(dependsOnTxs)
			depDetector.addWaitingTx(txNode)
			txsNode[i] = txNode
		}

		p.sendTxsNodeToGlobalDependencyManager(
			txs.id,
			&transactionNodeBatch{
				txsNode:          txsNode,
				localDepDetector: depDetector,
			},
		)
	}
}

func (p *localDependencyConstructor) sendTxsNodeToGlobalDependencyManager(id uint64, txsNode *transactionNodeBatch) {
	p.orderEnforcer.L.Lock()

	// NOTE: We are running the construction of local dependencies in parallel
	//       for various transaction batches. However, when we output the
	// 	     transaction batches to the global dependency manager, we need to
	//       ensure that the order of the transaction batches is the same as
	//       the order in which they were received by the scalable committer.
	//       This is achieved by using the lastOutputtedID field. The first
	//       ever value of id field is 1 and should not be 0.
	for !p.lastOutputtedID.CompareAndSwap(id-1, id) {
		// We intentionally cause the goroutine to wait such that the one needing
		// to complete the task and enqueue the next ID can get the CPU.
		// Thus limiting to processing only N batches ahead of the last outputted batch,
		// where N is the number of workers.
		p.orderEnforcer.Wait()
	}

	p.outgoingTransactionsNode <- txsNode

	logger.Debugf("Constructed dependencies for txs with id %d", id)

	p.orderEnforcer.L.Unlock()
	p.orderEnforcer.Broadcast()
}
