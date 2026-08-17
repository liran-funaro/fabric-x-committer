/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package monitoring

import (
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

// The helpers here build gauges whose value is read on scrape, so none needs a goroutine sampling
// on a timer: the value is exact rather than as fresh as the sampling interval, and there is no
// separate wiring step to lose. Every closure runs on the scrape path, so each must stay cheap and
// treat an absent source as zero.

type (
	// gaugeable is the set of types an atomic value may hold and be reported as a gauge.
	gaugeable interface {
		~int32 | ~int64 | ~uint32 | ~uint64
	}

	// atomicLoader is satisfied by the sync/atomic integer types.
	atomicLoader[T gaugeable] interface {
		Load() T
	}
)

// NewAtomicValueGauge creates a new prometheus gauge that reports the value held by v on every
// scrape. Use it for a counter the code already maintains atomically — reporting it costs one
// registration, whereas mirroring it into a settable gauge means every writer has to remember to
// update both, and one that forgets is invisible.
func NewAtomicValueGauge[T gaugeable, A atomicLoader[T]](
	p *Provider,
	opts prometheus.GaugeOpts,
	v A,
) prometheus.GaugeFunc {
	return p.NewGaugeFunc(opts, func() float64 {
		return float64(v.Load())
	})
}

// NewChannelLenGauge creates a new prometheus gauge that reports the number of items buffered in
// ch on every scrape. Prefer it over a NewGauge that a dedicated goroutine samples on a timer.
// A nil channel reports zero.
func NewChannelLenGauge[T any](
	p *Provider,
	opts prometheus.GaugeOpts,
	ch <-chan T,
) prometheus.GaugeFunc {
	return p.NewGaugeFunc(opts, func() float64 {
		return float64(len(ch))
	})
}

// NewAtomicChannelLenGauge creates a new prometheus gauge that reports the number of items
// buffered in the channel ch currently points to. Use it for a queue that is replaced during the
// service's lifetime, such as one recreated per session: the value follows whichever channel is
// installed, and an unset pointer reports zero rather than the previous channel's last value.
func NewAtomicChannelLenGauge[T any](
	p *Provider,
	opts prometheus.GaugeOpts,
	ch *atomic.Pointer[chan T],
) prometheus.GaugeFunc {
	return p.NewGaugeFunc(opts, func() float64 {
		c := ch.Load()
		if c == nil {
			return 0
		}
		return float64(len(*c))
	})
}
