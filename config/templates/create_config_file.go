package configtempl

import (
	"bytes"
	"html/template"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
		OrdererEndpoints    []string
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
func CreateConfigFile(t *testing.T, config any, templateFilePath, outputFilePath string) {
	tmpl, err := template.ParseFiles(templateFilePath)
	require.NoError(t, err)

	var renderedConfig bytes.Buffer
	require.NoError(t, tmpl.Execute(&renderedConfig, config))

	outputFile, err := os.Create(outputFilePath) // nolint:gosec
	require.NoError(t, err)
	defer func() {
		_ = outputFile.Close()
	}()

	_, err = outputFile.Write(renderedConfig.Bytes())
	require.NoError(t, err)
}
