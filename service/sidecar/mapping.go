/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sidecar

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"go.uber.org/zap/zapcore"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/service/verifier/policy"
	"github.com/hyperledger/fabric-x-committer/utils/retry"
	"github.com/hyperledger/fabric-x-committer/utils/serialization"
)

type (
	blockMappingResult struct {
		blockNumber uint64
		block       *servicepb.CoordinatorBatch
		withStatus  *blockWithStatus
		isConfig    bool
		// snapshotTx holds the accepted snapshot TX, kept out of block.Txs (unlike a regular TX) so
		// it can be submitted to the coordinator as its own single-TX segment, separately from and
		// after the block's other TXs. See submitSnapshotBlock in relay.go. A non-nil snapshotTx
		// is the sole signal that the block has an accepted snapshot TX. Only the first snapshot TX
		// in a block is accepted; any further snapshot TXs in the same block are rejected with
		// REJECTED_DUPLICATE_SNAPSHOT_IN_BLOCK.
		snapshotTx *servicepb.TxWithRef
		// dedup is a reference to the relay's in-flight TX ID set, and txIDs collects the IDs this
		// block added to it. Both are only used while constructing this blockMappingResult
		// (mapBlock/addTxIDMapping); nothing reads them back off the struct afterwards, so callers
		// that build further blockMappingResult values (e.g. submitSnapshotBlock's segments) do not
		// need to carry them forward.
		dedup *txIDDedup
		txIDs []string
		// refs and txWithRefs back every TxRef and TxWithRef the block needs, one entry per
		// message, allocated once for the block instead of once per transaction. Mapping is on the
		// path that decides how fast the sidecar allocates, and these were two of its allocations
		// per transaction. Nothing may copy an element out of them — they are proto messages, and
		// only their addresses are ever handed on — which go vet's copylocks check enforces.
		refs       []committerpb.TxRef
		txWithRefs []servicepb.TxWithRef
	}

	// parsedMessage is what mapping can settle about one message of a block without looking at any
	// other: its reference, and either the TX to accept, the status to reject it with, or the error
	// that fails the whole block. parseMessages fills these concurrently and applyParsedMessage
	// folds them into the block one at a time, in order.
	parsedMessage struct {
		ref *committerpb.TxRef
		// tx is the TX to accept, nil unless the message is well formed.
		tx *applicationpb.Tx
		// status and reason are the rejection, set only when tx is nil and err is nil.
		status committerpb.Status
		reason string
		// err fails the whole block rather than the TX. Only an unprocessable config TX sets it.
		err        error
		isConfig   bool
		isSnapshot bool
	}

	blockWithStatus struct {
		block        *common.Block
		txStatus     []committerpb.Status
		pendingCount atomic.Int32

		// Fields for StreamAllTransactions support
		blockNumber uint64                 // Block number
		txs         []*servicepb.TxWithRef // Transaction content (from coordinatorBatch.Txs)
	}
)

const (
	statusNotYetValidated = committerpb.Status_STATUS_UNSPECIFIED
	statusIdx             = int(common.BlockMetadataIndex_TRANSACTIONS_FILTER)

	// minMsgsPerMapWorker is the smallest share of a block worth giving a goroutine of its own.
	// Parsing one message costs on the order of a microsecond, so a smaller share is mapped in
	// place instead: handing it over would cost more than it saves, and a deployment that cuts
	// small blocks has spare cores anyway, since small blocks cap throughput well below the point
	// where parsing is what limits it.
	minMsgsPerMapWorker = 256

	// maxKeysForPairwiseCheck is the largest number of keys in one namespace that checkKeys
	// compares pairwise before it switches to building a set. At this count the comparison is a few
	// hundred short byte-slice compares, well under the cost of the map it replaces.
	maxKeysForPairwiseCheck = 24
)

