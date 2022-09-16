package main

import (
	"fmt"
	"path/filepath"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/performance"
)

const prometheusDefaultLocalPort = 9090
const grafanaDefaultLocalPort = 3000
const grafanaProvisioningDir = "/etc/grafana/provisioning/"

//TODO: Extract to config
const prometheusMappedPort = 9091
const grafanaMappedPort = 3001

var logger = logging.New("dockerrunner")

var prometheusParams = &performance.DockerRunParams{
	Name:     "prometheus-instance",
	Image:    "prom/prometheus:latest",
	Hostname: "prometheus",
	Mounts: map[string]string{
		configFile("/prometheus.yml"): "/etc/prometheus/prometheus.yml",
	},
	PortMappings: map[int]int{
		prometheusDefaultLocalPort: prometheusMappedPort,
	},
}
var grafanaParams = &performance.DockerRunParams{
	Name:     "grafana-instance",
	Image:    "grafana/grafana:latest",
	Hostname: "grafana",
	Envs: map[string]string{
		"GF_AUTH_PROXY_ENABLED":   "true",
		"GF_PATHS_PROVISIONING":   grafanaProvisioningDir,
		"_GF_PROMETHEUS_ENDPOINT": fmt.Sprintf("http://%s:%d", "prometheus-instance", prometheusDefaultLocalPort),
	},
	Mounts: map[string]string{
		configFile("grafana-datasources.yml"):   grafanaProvisioningDir + "datasources/datasource.yml",
		configFile("grafana-dashboards.yml"):    grafanaProvisioningDir + "dashboards/dashboard.yml",
		configFile("prometheus-dashboard.json"): grafanaProvisioningDir + "dashboards/prometheus-dashboard.json",
	},
	//TODO: Use user-defined network instead of link
	Links: []string{prometheusParams.Name},
	PortMappings: map[int]int{
		grafanaDefaultLocalPort: grafanaMappedPort,
	},
}

func main() {
	runner, err := performance.DockerContainerRunner()
	if err != nil {
		panic(err)
	}

	err = runner.Start(prometheusParams)
	if err != nil {
		logger.Fatal(err)
	}

	logger.Infof("Prometheus client running on http://localhost:%d", prometheusMappedPort)

	err = runner.Start(grafanaParams)
	if err != nil {
		logger.Fatal(err)
	}
	logger.Infof("Grafana running on http://localhost:%d (user: admin, pass: admin)", grafanaMappedPort)
}

func configFile(filename string) string {
	return filepath.Join(config.DirPath, filename)
}
