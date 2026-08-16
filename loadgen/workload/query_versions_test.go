/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package workload

import (
	"testing"

	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/prometheus/client_golang/prometheus"
	promgo "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// counterValue reads a counter's current value without pulling in prometheus/testutil, which would add
// a module dependency for a single assertion. Mirrors getCounterValue in loadgen/metrics.
func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m promgo.Metric
	require.NoError(t, c.Write(&m))
	return m.GetCounter().GetValue()
}

func TestKeyVersionLabel(t *testing.T) {
	t.Parallel()
	v := func(n uint64) *uint64 { return &n }
	for _, tc := range []struct {
		name    string
		version *uint64
		want    string
	}{
		{"miss keeps a nil version", nil, MissKeyVersionLabel},
		{"version zero is its own bucket, not a miss", v(0), "0"},
		{"version one", v(1), "1"},
		{"last exact bucket", v(MaxTrackedKeyVersion - 1), "15"},
		{"at the bound folds into the overflow bucket", v(MaxTrackedKeyVersion), MaxTrackedKeyVersionLabel},
		{"far above the bound also folds", v(1 << 40), MaxTrackedKeyVersionLabel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, KeyVersionLabel(tc.version))
		})
	}
}

// TestFillSelectedVersionsRecordsOutcomes asserts the counter distinguishes a hit from a miss for every
// read the query stage resolved, which is what makes the hit rate derivable: total minus the "nil" bucket.
func TestFillSelectedVersionsRecordsOutcomes(t *testing.T) {
	t.Parallel()
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_queried_key_versions_total"},
		[]string{"version"})
	f := &queryFiller{versions: vec}

	hitRead := &applicationpb.Read{Key: []byte("hit-read")}
	missRead := &applicationpb.Read{Key: []byte("miss-read")}
	hitRW := &applicationpb.ReadWrite{Key: []byte("hit-rw")}
	missRW := &applicationpb.ReadWrite{Key: []byte("miss-rw")}

	selected := map[string]*nsSelection{
		"0": {reads: []*applicationpb.Read{hitRead, missRead},
			readWrites: []*applicationpb.ReadWrite{hitRW, missRW}},
	}
	// Only the two "hit" keys have a committed version; the other two are absent from the response.
	versions := map[string]map[string]uint64{
		"0": {"hit-read": 7, "hit-rw": uint64(MaxTrackedKeyVersion) + 5},
	}

	f.fillSelectedVersions(selected, versions)

	// Versions are filled in place for hits and left nil for misses.
	require.NotNil(t, hitRead.Version)
	require.Equal(t, uint64(7), *hitRead.Version)
	require.Nil(t, missRead.Version)
	require.NotNil(t, hitRW.Version)
	require.Nil(t, missRW.Version)

	require.Equal(t, 1.0, counterValue(t, vec.WithLabelValues("7")))
	require.Equal(t, 1.0, counterValue(t, vec.WithLabelValues(MaxTrackedKeyVersionLabel)))
	// Both misses land in the single nil bucket.
	require.Equal(t, 2.0, counterValue(t, vec.WithLabelValues(MissKeyVersionLabel)))
}

// A nil vec must be a no-op rather than a panic: tests and any caller without metrics pass nil.
func TestFillSelectedVersionsWithoutMetrics(t *testing.T) {
	t.Parallel()
	f := &queryFiller{}
	read := &applicationpb.Read{Key: []byte("k")}
	selected := map[string]*nsSelection{"0": {reads: []*applicationpb.Read{read}}}
	require.NotPanics(t, func() {
		f.fillSelectedVersions(selected, map[string]map[string]uint64{"0": {"k": 3}})
	})
	require.NotNil(t, read.Version)
	require.Equal(t, uint64(3), *read.Version)
}