// mapWorkers bounds how many goroutines one block's parsing is split across. Parsing is CPU bound,
// so more workers than cores cannot help, and the cap leaves cores for the pipeline's other
// stages — the relay's sender and receiver, the block store, and the notifier — which need to keep
// up with mapping for the extra parsing rate to turn into throughput.
var mapWorkers = min(runtime.NumCPU(), 16)

// mapBlock maps an orderer block into the batch the relay submits to the coordinator. It records
// every accepted TX ID in dedup, rejecting a TX whose ID is already in flight, and hands dedup the
// block's IDs so they are released once the block is committed.
func mapBlock(block *common.Block, dedup *txIDDedup) (*blockMappingResult, error) {
	// Prepare block's metadata.
	if block.Metadata == nil {
		block.Metadata = &common.BlockMetadata{}
	}
	metadataSize := len(block.Metadata.Metadata)
	if metadataSize <= statusIdx {
		block.Metadata.Metadata = append(block.Metadata.Metadata, make([][]byte, statusIdx+1-metadataSize)...)
	}

	blockNumber := block.Header.Number

	if block.Data == nil {
		logger.Warnf("Received a block [%d] without data", block.Header.Number)
		return &blockMappingResult{
			blockNumber: blockNumber,
			block:       &servicepb.CoordinatorBatch{},
			withStatus: &blockWithStatus{
				block:       block,
				blockNumber: blockNumber,
			},
			dedup: dedup,
		}, nil
	}

	txCount := len(block.Data.Data)
	mappedBlock := &blockMappingResult{
		blockNumber: blockNumber,
		block: &servicepb.CoordinatorBatch{
			Txs:      make([]*servicepb.TxWithRef, 0, txCount),
			Rejected: make([]*committerpb.TxStatus, 0, txCount),
		},
		withStatus: &blockWithStatus{
			block:       block,
			txStatus:    make([]committerpb.Status, txCount),
			txs:         make([]*servicepb.TxWithRef, txCount),
			blockNumber: blockNumber,
		},
		dedup:      dedup,
		txIDs:      make([]string, 0, txCount),
		refs:       make([]committerpb.TxRef, txCount),
		txWithRefs: make([]servicepb.TxWithRef, txCount),
	}
	mappedBlock.withStatus.pendingCount.Store(int32(txCount)) //nolint:gosec // int -> int32

	parsed := parseMessages(blockNumber, block.Data.Data, mappedBlock.refs)
	for msgIndex := range parsed {
		logger.Debugf("Mapping transaction [blk,tx] = [%d,%d]", blockNumber, msgIndex)
		if err := mappedBlock.applyParsedMessage(&parsed[msgIndex]); err != nil {
			// Either a config TX that cannot be processed (see unprocessableConfigTx),
			// or a bug in the relay.
			return nil, err
		}
	}

	dedup.trackBlock(blockNumber, mappedBlock.txIDs)
	return mappedBlock, nil
}

// parseMessages works out what can be decided about each message on its own — parsing its
// envelope, classifying it, and validating its form — for every message of a block at once.
//
// This is the bulk of mapping, and none of it depends on the other messages, so it is split across
// goroutines: a single goroutine parsing a block caps the whole sidecar at the rate one core can
// parse, whatever else the machine has spare. What is left afterwards does depend on the other
// messages — the TX ID dedup set, the one-snapshot-per-block rule, and the order of the batch the
// coordinator receives — and applyParsedMessage does it serially, in message order, so the
// transactions a block accepts and rejects do not depend on how the parsing happened to be split.
func parseMessages(blockNumber uint64, msgs [][]byte, refs []committerpb.TxRef) []parsedMessage {
	parsed := make([]parsedMessage, len(msgs))
	parseRange := func(start, end int) {
		for msgIndex := start; msgIndex < end; msgIndex++ {
			ref := &refs[msgIndex]
			ref.BlockNum = blockNumber
			ref.TxNum = uint32(msgIndex) //nolint:gosec // int -> uint32.
			parsed[msgIndex] = parseMessage(ref, msgs[msgIndex])
		}
	}

	workers := min(mapWorkers, len(msgs)/minMsgsPerMapWorker)
	if workers < 2 {
		parseRange(0, len(msgs))
		return parsed
	}

	var wg sync.WaitGroup
	for worker := range workers {
		start, end := len(msgs)*worker/workers, len(msgs)*(worker+1)/workers
		wg.Go(func() {
			parseRange(start, end)
		})
	}
	wg.Wait()
	return parsed
}

