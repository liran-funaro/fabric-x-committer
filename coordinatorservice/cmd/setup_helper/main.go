package main

import (
	"flag"
	"fmt"

	"github.com/spf13/pflag"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload/client"
)

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
	pubBytes, err := signature.GetSerializedKeyFromCert(keyPath)
	utils.Must(err)

	fmt.Println("Successfully retrieved public key from path.")
	cl := client.OpenCoordinatorAdapter(coordinatorEndpoint)
	fmt.Println("Successfully connected to coordinator.")
	utils.Must(cl.SetVerificationKey(pubBytes))
	fmt.Println("Successfully set public key to coordinator.")
}
