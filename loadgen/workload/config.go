/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package workload

import (
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	commontypes "github.com/hyperledger/fabric-x-common/api/types"

	"github.com/hyperledger/fabric-x-committer/utils/ordererdial"
	"github.com/hyperledger/fabric-x-committer/utils/signature"
)

// Defines Policy.Scheme.
const (
	PolicySchemeMSP         = "MSP"
	PolicySchemeDefault     = PolicySchemeMSP
	PolicySchemeUnspecified = ""
)

// Probability is a float in the closed interval [0,1].
type Probability = float64

// Profile describes the generated workload characteristics.
// It only contains parameters that deterministically affect the
// generated items.
// The items order, however, might be affected by other parameters.
type Profile struct {
	Block       BlockProfile       `mapstructure:"block" yaml:"block"`
	Transaction TransactionProfile `mapstructure:"transaction" yaml:"transaction"`
	Policy      PolicyProfile      `mapstructure:"policy" yaml:"policy"`

	// Seed is the single PRF root for the whole workload. Every generated item (keys, values, nonce,
	// metadata, and the new-vs-existing layout) is a pure function of this seed and the item's global
	// transaction index, so the same Seed reproduces the same items.
	Seed int64 `mapstructure:"seed" yaml:"seed"`

	// Workers is the number of parallel producers. They share one global transaction-index counter and
	// the same Seed, so the multiset of generated transactions is independent of the worker count —
	// workers are pure parallelism. The count therefore does not need to be preserved between runs to
	// reproduce items (unlike the previous per-worker-seed scheme).
	Workers uint32 `mapstructure:"workers" yaml:"workers"`
}

// BlockProfile describes generate block characteristics
// (when applying load to the VC or Verifier, blocks are translated to batches).
// The generated block size is aimed to be MaxSize, if the generated
// TXs rate is sufficient.
// If the generated TXs rate is too low, the block size might
// be less than MaxSize, but at least MinSize.
// In such case, the block is generated at a preferred rate of PreferredRate.
// Blocks wait up to PreferredRate (default: 1 second) before submission.
// If a full block is not ready by then, a partial block is
// submitted if it meets MinSize (default: 1);
// otherwise, the system waits until MinSize is available.
// If the MaxSize is less than or equal to MinSize, PreferredRate is ignored.
type BlockProfile struct {
	MaxSize       uint64        `mapstructure:"max-size" yaml:"max-size"`
	MinSize       uint64        `mapstructure:"min-size" yaml:"min-size"`
	PreferredRate time.Duration `mapstructure:"preferred-rate" yaml:"preferred-rate"`
}

