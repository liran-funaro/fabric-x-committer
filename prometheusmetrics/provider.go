package prometheusmetrics

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
)

// Provider is a prometheus metrics provider.
type Provider struct {
	registry *prometheus.Registry
	server   *http.Server
	url      string
	config   *metrics.Config
}

// NewProvider creates a new prometheus metrics provider.
func NewProvider(c *metrics.Config) *Provider {
	return &Provider{
		registry: prometheus.NewRegistry(),
		server: &http.Server{
			ReadTimeout: 30 * time.Second,
		},
		url:    "",
		config: c,
	}
}

// StartPrometheusServer starts a prometheus server in a separate goroutine
// and returns an error channel that will receive an error if the server
// stops unexpectedly.
func (p *Provider) StartPrometheusServer() <-chan error {
	mux := http.NewServeMux()
	mux.Handle(
		"/metrics",
		promhttp.HandlerFor(
			p.Registry(),
			promhttp.HandlerOpts{
				Registry: p.Registry(),
			},
		),
	)
	p.server.Handler = mux

	errorChan := make(chan error)
	l, err := net.Listen("tcp", p.config.Endpoint.Address())
	if err != nil {
		errorChan <- fmt.Errorf("failed to start prometheus server: %w", err)
	}

	p.url = fmt.Sprintf("http://%s/metrics", l.Addr().String())

	go func() {
		err = p.server.Serve(l)
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			errorChan <- nil
		} else {
			errorChan <- fmt.Errorf("prometheus server stopped unexpectedly: %w", err)
		}
	}()

	return errorChan
}

// StopServer stops the prometheus server.
func (p *Provider) StopServer() error {
	p.url = ""
	return p.server.Close()
}

// URL returns the prometheus server URL.
func (p *Provider) URL() string {
	return p.url
}

// NewCounter creates a new prometheus counter.
func (p *Provider) NewCounter(opts prometheus.CounterOpts) prometheus.Counter {
	c := prometheus.NewCounter(opts)
	p.registry.MustRegister(c)
	return c
}

// NewCounterVec creates a new prometheus counter vector.
func (p *Provider) NewCounterVec(opts prometheus.CounterOpts, labels []string) *prometheus.CounterVec {
	cv := prometheus.NewCounterVec(opts, labels)
	p.registry.MustRegister(cv)
	return cv
}

// NewGauge creates a new prometheus gauge.
func (p *Provider) NewGauge(opts prometheus.GaugeOpts) prometheus.Gauge {
	g := prometheus.NewGauge(opts)
	p.registry.MustRegister(g)
	return g
}

// NewGaugeVec creates a new prometheus gauge vector.
func (p *Provider) NewGaugeVec(opts prometheus.GaugeOpts, labels []string) *prometheus.GaugeVec {
	gv := prometheus.NewGaugeVec(opts, labels)
	p.registry.MustRegister(gv)
	return gv
}

// NewHistogram creates a new prometheus histogram.
func (p *Provider) NewHistogram(opts prometheus.HistogramOpts) prometheus.Histogram {
	h := prometheus.NewHistogram(opts)
	p.registry.MustRegister(h)
	return h
}

// NewHistogramVec creates a new prometheus histogram vector.
func (p *Provider) NewHistogramVec(opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	hv := prometheus.NewHistogramVec(opts, labels)
	p.registry.MustRegister(hv)
	return hv
}

// Registry returns the prometheus registry.
func (p *Provider) Registry() *prometheus.Registry {
	return p.registry
}
