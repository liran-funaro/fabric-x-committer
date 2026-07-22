/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package workload

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ratePtr returns a pointer to a new-keys-rate value; nil means the split is disabled.
func ratePtr(v float64) *float64 { return &v }

func TestTransactionProfileValidate(t *testing.T) {
	t.Parallel()

	// Split disabled (new-keys-rate unset): always valid; reference-gap / lookback-window are ignored.
	require.NoError(t, (&TransactionProfile{}).Validate())
	require.NoError(t, (&TransactionProfile{ReadWriteCount: 2}).Validate())
	require.NoError(t, (&TransactionProfile{ReadOnlyCount: 2, ReferenceGap: 5}).Validate())

	// Split enabled: rate bounded by write slots; window must cover the total slot count.
	valid := &TransactionProfile{
		ReadOnlyCount: 2, ReadWriteCount: 2, BlindWriteCount: 1,
		NewKeysRate: ratePtr(2.5), ReferenceGap: 10, LookbackWindow: 100,
	}
	require.NoError(t, valid.Validate()) // 2.5 <= W=3 ; window 100 >= 5 slots

	// new-keys-rate 0 (no creates) is valid as long as the window covers the slots.
	zero := *valid
	zero.NewKeysRate = ratePtr(0)
	require.NoError(t, zero.Validate())

	// new-keys-rate > write slots is rejected.
	tooManyWrites := *valid
	tooManyWrites.NewKeysRate = ratePtr(3.5) // > W=3
	require.Error(t, tooManyWrites.Validate())

	// A negative rate is rejected.
	require.Error(t, (&TransactionProfile{NewKeysRate: ratePtr(-1)}).Validate())

	// A window smaller than the total slot count is rejected (references could collide within a tx).
	tooSmallWindow := *valid
	tooSmallWindow.LookbackWindow = 4 // < 5 slots
	require.Error(t, tooSmallWindow.Validate())
}
