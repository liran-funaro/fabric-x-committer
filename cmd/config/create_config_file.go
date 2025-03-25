package config

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/fabricx-config/internaltools/configtxgen"

	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type (
	// OrdererConfig represents the configuration of the orderer process.
	OrdererConfig struct {
		ServerEndpoint  string
		BlockSize       uint64
		BlockTimeout    time.Duration
		ConfigBlockPath string
	}

	// CommonEndpoints holds commonly used endpoints across services.
	CommonEndpoints struct {
		ServerEndpoint  string
		MetricsEndpoint string
	}

	// SigVerifierConfig represents the configuration of the sigverifier process.
	SigVerifierConfig struct {
		CommonEndpoints
	}

	// QueryServiceOrVCServiceConfig represents either the configuration of a query-service or a vc service.
	QueryServiceOrVCServiceConfig struct {
		CommonEndpoints
		DatabaseEndpoints []*connection.Endpoint
		DatabaseName      string
		LoadBalance       bool
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
		ConfigBlockPath     string
	}

	// LoadGenConfig represents the configuration of the load generator.
	LoadGenConfig struct {
		CommonEndpoints
		OrdererEndpoints    []string
		CoordinatorEndpoint string
		SidecarEndpoint     string
		ChannelID           string
		BlockSize           uint64
		Policy              *workload.PolicyProfile
	}

	// ConfigBlock represents the configuration of the config block.
	ConfigBlock = workload.ConfigBlock
)

// CreateConfigFile creates a configuration file by applying the given config on a template.
func CreateConfigFile(t *testing.T, config any, templateFilePath, outputFilePath string) {
	t.Helper()
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

// CreateConfigBlock create and writes a config block to file.
func CreateConfigBlock(t *testing.T, conf *ConfigBlock) string {
	t.Helper()
	block, err := workload.CreateDefaultConfigBlock(conf)
	require.NoError(t, err)
	return WriteConfigBlock(t, block)
}

// WriteConfigBlock writes a config block to file.
func WriteConfigBlock(t *testing.T, block *common.Block) string {
	t.Helper()
	blockDir := t.TempDir()
	configBlockPath := filepath.Join(blockDir, "config.block")
	require.NoError(t, configtxgen.WriteOutputBlock(block, configBlockPath))
	return configBlockPath
}
