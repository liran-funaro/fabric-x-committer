package test

import (
	"io"
	"net/http"
	"testing"

	"github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	promgo "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func FailHandler(t *testing.T) {
	gomega.RegisterFailHandler(func(message string, _ ...int) {
		t.Fatalf(message)
	})
}

var TxSize = 1
var ClientInputDelay = NoDelay
var BatchSize = 100

// CheckMetrics checks the metrics endpoint for the expected metrics.
func CheckMetrics(t *testing.T, client *http.Client, url string, expectedMetrics []string) {
	resp, err := client.Get(url)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	bys, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	metricsOutput := string(bys)

	for _, expected := range expectedMetrics {
		require.Contains(t, metricsOutput, expected)
	}
}

// GetMetricValue returns the value of a prometheus metric.
func GetMetricValue(t *testing.T, m prometheus.Metric) float64 {
	gm := promgo.Metric{}
	require.NoError(t, m.Write(&gm))

	switch m.(type) {
	case prometheus.Gauge:
		return gm.Gauge.GetValue()
	case prometheus.Counter:
		return gm.Counter.GetValue()
	default:
		require.Fail(t, "metric is not counter or gauge")
	}

	return 0
}