// parseMessage classifies and validates one message. It reads nothing but the message, and writes
// nothing but its return value, which is what lets parseMessages run it concurrently.
func parseMessage(ref *committerpb.TxRef, msg []byte) parsedMessage {
	parsed := parsedMessage{ref: ref}

	// UnwrapEnvelopeLite extracts only HeaderType, TxID, and Data from the envelope
	// by scanning the protobuf wire format directly. Unlike UnwrapEnvelope, which
	// fully deserializes all nested proto messages and validates every field, this
	// skips unused ChannelHeader fields (version, timestamp, channel_id, epoch,
	// extension, tls_cert_hash) and the Header's signature_header. Corruption in
	// those fields will go undetected. This is acceptable because the committer
	// does not use them, and for the same reason, they are not validated in the
	// sidecar. TODO: remove unused fields from the ChannelHeader proto.
	envLite, envErr := serialization.UnwrapEnvelopeLite(msg)
	if envErr != nil {
		return parsed.reject(committerpb.Status_MALFORMED_BAD_ENVELOPE, envErr.Error())
	}
	headerType := common.HeaderType(envLite.HeaderType)

	// A config TX is classified before its TX ID is resolved: it does not carry its TX ID where
	// every other message type does. See parseConfigTx.
	if headerType == common.HeaderType_CONFIG {
		return parsed.parseConfigTx(envLite, msg)
	}

	if envLite.TxID == "" || !utf8.ValidString(envLite.TxID) {
		return parsed.reject(committerpb.Status_MALFORMED_MISSING_TX_ID, "no TX ID")
	}
	parsed.ref.TxId = envLite.TxID

	if headerType != common.HeaderType_MESSAGE {
		return parsed.reject(committerpb.Status_MALFORMED_UNSUPPORTED_ENVELOPE_PAYLOAD,
			"unsupported message type: "+headerType.String())
	}

	tx, err := serialization.UnmarshalTx(envLite.Data)
	if err != nil {
		return parsed.reject(committerpb.Status_MALFORMED_BAD_ENVELOPE_PAYLOAD, err.Error())
	}
	if status := verifyTxForm(tx); status != statusNotYetValidated {
		return parsed.reject(status, "malformed tx")
	}
	parsed.tx = tx
	parsed.isSnapshot = isSnapshotTx(tx)
	return parsed
}

// parseConfigTx validates a config TX and resolves its TX ID. Unlike a data TX, a config TX cannot
// be rejected: the ordering service has already validated it and applied it to the channel config,
// so a committer that rejects it would diverge from the rest of the network. It is validated here,
// while failing the block is still an option; the verifier and the coordinator parse it again
// later, where a failure could no longer be recovered from.
func (p parsedMessage) parseConfigTx(envLite *serialization.EnvelopeLite, msg []byte) parsedMessage {
	if err := policy.ValidateConfigTx(msg); err != nil {
		return p.failBlock(err)
	}
	txID, err := configTxID(envLite)
	if err != nil {
		return p.failBlock(err)
	}
	p.ref.TxId = txID
	p.tx = configTx(msg)
	p.isConfig = true
	return p
}

// reject marks the message as one to reject with a stored or non-stored status, as its status
// decides. Rejecting is recorded rather than performed here, because whether a status can be
// stored also depends on the TX ID not being a duplicate, which only applyParsedMessage knows.
func (p parsedMessage) reject(status committerpb.Status, reason string) parsedMessage {
	p.status = status
	p.reason = reason
	return p
}

