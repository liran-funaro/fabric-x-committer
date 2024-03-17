package workload

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	sigverificationtest "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	tokenutil "github.ibm.com/decentralized-trust-research/scalable-committer/utils/token"
)

type signerFunc func(tx *protoblocktx.Tx, nsIndex int) ([]byte, error)

const defaultDoubleSpendPoolSize = 1000

type statisticalConflictHandler struct {
	delegate                  sigverificationtest.TxGenerator
	invalidSignatureGenerator *test.BooleanGenerator
	doubleSpendGenerator      *test.BooleanGenerator
	doubleSpendTxs            []*protoblocktx.Tx
	signFnc                   signerFunc
}

func newStatisticalConflicts(txGenerator sigverificationtest.TxGenerator, profile *StatisticalConflicts, signerFunc signerFunc) sigverificationtest.TxGenerator {
	poolSize := profile.DoubleSpendPoolSize
	if poolSize <= 0 {
		poolSize = defaultDoubleSpendPoolSize
	}
	doubleSpendTx := make([]*protoblocktx.Tx, poolSize)
	for i := 0; i < poolSize; i++ {
		doubleSpendTx[i] = txGenerator.Next().Tx
	}
	return &statisticalConflictHandler{
		delegate:                  txGenerator,
		invalidSignatureGenerator: test.NewBooleanGenerator(test.PercentageUniformDistribution, profile.InvalidSignatures, 100),
		doubleSpendGenerator:      test.NewBooleanGenerator(test.PercentageUniformDistribution, profile.DoubleSpends, 101),
		signFnc:                   signerFunc,
		doubleSpendTxs:            doubleSpendTx,
	}
}

func (h *statisticalConflictHandler) Next() *sigverificationtest.TxWithStatus {
	if h.doubleSpendGenerator.Next() {
		tx := h.doubleSpendTxs[rand.Intn(len(h.doubleSpendTxs))]
		// Copy SNs and signatures (we avoid to calculate the signature again, because it slows down the generator significantly)
		return &sigverificationtest.TxWithStatus{
			Tx:     tx,
			Status: protoblocktx.Status_ABORTED_DUPLICATE_TXID,
		}
	}

	if h.invalidSignatureGenerator.Next() {
		tx := h.delegate.Next().Tx
		for _, s := range tx.Signatures {
			sigverificationtest.Reverse(s)
		}
		return &sigverificationtest.TxWithStatus{Tx: tx, Status: protoblocktx.Status_ABORTED_SIGNATURE_INVALID}
	}

	return h.delegate.Next()
}

func NewConflictDecorator(txGenerator sigverificationtest.TxGenerator, conflicts *ConflictProfile, signerFunc signerFunc) sigverificationtest.TxGenerator {
	if conflicts == nil || conflicts.Scenario == nil && conflicts.Statistical == nil {
		fmt.Printf("pass through\n")
		return txGenerator
	}
	if conflicts.Scenario != nil && conflicts.Statistical != nil {
		panic("only one type supported")
	}
	if conflicts.Scenario != nil {
		return newScenarioConflicts(txGenerator, conflicts.Scenario, signerFunc)
	}
	return newStatisticalConflicts(txGenerator, conflicts.Statistical, signerFunc)
}

type scenarioHandler struct {
	counter       TxAbsoluteOrder
	delegate      sigverificationtest.TxGenerator
	isSigConflict map[TxAbsoluteOrder]bool
	doubles       map[TxAbsoluteOrder]map[SNRelativeOrder]*tokenutil.SerialNumber
	signFnc       signerFunc
}

const separator = "-"

func newScenarioConflicts(txGenerator sigverificationtest.TxGenerator, pp *ScenarioConflicts, signerFnc signerFunc) sigverificationtest.TxGenerator {
	c := &scenarioHandler{
		delegate:      txGenerator,
		isSigConflict: make(map[TxAbsoluteOrder]bool),                                        // txid to invalid sig
		doubles:       make(map[TxAbsoluteOrder]map[SNRelativeOrder]*tokenutil.SerialNumber), // snid to bytes
		signFnc:       signerFnc,
	}

	for _, txOrder := range pp.InvalidSignatures {
		c.isSigConflict[txOrder] = true
	}
	for _, doubleSpendGroup := range pp.DoubleSpends {
		sn := &txGenerator.Next().Tx.Namespaces[0].BlindWrites[0].Key
		for _, txSnOrder := range doubleSpendGroup {
			parts := strings.Split(txSnOrder, separator)
			if len(parts) != 2 {
				panic("invalid conflict format")
			}
			txOrder, _ := strconv.Atoi(parts[0])
			snOrder, _ := strconv.Atoi(parts[1])
			txDoubles, ok := c.doubles[uint64(txOrder)]
			if !ok {
				txDoubles = make(map[SNRelativeOrder]*tokenutil.SerialNumber)
				c.doubles[uint64(txOrder)] = txDoubles
			}
			txDoubles[uint32(snOrder)] = sn
		}
	}

	return c
}

func (h *scenarioHandler) Next() *sigverificationtest.TxWithStatus {
	order := atomic.AddUint64(&h.counter, 1)

	tx := h.delegate.Next().Tx
	if h.isSigConflict[order] {
		for _, s := range tx.Signatures {
			sigverificationtest.Reverse(s)
		}
		return &sigverificationtest.TxWithStatus{Tx: tx, Status: protoblocktx.Status_ABORTED_SIGNATURE_INVALID}
	}
	if txDoubles, ok := h.doubles[order]; ok {
		for snOrder, sn := range txDoubles {
			if snOrder < uint32(len(tx.Namespaces[0].BlindWrites)) {
				tx.Namespaces[0].BlindWrites[snOrder].Key = *sn
			}
		}
		s, _ := h.signFnc(tx, 0)
		tx.Signatures = append(tx.Signatures, s)
		return &sigverificationtest.TxWithStatus{Tx: tx, Status: protoblocktx.Status_ABORTED_MVCC_CONFLICT}
	}

	return &sigverificationtest.TxWithStatus{Tx: tx, Status: protoblocktx.Status_COMMITTED}
}
