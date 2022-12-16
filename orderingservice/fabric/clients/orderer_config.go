package clients

import (
	"fmt"
	"os"

	"github.com/hyperledger/fabric/bccsp/factory"
	"github.com/hyperledger/fabric/msp"
	mspmgmt "github.com/hyperledger/fabric/msp/mgmt"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients/pkg/tls"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const maxMsgSize = 100 * 1024 * 1024

func connect(endpoint connection.Endpoint, transportCredentials credentials.TransportCredentials) (*grpc.ClientConn, error) {
	var dialOpts []grpc.DialOption

	dialOpts = append(dialOpts, grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(maxMsgSize),
		grpc.MaxCallSendMsgSize(maxMsgSize),
	))

	dialOpts = append(dialOpts, grpc.WithTransportCredentials(transportCredentials))

	// let's connect to every ordering node
	conn, err := grpc.Dial(endpoint.Address(), dialOpts...)
	if err != nil {
		fmt.Println("Error connecting:", err)
		return nil, err
	}
	return conn, nil
}

type FabricOrdererConnectionOpts struct {
	ChannelID   string
	Endpoint    connection.Endpoint
	Credentials credentials.TransportCredentials
	Signer      msp.SigningIdentity
}

func GetDefaultConfigValues() *FabricOrdererConnectionOpts {
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

	return &FabricOrdererConnectionOpts{
		ChannelID:   "mychannel",
		Endpoint:    connection.Endpoint{Host: conf.General.ListenAddress, Port: int(conf.General.ListenPort)},
		Credentials: tlsCredentials,
		Signer:      signer,
	}
}
