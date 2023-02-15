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
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	config.ServerConfig("sidecar")
	config.ParseFlags()

	c := sidecarclient.ReadConfig()
	p := connection.ReadConnectionProfile(c.OrdererConnectionProfile)
	creds, signer := connection.GetOrdererConnectionCreds(p)

	opts := &sidecarclient.ClientInitOptions{
		CommitterEndpoint: c.Committer,

		OrdererEndpoints:   c.Orderers,
		OrdererCredentials: creds,
		OrdererSigner:      signer,

		SidecarEndpoint:    c.Sidecar,
		SidecarCredentials: insecure.NewCredentials(),
		SidecarSigner:      nil,

		ChannelID:            c.ChannelID,
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

	go client.SendReplicated(txs, func() { tracker.RequestSent(1) })

	client.StartListening(func(block *common.Block) {

		tracker.ResponseReceived(coordinatorservice.Status_VALID, len(block.Data.Data))
		fmt.Printf("Block received %d:%d\n", block.Header.Number, len(block.Data.Data))
	})
}
