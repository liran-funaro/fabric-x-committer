package main

import (
	"fmt"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/sidecarclient"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {
	config.String("orderer-creds-path", "sidecar-client.creds-path", "The path to the output folder containing the root CA and the client credentials.")
	config.String("orderer-config-path", "sidecar-client.config-path", "The path to the output folder containing the orderer config.")
	config.ParseFlags()

	c := sidecarclient.ReadConfig()

	creds, signer := connection.GetDefaultSecurityOpts(c.CredsPath, c.ConfigPath)

	opts := &sidecarclient.ClientInitOptions{
		CommitterEndpoint:    c.Committer,
		SidecarEndpoint:      c.Sidecar,
		Credentials:          creds,
		Signer:               signer,
		ChannelID:            c.ChannelID,
		OrdererEndpoints:     c.Orderers,
		Parallelism:          c.Parallelism,
		InputChannelCapacity: c.InputChannelCapacity,
		SignedEnvelopes:      c.SignedEnvelopes,
	}

	tracker := workload.NewMetricTracker(c.Prometheus)

	client, err := sidecarclient.NewClient(opts)
	utils.Must(err)

	var txs chan *token.Tx
	if len(c.Profile) > 0 {
		profile := workload.LoadProfileFromYaml(c.Profile)
		publicKey, _, txCh := workload.StartTxGenerator(&profile.Transaction, 100)
		utils.Must(client.SetCommitterKey(publicKey))
		txs = txCh
	} else {
		txs = make(chan *token.Tx)
	}

	done := make(chan struct{})
	client.StartListening(func(block *common.Block) {
		tracker.ResponseReceived(coordinatorservice.Status_VALID, len(block.Data.Data))
		fmt.Printf("Block received %d:%d\n", block.Header.Number, len(block.Data.Data))
	}, func(err error) {
		close(done)
	})

	client.SendReplicated(txs, func() { tracker.RequestSent(1) })

	<-done
}
