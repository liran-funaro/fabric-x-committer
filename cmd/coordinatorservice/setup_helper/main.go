package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/spf13/pflag"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

func main() {
	var (
		coordinatorEndpoint connection.Endpoint
		keyPath             string
		scheme              string
	)
	connection.EndpointVar(
		&coordinatorEndpoint,
		"coordinator",
		connection.Endpoint{Host: "0.0.0.0", Port: 5002},
		"Coordinator endpoint.",
	)
	pflag.StringVar(&keyPath, "key-path", "./sc_pubkey.pem", "The path to the public key to set to the committer.")
	pflag.StringVar(&scheme, "scheme", "ECDSA", "Signature scheme to use.")
	pflag.CommandLine.AddGoFlag(flag.CommandLine.Lookup("coordinator"))
	pflag.Parse()

	fmt.Printf(
		"Starting setup helper:\n\tCoordinator: %s\n\tKey path: %s\n\tScheme: %s\n",
		coordinatorEndpoint.Address(),
		keyPath,
		scheme,
	)
	pubBytes, err := signature.GetSerializedKeyFromCert(keyPath)
	utils.Must(err)

	fmt.Println("Successfully retrieved public key from path.")
	// NOTE: No unit test has been added for this setup_helper. This command
	//       is used to set the verification key in all-in-one test image.
	//       Once we have the code in place to process config block, we can
	//       remove this setup helper.
	utils.Must(setVerificationKey(coordinatorEndpoint, pubBytes, scheme))
	fmt.Println("Successfully set public key to coordinator.")
}

func setVerificationKey(endpoint connection.Endpoint, publicKey []byte, scheme string) error {
	clientConfig := connection.NewDialConfig(&endpoint)
	conn, err := connection.Connect(clientConfig)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := protocoordinatorservice.NewCoordinatorClient(conn)

	_, err = client.SetMetaNamespaceVerificationKey(ctx, &protosigverifierservice.Key{
		NsId:            uint32(types.MetaNamespaceID),
		SerializedBytes: publicKey,
		Scheme:          scheme,
	})
	return err
}
