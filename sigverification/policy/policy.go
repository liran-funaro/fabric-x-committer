package policy

import (
	"regexp"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-lib-go/bccsp/factory"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/decentralized-trust-research/fabricx-config/common/channelconfig"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
)

// KeyValue represents any key/value implementation.
type KeyValue interface {
	GetKey() []byte
	GetValue() []byte
}

// maxNamespaceIDLength defines the maximum number of characters allowed for namespace IDs.
// PostgreSQL limits identifiers to NAMEDATALEN-1, where NAMEDATALEL=64.
// The namespace tables have the prefix 'ns_', thus there are 60 characters remaining.
// See: https://www.postgresql.org/docs/current/sql-syntax-lexical.html
const maxNamespaceIDLength = 60

// validNamespaceID describes the allowed characters in a namespace ID.
// The name may contain letters, digits, or underscores.
// PostgreSQL requires the name to begin with a letter. This is ensured by our namespace table prefix.
// In addition, we restrict to lowercase letters as PostgreSQL converts does not distinguish between upper/lower case.
// See: https://www.postgresql.org/docs/current/sql-syntax-lexical.html
// The regexp is wrapped with "^...$" to ensure we only match the entire string (namespace ID).
// We use the flags: i - ignore case, s - single line.
var validNamespaceID = regexp.MustCompile(`^[a-z0-9_]+$`)

// ErrInvalidNamespaceID is returned when the namespace ID cannot be parsed.
var ErrInvalidNamespaceID = errors.New("invalid namespace ID")

// ListPolicyItems translates key-value list to policy items.
func ListPolicyItems[T KeyValue](rws []T) []*protoblocktx.PolicyItem {
	pd := make([]*protoblocktx.PolicyItem, len(rws))
	for i, rw := range rws {
		pd[i] = &protoblocktx.PolicyItem{
			Namespace: string(rw.GetKey()),
			Policy:    rw.GetValue(),
		}
	}
	return pd
}

// ParsePolicyItem parses policy item to a namespace policy.
func ParsePolicyItem(pd *protoblocktx.PolicyItem) (*protoblocktx.NamespacePolicy, error) {
	if err := validateNamespaceID(pd.Namespace); err != nil {
		return nil, err
	}
	switch pd.Namespace {
	case types.MetaNamespaceID:
		return policyFromConfigTx(pd.Policy)
	default:
		return policyFromMetaNamespaceTx(pd.Policy)
	}
}

// validateNamespaceID checks that a given namespace fulfills namespace naming conventions.
func validateNamespaceID(nsID string) error {
	// if it matches our holy MetaNamespaceID it is valid.
	if nsID == types.MetaNamespaceID {
		return nil
	}

	// length checks.
	if len(nsID) == 0 || len(nsID) > maxNamespaceIDLength {
		return ErrInvalidNamespaceID
	}

	// characters check.
	if !validNamespaceID.MatchString(nsID) {
		return ErrInvalidNamespaceID
	}

	return nil
}

// policyFromMetaNamespaceTx parse a namespace policy.
func policyFromMetaNamespaceTx(value []byte) (*protoblocktx.NamespacePolicy, error) {
	p := &protoblocktx.NamespacePolicy{}
	err := proto.Unmarshal(value, p)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal namespace policy")
	}
	return p, nil
}

func policyFromConfigTx(value []byte) (*protoblocktx.NamespacePolicy, error) {
	envelope, err := protoutil.UnmarshalEnvelope(value)
	if err != nil {
		return nil, errors.Wrap(err, "error unmarshalling envelope")
	}
	bundle, err := channelconfig.NewBundleFromEnvelope(envelope, factory.GetDefault())
	if err != nil {
		return nil, errors.Wrap(err, "error parsing config")
	}
	ac, ok := bundle.ApplicationConfig()
	if !ok {
		return nil, errors.New("application configuration is missing")
	}
	acx, ok := ac.(*channelconfig.ApplicationConfig)
	if !ok {
		return nil, errors.New("application configuration of incorrect type")
	}
	key := acx.MetaNamespaceVerificationKey()
	return &protoblocktx.NamespacePolicy{
		PublicKey: key.KeyMaterial,
		// We use existing proto here to avoid introducing new ones.
		// So we encode the key schema as the identifier.
		// This will be replaced in the future with a generic policy mechanism.
		Scheme: key.KeyIdentifier,
	}, nil
}
