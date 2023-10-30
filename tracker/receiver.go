package tracker

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type loader interface {
	LoadAndDelete(key any) (value any, loaded bool)
}

type latencyReceiverTracker struct {
	latencyTracker loader
	validLatency   prometheus.Histogram
	invalidLatency prometheus.Histogram
}

func (c *latencyReceiverTracker) registerLatency(duration time.Duration, success bool) {
	if success {
		c.validLatency.Observe(duration.Seconds())
	} else {
		c.invalidLatency.Observe(duration.Seconds())
	}
}

func (c *latencyReceiverTracker) OnReceiveTransaction(txID string, success bool) {
	if t, ok := c.latencyTracker.LoadAndDelete(txID); ok {
		logger.Infof("Tracked transaction %s returned", txID)
		start, _ := t.(time.Time)
		c.registerLatency(time.Since(start), success)
	}
}
