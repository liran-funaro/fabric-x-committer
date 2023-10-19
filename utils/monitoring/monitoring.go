package monitoring

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.8.0"
)

const metricPrefix = "sc"

var logger = logging.New("monitoring")

func LaunchMonitoring(config Config, appMetrics Provider) metrics.AppMetrics {
	//TODO: Support for metric providers instead of boolean
	//if !config.IsMetricsEnabled() {
	//	return nil
	//}

	tracer := startLatencyExporter(config.Latency, appMetrics.ComponentName(), appMetrics.LatencyLabels())

	monitoring := appMetrics.NewMonitoring(config.IsMetricsEnabled(), tracer)
	startMetricExporter(config.Metrics, appMetrics.ComponentName(), monitoring.AllMetrics(), tracer.Collectors())

	return monitoring
}

func startMetricExporter(m *metrics.Config, componentName string, metricCollectors, tracerCollectors []prometheus.Collector) {
	registry := prometheus.NewRegistry()
	customMetricRegisterer := registerer(metricPrefix, componentName, registry)
	defaultMetrics := metrics.New(true)

	allMetrics := append(append(metricCollectors, defaultMetrics.AllMetrics()...), tracerCollectors...)
	for _, collector := range allMetrics {
		customMetricRegisterer.Register(collector)
	}
	registry.Register(collectors.NewGoCollector())
	defaultMetrics.ComponentType.Set(0) // No value is needed. The registerer already adds the label 'component'.

	http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	go func() {
		logger.Infof("Starting prometheus exporter:\n"+
			"\tEndpoint:\t\t%v\n"+
			"\tTotal metrics:\t%d\n", m.Endpoint, len(allMetrics))
		utils.Must(http.ListenAndServe(m.Endpoint.Address(), nil))
	}()
}

func startLatencyExporter(l *latency.Config, componentName string, labels []string) latency.AppTracer {
	if l == nil {
		logger.Infof("NoOp latency tracer set.")
		return &latency.NoOpTracer{}
	}
	name := fmt.Sprintf("%s_latency", componentName)
	logger.Infof("Starting latency tracer:\n"+
		"\tName:\t%s\n"+
		"\tRange:\t0-%v (%d buckets)\n"+
		"\tSampler:\t%v\n"+
		"\tExporter:\t%s\n"+
		"\tEndpoint:\t%v\n", name, l.MaxLatency, l.BucketCount, l.Sampler, l.SpanExporter, l.Endpoint)
	return latency.NewLatencyTracer(latency.TracerOpts{
		Name:           name,
		Help:           "Total latency on the component",
		Count:          l.BucketCount,
		From:           0,
		To:             l.MaxLatency,
		Sampler:        l.Sampler.TxSampler(),
		IgnoreNotFound: true,
		Labels:         labels,
	}, traceProvider(componentName, spanExporter(l.SpanExporter, l.Endpoint)))
}

func traceProvider(componentName string, exporter sdktrace.SpanExporter) *sdktrace.TracerProvider {
	return sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(componentName),
			semconv.ServiceVersionKey.String("0.0.1")),
		),
	)
}

func spanExporter(exporterType latency.SpanExporterType, endpoint *connection.Endpoint) sdktrace.SpanExporter {
	switch exporterType {
	case latency.Jaeger:
		exporter, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(fmt.Sprintf("http://%s/api/traces", endpoint.Address()))))

		if err != nil {
			panic(err)
		}
		return exporter
	case latency.Console:
		exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			panic(err)
		}
		return exporter
	default:
		panic("unknown exporter type " + exporterType)
	}
}

func registerer(prefix, componentName string, registry *prometheus.Registry) prometheus.Registerer {
	return prometheus.WrapRegistererWithPrefix(prefix+"_",
		prometheus.WrapRegistererWith(prometheus.Labels{"component": componentName}, registry))
}
