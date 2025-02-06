package policy

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"google.golang.org/protobuf/proto"
)

// KeyValue represents any key/value implementation.
type KeyValue interface {
	GetKey() []byte
	GetValue() []byte
}

// ListPolicyItems translates key-value list to policy items.
func ListPolicyItems[T KeyValue](rws []T) []*protosigverifierservice.PolicyItem {
	pd := make([]*protosigverifierservice.PolicyItem, len(rws))
	for i, rw := range rws {
		pd[i] = &protosigverifierservice.PolicyItem{
			Namespace: string(rw.GetKey()),
			Policy:    rw.GetValue(),
		}
	}
	return pd
}

// ParsePolicyItem parses policy item to a namespace policy.
func ParsePolicyItem(pd *protosigverifierservice.PolicyItem) (
	ns string, key *protoblocktx.NamespacePolicy, err error,
) {
	err = types.ValidateNamespaceID(pd.Namespace)
	if err != nil {
		return ns, key, err
	}
	key, err = keysFromMetaNamespaceTx(pd.Policy)
	return pd.Namespace, key, err
}

func keysFromMetaNamespaceTx(value []byte) (*protoblocktx.NamespacePolicy, error) {
	p := &protoblocktx.NamespacePolicy{}
	err := proto.Unmarshal(value, p)
	if err != nil {
		return nil, err
	}
	return p, nil
}
