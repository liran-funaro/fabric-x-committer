package monitoring

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

type Prometheus struct {
	Enabled  bool                `mapstructure:"enabled"`
	Endpoint connection.Endpoint `mapstructure:"endpoint"`
}

func LaunchPrometheus(config Prometheus, componentName string, collectors []prometheus.Collector) {
	if !config.Enabled {
		return
	}
	registry := prometheus.NewRegistry()
	registerer := prometheus.WrapRegistererWithPrefix("sc_",
		prometheus.WrapRegistererWith(prometheus.Labels{"server": componentName}, registry))

	for _, collector := range append(collectors, metrics.AllMetrics...) {
		registerer.MustRegister(collector)
	}

	http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	go func() {
		utils.Must(http.ListenAndServe(config.Endpoint.Address(), nil))
	}()
}
