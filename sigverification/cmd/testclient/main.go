package main

import (
	"context"
	"flag"
	"github.com/pkg/errors"
	"os"

	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/sigverification"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload"
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
	test.DistributionVar(&clientConfig.Input.RequestBatch.Tx.SerialNumberCount, "sn-count", sigverification_test.SerialNumberCountDistribution, "How many serial numbers are in each TX")
	test.DistributionVar(&clientConfig.Input.RequestBatch.Tx.SerialNumberSize, "sn-size", sigverification_test.SerialNumberSize, "How many bytes each serial number contains")
	test.DistributionVar(&clientConfig.Input.RequestBatch.Tx.OutputCount, "output-count", sigverification_test.OutputCountDistribution, "How many are in each TX")
	test.DistributionVar(&clientConfig.Input.RequestBatch.Tx.OutputSize, "output-size", sigverification_test.OutputSize, "How many bytes each output contains")

	verificationKeyPath := flag.String("verification-verificationKey", "./key.pub", "Path to the verification verificationKey")
	signingKeyPath := flag.String("signing-verificationKey", "./key.priv", "Path to the signing verificationKey")
	removeOldKeys := flag.Bool("remove-old-keys", true, "Remove old keys and force generation of new keys")

	config.ParseFlagsWithoutConfig()

	clientConfig.Connections = make([]*connection.DialConfig, len(endpoints))
	for i, endpoint := range endpoints {
		clientConfig.Connections[i] = connection.NewDialConfig(*endpoint)
	}

	signatureProfile := signature.Profile{
		Scheme: clientConfig.Input.RequestBatch.Tx.Scheme,
		KeyPath: &signature.KeyPath{
			SigningKey:      *signingKeyPath,
			VerificationKey: *verificationKeyPath,
		},
	}
	if *removeOldKeys {
		logger.Infoln("Removing old keys if exist.")
		os.RemoveAll(*signingKeyPath)
		os.RemoveAll(*verificationKeyPath)
		logger.Infoln("Old keys removed.")
	}
	signingKey, verificationKey, err := sigverification_test.ReadOrGenerateKeys(signatureProfile)
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

	_, blocks := workload.StartBlockGenerator(&workload.Profile{
		Block:       workload.BlockProfile{-1, 100},
		Transaction: workload.TransactionProfile{workload.Always(2), workload.Always(1), signatureProfile},
	})
	for {
		b := <-blocks
		reqs := make([]*sigverification.Request, len(b.Block.Txs))
		for i, tx := range b.Block.Txs {
			reqs[i] = &sigverification.Request{
				BlockNum: b.Block.Number,
				TxNum:    uint64(i),
				Tx:       tx,
			}
		}
		requests <- &sigverification.RequestBatch{Requests: reqs}
	}
}

func maybe(_ interface{}, err error) {
	if err != nil {
		logger.Error(err)
	}
}

func startStream(conn *connection.DialConfig, verificationKey signature.PublicKey) (sigverification.Verifier_StartStreamClient, error) {
	clientConnection, err := connection.Connect(conn)
	if err != nil {
		return nil, errors.Wrap(err, "failed connecting to server")
	}
	logger.Infof("Connected to server %s", conn.Address())
	client := sigverification.NewVerifierClient(clientConnection)
	logger.Infoln("Created verifier client")

	_, err = client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	if err != nil {
		return nil, errors.Wrap(err, "failed setting verification key")
	}
	logger.Infoln("Set verification verificationKey")

	stream, err := client.StartStream(context.Background())
	if err != nil {
		return nil, errors.Wrap(err, "failed starting stream")
	}
	logger.Infof("Started stream to %s", conn.Address())
	return stream, nil
}
