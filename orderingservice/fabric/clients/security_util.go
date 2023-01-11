package clients

import (
	"fmt"
	"os"

	"github.com/hyperledger/fabric/bccsp/factory"
	"github.com/hyperledger/fabric/msp"
	mspmgmt "github.com/hyperledger/fabric/msp/mgmt"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/pkg/tls"
	"google.golang.org/grpc/credentials"
)

var (
	projectPath       = os.Getenv("GOPATH") + "/src/github.com/decentralized-trust-research/scalable-committer/orderingservice/fabric"
	DefaultOutPath    = projectPath + "/out"
	DefaultConfigPath = projectPath + "/testdata"
)

const (
	localMspId  = "Org1"
	localMspDir = "/msp"
	rootCAPath  = "/ca.crt"
)

func GetDefaultSecurityOpts(credsPath, configPath string) (credentials.TransportCredentials, msp.SigningIdentity) {
	os.Setenv("FABRIC_CFG_PATH", configPath)
	os.Setenv("ORDERER_GENERAL_TLS_ENABLED", "true")
	//os.Setenv("ORDERER_GENERAL_LOCALMSPID", "Org1")
	//os.Setenv("ORDERER_GENERAL_LOCALMSPDIR", credsPath+"/msp")
	//os.Setenv("ORDERER_GENERAL_TLS_ROOTCAS", "["+credsPath+"/ca.crt]")
	//os.Setenv("ORDERER_GENERAL_LISTENADDRESS", "localhost")
	//os.Setenv("ORDERER_GENERAL_TLS_PRIVATEKEY", peerPath+"/tls/client.key")
	//os.Setenv("ORDERER_GENERAL_TLS_CERTIFICATE", peerPath+"/tls/client.crt")

	// Load local MSP
	conf, err := localconfig.Load()
	if err != nil {
		fmt.Println("failed to load config:", err)
		os.Exit(1)
	}
	mspConfig, err := msp.GetLocalMspConfig(credsPath+localMspDir, conf.General.BCCSP, localMspId)
	if err != nil {
		fmt.Println("Failed to load MSP config:", err)
		os.Exit(0)
	}
	err = mspmgmt.GetLocalMSP(factory.GetDefault()).Setup(mspConfig)
	if err != nil { // Handle errors reading the config file
		fmt.Println("Failed to initialize local MSP:", err)
		os.Exit(0)
	}

	signer, err := mspmgmt.GetLocalMSP(factory.GetDefault()).GetDefaultSigningIdentity()
	if err != nil {
		fmt.Println("Failed to load local signing identity:", err)
		os.Exit(0)
	}

	tlsCredentials, err := tls.LoadTLSCredentials([]string{credsPath + rootCAPath})
	if err != nil {
		fmt.Println("cannot load TLS credentials: :", err)
		os.Exit(0)
	}

	return tlsCredentials, signer
}
