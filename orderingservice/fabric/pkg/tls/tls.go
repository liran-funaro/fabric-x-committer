package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"

	"google.golang.org/grpc/credentials"
)

func LoadTLSCredentials(certs []string) (credentials.TransportCredentials, error) {
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
