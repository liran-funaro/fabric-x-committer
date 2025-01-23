package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func LoadTLSCredentials(certPaths []string) (*tls.Config, error) {
	certs := make([][]byte, len(certPaths))
	var err error
	for i, p := range certPaths {
		// Load certificate of the CA who signed server's certificate
		certs[i], err = os.ReadFile(p)
		if err != nil {
			return nil, err
		}
	}
	return LoadTLSCredentialsRaw(certs)
}

func LoadTLSCredentialsRaw(certs [][]byte) (*tls.Config, error) {
	if len(certs) < 1 {
		return nil, fmt.Errorf("no ROOT CAS")
	}

	certPool := x509.NewCertPool()
	for _, cert := range certs {
		if !certPool.AppendCertsFromPEM(cert) {
			return nil, fmt.Errorf("failed to add server CA's certificate")
		}
	}

	// Create the credentials and return it
	return &tls.Config{RootCAs: certPool}, nil
}
