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
)

var logger = logging.New("sigverification-client")

type ClientConfig struct {
	Connections []*connection.DialConfig
	Input       sigverification_test.InputGeneratorParams
}

var clientConfig ClientConfig

func main() {
	var endpoints []*connection.Endpoint
	connection.EndpointVars(&endpoints, "servers", []*connection.Endpoint{{Host: "localhost", Port: config.DefaultGRPCPortSigVerifier}}, "Server host to connect to")
	test.DistributionVar(&clientConfig.Input.InputDelay, "input-delay", test.ClientInputDelay, "Interval between two batches are sent")

	test.DistributionVar(&clientConfig.Input.RequestBatch.BatchSize, "request-batch-size", sigverification_test.BatchSizeDistribution, "Request batch size")
	signature.SchemeVar(&clientConfig.Input.RequestBatch.Tx.Scheme, "scheme", sigverification_test.VerificationScheme, "Verification scheme")
	flag.Float64Var(&clientConfig.Input.RequestBatch.Tx.ValidSigRatio, "valid-sig-ratio", sigverification_test.SignatureValidRatio, "Percentage of transactions that should be valid (values from 0 to 1)")
	test.DistributionVar(&clientConfig.Input.RequestBatch.Tx.TxSize, "tx-size", sigverification_test.TxSizeDistribution, "How many serial numbers are in each TX")
	test.DistributionVar(&clientConfig.Input.RequestBatch.Tx.SerialNumberSize, "sn-size", sigverification_test.SerialNumberSize, "How many bytes contains each serial number")

	verificationKeyPath := flag.String("verification-verificationKey", "./key.pub", "Path to the verification verificationKey")
	signingKeyPath := flag.String("signing-verificationKey", "./key.priv", "Path to the signing verificationKey")

	config.ParseFlags()

	clientConfig.Connections = make([]*connection.DialConfig, len(endpoints))
	for i, endpoint := range endpoints {
		clientConfig.Connections[i] = connection.NewDialConfig(*endpoint)
	}

	signingKey, verificationKey, err := readOrGenerateKeys(*verificationKeyPath, *signingKeyPath)
	if err != nil {
		logger.Fatal(err)
	}

	clientConfig.Input.RequestBatch.Tx.SigningKey = signingKey

	requests := make(chan *sigverification.RequestBatch, len(clientConfig.Connections))
	for _, conn := range clientConfig.Connections {
		stream, err := startStream(conn, verificationKey)
		if err != nil {
			logger.Fatal(err)
		}
		go func() {
			for {
				maybe(stream.Recv())
			}
		}()
		go func() {
			for {
				utils.Must(stream.Send(<-requests))
			}
		}()
	}

	inputGenerator := sigverification_test.NewInputGenerator(&clientConfig.Input)
	for {
		requests <- inputGenerator.NextRequestBatch()
	}
}

func maybe(_ interface{}, err error) {
	if err != nil {
		logger.Error(err)
	}
}

func startStream(conn *connection.DialConfig, verificationKey signature.PublicKey) (sigverification.Verifier_StartStreamClient, error) {
	clientConnection, _ := connection.Connect(conn)
	client := sigverification.NewVerifierClient(clientConnection)

	_, err := client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	if err != nil {
		return nil, err
	}
	logger.Infoln("Set verification verificationKey")

	stream, err := client.StartStream(context.Background())
	if err != nil {
		return nil, err
	}
	logger.Infof("Started stream to %s", conn.Address())
	return stream, nil
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
