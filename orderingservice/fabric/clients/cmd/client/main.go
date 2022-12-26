package main

import (
	"fmt"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecarclient"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {
	clients.SetEnvVars()
	defaults := clients.GetDefaultSecurityOpts()

	config.ParseFlags()

	c := sidecarclient.ReadConfig()

	profile := workload.LoadProfileFromYaml(c.Profile)

	opts := &sidecarclient.ClientInitOptions{
		CommitterEndpoint:    c.Committer,
		SidecarEndpoint:      c.Sidecar,
		OrdererSecurityOpts:  defaults,
		ChannelID:            c.ChannelID,
		OrdererEndpoints:     c.Orderers,
		Parallelism:          c.Parallelism,
		InputChannelCapacity: c.InputChannelCapacity,
	}

	tracker := workload.NewMetricTracker(c.Prometheus)

	publicKey, _, txs := workload.StartTxGenerator(&profile.Transaction, 100)

	client, err := sidecarclient.NewClient(opts)
	utils.Must(err)
	defer func() {
		utils.Must(client.Close())
	}()

	utils.Must(client.SetCommitterKey(publicKey))

	done := make(chan struct{})
	client.StartListening(func(block *common.Block) {
		tracker.ResponseReceived(coordinatorservice.Status_VALID, len(block.Data.Data))
		fmt.Printf("Block received %d:%d\n", block.Header.Number, len(block.Data.Data))
	}, func(err error) {
		close(done)
	})

	client.SendReplicated(func() (*token.Tx, bool) {
		tracker.RequestSent(1)
		tx, ok := <-txs
		return tx, ok
	}).Wait()

	<-done
}
