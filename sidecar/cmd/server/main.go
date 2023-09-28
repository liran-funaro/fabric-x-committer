package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/aggregator"
	config2 "github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/coordinator"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/ledger"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

func main() {

	// TODO in the future we may want to use cobra to be consistent with other apps, i.e., coordinator service ...

	config.ServerConfig("sidecar")
	config.ParseFlags()

	c := config2.ReadConfig()

	// TODO get metrics back in place
	//m := monitoring.LaunchMonitoring(c.Monitoring, &metrics.Provider{}).(*metrics.Metrics)

	blockChan := make(chan *common.Block, 100)
	statusChan := make(chan *protocoordinatorservice.TxValidationStatusBatch, 100)
	coordinatorInputChan := make(chan *protoblocktx.Block, 100)

	ctx, cancel := context.WithCancel(context.Background())
	registerInterrupt(cancel)

	// start ledger service
	// that serves the block deliver api and receives completed blocks from the aggregator
	fmt.Printf("Create ledger service at %v\n", c.Server.Endpoint.Address())
	ledgerService := ledger.NewLedgerDeliverServer(c.Orderer.ChannelID, c.Ledger.Path)
	listener, err := net.Listen("tcp", c.Server.Endpoint.Address())
	utils.Must(err)
	grpcServer := grpc.NewServer(c.Server.Opts()...)
	peer.RegisterDeliverServer(grpcServer, ledgerService)
	serverErrChan := make(chan error)
	go func() {
		serverErrChan <- grpcServer.Serve(listener)
	}()

	// start orderer client that forwards blocks to aggregator
	fmt.Printf("Create orderer client and connect to %v\n", c.Orderer.Endpoint)
	creds, signer := connection.GetOrdererConnectionCreds(c.Orderer.OrdererConnectionProfile)
	ordererDialConfig := connection.NewDialConfigWithCreds(c.Orderer.Endpoint, creds)
	ordererConn, err := connection.Connect(ordererDialConfig)
	utils.Must(err)

	ordererClient := orderer.NewOrdererClient(ordererConn, signer, c.Orderer.ChannelID, int64(0))
	ordererErrChan, err := ordererClient.Start(ctx, blockChan)
	utils.Must(err)

	// start coordinator client
	// that forwards scBlocks to the coordinator and receives status batches from the coordinator
	fmt.Printf("Create coordinator client and connect to %v\n", c.Committer.Endpoint)
	coordinatorDialConfig := connection.NewDialConfig(c.Committer.Endpoint)
	coordinatorConn, err := connection.Connect(coordinatorDialConfig)
	utils.Must(err)

	coordinatorClient := coordinator.NewCoordinatorClient(coordinatorConn)
	coordinatorErrChan, err := coordinatorClient.Start(ctx, coordinatorInputChan, statusChan)
	utils.Must(err)

	// start aggregator
	fmt.Println("Start aggregator")
	agg := aggregator.New(
		// sendToCoordinator
		func(scBlock *protoblocktx.Block) {
			coordinatorInputChan <- scBlock
		},
		// output to ledger service
		func(block *common.Block) {
			ledgerService.Input() <- block
		},
	)

	aggErrChan := agg.Start(ctx, blockChan, statusChan)

	// run until the end
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-serverErrChan:
			utils.Must(err)
		case err := <-ordererErrChan:
			utils.Must(err)
		case err := <-coordinatorErrChan:
			utils.Must(err)
		case err := <-aggErrChan:
			utils.Must(err)
		}
	}
}

func registerInterrupt(cancel context.CancelFunc) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
		os.Exit(1)
	}()
}
