package main

import (
	"context"
	"flag"
	"os"

	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
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

	verificationKeyPath := flag.String("verification-verificationKey", "./key.pub", "Path to the verification verificationKey")
	signingKeyPath := flag.String("signing-verificationKey", "./key.priv", "Path to the signing verificationKey")

	config.ParseFlags()

	signingKey, verificationKey, err := readOrGenerateKeys(*verificationKeyPath, *signingKeyPath)
	if err != nil {
		logger.Fatal(err)
	}

	clientConfig.Input.RequestBatch.Tx.SigningKey = signingKey

	inputGenerator := sigverification_test.NewInputGenerator(&clientConfig.Input)
	clientConnection, _ := connection.Connect(&clientConfig.Connection)
	client := sigverification.NewVerifierClient(clientConnection)

	_, err = client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	if err != nil {
		logger.Fatalf("Failed to set verification key: %v\n", err)
	}
	logger.Infoln("Set verification verificationKey")

	stream, err := client.StartStream(context.Background())
	if err != nil {
		logger.Fatalf("Failed to start stream: %v\n", err)
	}
	logger.Infoln("Started stream")

	go handleResponses(stream)

	produceRequests(inputGenerator, stream)
}

func readOrGenerateKeys(verificationKeyPath string, signingKeyPath string) (sigverification_test.PrivateKey, signature.PublicKey, error) {
	if !utils.FileExists(verificationKeyPath) || !utils.FileExists(signingKeyPath) {
		logger.Info("No verification/signing keys found in files %s/%s. Generating...", verificationKeyPath, signingKeyPath)
		signingKey, verificationKey := sigverification_test.GetSignatureFactory(clientConfig.Input.RequestBatch.Tx.Scheme).NewKeys()
		err := utils.WriteFile(verificationKeyPath, verificationKey)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "could not write public key into %s.", verificationKeyPath)
		}
		err = utils.WriteFile(signingKeyPath, signingKey)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "could not write private key into %s", signingKeyPath)
		}
		logger.Infoln("Keys successfully exported!")
		return signingKey, verificationKey, nil
	}

	logger.Infof("Verification/signing keys found in files %s/%s. Importing...", verificationKeyPath, signingKeyPath)
	verificationKey, err := os.ReadFile(verificationKeyPath)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "could not read public key from %s", verificationKeyPath)
	}
	signingKey, err := os.ReadFile(signingKeyPath)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "could not read private key from %s", signingKeyPath)
	}
	logger.Infoln("Keys successfully imported!")
	return signingKey, verificationKey, nil
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
