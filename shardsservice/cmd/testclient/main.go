package main

import (
	"flag"
	"time"

	connectionUtils "github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/cmd/testclient/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

var clientConfig connectionUtils.ClientConfig

func main() {
	var endpoints []*connection.Endpoint
	connection.EndpointVars(&endpoints, "servers", []*connection.Endpoint{{Host: "localhost", Port: config.DefaultGRPCPortShardsServer}}, "Server host to connect to")

	flag.IntVar(&clientConfig.NumShardsPerServer, "num-shards", 1, "Number of shards per shards server")

	test.DistributionVar(&clientConfig.Input.InputDelay, "input-delay", test.ClientInputDelay, "Interval between two batches are sent")
	flag.IntVar(&clientConfig.Input.BlockCount, "block-count", -1, "Total blocks for the experiment")
	flag.IntVar(&clientConfig.Input.BlockSize, "block-size", test.BatchSize, "TXs per block")
	flag.BoolVar(&clientConfig.Input.SignatureBytes, "signature-bytes", false, "Add signature bytes")
	flag.IntVar(&clientConfig.Input.TxSize, "tx-size", test.TxSize, "SNs per TX")

	flag.Parse()

	clientConfig.Connections = make([]*connection.DialConfig, len(endpoints))
	for i, endpoint := range endpoints {
		clientConfig.Connections[i] = connection.NewDialConfig(*endpoint)
	}

	client, err := connectionUtils.NewClient(clientConfig)
	utils.Must(err)
	utils.Every(time.Second, client.LogDebug)
	client.Start()
	client.WaitUntilDone()
	client.LogDebug()
}
