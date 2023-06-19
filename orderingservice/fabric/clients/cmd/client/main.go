package main

import (
	"encoding/binary"
	"fmt"
	"sync/atomic"

	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/deliver"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/ordererclient"
)

func main() {
	config.ParseFlags()

	c := ReadClientConfig()
	fmt.Printf("%v", c)
	p := connection.ReadConnectionProfile(c.OrdererConnectionProfile)
	creds, signer := connection.GetOrdererConnectionCreds(p)

	client, err := ordererclient.NewClient(&ordererclient.ClientInitOptions{
		DeliverEndpoint:       *c.Orderers[0],
		DeliverCredentials:    creds,
		DeliverSigner:         signer,
		DeliverClientProvider: &sidecar.OrdererDeliverClientProvider{},

		OrdererEndpoints:   c.Orderers,
		OrdererCredentials: creds,
		ChannelID:          c.ChannelID,
		OrdererType:        c.OrdererType,
		StartBlock:         deliver.SeekSinceOldestBlock,
		SignedEnvelopes:    c.SignedEnvelopes,
		OrdererSigner:      signer,

		Parallelism:              c.Parallelism,
		InputChannelCapacity:     c.InputChannelCapacity,
		RemoteControllerListener: c.RemoteControllerListener,
	})
	if err != nil {
		panic(err)
	}

	tracker := newMetricTracker(c.Monitoring)

	client.Start(messageGenerator(c.Messages, c.Parallelism, c.MessageSize, c.MessageStart), tracker.TxSent, tracker.BlockReceived)
}

func messageGenerator(totalMessages, parallelism, messageSize, messageStart int) <-chan []byte {
	msgsPerGo := totalMessages / parallelism
	ops := uint64(messageStart)
	envelopesToSend := make(chan []byte, 10000)

	for workerID := 0; workerID < parallelism; workerID++ {
		go func() {
			for i := 0; i < msgsPerGo; i++ {
				message := make([]byte, messageSize)
				n := atomic.AddUint64(&ops, 1)
				binary.LittleEndian.PutUint32(message, uint32(n))
				envelopesToSend <- message
			}
		}()
	}
	return envelopesToSend
}
