package workload

import (
	"fmt"
	"strconv"
	"strings"

	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	sigverificationtest "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

type SignerFunc func([][]byte) ([]byte, error)

type ConflictHandler interface {
	// ApplyConflicts modifies tx if a conflict is specified within the configHelper.
	// If a conflict is applied, the corresponding status is returned; otherwise it returns valid status
	ApplyConflicts(txId token.TxSeqNum, tx *token.Tx) coordinatorservice.Status
}

type statisticalConflictHandler struct {
	invalidSignatureGenerator *test.BooleanGenerator
	doubleSpendGenerator      *test.BooleanGenerator
	doubleSpendTx             *slidingSlice
	signFnc                   SignerFunc
}

func NewStatisticalConflicts(profile *StatisticalConflicts, signerFunc SignerFunc) ConflictHandler {
	return &statisticalConflictHandler{
		invalidSignatureGenerator: test.NewBooleanGenerator(test.PercentageUniformDistribution, profile.InvalidSignature, 100),
		doubleSpendGenerator:      test.NewBooleanGenerator(test.PercentageUniformDistribution, profile.DoubleSpends, 101),
		signFnc:                   signerFunc,
		doubleSpendTx:             NewSlidingSlice(100),
	}
}

type slidingSlice struct {
	items      []token.Tx
	start, end int
}

func NewSlidingSlice(length int) *slidingSlice {
	return &slidingSlice{items: make([]token.Tx, length)}
}

func (s *slidingSlice) Push(item token.Tx) {
	s.items[s.end%len(s.items)] = item
	s.end++
}
func (s *slidingSlice) Pop() token.Tx {
	item := s.items[s.start%len(s.items)]
	s.start++
	return item
}
func (s *slidingSlice) IsEmpty() bool {
	return s.start == s.end
}
func (s *slidingSlice) IsFull() bool {
	return s.start == s.end-len(s.items)
}

func (h *statisticalConflictHandler) ApplyConflicts(_ token.TxSeqNum, tx *token.Tx) coordinatorservice.Status {
	if h.invalidSignatureGenerator.Next() {
		sigverificationtest.Reverse(tx.Signature)
		return coordinatorservice.Status_INVALID_SIGNATURE
	}

	if !h.doubleSpendTx.IsEmpty() && h.doubleSpendGenerator.Next() {
		// Copy SNs and signatures (we avoid to calculate the signature again, because it slows down the generator significantly)
		doubleSpentTx := h.doubleSpendTx.Pop()
		tx.SerialNumbers = doubleSpentTx.GetSerialNumbers()
		tx.Signature = doubleSpentTx.GetSignature()
		return coordinatorservice.Status_DOUBLE_SPEND
	}

	// Use later for double spends
	if !h.doubleSpendTx.IsFull() {
		h.doubleSpendTx.Push(*tx)
	}

	return coordinatorservice.Status_VALID
}

type noConflictHandler struct{}

func NewNoConflicts() ConflictHandler {
	return &noConflictHandler{}
}

func (h *noConflictHandler) ApplyConflicts(token.TxSeqNum, *token.Tx) coordinatorservice.Status {
	// let's do nothing if there are no conflicts specified for this run
	return coordinatorservice.Status_VALID
}

type scenarioHandler struct {
	isSigConflict     map[string]bool
	hasDoubleConflict map[string][]int
	doublesMapping    map[string]string
	doubles           map[string][]byte
	signFnc           SignerFunc
}

func NewConflictHandler(scenario *ScenarioConflicts, statistical *StatisticalConflicts, signerFunc SignerFunc) ConflictHandler {
	if scenario == nil && statistical == nil {
		return NewNoConflicts()
	}
	if scenario != nil && statistical != nil {
		panic("only one type supported")
	}
	if scenario != nil {
		return NewScenarioConflicts(scenario, signerFunc)
	}
	return NewStatisticalConflicts(statistical, signerFunc)
}

func NewScenarioConflicts(pp *ScenarioConflicts, signerFnc SignerFunc) ConflictHandler {

	// TODO let's think about a different construction of this conflict mapper
	// as Alex suggested we could use `map[BlkNum]map[TxNum]map[SnNum]SerialNumber`

	c := &scenarioHandler{
		// TODO find better name for these helpers
		isSigConflict:     make(map[string]bool),   // txid to invalid sig
		hasDoubleConflict: make(map[string][]int),  // txid to list of sn positions
		doublesMapping:    make(map[string]string), // txid to snid
		doubles:           make(map[string][]byte), // snid to bytes
		signFnc:           signerFnc,
	}

	for txid, o := range *pp {
		if o.InvalidSignature {
			c.isSigConflict[txid] = true
		}

		if len(o.DoubleSpends) == 0 {
			continue
		}

		// current txid

		var sns []int
		for sn, targetSnid := range o.DoubleSpends {
			snid := fmt.Sprintf(snFormatter, txid, sn)
			// sanity check targetSnid < snid
			if !inThePast(snid, targetSnid) {
				panic("INVALID Your conflict does not make sense. " + snid + " references " + targetSnid)
			}

			sns = append(sns, sn)
			c.doubles[targetSnid] = nil
			c.doublesMapping[snid] = targetSnid
		}
		c.hasDoubleConflict[txid] = sns
	}

	return c
}

// inThePast checks that transaction a is before transaction a.
// Returns true if b < a; otherwise false
func inThePast(a, b string) bool {
	aaa := strings.Split(a, "-")
	bbb := strings.Split(b, "-")

	if len(aaa) != len(bbb) && len(aaa) != 3 {
		panic("inThePast inputs are bad! a: " + a + " b: " + b)
	}

	toNum := func(s string) int64 {
		num, err := strconv.ParseInt(s, 10, 64)
		utils.Must(err)
		return num
	}

	aBlNum, bBlNum := toNum(aaa[0]), toNum(bbb[0])
	aTxNum, bTxNum := toNum(aaa[1]), toNum(bbb[1])
	// note that we don't care about the sn here

	return bBlNum <= aBlNum && bTxNum < aTxNum
}

func (h *scenarioHandler) ApplyConflicts(txId token.TxSeqNum, tx *token.Tx) coordinatorservice.Status {
	txid := fmt.Sprintf("%d-%d", txId.BlkNum, txId.TxNum)

	// collect sn as double candidate as we need to use it later
	for i, sn := range tx.SerialNumbers {
		// store the sn for later if we need it
		snid := fmt.Sprintf(snFormatter, txid, i)
		if _, ok := h.doubles[snid]; ok {
			h.doubles[snid] = sn
		}
	}

	// note that we currently create conflicts which are either INVALID_SIGNATURE or DOUBLE_SPEND

	// apply signature conflicts
	if _, ok := h.isSigConflict[txid]; ok {
		sigverificationtest.Reverse(tx.Signature)
		return coordinatorservice.Status_INVALID_SIGNATURE
	}

	// apply double spends
	if sns, ok := h.hasDoubleConflict[txid]; ok {
		// replace
		for _, j := range sns {
			snid := fmt.Sprintf(snFormatter, txid, j)
			targetSnid, ok := h.doublesMapping[snid]
			if !ok {
				panic("mama mia! cannot apply conflict as we have not seen" + targetSnid)
			}

			// lookup
			newSn, ok := h.doubles[targetSnid]
			if !ok {
				panic("ohhh gosh: " + snid + " cannot find")
			}

			// replace
			tx.SerialNumbers[j-1] = newSn
		}
		// re-sign
		var err error
		tx.Signature, err = h.signFnc(tx.SerialNumbers)
		utils.Must(err)

		return coordinatorservice.Status_DOUBLE_SPEND
	}

	return coordinatorservice.Status_VALID
}
