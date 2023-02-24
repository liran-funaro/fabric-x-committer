package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/crypto"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
)

func getSerializedKeyFromCert(pemBytes []byte) ([]byte, error) {
	block, _ := pem.Decode(pemBytes)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse cert")
	}

	if cert.PublicKeyAlgorithm != x509.ECDSA {
		return nil, fmt.Errorf("pubkey not ECDSA")
	}

	pubBytes, err := crypto.SerializeVerificationKey(cert.PublicKey.(*ecdsa.PublicKey))
	if err != nil {
		return nil, errors.Wrap(err, "cannot serialize ecdsa pub key")
	}

	return pubBytes, nil
}

func getEnv(key string) string {
	v, exists := os.LookupEnv(key)
	if !exists {
		fmt.Printf("%s not set\n", key)
		os.Exit(1)
	}

	return v
}

func main() {
	var (
		coordinatorEndpoint connection.Endpoint
		keyPath             string
	)
	connection.EndpointVar(&coordinatorEndpoint, "coordinator", connection.Endpoint{"0.0.0.0", 5002}, "Coordinator endpoint.")
	pflag.StringVar(&keyPath, "key-path", "./sc_pubkey.pem", "The path to the public key to set to the committer.")
	pflag.CommandLine.AddGoFlag(flag.CommandLine.Lookup("coordinator"))
	pflag.Parse()

	fmt.Printf("Starting setup helper:\n\tCoordinator: %s\n\tKey path: %s\n", coordinatorEndpoint.Address(), keyPath)
	pemContent, err := os.ReadFile(keyPath)
	utils.Must(err)

	pubBytes, err := getSerializedKeyFromCert(pemContent)
	utils.Must(err)

	fmt.Println("Successfully retrieved public key from path.")
	cl := client.OpenCoordinatorAdapter(coordinatorEndpoint)
	fmt.Println("Successfully connected to coordinator.")
	utils.Must(cl.SetVerificationKey(pubBytes))
	fmt.Println("Successfully set public key to coordinator.")
}
