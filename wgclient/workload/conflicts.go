package workload

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	sigverificationtest "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type signerFunc func([]token.SerialNumber, []token.TxOutput) ([]byte, error)

type statisticalConflictHandler struct {
	delegate                  sigverificationtest.TxGenerator
	invalidSignatureGenerator *test.BooleanGenerator
	doubleSpendGenerator      *test.BooleanGenerator
	doubleSpendTx             token.Tx
	signFnc                   signerFunc
}

func newStatisticalConflicts(txGenerator sigverificationtest.TxGenerator, profile *StatisticalConflicts, signerFunc signerFunc) sigverificationtest.TxGenerator {
	doubleSpendTx := txGenerator.Next().Tx
	return &statisticalConflictHandler{
		delegate:                  txGenerator,
		invalidSignatureGenerator: test.NewBooleanGenerator(test.PercentageUniformDistribution, profile.InvalidSignatures, 100),
		doubleSpendGenerator:      test.NewBooleanGenerator(test.PercentageUniformDistribution, profile.DoubleSpends, 101),
		signFnc:                   signerFunc,
		doubleSpendTx:             *doubleSpendTx,
	}
}

func (h *statisticalConflictHandler) Next() *sigverificationtest.TxWithStatus {
	if h.doubleSpendGenerator.Next() {
		// Copy SNs and signatures (we avoid to calculate the signature again, because it slows down the generator significantly)
		return &sigverificationtest.TxWithStatus{
			Tx: &token.Tx{
				SerialNumbers: h.doubleSpendTx.SerialNumbers,
				Outputs:       h.doubleSpendTx.Outputs,
				Signature:     h.doubleSpendTx.Signature,
			},
			Status: coordinatorservice.Status_DOUBLE_SPEND,
		}
	}

	if h.invalidSignatureGenerator.Next() {
		tx := h.delegate.Next().Tx
		sigverificationtest.Reverse(tx.Signature)
		return &sigverificationtest.TxWithStatus{tx, coordinatorservice.Status_INVALID_SIGNATURE}
	}

	return h.delegate.Next()
}

func NewConflictDecorator(txGenerator sigverificationtest.TxGenerator, conflicts *ConflictProfile, signerFunc signerFunc) sigverificationtest.TxGenerator {
	if conflicts == nil || conflicts.Scenario == nil && conflicts.Statistical == nil {
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
	doubles       map[TxAbsoluteOrder]map[SNRelativeOrder]*token.SerialNumber
	signFnc       signerFunc
}

const separator = "-"

func newScenarioConflicts(txGenerator sigverificationtest.TxGenerator, pp *ScenarioConflicts, signerFnc signerFunc) sigverificationtest.TxGenerator {
	c := &scenarioHandler{
		delegate:      txGenerator,
		isSigConflict: make(map[TxAbsoluteOrder]bool),                                    // txid to invalid sig
		doubles:       make(map[TxAbsoluteOrder]map[SNRelativeOrder]*token.SerialNumber), // snid to bytes
		signFnc:       signerFnc,
	}

	for _, txOrder := range pp.InvalidSignatures {
		c.isSigConflict[txOrder] = true
	}
	for _, doubleSpendGroup := range pp.DoubleSpends {
		sn := &txGenerator.Next().Tx.SerialNumbers[0]
		for _, txSnOrder := range doubleSpendGroup {
			parts := strings.Split(txSnOrder, separator)
			if len(parts) != 2 {
				panic("invalid conflict format")
			}
			txOrder, _ := strconv.Atoi(parts[0])
			snOrder, _ := strconv.Atoi(parts[1])
			txDoubles, ok := c.doubles[uint64(txOrder)]
			if !ok {
				txDoubles = make(map[SNRelativeOrder]*token.SerialNumber)
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
		sigverificationtest.Reverse(tx.Signature)
		return &sigverificationtest.TxWithStatus{tx, coordinatorservice.Status_INVALID_SIGNATURE}
	}
	if txDoubles, ok := h.doubles[order]; ok {
		for snOrder, sn := range txDoubles {
			if snOrder < uint32(len(tx.SerialNumbers)) {
				tx.SerialNumbers[snOrder] = *sn
			}
		}
		tx.Signature, _ = h.signFnc(tx.SerialNumbers, tx.Outputs)
		return &sigverificationtest.TxWithStatus{tx, coordinatorservice.Status_DOUBLE_SPEND}
	}

	return &sigverificationtest.TxWithStatus{tx, coordinatorservice.Status_VALID}
}
