package main

import (
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/ordererclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload/client"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	config.ServerConfig("sidecar")
	config.ParseFlags()

	c := ReadConfig()

	if len(c.Profile) == 0 {
		panic("no profile passed")
	}

	creds, signer := connection.GetOrdererConnectionCreds(c.OrdererConnectionProfile)

	committerClient := client.OpenCoordinatorAdapter(c.Committer, nil)
	ordererClient, err := ordererclient.NewClient(&ordererclient.ClientInitOptions{

		SignedEnvelopes: c.SignedEnvelopes,
		OrdererSigner:   signer,

		OrdererEndpoints:   c.Orderers,
		OrdererCredentials: creds,

		DeliverEndpoint:       &c.Sidecar,
		DeliverCredentials:    insecure.NewCredentials(),
		DeliverSigner:         nil,
		DeliverClientProvider: &orderer.PeerDeliverClientProvider{},

		ChannelID:   c.ChannelID,
		Parallelism: c.Parallelism,
		OrdererType: c.OrdererType,
		StartBlock:  0,
	})
	utils.Must(err)

	profile := workload.LoadProfileFromYaml(c.Profile)
	publicKey, txCh := workload.StartTxGenerator(&profile.Transaction, profile.Conflicts, 100)

	utils.Must(committerClient.SetVerificationKey(publicKey))

	tracker := newMetricTracker(c.Monitoring)

	ordererClient.Start(messageGenerator(txCh), tracker.TxSent, tracker.BlockReceived)
}

func messageGenerator(txs chan *sigverification_test.TxWithStatus) <-chan []byte {
	messages := make(chan []byte, 100)
	for workerID := 0; workerID < 10; workerID++ {
		go func() {
			for {
				select {
				case tx := <-txs:
					message := MarshalTx(tx.Tx)
					messages <- message
				}
			}
		}()
	}
	return messages
}

func MarshalTx(tx *protoblocktx.Tx) []byte {
	return protoutil.MarshalOrPanic(tx)
}
