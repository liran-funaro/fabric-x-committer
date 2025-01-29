package configtempl

import (
	"bytes"
	"html/template"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/fabricx-config/core/config/configtest"
	"github.ibm.com/decentralized-trust-research/fabricx-config/internaltools/configtxgen"
	"github.ibm.com/decentralized-trust-research/fabricx-config/internaltools/configtxgen/genesisconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
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
	}

	// ConfigBlock represents the configuration of the config block.
	ConfigBlock struct {
		ChannelID                    string
		OrdererEndpoints             []*connection.OrdererEndpoint
		MetaNamespaceVerificationKey []byte
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

// CreateConfigBlock writes a config block to file.
func CreateConfigBlock(t *testing.T, conf *ConfigBlock) string {
	blockDir := t.TempDir()

	configBlock := genesisconfig.Load(genesisconfig.SampleFabricX, configtest.GetDevConfigDir())
	tlsCertPath := filepath.Join(configtest.GetDevConfigDir(), "msp", "tlscacerts", "tlsroot.pem")
	for _, consenter := range configBlock.Orderer.ConsenterMapping {
		consenter.Identity = tlsCertPath
		consenter.ClientTLSCert = tlsCertPath
		consenter.ServerTLSCert = tlsCertPath
	}

	metaPubKeyPath := filepath.Join(blockDir, "meta.pem")
	if conf.MetaNamespaceVerificationKey == nil {
		conf.MetaNamespaceVerificationKey, _ = workload.NewHashSignerVerifier(&workload.SignatureProfile{
			Scheme: signature.Ecdsa,
			Seed:   rand.Int64(),
		}).GetVerificationKeyAndSigner()
	}
	require.NoError(t, os.WriteFile(metaPubKeyPath, conf.MetaNamespaceVerificationKey, 0o600))
	configBlock.Application.MetaNamespaceVerificationKeyPath = metaPubKeyPath

	require.GreaterOrEqual(t, len(configBlock.Orderer.Organizations), 1)
	sourceOrg := *configBlock.Orderer.Organizations[0]
	configBlock.Orderer.Organizations = nil

	orgMap := make(map[string]*[]string)
	for _, e := range conf.OrdererEndpoints {
		orgEndpoints, ok := orgMap[e.MspID]
		if !ok {
			org := sourceOrg
			org.ID = e.MspID
			org.Name = e.MspID
			org.OrdererEndpoints = nil
			configBlock.Orderer.Organizations = append(configBlock.Orderer.Organizations, &org)
			orgMap[e.MspID] = &org.OrdererEndpoints
			orgEndpoints = &org.OrdererEndpoints
		}
		*orgEndpoints = append(*orgEndpoints, e.String())
	}

	channelID := conf.ChannelID
	if channelID == "" {
		channelID = "chan"
	}

	configBlockPath := filepath.Join(blockDir, "config.block")
	require.NoError(t, configtxgen.DoOutputBlock(configBlock, channelID, configBlockPath))
	return configBlockPath
}
