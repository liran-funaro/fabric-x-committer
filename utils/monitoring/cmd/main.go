package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
)

const (
	prometheusDefaultLocalPort = 9090
	grafanaDefaultLocalPort    = 3000
	grafanaProvisioningDir     = "/etc/grafana/provisioning/"
	prometheusInstanceName     = "prometheus-instance"
)

var logger = logging.New("dockerrunner")

var runOpts = &monitoring.DockerRunOpts{
	RemoveIfExists: true,
}

func prometheusParams(dir string, port int) *monitoring.DockerRunParams {
	return &monitoring.DockerRunParams{
		Name:     prometheusInstanceName,
		Image:    "prom/prometheus:latest",
		Hostname: "prometheus",
		Mounts: map[string]string{
			filepath.Join(dir, "/prometheus.yml"): "/etc/prometheus/prometheus.yml",
		},
		PortMappings: map[int]int{
			prometheusDefaultLocalPort: port,
		},
	}
}
func grafanaParams(dir string, port int) *monitoring.DockerRunParams {
	return &monitoring.DockerRunParams{
		Name:     "grafana-instance",
		Image:    "grafana/grafana:latest",
		Hostname: "grafana",
		Envs: map[string]string{
			"GF_AUTH_PROXY_ENABLED":   "true",
			"GF_PATHS_PROVISIONING":   grafanaProvisioningDir,
			"_GF_PROMETHEUS_ENDPOINT": fmt.Sprintf("http://%s:%d", "prometheus-instance", prometheusDefaultLocalPort),
		},
		Mounts: map[string]string{
			filepath.Join(dir, "grafana-datasources.yml"):      grafanaProvisioningDir + "datasources/datasource.yml",
			filepath.Join(dir, "grafana-dashboards.yml"):       grafanaProvisioningDir + "dashboards/dashboard.yml",
			filepath.Join(dir, "prometheus-dashboard.json"):    grafanaProvisioningDir + "dashboards/prometheus-dashboard.json",
			filepath.Join(dir, "node-exporter-dashboard.json"): grafanaProvisioningDir + "dashboards/node-exporter-dashboard.json",
		},
		//TODO: Use user-defined network instead of link
		Links: []string{prometheusInstanceName},
		PortMappings: map[int]int{
			grafanaDefaultLocalPort: port,
		},
	}
}

func main() {
	configPath := flag.String("config-dir", ".", "Relative dir path with config files.")
	prometheusPort := flag.Int("prometheus-port", 9091, "Port Prometheus listens to.")
	grafanaPort := flag.Int("grafana-port", 3001, "Port Prometheus listens to.")
	flag.Parse()

	configAbsPath, err := filepath.Abs(*configPath)
	if err != nil {
		panic(err)
	}

	runner, err := monitoring.DockerContainerRunner()
	if err != nil {
		panic(err)
	}

	err = runner.Start(prometheusParams(configAbsPath, *prometheusPort), runOpts)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Infof("Prometheus client running on http://localhost:%d", *prometheusPort)

	err = runner.Start(grafanaParams(configAbsPath, *grafanaPort), runOpts)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Infof("Grafana running on http://localhost:%d (user: admin, pass: admin)", *grafanaPort)
}
