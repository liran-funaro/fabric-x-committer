package monitoring

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.8.0"
)

type Prometheus struct {
	Endpoint        connection.Endpoint `mapstructure:"endpoint"`
	LatencyEndpoint connection.Endpoint `mapstructure:"latency-endpoint"`
}

func (p *Prometheus) IsEnabled() bool {
	return p.Endpoint.Port > 0
}

func (p *Prometheus) IsLatencyEnabled() bool {
	return p.LatencyEndpoint.Port > 0
}

var componentTypeMap = map[ComponentType]string{
	Coordinator:   "coordinator",
	SigVerifier:   "sigverifier",
	ShardsService: "shards-service",
	Generator:     "generator",
}

func LaunchPrometheus(config Prometheus, componentType ComponentType, appMetrics metrics.AppMetrics) {
	if !appMetrics.IsEnabled() {
		return
	}
	registry := prometheus.NewRegistry()

	customMetricRegisterer := registerer("sc", componentTypeMap[componentType], registry)

	if config.IsLatencyEnabled() {
		appMetrics.SetTracerProvider(traceProvider(componentType, jaegerExporter(config.LatencyEndpoint)))
	}

	defaultMetrics := metrics.New(true)
	for _, collector := range append(appMetrics.AllMetrics(), defaultMetrics.AllMetrics()...) {
		customMetricRegisterer.Register(collector)
	}
	defaultMetrics.ComponentType.Set(float64(componentType))

	http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	go func() {
		utils.Must(http.ListenAndServe(config.Endpoint.Address(), nil))
	}()
}

func traceProvider(componentType ComponentType, exporter sdktrace.SpanExporter) *sdktrace.TracerProvider {
	return sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exporter, sdktrace.WithMaxExportBatchSize(1)),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(componentTypeMap[componentType]),
			semconv.ServiceVersionKey.String("0.0.1")),
		),
	)
}

func consoleExporter() *stdouttrace.Exporter {
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		panic(err)
	}
	return exporter
}

func jaegerExporter(endpoint connection.Endpoint) *jaeger.Exporter {
	exporter, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(fmt.Sprintf("http://%s/api/traces", endpoint.Address()))))

	if err != nil {
		panic(err)
	}
	return exporter
}

func registerer(prefix, componentName string, registry *prometheus.Registry) prometheus.Registerer {
	return prometheus.WrapRegistererWithPrefix(prefix+"_",
		prometheus.WrapRegistererWith(prometheus.Labels{"component": componentName}, registry))
}
