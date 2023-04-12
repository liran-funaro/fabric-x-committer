package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
)

const (
	prometheusDefaultLocalPort      = 9090
	grafanaDefaultLocalPort         = 3000
	jaegerDefaultLocalUiPort        = 16686
	jaegerDefaultLocalCollectorPort = 14268
)

var logger = logging.New("dockerrunner")

func prometheusParams(dir, prometheusConfig string, port int) *monitoring.DockerRunParams {
	logger.Infof("Prometheus config: %s/%s", dir, prometheusConfig)
	return &monitoring.DockerRunParams{
		Image:    "prom/prometheus:latest",
		Hostname: "prometheus",
		Mounts: map[string]string{
			filepath.Join(dir, prometheusConfig): "/etc/prometheus/prometheus.yml",
			"/etc/hosts":                         "/etc/hosts",
		},
		PortMappings: map[int]int{
			prometheusDefaultLocalPort: port,
		},
	}
}
func jaegerParams(uiPort, collectorPort int) *monitoring.DockerRunParams {
	return &monitoring.DockerRunParams{
		Image:    "jaegertracing/all-in-one:1.22",
		Hostname: "jaeger",
		Envs: map[string]string{
			"JAEGER_SAMPLER_TYPE":  "const",
			"JAEGER_SAMPLER_PARAM": "1",
		},
		PortMappings: map[int]int{
			jaegerDefaultLocalUiPort:        uiPort,
			jaegerDefaultLocalCollectorPort: collectorPort,
			14269:                           14269,
		},
	}
}
func grafanaParams(dir, keyPath, certPath string, port int, prometheusInstanceName string) *monitoring.DockerRunParams {
	return &monitoring.DockerRunParams{
		Image:    "grafana/grafana:latest",
		Hostname: "grafana",
		Envs: map[string]string{
			"GF_AUTH_PROXY_ENABLED":   "true",
			"GF_PATHS_PROVISIONING":   "/etc/grafana/provisioning/",
			"_GF_PROMETHEUS_ENDPOINT": fmt.Sprintf("http://%s:%d", prometheusInstanceName, prometheusDefaultLocalPort),
		},
		Mounts: map[string]string{
			filepath.Join(dir, "grafana.ini"): "/etc/grafana/grafana.ini",
			keyPath:                           "/etc/grafana/grafana.key",
			certPath:                          "/etc/grafana/grafana.crt",
			filepath.Join(dir, "grafana-datasources.yml"):      "/etc/grafana/provisioning/datasources/datasource.yml",
			filepath.Join(dir, "grafana-dashboards.yml"):       "/etc/grafana/provisioning/dashboards/dashboard.yml",
			filepath.Join(dir, "prometheus-dashboard.json"):    "/etc/grafana/provisioning/dashboards/prometheus-dashboard.json",
			filepath.Join(dir, "node-exporter-dashboard.json"): "/etc/grafana/provisioning/dashboards/node-exporter-dashboard.json",
		},
		//TODO: Use user-defined network instead of link
		Links: []string{prometheusInstanceName},
		PortMappings: map[int]int{
			grafanaDefaultLocalPort: port,
		},
	}
}

func main() {
	configPath := flag.String("config-dir", ".", "Relative dir path with static config files.")
	prometheusPort := flag.Int("prometheus-port", 9091, "Port Prometheus listens to.")
	prometheusConfig := flag.String("prometheus-config", "prometheus.yml", "File containing the prometheus YAML file.")
	jaegerUiPort := flag.Int("jaeger-ui-port", jaegerDefaultLocalUiPort, "Port Jaeger UI listens to.")
	jaegerCollectorPort := flag.Int("jaeger-collector-port", jaegerDefaultLocalCollectorPort, "Port Jaeger Collector listens to.")
	grafanaPort := flag.Int("grafana-port", 3001, "Port Prometheus listens to.")
	grafanaKeyPath := flag.String("grafana-key", "", "Path to private key for Grafana.")
	grafanaCertPath := flag.String("grafana-cert", "", "Path to cert for Grafana.")
	removeExisting := flag.Bool("remove-existing", false, "Remove existing images")
	flag.Parse()

	if *grafanaKeyPath == "" || *grafanaCertPath == "" {
		panic("No paths passed for key and cert.")
	}

	runOpts := &monitoring.DockerRunOpts{RemoveIfExists: *removeExisting}

	configAbsPath, err := filepath.Abs(*configPath)
	if err != nil {
		panic(err)
	}

	runner, err := monitoring.DockerContainerRunner()
	if err != nil {
		panic(err)
	}

	prometheusInstanceName, err := runner.Start(prometheusParams(configAbsPath, *prometheusConfig, *prometheusPort), runOpts)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Infof("Prometheus client running on http://localhost:%d", *prometheusPort)

	_, err = runner.Start(jaegerParams(*jaegerUiPort, *jaegerCollectorPort), runOpts)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Infof("Jaeger UI running on http://localhost:%d", *jaegerUiPort)

	_, err = runner.Start(grafanaParams(configAbsPath, *grafanaKeyPath, *grafanaCertPath, *grafanaPort, prometheusInstanceName), runOpts)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Infof("Grafana running on https://localhost:%d (user: admin, pass: admin)", *grafanaPort)
}