// failBlock marks the message as one that fails the whole block, which only an unprocessable
// config TX does. The error is built in applyParsedMessage, which logs it in message order.
func (p parsedMessage) failBlock(err error) parsedMessage {
	p.err = err
	return p
}

// applyParsedMessage folds one parsed message into the block being mapped: it records the TX ID in
// the dedup set, appends the TX to the batch or its rejection to the rejected list, and fills the
// block's per-TX status. Every step of it depends on the messages before this one, so mapBlock runs
// it serially and in message order.
func (b *blockMappingResult) applyParsedMessage(parsed *parsedMessage) error {
	switch {
	case parsed.err != nil:
		return b.unprocessableConfigTx(parsed.ref, parsed.err)
	case parsed.status != statusNotYetValidated:
		return b.rejectTx(parsed.ref, parsed.status, parsed.reason)
	case parsed.isConfig:
		return b.appendConfigTx(parsed.ref, parsed.tx)
	case !parsed.isSnapshot:
		return b.appendTx(parsed.ref, parsed.tx)
	case b.snapshotTx != nil:
		// Only the first snapshot TX in a block is processed; reject the rest with a
		// stored status so the outcome is recorded, regardless of the first's outcome.
		return b.rejectTx(parsed.ref, committerpb.Status_REJECTED_DUPLICATE_SNAPSHOT_IN_BLOCK,
			"duplicate snapshot tx in block")
	}

	txWithRef, err := b.prepareTx(parsed.ref, parsed.tx)
	if err != nil || txWithRef == nil {
		// A nil TxWithRef means a duplicate TX ID, already rejected by prepareTx.
		return err
	}
	// Kept off block.Txs; see the snapshotTx field comment.
	b.snapshotTx = txWithRef
	return nil
}

// appendConfigTx appends an accepted config TX to the batch and marks the block as a config block.
func (b *blockMappingResult) appendConfigTx(ref *committerpb.TxRef, tx *applicationpb.Tx) error {
	txWithRef, err := b.prepareTx(ref, tx)
	if err != nil {
		return err
	}
	if txWithRef == nil {
		// The TX ID is already in flight, and prepareTx rejected the TX as a duplicate. A config TX
		// cannot be rejected either, so the block fails instead: by the time it is fetched again,
		// the TX that holds the ID has likely been processed and released it.
		return b.unprocessableConfigTx(ref, errors.Newf("duplicate TX ID [%s]", ref.TxId))
	}
	b.isConfig = true
	b.block.Txs = append(b.block.Txs, txWithRef)
	return nil
}

// unprocessableConfigTx fails the block holding a config TX the sidecar cannot process. The
// returned error unwinds the relay, making the sidecar restart its block feed and fetch the
// block again, possibly from another orderer. Since the config TX cannot be rejected, this is the
// only way to recover from a config TX that arrived corrupted. It is retried with a backoff, and
// the sidecar stops once the retry profile is exhausted, as a config TX that is consistently
// unprocessable requires human intervention.
func (b *blockMappingResult) unprocessableConfigTx(ref *committerpb.TxRef, err error) error {
	err = errors.Wrapf(err, "cannot process the config TX [blk:%d,num:%d]", b.blockNumber, ref.TxNum)
	logger.Errorf("%+v", err)
	return errors.Join(retry.ErrBackOff, err)
}

