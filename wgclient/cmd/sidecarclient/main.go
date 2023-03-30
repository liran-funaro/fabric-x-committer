package main

import (
	"fmt"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar"
	sigverification_test "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/serialization"
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
		OrdererType:          c.OrdererType,
		StartBlock:           0,

		RemoteControllerListener: c.RemoteControllerListener,
	}

	tracker := workload.NewMetricTracker(c.Monitoring)

	client, err := sidecarclient.NewClient(opts)
	utils.Must(err)

	var txs chan *sigverification_test.TxWithStatus
	if len(c.Profile) > 0 {
		profile := workload.LoadProfileFromYaml(c.Profile)
		publicKey, txCh := workload.StartTxGenerator(&profile.Transaction, profile.Conflicts, 100)
		utils.Must(client.SetCommitterKey(publicKey))
		txs = txCh
	} else {
		txs = make(chan *sigverification_test.TxWithStatus)
	}

	go client.Send(txs, func(tx *sigverification_test.TxWithStatus, env *common.Envelope) {
		_, header, _ := serialization.ParseEnvelope(env)
		tracker.RequestSent(TxId(header.TxId), tx.Status, time.Now())
	})

	client.StartListening(func(block *common.Block) {
		blockReceivedAt := time.Now()
		for i, statusCode := range block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER] {
			if _, channelHeader, err := serialization.UnwrapEnvelope(block.Data.Data[i]); err == nil {
				tracker.ResponseReceived(TxId(channelHeader.TxId), sidecar.StatusInverseMap[statusCode], blockReceivedAt)
			}
		}
		fmt.Printf("Block received %d:%d\n", block.Header.Number, len(block.Data.Data))
	})
}

type TxId string

func (i TxId) String() string {
	return string(i)
}
