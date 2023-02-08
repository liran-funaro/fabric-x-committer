package connection

import (
	"fmt"
	"net/http"
	"os"

	"github.com/hyperledger/fabric/bccsp/factory"
	"github.com/hyperledger/fabric/msp"
	mspmgmt "github.com/hyperledger/fabric/msp/mgmt"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/tls"
	"google.golang.org/grpc/credentials"
	"gopkg.in/yaml.v3"
)

func GetDefaultSecurityOpts(credsPath, configPath, rootCAPath, localMspDir, localMspId string) (credentials.TransportCredentials, msp.SigningIdentity) {
	fmt.Printf("Initialize creds:"+
		"\tMSP Dir: %s\n"+
		"\tMSP ID: %s\n"+
		"\tCreds Dir: %s/msp\n"+
		"\tRoot CA Path: %s\n"+
		"\tConfig Path: %s/orderer.yaml\n", localMspDir, localMspId, credsPath, rootCAPath, configPath)

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

	tlsConfig, err := tls.LoadTLSCredentials([]string{rootCAPath})
	if err != nil {
		fmt.Println("cannot load TLS credentials: :", err)
		os.Exit(0)
	}

	return credentials.NewTLS(tlsConfig), signer
}

func GetOrdererConnectionCreds(config OrdererConnectionProfile) (credentials.TransportCredentials, msp.SigningIdentity) {
	fmt.Printf("Initialize creds:\n"+
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
		fmt.Println("Failed to initialize local MSP:", err)
		os.Exit(0)
	}

	signer, err := mspmgmt.GetLocalMSP(factory.GetDefault()).GetDefaultSigningIdentity()
	if err != nil {
		fmt.Println("Failed to load local signing identity:", err)
		os.Exit(0)
	}

	tlsConfig, err := tls.LoadTLSCredentials(config.RootCAPaths)
	if err != nil {
		fmt.Println("cannot load TLS credentials: :", err)
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
	RootCAPaths []string             `yaml:"RootCAPaths"`
	MSPDir      string               `yaml:"MSPDir"`
	MSPID       string               `yaml:"MSPID"`
	BCCSP       *factory.FactoryOpts `yaml:"BCCSP"`
}

func ReadConnectionProfile(filePath string) OrdererConnectionProfile {
	content, err := utils.ReadFile(filePath)
	if err != nil {
		panic(err)
	}
	var profile OrdererConnectionProfile
	utils.Must(yaml.Unmarshal(content, &profile))
	return profile
}
