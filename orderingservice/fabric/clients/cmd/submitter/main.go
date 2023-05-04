// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/distributed-trust-research/scalable-committer/orderingservice/fabric"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/limiter"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/sidecarclient"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {
	config.ParseFlags()

	c := fabric.ReadSubmitterConfig()
	p := connection.ReadConnectionProfile(c.OrdererConnectionProfile)
	creds, signer := connection.GetOrdererConnectionCreds(p)

	msgsPerGo := c.Messages / c.GoRoutines
	roundMsgs := msgsPerGo * c.GoRoutines
	bar := workload.NewProgressBar("Submitting transactions...", int64(roundMsgs), "tx")
	opts := &sidecarclient.FabricOrdererBroadcasterOpts{
		Endpoints:            c.Orderers,
		Credentials:          creds,
		Parallelism:          c.GoRoutines,
		InputChannelCapacity: 10,
		OrdererType:          c.OrdererType,
		OnAck: func(err error) {
			if err == nil && bar != nil {
				bar.Add(1)
			}
		},
	}

	s, err := sidecarclient.NewFabricOrdererBroadcaster(opts)
	utils.Must(err)

	rl := limiter.New(&c.RemoteControllerListener)

	envelopes := launchEnvelopeGenerator(c, signer, msgsPerGo, c.GoRoutines, c.MessageSize)

	var wg sync.WaitGroup
	wg.Add(c.GoRoutines)
	for workerID := 0; workerID < c.GoRoutines; workerID++ {
		go func() {
			defer wg.Done()
			send := s.EnvelopeSender()
			for sendCounter := 0; sendCounter < msgsPerGo; sendCounter++ {
				send(<-envelopes)

				// Rate limit
				_ = rl.Take()
			} // send iteration for worker
		}()
	} // for all workers

	wg.Wait()

	utils.Must(s.CloseStreamsAndWait())

	fmt.Printf("----------------------broadcast message finish-------------------------------")
}

func launchEnvelopeGenerator(c fabric.SubmitterConfig, signer msp.SigningIdentity, msgsPerGo, goRoutines, messageSize int) chan *common.Envelope {
	envelopeCreator := sidecarclient.NewEnvelopeCreator(c.ChannelID, signer, c.SignedEnvelopes)
	var ops uint64
	envelopesToSend := make(chan *common.Envelope, 10000)

	for workerID := 0; workerID < goRoutines; workerID++ {
		go func() {
			for i := 0; i < msgsPerGo; i++ {
				message := make([]byte, messageSize)
				n := atomic.AddUint64(&ops, 1)
				binary.LittleEndian.PutUint32(message, uint32(n))
				env, _ := envelopeCreator.CreateEnvelope(message)

				envelopesToSend <- env
			}
		}()
	}
	return envelopesToSend
}
