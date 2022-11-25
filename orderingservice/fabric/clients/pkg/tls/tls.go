package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"

	"google.golang.org/grpc/credentials"
	"gopkg.in/yaml.v2"
)

func LoadTLSCredentials() (credentials.TransportCredentials, error) {

	certsString := os.Getenv("ORDERER_GENERAL_TLS_ROOTCAS")
	if certsString == "" {
		return nil, fmt.Errorf("cannot load ORDERER_GENERAL_TLS_ROOTCAS")
	}

	var certs []string
	err := yaml.Unmarshal([]byte(certsString), &certs)
	if err != nil {
		return nil, err
	}

	if len(certs) < 1 {
		return nil, fmt.Errorf("no ROOT CAS")

	}

	// Load certificate of the CA who signed server's certificate
	pemServerCA, err := ioutil.ReadFile(certs[0])
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(pemServerCA) {
		return nil, fmt.Errorf("failed to add server CA's certificate")
	}

	// Create the credentials and return it
	config := &tls.Config{
		RootCAs: certPool,
	}

	return credentials.NewTLS(config), nil
}
