package main

import (
	"context"
	"flag"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc/credentials/insecure"
)

var logger = logging.New("sigverification-client")

type ClientConfig struct {
	Connection connection.DialConfig
	Input      sigverification_test.InputGeneratorParams
}

var clientConfig ClientConfig

func main() {
	clientConfig.Connection.Credentials = insecure.NewCredentials()
	flag.StringVar(&clientConfig.Connection.Host, "host", "localhost", "Server host to connect to")
	flag.IntVar(&clientConfig.Connection.Port, "port", config.DefaultGRPCPortSigVerifier, "Server port to connect to")

	test.DistributionVar(&clientConfig.Input.InputDelay, "input-delay", test.NoDelay, "Interval between two batches are sent")

	test.DistributionVar(&clientConfig.Input.RequestBatch.BatchSize, "request-batch-size", test.Constant(100), "Request batch size")
	signature.SchemeVar(&clientConfig.Input.RequestBatch.Tx.Scheme, "scheme", signature.Ecdsa, "Verification scheme")
	flag.Float64Var(&clientConfig.Input.RequestBatch.Tx.ValidSigRatio, "valid-sig-ratio", test.Always, "Percentage of transactions that should be valid (values from 0 to 1)")
	test.DistributionVar(&clientConfig.Input.RequestBatch.Tx.TxSize, "tx-size", test.Constant(1), "How many serial numbers are in each TX")
	test.DistributionVar(&clientConfig.Input.RequestBatch.Tx.SerialNumberSize, "sn-size", test.Constant(64), "How many bytes contains each serial number")

	config.ParseFlags()

	//TODO: Read key from file, so that multiple clients use the same keys for signing
	inputGenerator := sigverification_test.NewInputGenerator(&clientConfig.Input)

	clientConnection, _ := connection.Connect(&clientConfig.Connection)
	client := sigverification.NewVerifierClient(clientConnection)

	_, err := client.SetVerificationKey(context.Background(), inputGenerator.PublicKey())
	if err != nil {
		logger.Errorf("Failed to set verification key: %v\n", err)
		panic(err)
	}
	logger.Infoln("Set verification key")

	stream, err := client.StartStream(context.Background())
	if err != nil {
		logger.Errorf("Failed to start stream: %v\n", err)
		panic(err)
	}
	logger.Infoln("Started stream")

	go handleResponses(stream)

	produceRequests(inputGenerator, stream)
}

func produceRequests(inputGenerator *sigverification_test.InputGenerator, stream sigverification.Verifier_StartStreamClient) {
	for {
		batch := inputGenerator.NextRequestBatch()
		err := stream.Send(batch)
		logger.Debugf("Sent request: %d\n", len(batch.Requests))
		if err != nil {
			panic(err)
		}
	}
}

func handleResponses(stream sigverification.Verifier_StartStreamClient) {
	for {
		response, err := stream.Recv()
		if err != nil {
			logger.Errorf("Error occurred: %v\n", err)
		} else {
			valid := 0
			for _, r := range response.Responses {
				if r.IsValid {
					valid++
				}
			}
			logger.Debugf("Returned %d valid and %d invalid responses\n", valid, len(response.Responses)-valid)
		}
	}
}
