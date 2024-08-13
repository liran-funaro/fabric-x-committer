package prometheusmetrics

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("prometheus provider")

// Provider is a prometheus metrics provider.
type Provider struct {
	registry *prometheus.Registry
	server   *http.Server
	url      string
}

// NewProvider creates a new prometheus metrics provider.
func NewProvider() *Provider {
	return &Provider{
		registry: prometheus.NewRegistry(),
		server: &http.Server{
			ReadTimeout: 30 * time.Second,
		},
		url: "",
	}
}

// StartPrometheusServer starts a prometheus server in a separate goroutine
// and returns an error channel that will receive an error if the server
// stops unexpectedly.
func (p *Provider) StartPrometheusServer(e *connection.Endpoint) <-chan error {
	logger.Debugf("Creating prometheus server")
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
	l, err := net.Listen("tcp", e.Address())
	if err != nil {
		errorChan <- fmt.Errorf("failed to start prometheus server: %w", err)
	}

	p.url = fmt.Sprintf("http://%s/metrics", l.Addr().String())

	go func() {
		logger.Infof("Prometheus client serving on URL: %s", p.url)
		err = p.server.Serve(l)
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			logger.Debugf("Prometheus server started successfully")
			errorChan <- nil
		} else {
			logger.Errorf("Prometheus server started with error: %v", err)
			errorChan <- fmt.Errorf("prometheus server stopped unexpectedly: %w", err)
		}
	}()

	return errorChan
}

// StopServer stops the prometheus server.
func (p *Provider) StopServer() error {
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

// AddToCounter adds a value to a prometheus counter.
func AddToCounter(c prometheus.Counter, n int) {
	c.Add(float64(n))
}

// SetQueueSize sets a prometheus gauge to a value.
func SetQueueSize(queue prometheus.Gauge, size int) {
	queue.Set(float64(size))
}

// Observe observes a prometheus histogram.
func Observe(h prometheus.Observer, d time.Duration) {
	h.Observe(d.Seconds())
}

// ObserveSize observes a prometheus histogram size.
func ObserveSize(h prometheus.Observer, size int) {
	h.Observe(float64(size))
}