// configTxID returns the TX ID of a config TX: the TX ID of the client's config-update TX, nested
// in ConfigEnvelope.LastUpdate, or the outer envelope's TX ID when the config TX has no nested
// update at all — a bootstrap (genesis) config block, which no client submitted.
//
// The client's TX ID is the only acceptable ID for a config update, and it is the one the client
// waits for a notification on. The outer envelope of a config block that the ordering service
// creates for a config update is generated and signed by the consensus leader, so its TX ID is the
// leader's: a nested update that carries no TX ID, or that cannot be read, makes the config TX
// unprocessable rather than falling back to an ID that is not the client's.
//
// Do not fall back any further by computing a TX ID from an envelope's creator and nonce: the
// ordering service verifies that every TX ID is present and matches its creator and nonce, so a
// config TX that reaches the committer without one is malformed, and the committer must fail it
// rather than invent an ID for it.
func configTxID(envLite *serialization.EnvelopeLite) (string, error) {
	configEnv, err := protoutil.UnmarshalConfigEnvelope(envLite.Data)
	if err != nil {
		return "", errors.Wrap(err, "error unmarshalling config envelope")
	}

	if configEnv.LastUpdate == nil {
		if envLite.TxID == "" {
			return "", errors.New("no TX ID in the config TX")
		}
		return envLite.TxID, nil
	}

	_, channelHdr, err := serialization.ParseEnvelope(configEnv.LastUpdate)
	if err != nil {
		return "", errors.Wrap(err, "error parsing the config update envelope")
	}
	if channelHdr.TxId == "" {
		return "", errors.New("no TX ID in the config update")
	}
	return channelHdr.TxId, nil
}

func (b *blockMappingResult) appendTx(ref *committerpb.TxRef, tx *applicationpb.Tx) error {
	txWithRef, err := b.prepareTx(ref, tx)
	if err != nil || txWithRef == nil {
		return err
	}
	b.block.Txs = append(b.block.Txs, txWithRef)
	return nil
}

// prepareTx runs the shared dedup/creation logic for an accepted TX: it records the TX ID,
// stores the TxWithRef in withStatus.txs (keyed by original position), and logs it. It returns a
// nil TxWithRef (and nil error) when ref.TxId is a duplicate, since addTxIDMapping has already
// rejected it with a stored status. Callers append the returned TxWithRef to block.Txs themselves
// (immediately for appendTx, or deferred to end-of-block for the snapshot TX).
func (b *blockMappingResult) prepareTx(
	ref *committerpb.TxRef, tx *applicationpb.Tx,
) (*servicepb.TxWithRef, error) {
	if idAlreadyExists, err := b.addTxIDMapping(ref); idAlreadyExists || err != nil {
		return nil, err
	}
	txWithRef := &b.txWithRefs[ref.TxNum]
	txWithRef.Ref = ref
	txWithRef.Content = tx
	b.withStatus.txs[ref.TxNum] = txWithRef
	debugTx(ref, "included: %s", ref.TxId)
	return txWithRef, nil
}

func (b *blockMappingResult) rejectTx(ref *committerpb.TxRef, status committerpb.Status, reason string) error {
	if !IsStatusStoredInDB(status) {
		return b.rejectNonDBStatusTx(ref, status, reason)
	}
	if idAlreadyExists, err := b.addTxIDMapping(ref); idAlreadyExists || err != nil {
		return err
	}
	b.block.Rejected = append(b.block.Rejected, &committerpb.TxStatus{Ref: ref, Status: status})
	b.txWithRefs[ref.TxNum].Ref = ref
	b.withStatus.txs[ref.TxNum] = &b.txWithRefs[ref.TxNum]
	debugTx(ref, "rejected: %s (%s)", &status, reason)
	return nil
}

// rejectNonDBStatusTx is used to reject with statuses that are not stored in the state DB.
// Namely, statuses for cases where we don't have a TX ID, or there is a TX ID duplication.
// For such cases, no notification will be given by the notification service.
func (b *blockMappingResult) rejectNonDBStatusTx(
	ref *committerpb.TxRef, status committerpb.Status, reason string,
) error {
	if IsStatusStoredInDB(status) {
		// This can never occur unless there is a bug in the relay.
		return errors.Newf("[BUG] status should be stored [blk:%d,num:%d]: %s", b.blockNumber, ref.TxNum, &status)
	}
	err := b.withStatus.setFinalStatus(ref.TxNum, status)
	if err != nil {
		return err
	}
	b.txWithRefs[ref.TxNum].Ref = ref
	b.withStatus.txs[ref.TxNum] = &b.txWithRefs[ref.TxNum]
	debugTx(ref, "excluded: %s (%s)", &status, reason)
	return nil
}