// TransactionProfile describes generate TX characteristics.
type TransactionProfile struct {
	// The byte sizes of the generated key/values/metadata (size=0 => nil), ordered key, value, metadata.
	KeySize             uint32 `mapstructure:"key-size" yaml:"key-size"`
	ReadWriteValueSize  uint32 `mapstructure:"read-write-value-size" yaml:"read-write-value-size"`
	BlindWriteValueSize uint32 `mapstructure:"blind-write-value-size" yaml:"blind-write-value-size"`
	MetadataSize        uint32 `mapstructure:"metadata-size" yaml:"metadata-size"`

	// The number of keys to generate (read ver=nil)
	ReadOnlyCount uint32 `mapstructure:"read-only-count" yaml:"read-only-count"`
	// The number of keys to generate (read ver=nil/write)
	ReadWriteCount uint32 `mapstructure:"read-write-count" yaml:"read-write-count"`
	// The number of keys to generate (write)
	BlindWriteCount uint32 `mapstructure:"write-count" yaml:"write-count"`

	// By DEFAULT (new-keys-rate UNSET) every slot gets a fresh, globally-unique key: reads carry nil
	// versions, nothing is reused, and there is no contention. SETTING
	// new-keys-rate (to any value >= 0, including 0) switches on the split, where keys are REUSED across
	// transactions, producing the commit-time contention the coordinator must order.
	//
	// Reads and writes share ONE key space, and a key is only ever CREATED by a WRITE slot:
	//   - A READ-WRITE slot that targets a not-yet-created key is the canonical CHECKED INSERT: its
	//     nil-version read asserts the key does not exist, then the write creates it. A conflict here (a
	//     concurrent creator) is measured contention, but may waste a key - taken out of the queue but never written.
	//   - A READ-ONLY slot NEVER creates. Pointing a read-only slot at a not-yet-created key would "waste"
	//     it — a key index nothing ever creates, read forever as nil. So every read-only slot references
	//     an ALREADY-CREATED key.
	//
	// We do NOT explicitly synthesize forward references (a read of a key a later transaction creates). The orderer
	// may commit transactions out of order, and the parallel workers submit batches in arbitrary order,
	// so backward references near the frontier are reordered into forward references: a backward
	// window at transaction-distance `gap` is statistically identical to looking FORWARD AND BACKWARD at
	// gap/2 around a fuzzy commit frontier. One counter — the committable frontier — drives everything.

	// NewKeysRate is OPTIONAL: nil (unset) uses a fresh-key workload; a non-nil value
	// enables the split. It is the average number of NEW committable keys created per transaction by the
	// write slots (read-write + blind-write); the committable frontier grows as C(j) = floor(j * rate)
	// (flooring keeps the per-transaction create count deterministic). Per transaction the first
	// C(j+1)-C(j) write slots CREATE (read-write slots first — checked inserts — then blind writes), and
	// every remaining write slot and every read-only slot REFERENCES an already-created key. A value of 0
	// means NO creates at all: every slot references the static pre-genesis (negative) working set. A
	// positive value must not exceed the write-slot count (read-write-count + write-count).
	NewKeysRate *float64 `mapstructure:"new-keys-rate" yaml:"new-keys-rate"`
	// ReferenceGap is the MINIMUM transaction distance behind the current transaction from which existing
	// references are drawn: at transaction j the reference window ends at the committable frontier as of
	// j-gap, i.e. top = C(max(0, j-gap)). 0 (default) references the newest keys (in-flight, highest
	// contention); larger values reference older, already-committed keys. Expressed in TRANSACTIONS (a
	// distance), not keys. Ignored when new-keys-rate is unset.
	ReferenceGap uint64 `mapstructure:"reference-gap" yaml:"reference-gap"`
	// LookbackWindow is the size, IN KEYS, of the moving working set an existing reference is drawn from:
	// a reference lands in [top-W, top), spread across the window by a per-transaction random offset so
	// references are not all pinned to a single key. The window slides forward as the frontier grows, so
	// a larger window spreads references over more keys (less contention) and a smaller one concentrates
	// them (more contention). Required to be at least the total slot count (read-only + read-write +
	// write) when new-keys-rate is set, so all references within one transaction stay distinct. Expressed
	// in KEYS (a working-set size), not transactions. Ignored when new-keys-rate is unset.
	LookbackWindow uint64 `mapstructure:"lookback-window" yaml:"lookback-window"`

	// InvalidSignatures is the probability [0,1] that a transaction is stamped with a bad signature
	// (default: 0). The decision is derived deterministically from the transaction index.
	InvalidSignatures Probability `mapstructure:"invalid-signatures" yaml:"invalid-signatures" validate:"gte=0,lte=1"`
}

// Validate checks the split configuration. An unset new-keys-rate keeps the historical fresh-key
// workload (reference-gap and lookback-window are ignored). When set, the create rate must not exceed
// the write-slot count (so the per-transaction create count never overflows the write slots), and the
// lookback window must be at least the total slot count so every reference within a transaction is
// distinct.
func (p *TransactionProfile) Validate() error {
	if p.NewKeysRate == nil {
		return nil
	}
	rate := *p.NewKeysRate
	if rate < 0 {
		return errors.Newf("new-keys-rate must be non-negative, got %v", rate)
	}
	if writeSlots := float64(p.ReadWriteCount + p.BlindWriteCount); rate > writeSlots {
		return errors.Newf("new-keys-rate %v exceeds write slots %v (read-write-count + write-count)",
			rate, writeSlots)
	}
	totalSlots := uint64(p.ReadOnlyCount) + uint64(p.ReadWriteCount) + uint64(p.BlindWriteCount)
	if p.LookbackWindow < totalSlots {
		return errors.Newf("lookback-window %d must be at least the total slot count %d so every "+
			"reference within a transaction is distinct", p.LookbackWindow, totalSlots)
	}
	return nil
}

