package configtempl

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"time"
)

type (
	// OrdererConfig represents the configuration of the orderer process.
	OrdererConfig struct {
		ServerEndpoint string
		BlockSize      uint64
		BlockTimeout   time.Duration
	}

	// CommonEndpoints holds commonly used endpoints across services.
	CommonEndpoints struct {
		ServerEndpoint  string
		MetricsEndpoint string
		LatencyEndpoint string
	}

	// SigVerifierConfig represents the configuration of the sigverifier process.
	SigVerifierConfig struct {
		CommonEndpoints
	}

	// QueryServiceOrVCServiceConfig represents either the configuration of a query-service or a vc service.
	QueryServiceOrVCServiceConfig struct {
		CommonEndpoints
		DatabaseHost string
		DatabasePort int
		DatabaseName string
	}

	// CoordinatorConfig represents the configuration of the coordinator process.
	CoordinatorConfig struct {
		CommonEndpoints
		SigVerifierEndpoints []string
		VCServiceEndpoints   []string
	}

	// SidecarConfig represents the configuration of the sidecar process.
	SidecarConfig struct {
		CommonEndpoints
		OrdererEndpoint     string
		CoordinatorEndpoint string
		ChannelID           string
		LedgerPath          string
	}

	// LoadGenConfig represents the configuration of the load generator.
	LoadGenConfig struct {
		CommonEndpoints
		OrdererEndpoints    []string
		CoordinatorEndpoint string
		SidecarEndpoint     string
		ChannelID           string
		BlockSize           uint64
	}
)

// CreateConfigFile creates a configuration file by applying the given config on a template.
func CreateConfigFile(config any, templateFilePath, outputFilePath string) error {
	tmpl, err := template.ParseFiles(templateFilePath)
	if err != nil {
		return err
	}

	var renderedConfig bytes.Buffer
	if err = tmpl.Execute(&renderedConfig, config); err != nil {
		return err
	}

	outputFile, err := os.Create(outputFilePath) // nolint:gosec
	if err != nil {
		return err
	}
	defer func() {
		_ = outputFile.Close()
	}()

	fmt.Println(renderedConfig.String())
	_, err = outputFile.Write(renderedConfig.Bytes())
	return err
}
