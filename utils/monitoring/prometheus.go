package monitoring

import (
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

type ComponentType = int

const (
	Coordinator ComponentType = iota
	SigVerifier
	ShardsService
)

var componentTypeMap = map[ComponentType]string{
	Coordinator:   "coordinator",
	SigVerifier:   "sigverifier",
	ShardsService: "shards-service",
}

var componentTypeGauge = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "component_type",
	Help: "Current component type",
})

func LaunchPrometheus(config Prometheus, componentType ComponentType, customCollectors []prometheus.Collector) {
	if !config.Enabled {
		return
	}
	registry := prometheus.NewRegistry()

	customMetricRegisterer := registerer("sc", componentTypeMap[componentType], registry)

	for _, collector := range append(customCollectors, componentTypeGauge) {
		if err := customMetricRegisterer.Register(collector); err != nil {
			logger.Infof("Error registering: %v", err)
		}
	}
	componentTypeGauge.Set(float64(componentType))

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
