package connection

import (
	"fmt"
	"net/http"
	"os"

	"github.com/hyperledger/fabric/bccsp/factory"
	"github.com/hyperledger/fabric/msp"
	mspmgmt "github.com/hyperledger/fabric/msp/mgmt"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/tls"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func GetOrdererConnectionCreds(config *OrdererConnectionProfile) (credentials.TransportCredentials, msp.SigningIdentity) {
	if config == nil {
		logger.Infof("Returning empty creds")
		return insecure.NewCredentials(), nil
	}
	logger.Infof("Initialize creds:\n"+
		"\tMSP Dir: %s\n"+
		"\tMSP ID: %s\n"+
		"\tRoot CA Paths: %v\n"+
		"\tBCCSP: %v\n", config.MSPDir, config.MSPID, config.RootCAPaths, config.BCCSP)
	mspConfig, err := msp.GetLocalMspConfig(config.MSPDir, config.BCCSP, config.MSPID)
	if err != nil {
		fmt.Println("Failed to load MSP config:", err)
		os.Exit(0)
	}
	err = mspmgmt.GetLocalMSP(factory.GetDefault()).Setup(mspConfig)
	if err != nil { // Handle errors reading the config file
		logger.Errorf("failed to initialize local MSP: %v", err)
		os.Exit(0)
	}

	signer, err := mspmgmt.GetLocalMSP(factory.GetDefault()).GetDefaultSigningIdentity()
	if err != nil {
		logger.Errorf("failed to load local signing identity: %v", err)
		os.Exit(0)
	}

	tlsConfig, err := tls.LoadTLSCredentials(config.RootCAPaths)
	if err != nil {
		logger.Errorf("cannot load TLS credentials: %v", err)
		os.Exit(0)
	}

	return credentials.NewTLS(tlsConfig), signer
}

func SecureClient(rootCAPaths ...string) *http.Client {
	tlsConfig, err := tls.LoadTLSCredentials(rootCAPaths)
	if err != nil {
		fmt.Println("cannot load TLS credentials: :", err)
		os.Exit(0)
	}

	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
}

type OrdererConnectionProfile struct {
	//RootCAPaths The path to the root CAs for the orderers
	RootCAPaths []string             `mapstructure:"root-ca-paths"`
	MSPDir      string               `mapstructure:"msp-dir"`
	MSPID       string               `mapstructure:"msp-id"`
	BCCSP       *factory.FactoryOpts `mapstructure:"bccsp"`
}