func (b *blockMappingResult) addTxIDMapping(ref *committerpb.TxRef) (
	idAlreadyExists bool, err error,
) {
	if b.dedup.add(ref.TxId) {
		b.txIDs = append(b.txIDs, ref.TxId)
		return false, nil
	}
	return true, b.rejectNonDBStatusTx(ref, committerpb.Status_REJECTED_DUPLICATE_TX_ID, "duplicate tx")
}

// holds reports whether ref refers to a transaction of this block: the block must carry that TX ID
// at that position. mapBlock fills txs for every position of the block, including the transactions
// it rejects itself, so a ref that does not match belongs to a submission the relay no longer
// tracks — see processStatusBatch.
func (b *blockWithStatus) holds(ref *committerpb.TxRef) bool {
	return int(ref.TxNum) < len(b.txs) && b.txs[ref.TxNum].Ref.TxId == ref.TxId
}

func (b *blockWithStatus) setFinalStatus(txNum uint32, status committerpb.Status) error {
	if b.txStatus[txNum] != statusNotYetValidated {
		// This can never occur unless there is a bug in the relay or the coordinator.
		return errors.Newf("two results for a TX [blockNum: %d, txNum: %d]", b.block.Header.Number, txNum)
	}
	b.txStatus[txNum] = status
	b.pendingCount.Add(-1)
	return nil
}

func (b *blockWithStatus) setStatusMetadataInBlock() {
	statusMetadata := make([]byte, len(b.txStatus))
	for i, s := range b.txStatus {
		statusMetadata[i] = byte(s)
	}
	b.block.Metadata.Metadata[statusIdx] = statusMetadata
}

// IsStatusStoredInDB returns true if the given status code can be stored in the state DB.
func IsStatusStoredInDB(status committerpb.Status) bool {
	switch status {
	case committerpb.Status_MALFORMED_BAD_ENVELOPE,
		committerpb.Status_MALFORMED_MISSING_TX_ID,
		committerpb.Status_REJECTED_DUPLICATE_TX_ID:
		return false
	default:
		return true
	}
}

func debugTx(ref *committerpb.TxRef, format string, a ...any) {
	if !logger.IsEnabledFor(zapcore.DebugLevel) {
		return
	}
	txID := "<no-id>"
	if ref.TxId != "" {
		txID = ref.TxId
	}
	logger.Debugf("ID [%s]: %s", txID, fmt.Sprintf(format, a...))
}

func configTx(value []byte) *applicationpb.Tx {
	return &applicationpb.Tx{
		Namespaces: []*applicationpb.TxNamespace{{
			NsId:      committerpb.ConfigNamespaceID,
			NsVersion: 0,
			BlindWrites: []*applicationpb.Write{{
				Key:   []byte(committerpb.ConfigKey),
				Value: value,
			}},
		}},
	}
}

// verifyTxForm verifies that a TX is not malformed.
// It returns status MALFORMED_<reason> if it is malformed, or not-validated otherwise.
func verifyTxForm(tx *applicationpb.Tx) committerpb.Status {
	if len(tx.Namespaces) == 0 {
		return committerpb.Status_MALFORMED_EMPTY_NAMESPACES
	}
	if status := checkEndorsements(tx); status != statusNotYetValidated {
		return status
	}
	if status := checkStandaloneSystemTx(tx); status != statusNotYetValidated {
		return status
	}

	nsIDs := make(map[string]any, len(tx.Namespaces))
	for _, ns := range tx.Namespaces {
		// Checks that the application does not submit a config TX.
		if ns.NsId == committerpb.ConfigNamespaceID {
			return committerpb.Status_MALFORMED_NAMESPACE_ID_INVALID
		}
		if !committerpb.IsSystemNamespace(ns.NsId) && policy.ValidateNamespaceID(ns.NsId) != nil {
			return committerpb.Status_MALFORMED_NAMESPACE_ID_INVALID
		}
		if _, ok := nsIDs[ns.NsId]; ok {
			return committerpb.Status_MALFORMED_DUPLICATE_NAMESPACE
		}

		for _, check := range []func(ns *applicationpb.TxNamespace) committerpb.Status{
			checkNamespaceReadsWrites, checkSystemNamespace,
		} {
			if status := check(ns); status != statusNotYetValidated {
				return status
			}
		}
		nsIDs[ns.NsId] = nil
	}
	return statusNotYetValidated
}

