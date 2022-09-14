package main

import (
	"context"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

var logger = logging.New("sigverification-client")

type ClientConfig struct {
	Connection *utils.DialConfig
	Input      *sigverification_test.InputGeneratorParams
}

var defaultClientConfig = ClientConfig{
	Connection: utils.NewDialConfig(utils.Endpoint{
		Host: "localhost",
		Port: config.DefaultGRPCPortSigVerifier,
	}),
	Input: &sigverification_test.InputGeneratorParams{
		InputDelay: test.Constant(int64(3 * time.Second)),
		RequestBatch: &sigverification_test.RequestBatchGeneratorParams{
			Tx: &sigverification_test.TxGeneratorParams{
				Scheme:           signature.Ecdsa,
				ValidSigRatio:    test.Always,
				TxSize:           test.Constant(1),
				SerialNumberSize: test.Constant(64),
			},
			BatchSize: test.Stable(100),
		},
	},
}

func main() {
	//TODO: Use flags to overwrite the default input
	config := defaultClientConfig

	//TODO: Read key from file, so that multiple clients use the same keys for signing
	inputGenerator := sigverification_test.NewInputGenerator(config.Input)

	clientConnection, _ := utils.Connect(config.Connection)
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
