package clients

import (
	"fmt"
	"os"

	"github.com/hyperledger/fabric/bccsp/factory"
	"github.com/hyperledger/fabric/msp"
	mspmgmt "github.com/hyperledger/fabric/msp/mgmt"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/pkg/tls"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc/credentials"
)

var logger = logging.New("clients")

func GetDefaultSecurityOpts() (credentials.TransportCredentials, msp.SigningIdentity) {
	setEnvVars()

	conf, err := localconfig.Load()
	if err != nil {
		fmt.Println("failed to load config:", err)
		os.Exit(1)
	}

	// Load local MSP
	mspConfig, err := msp.GetLocalMspConfig(conf.General.LocalMSPDir, conf.General.BCCSP, conf.General.LocalMSPID)
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

	tlsCredentials, err := tls.LoadTLSCredentials()
	if err != nil {
		fmt.Println("cannot load TLS credentials: :", err)
		os.Exit(0)
	}

	return tlsCredentials, signer
}

//SetEnvVars is for testing only
//TODO: Remove
func setEnvVars() {
	goPath := os.Getenv("GOPATH")
	projectPath := goPath + "/src/github.com/decentralized-trust-research/scalable-committer/orderingservice/fabric"
	orgsPath := projectPath + "/out/orgs"
	peerPath := orgsPath + "/peerOrganizations/org1.com/users/User1@org1.com"
	ordererPath := orgsPath + "/ordererOrganizations/orderer.org/orderers/raft0.orderer.org"
	//os.Setenv("FABRIC_CFG_PATH", goPath+"/src/github.com/hyperledger/fabric")
	os.Setenv("FABRIC_CFG_PATH", projectPath)
	os.Setenv("ORDERER_GENERAL_LOCALMSPID", "Org1")
	os.Setenv("ORDERER_GENERAL_LOCALMSPDIR", peerPath+"/msp")
	os.Setenv("ORDERER_GENERAL_LISTENADDRESS", "localhost")
	os.Setenv("ORDERER_GENERAL_TLS_ENABLED", "true")
	os.Setenv("ORDERER_GENERAL_TLS_PRIVATEKEY", peerPath+"/tls/client.key")
	os.Setenv("ORDERER_GENERAL_TLS_CERTIFICATE", peerPath+"/tls/client.crt")
	os.Setenv("ORDERER_GENERAL_TLS_ROOTCAS", "["+ordererPath+"/tls/ca.crt]")
}
