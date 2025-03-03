package monitoring

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

const (
	scheme         = "http://"
	metricsSubPath = "/metrics"
)

// Provider is a prometheus metrics provider.
type Provider struct {
	registry *prometheus.Registry
	url      string
}

var logger = logging.New("monitoring")

// NewProvider creates a new prometheus metrics provider.
func NewProvider() *Provider {
	return &Provider{registry: prometheus.NewRegistry()}
}

// StartPrometheusServer starts a prometheus server.
// It also starts the given monitoring methods. Their context will cancel once the server is cancelled.
// This method returns once the server is shutdown and all monitoring methods returns.
func (p *Provider) StartPrometheusServer(
	ctx context.Context, serverConfig *connection.ServerConfig, monitor ...func(context.Context),
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

	l, err := connection.Listen(serverConfig)
	if err != nil {
		return errors.Wrap(err, "failed to start prometheus server")
	}
	defer connection.CloseConnectionsLog(l)

	p.url, err = url.JoinPath(scheme, l.Addr().String(), metricsSubPath)
	if err != nil {
		return errors.Wrap(err, "failed formatting URL")
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
				return errors.Wrap(err, "failed to close prometheus server")
			}
			return nil
		})
	})
	defer stopAfter()

	if err = g.Wait(); !errors.Is(err, http.ErrServerClosed) {
		return errors.Wrap(err, "prometheus server stopped with an error")
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

// NewIntCounter creates a new prometheus integer counter.
func (p *Provider) NewIntCounter(opts prometheus.CounterOpts) *IntCounter {
	return &IntCounter{Counter: p.NewCounter(opts)}
}

// NewIntGauge creates a new prometheus integer gauge.
func (p *Provider) NewIntGauge(opts prometheus.GaugeOpts) *IntGauge {
	return &IntGauge{Gauge: p.NewGauge(opts)}
}

// NewDurationHistogram creates a new prometheus duration histogram.
func (p *Provider) NewDurationHistogram(opts prometheus.HistogramOpts) *DurationHistogram {
	return &DurationHistogram{Histogram: p.NewHistogram(opts)}
}

// NewThroughputCounter creates a new prometheus throughput counter.
func (p *Provider) NewThroughputCounter(component, subComponent string, direction ThroughputDirection) *IntCounter {
	return p.NewIntCounter(prometheus.CounterOpts{
		Namespace: component,
		Subsystem: subComponent,
		Name:      fmt.Sprintf("%s_throughput", direction),
		Help:      "Incoming requests/Outgoing responses for a component",
	})
}

// NewChannelBufferGauge creates a new prometheus buffer size counter.
func (p *Provider) NewChannelBufferGauge(opts BufferGaugeOpts) *IntGauge {
	return p.NewIntGauge(prometheus.GaugeOpts{
		Namespace: opts.Namespace,
		Subsystem: opts.Subsystem,
		Name:      fmt.Sprintf("%s_channel_size", opts.Channel),
		Help:      "The pending objects in a channel buffer",
	})
}

// Registry returns the prometheus registry.
func (p *Provider) Registry() *prometheus.Registry {
	return p.registry
}

// AddToCounter adds a value to a prometheus counter.
func AddToCounter(c prometheus.Counter, n int) {
	c.Add(float64(n))
}

// AddToGauge adds a value to a prometheus gauge.
func AddToGauge(c prometheus.Gauge, n int) {
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
