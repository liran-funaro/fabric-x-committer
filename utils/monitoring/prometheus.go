package monitoring

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

type Prometheus struct {
	Enabled  bool                `mapstructure:"enabled"`
	Endpoint connection.Endpoint `mapstructure:"endpoint"`
}

func LaunchPrometheus(config Prometheus, componentName string, customCollectors []prometheus.Collector) {
	if !config.Enabled {
		return
	}
	registry := prometheus.NewRegistry()

	customMetricRegisterer := registerer("sc", componentName, registry)
	for _, collector := range append(append(metrics.New().AllMetrics(), customCollectors...), metrics.ChannelBufferLengthGauge, metrics.ThroughputCounter) {
		customMetricRegisterer.MustRegister(collector)
	}

	nodeExporterMetricRegisterer := registerer("ne", componentName, registry)
	nodeExporterMetricRegisterer.MustRegister(collectors.NewGoCollector(collectors.WithGoCollectorRuntimeMetrics(collectors.MetricsAll)))
	nodeExporterMetricRegisterer.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	go func() {
		utils.Must(http.ListenAndServe(config.Endpoint.Address(), nil))
	}()
}

func registerer(prefix, componentName string, registry *prometheus.Registry) prometheus.Registerer {
	return prometheus.WrapRegistererWithPrefix(prefix+"_",
		prometheus.WrapRegistererWith(prometheus.Labels{"component": componentName}, registry))
}

type ProcessCollector struct {
}
