package prometheusmetrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

const (
	schema         = "http://"
	metricsSubPath = "/metrics"
)

var logger = logging.New("prometheus provider")

// Provider is a prometheus metrics provider.
type Provider struct {
	registry *prometheus.Registry
	url      string
}

// NewProvider creates a new prometheus metrics provider.
func NewProvider() *Provider {
	return &Provider{
		registry: prometheus.NewRegistry(),
	}
}

// StartPrometheusServer starts a prometheus server.
// It also starts the given monitoring methods. Their context will cancel once the server is cancelled.
// This method returns once the server is shutdown and all monitoring methods returns.
func (p *Provider) StartPrometheusServer(
	ctx context.Context, e *connection.Endpoint, monitor ...func(context.Context),
) error {
	logger.Debugf("Creating prometheus server")
	mux := http.NewServeMux()
	mux.Handle(
		metricsSubPath,
		promhttp.HandlerFor(
			p.Registry(),
			promhttp.HandlerOpts{
				Registry: p.Registry(),
			},
		),
	)
	server := &http.Server{
		ReadTimeout: 30 * time.Second,
		Handler:     mux,
	}

	l, err := net.Listen("tcp", e.Address())
	if err != nil {
		return fmt.Errorf("failed to start prometheus server: %w", err)
	}

	p.url, err = url.JoinPath(schema, l.Addr().String(), metricsSubPath)
	if err != nil {
		return fmt.Errorf("failed formatting URL: %w", err)
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		logger.Infof("Prometheus serving on URL: %s", p.url)
		defer logger.Info("Prometheus stopped serving")
		return server.Serve(l)
	})

	// The following ensures the method does not return before all monitor methods return.
	for _, m := range monitor {
		g.Go(func() error {
			m(gCtx)
			return nil
		})
	}

	// The following ensures the method does not return before the close procedure is complete.
	stopAfter := context.AfterFunc(ctx, func() {
		g.Go(func() error {
			if err := server.Close(); err != nil {
				return fmt.Errorf("failed to close prometheus server: %w", err)
			}
			return nil
		})
	})
	defer stopAfter()

	if err = g.Wait(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("prometheus server stopped with an error: %w", err)
	}
	return nil
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