func checkEndorsements(tx *applicationpb.Tx) committerpb.Status {
	if len(tx.Namespaces) != len(tx.Endorsements) {
		return committerpb.Status_MALFORMED_MISSING_SIGNATURE
	}
	for _, e := range tx.Endorsements {
		if e == nil || len(e.EndorsementsWithIdentity) == 0 {
			return committerpb.Status_MALFORMED_MISSING_SIGNATURE
		}
		for _, ei := range e.EndorsementsWithIdentity {
			if ei == nil || len(ei.Endorsement) == 0 {
				return committerpb.Status_MALFORMED_MISSING_SIGNATURE
			}
		}
		// Note: we do not validate the Identity field here because the sidecar does not know
		// whether the namespace uses an MSP rule or a threshold rule for endorsement.
		// Threshold rules do not require an identity. Identity validation is left to the
		// signature verifier, which has the policy context to determine what is required.
	}
	return statusNotYetValidated
}

func isSnapshotTx(tx *applicationpb.Tx) bool {
	return len(tx.Namespaces) == 1 && tx.Namespaces[0].NsId == committerpb.SnapshotNamespaceID
}

func checkStandaloneSystemTx(tx *applicationpb.Tx) committerpb.Status {
	for _, ns := range tx.Namespaces {
		if ns.NsId != committerpb.SnapshotNamespaceID && ns.NsId != committerpb.CheckpointNamespaceID {
			continue
		}
		if len(tx.Namespaces) == 1 {
			continue
		}
		// System TX must be standalone; not namespace ID/snapshot marker/checkpoint key error.
		return committerpb.Status_MALFORMED_SYSTEM_TX_NOT_STANDALONE
	}
	return statusNotYetValidated
}

func checkSystemNamespace(ns *applicationpb.TxNamespace) committerpb.Status {
	switch ns.NsId {
	case committerpb.MetaNamespaceID:
		return checkMetaNamespace(ns)
	case committerpb.SnapshotNamespaceID:
		if len(ns.ReadsOnly) > 0 || len(ns.ReadWrites) > 0 || len(ns.BlindWrites) > 0 {
			return committerpb.Status_MALFORMED_SNAPSHOT_NOT_MARKER_ONLY
		}
	case committerpb.CheckpointNamespaceID:
		if len(ns.ReadsOnly) > 0 || len(ns.BlindWrites) > 0 || len(ns.ReadWrites) != 1 {
			return committerpb.Status_MALFORMED_CHECKPOINT_INVALID_KEY
		}
		_, n, err := servicepb.NewHeightFromBytes(ns.ReadWrites[0].Key)
		if err != nil || n != len(ns.ReadWrites[0].Key) {
			return committerpb.Status_MALFORMED_CHECKPOINT_INVALID_KEY
		}
	default:
		return statusNotYetValidated
	}
	return statusNotYetValidated
}

// checkNamespaceReadsWrites validates the reads/writes shape shared by user and system
// namespaces: it rejects a namespace with no writes (except the marker-only `_snapshot` and
// `_checkpoint` system namespaces, whose write requirements are checked in checkSystemNamespace)
// and validates all keys. It runs for every namespace in the transaction.
func checkNamespaceReadsWrites(ns *applicationpb.TxNamespace) committerpb.Status {
	if len(ns.ReadWrites) == 0 && len(ns.BlindWrites) == 0 &&
		ns.NsId != committerpb.SnapshotNamespaceID && ns.NsId != committerpb.CheckpointNamespaceID {
		return committerpb.Status_MALFORMED_NO_WRITES
	}
	return checkKeys(ns)
}

