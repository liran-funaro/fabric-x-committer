package policy

import (
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/protobuf/proto"
)

// MakePolicy generates a policy item from a namespace policy.
func MakePolicy(
	t test.TestingT,
	ns string,
	nsPolicy *protoblocktx.NamespacePolicy,
) *protosigverifierservice.PolicyItem {
	pBytes, err := proto.Marshal(nsPolicy)
	require.NoError(t, err)
	return &protosigverifierservice.PolicyItem{
		Namespace: ns,
		Policy:    pBytes,
	}
}