// PolicyProfile holds the policy information for the load generation.
type PolicyProfile struct {
	// NamespacePolicies specifies the namespace policies.
	NamespacePolicies map[string]*Policy `mapstructure:"namespace-policies" yaml:"namespace-policies"`

	// OrdererEndpoints may specify the endpoints to add to the config block.
	// If this field is empty, no endpoints will be configured.
	// If ConfigBlockPath is specified, this value is ignored.
	OrdererEndpoints []*commontypes.OrdererEndpoint `mapstructure:"orderer-endpoints" yaml:"orderer-endpoints"`

	// ArtifactsPath may specify the path to the artifacts generated by CreateOrExtendConfigBlockWithCrypto().
	// If this field is empty, the artifacts will be generated into a temporary folder.
	// If this path does not exist, or it is empty, the artifacts will be generated into it.
	// The config block will be fetched from ArtifactsPath.
	ArtifactsPath string `mapstructure:"artifacts-path" yaml:"artifacts-path"`

	// ChannelID and Identity are used to create the TX envelop.
	ChannelID string                      `mapstructure:"channel-id"`
	Identity  *ordererdial.IdentityConfig `mapstructure:"identity"`

	// PeerOrganizationCount may specify the number of peer organizations to generate if the ArtifactsPath
	// is not provided.
	PeerOrganizationCount uint32 `mapstructure:"peer-organization-count"`
}

// Policy describes how to sign/verify a TX.
// It supports a signing with a raw signing key, or via a local MSP.
// Scheme can be a valid signature schemes (NONE, ECDSA, BLS, or EDDSA) or MSP to indicate using a local MSP.
// When Scheme is not MSP, we generate a key using the given Seed, or loading one if KeyPath is given,
// ignoring MSPIdentities.
// When Scheme is MSP, we load the signing identities from MSPIdentities, ignoring Seed and KeyPath.
// In such case, we use the default rule, which state that all peer organization should sign.
// If MSPIdentities is not provided, we load the signing identities from ArtifactsPath.
type Policy struct {
	Scheme        signature.Scheme              `mapstructure:"scheme" yaml:"scheme"`
	Seed          int64                         `mapstructure:"seed" yaml:"seed"`
	KeyPath       *KeyPath                      `mapstructure:"key-path" yaml:"key-path"`
	MSPIdentities []*ordererdial.IdentityConfig `mapstructure:"msp-identities" yaml:"msp-identities"`
}

// KeyPath describes how to find/generate the signature keys.
type KeyPath struct {
	SigningKey      string `mapstructure:"signing-key" yaml:"signing-key"`
	VerificationKey string `mapstructure:"verification-key" yaml:"verification-key"`
	SignCertificate string `mapstructure:"sign-certificate" yaml:"sign-certificate"`
}

// StreamOptions allows adjustment to the stream rate.
// It only contains parameters that do not affect the produced items.
// However, these parameters might affect the order of the items.
type StreamOptions struct {
	// GenBatch impacts the rate by batching generated items before inserting then the channel.
	// This helps overcome the inherit rate limitation of Go channels.
	GenBatch uint32 `mapstructure:"gen-batch" yaml:"gen-batch"`
	// BuffersSize impact the rate by masking fluctuation in performance.
	BuffersSize int `mapstructure:"buffers-size" yaml:"buffers-size"`
	// RateLimit directly impacts the rate by limiting it.
	// TXs are released at RateLimit (default: unlimited).
	RateLimit uint64 `mapstructure:"rate-limit" yaml:"rate-limit"`
}

// Validate checks that the PolicyProfile does not contain invalid entries.
// System namespace "_config" must not be provided explicitly as it is not a real namespace.
// System namespace "_meta" can be derived from the artifacts' path when given.
// But it can be provided explicitly if desired.
// If provided explicitly, it must use a MSP rule.
func (p *PolicyProfile) Validate() error {
	if _, ok := p.NamespacePolicies[committerpb.ConfigNamespaceID]; ok {
		return errors.Newf("system namespace %q must not be provided in the policy profile",
			committerpb.ConfigNamespaceID)
	}

	if getPolicyScheme(p.NamespacePolicies[committerpb.MetaNamespaceID]) != PolicySchemeMSP {
		return errors.Newf("system namespace %q must use scheme %q", committerpb.MetaNamespaceID, PolicySchemeMSP)
	}
	return nil
}

func getPolicyScheme(policy *Policy) string {
	if policy == nil {
		return PolicySchemeDefault
	}
	scheme := strings.ToUpper(policy.Scheme)
	if scheme == PolicySchemeUnspecified {
		return PolicySchemeDefault
	}
	return scheme
}
