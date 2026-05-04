/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package monitoring

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring/promutil"
)

type (
	// MetricsParameters describes metrics namespace and subsystem.
	MetricsParameters struct {
		Namespace string
		Subsystem string
	}

	// ConnectionMetrics supports common connection metrics.
	ConnectionMetrics struct {
		Status       *prometheus.GaugeVec
		FailureTotal *prometheus.CounterVec
		connected    sync.Map // tracks connected grpc targets using map[string]any
	}
)

// Throughput counter direction constants.
const (
	In  = "in"
	Out = "out"
)

// NewThroughputCounter creates a new prometheus throughput counter.
func NewThroughputCounter(p *Provider, params MetricsParameters) *prometheus.CounterVec {
	return p.NewCounterVec(prometheus.CounterOpts{
		Namespace: params.Namespace,
		Subsystem: params.Subsystem,
		Name:      "throughput",
		Help:      "Incoming requests/Outgoing responses for a component",
	}, []string{"direction"})
}

// NewConnectionMetrics supports common connection metrics.
func NewConnectionMetrics(p *Provider, params MetricsParameters) *ConnectionMetrics {
	return &ConnectionMetrics{
		Status: p.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: params.Namespace,
			Subsystem: params.Subsystem,
			Name:      "connection_status",
			Help: fmt.Sprintf(
				"Connection status to %s service by grpc target (1 = connected, 0 = disconnected).",
				params.Subsystem,
			),
		}, []string{"grpc_target"}),
		FailureTotal: p.NewCounterVec(prometheus.CounterOpts{
			Namespace: params.Namespace,
			Subsystem: params.Subsystem,
			Name:      "connection_failure_total",
			Help: fmt.Sprintf(
				"Total number of connection failures to %s service. Short-lived failures may not always be captured.",
				params.Subsystem,
			),
		}, []string{"grpc_target"}),
	}
}

// Connected observed connected.
func (m *ConnectionMetrics) Connected(grpcTarget string) {
	promutil.SetGaugeVec(m.Status, []string{grpcTarget}, connection.Connected)
	m.connected.Store(grpcTarget, nil)
}

// Disconnected observe disconnected. The failure count is increased only if the status was connected.
func (m *ConnectionMetrics) Disconnected(grpcTarget string) {
	promutil.SetGaugeVec(m.Status, []string{grpcTarget}, connection.Disconnected)
	if _, loaded := m.connected.LoadAndDelete(grpcTarget); loaded {
		promutil.AddToCounterVec(m.FailureTotal, []string{grpcTarget}, 1)
	}
}
