/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package monitoring

import (
	"github.com/prometheus/client_golang/prometheus"
)

// The helpers here build gauges whose value is read on scrape, so none needs a goroutine sampling
// on a timer: the value is exact rather than as fresh as the sampling interval, and there is no
// separate wiring step to lose. Every closure runs on the scrape path, so each must stay cheap and
// treat an absent source as zero.

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
