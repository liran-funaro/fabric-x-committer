package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/pkg/errors"
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

	endpoint := getEnv("SC_COORDINATOR_ENDPOINT")
	pubKeyPath := getEnv("SC_COORDINATOR_PUBKEY_PATH")

	pemContent, err := os.ReadFile(pubKeyPath)
	utils.Must(err)

	pubBytes, err := getSerializedKeyFromCert(pemContent)
	utils.Must(err)

	cl := client.OpenCoordinatorAdapter(*connection.CreateEndpoint(endpoint))
	utils.Must(cl.SetVerificationKey(pubBytes))
}
