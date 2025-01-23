package broadcastdeliver

import (
	cryptotls "crypto/tls"
	"net/http"

	"github.com/hyperledger/fabric-lib-go/bccsp/factory"
	"github.com/hyperledger/fabric/msp"
	mspmgmt "github.com/hyperledger/fabric/msp/mgmt"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/tls"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func GetOrdererConnectionCreds(config *OrdererConnectionProfile) (credentials.TransportCredentials, msp.SigningIdentity) {
	tlsCreds := insecure.NewCredentials()
	if config == nil {
		logger.Infof("Returning empty creds")
		return tlsCreds, nil
	}
	logger.Infof("Initialize creds:\n"+
		"\tMSP Dir: %s\n"+
		"\tMSP ID: %s\n"+
		"\tRoot CA Paths: %v\n"+
		"\tBCCSP: %v\n", config.MSPDir, config.MspID, config.RootCAPaths, config.BCCSP)

	var signer msp.SigningIdentity
	if config.MspID == "" && config.MSPDir == "" {
		logger.Infof("MspID and MSPDir not set, skipping signer initialization")
	} else {
		mspConfig, err := msp.GetLocalMspConfig(config.MSPDir, config.BCCSP, config.MspID)
		if err != nil {
			logger.Fatalf("Failed to load MSP config: %s", err)
		}
		err = mspmgmt.GetLocalMSP(factory.GetDefault()).Setup(mspConfig)
		if err != nil { // Handle errors reading the config file
			logger.Fatalf("failed to initialize local MSP: %v", err)
		}

		signer, err = mspmgmt.GetLocalMSP(factory.GetDefault()).GetDefaultSigningIdentity()
		if err != nil {
			logger.Fatalf("failed to load local signing identity: %v", err)
		}
	}

	var tlsConfig *cryptotls.Config
	var err error
	if len(config.RootCA) > 0 {
		tlsConfig, err = tls.LoadTLSCredentialsRaw(config.RootCA)
		if err != nil {
			logger.Fatalf("cannot load TLS credentials: %v", err)
		}
	}
	if len(config.RootCAPaths) > 0 {
		tlsConfig, err = tls.LoadTLSCredentials(config.RootCAPaths)
		if err != nil {
			logger.Fatalf("cannot load TLS credentials: %v", err)
		}
	}
	if tlsConfig != nil {
		tlsCreds = credentials.NewTLS(tlsConfig)
	}
	return tlsCreds, signer
}

func SecureClient(rootCAPaths ...string) *http.Client {
	tlsConfig, err := tls.LoadTLSCredentials(rootCAPaths)
	if err != nil {
		logger.Fatalf("cannot load TLS credentials: %s", err)
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
}