func checkMetaNamespace(txNs *applicationpb.TxNamespace) committerpb.Status {
	if txNs.NsId != committerpb.MetaNamespaceID {
		return statusNotYetValidated
	}
	if len(txNs.BlindWrites) > 0 {
		return committerpb.Status_MALFORMED_BLIND_WRITES_NOT_ALLOWED
	}

	nsUpdate := make(map[string]any)
	u := policy.GetUpdatesFromNamespace(txNs)
	if u == nil {
		return statusNotYetValidated
	}
	for _, pd := range u.NamespacePolicies.Policies {
		// The identity deserializer is not needed because it is only
		// used when evaluating signatures. Since this policy is created
		// only to validate its form, we can skip the deserializer.
		_, err := policy.CreateNamespaceVerifier(pd, nil)
		if err != nil {
			if errors.Is(err, policy.ErrInvalidNamespaceID) {
				return committerpb.Status_MALFORMED_NAMESPACE_ID_INVALID
			}
			return committerpb.Status_MALFORMED_NAMESPACE_POLICY_INVALID
		}
		if pd.Namespace == committerpb.MetaNamespaceID {
			return committerpb.Status_MALFORMED_NAMESPACE_POLICY_INVALID
		}
		if _, ok := nsUpdate[pd.Namespace]; ok {
			return committerpb.Status_MALFORMED_NAMESPACE_POLICY_INVALID
		}
		nsUpdate[pd.Namespace] = nil
	}
	return statusNotYetValidated
}

// checkKeys verifies that a namespace has no empty key and no duplicate key.
//
// Duplicates are found by comparing the keys pairwise rather than by collecting them into a set.
// The set cost two allocations per namespace — the map, and the slice the keys were first copied
// into — which measured 2.8 allocations per transaction, on a path where the sidecar is bound by
// how fast it allocates rather than by CPU. Comparing pairwise costs nothing and is faster for the
// handful of keys a transaction carries, but it is quadratic, so a namespace with more keys than
// maxKeysForPairwiseCheck still builds a set.
func checkKeys(ns *applicationpb.TxNamespace) committerpb.Status {
	keyCount := len(ns.ReadsOnly) + len(ns.ReadWrites) + len(ns.BlindWrites)
	for i := range keyCount {
		if len(nsKey(ns, i)) == 0 {
			return committerpb.Status_MALFORMED_EMPTY_KEY
		}
	}

	if keyCount > maxKeysForPairwiseCheck {
		uniqueKeys := make(map[string]any, keyCount)
		for i := range keyCount {
			uniqueKeys[string(nsKey(ns, i))] = nil
		}
		if len(uniqueKeys) != keyCount {
			return committerpb.Status_MALFORMED_DUPLICATE_KEY_IN_READ_WRITE_SET
		}
		return statusNotYetValidated
	}

	for i := range keyCount {
		for j := i + 1; j < keyCount; j++ {
			if bytes.Equal(nsKey(ns, i), nsKey(ns, j)) {
				return committerpb.Status_MALFORMED_DUPLICATE_KEY_IN_READ_WRITE_SET
			}
		}
	}
	return statusNotYetValidated
}

// nsKey returns the i-th key of a namespace, counting the reads-only keys first, then the
// read-writes, then the blind writes. It lets checkKeys walk every key of a namespace without
// first copying them into one slice, which was an allocation per namespace.
func nsKey(ns *applicationpb.TxNamespace, i int) []byte {
	if i < len(ns.ReadsOnly) {
		return ns.ReadsOnly[i].Key
	}
	i -= len(ns.ReadsOnly)
	if i < len(ns.ReadWrites) {
		return ns.ReadWrites[i].Key
	}
	return ns.BlindWrites[i-len(ns.ReadWrites)].Key
}
