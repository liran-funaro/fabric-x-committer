package policy

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"google.golang.org/protobuf/proto"
)

func TestListPolicyItems(t *testing.T) {
	items := make([]*protoblocktx.ReadWrite, 5)
	for i := range items {
		items[i] = &protoblocktx.ReadWrite{
			Key:   []byte(fmt.Sprintf("key-%d", i)),
			Value: []byte(fmt.Sprintf("value-%d", i)),
		}
	}
	pd := ListPolicyItems(items)
	require.Len(t, pd, len(items))
	for i, p := range pd {
		require.Equal(t, items[i].Key, []byte(p.Namespace))
		require.Equal(t, items[i].Value, p.Policy)
	}
}

func TestParsePolicyItem(t *testing.T) {
	p := &protoblocktx.NamespacePolicy{
		Scheme:    "schema",
		PublicKey: []byte("public-key"),
	}
	for _, ns := range []string{"0", types.MetaNamespaceID} {
		t.Run(fmt.Sprintf("valid policy ns: %s", ns), func(t *testing.T) {
			pd := MakePolicy(t, ns, p)
			retNs, retP, err := ParsePolicyItem(pd)
			require.NoError(t, err)
			require.Equal(t, ns, retNs)
			require.True(t, proto.Equal(p, retP))
		})
	}

	t.Run("invalid ns", func(t *testing.T) {
		pd := MakePolicy(t, "0", p)
		// set some bad namespace
		pd.Namespace = ""
		_, _, err := ParsePolicyItem(pd)
		require.ErrorIs(t, err, types.ErrInvalidNamespaceID)
	})

	t.Run("invalid policy", func(t *testing.T) {
		pd := MakePolicy(t, "0", p)
		pd.Policy = []byte("bad-policy")
		_, _, err := ParsePolicyItem(pd)
		require.Error(t, err)
	})
}
