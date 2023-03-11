// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/orderingservice/fabric"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/sidecarclient"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"go.uber.org/ratelimit"
)

func main() {
	config.ParseFlags()

	c := fabric.ReadSubmitterConfig()
	p := connection.ReadConnectionProfile(c.OrdererConnectionProfile)
	creds, signer := connection.GetOrdererConnectionCreds(p)
	_ = signer

	m := monitoring.LaunchMonitoring(c.Monitoring, &Provider{}).(*Metrics)

	msgsPerGo := c.Messages / c.GoRoutines
	roundMsgs := msgsPerGo * c.GoRoutines
	bar := workload.NewProgressBar("Submitting transactions...", int64(roundMsgs), "tx")
	opts := &sidecarclient.FabricOrdererBroadcasterOpts{
		Endpoints:            c.Orderers,
		Credentials:          creds,
		Parallelism:          c.GoRoutines,
		InputChannelCapacity: 10,
		OnAck: func(err error) {
			if err == nil && bar != nil {
				bar.Add(1)
				m.Throughput.Add(1)
			}
		},
	}

	s, err := sidecarclient.NewFabricOrdererBroadcaster(opts)
	utils.Must(err)
	if roundMsgs != c.Messages {
		fmt.Println("Rounding messages to", roundMsgs)
	}

	//fmt.Printf("Sending the same message to all servers.\n")
	message := make([]byte, c.MessageSize)
	envelopeCreator := sidecarclient.NewEnvelopeCreator(c.ChannelID, signer, c.SignedEnvelopes)
	env, _ := envelopeCreator.CreateEnvelope(message)
	//env := &common.Envelope{Payload: message, Signature: nil}
	serializedEnv, err := protoutil.Marshal(env)
	utils.Must(err)
	fmt.Printf("Message size: %d\n", len(serializedEnv))

	//fmt.Printf("Prebuffer tx with %d worker\n", c.GoRoutines)
	//buffered := make(chan *common.Envelope, c.Messages)
	var ops uint64
	//{
	//	bar := workload.NewProgressBar("Prepping transactions...", int64(roundMsgs), "tx")
	//	var wg sync.WaitGroup
	//	for i := 0; i < c.GoRoutines; i++ {
	//		wg.Add(1)
	//		go func() {
	//			for j := 0; j < msgsPerGo; j++ {
	//				message := make([]byte, c.MessageSize)
	//				n := atomic.AddUint64(&ops, 1)
	//				binary.LittleEndian.PutUint32(message, uint32(n))
	//				envelopeCreator := sidecarclient.NewEnvelopeCreator(c.ChannelID, signer, c.Signed)
	//				env, _ := envelopeCreator.CreateEnvelope(message)
	//				//env := &common.Envelope{Payload: message, Signature: nil}
	//				buffered <- env
	//				bar.Add(1)
	//			}
	//			wg.Done()
	//		}()
	//	}
	//	wg.Wait()
	//}

	// we start by default with unlimited rate
	var rl ratelimit.Limiter
	rl = ratelimit.NewUnlimited()

	// start remote-limiter controller
	if c.RemoteControllerListener != "" {
		fmt.Printf("Start remote controller listener on %v\n", c.RemoteControllerListener)
		gin.SetMode(gin.ReleaseMode)
		router := gin.Default()
		router.POST("/setLimits", func(c *gin.Context) {

			type Limiter struct {
				Limit int `json:"limit"`
			}

			var limit Limiter
			if err := c.BindJSON(&limit); err != nil {
				return
			}

			if limit.Limit < 1 {
				rl = ratelimit.NewUnlimited()
				return
			}

			// create our new limiter
			rl = ratelimit.New(limit.Limit)

			c.IndentedJSON(http.StatusOK, limit)
		})
		fmt.Printf("Start remote controller on ")
		go router.Run(c.RemoteControllerListener)
	}

	var wg sync.WaitGroup
	wg.Add(len(s.Streams()))
	for _, ch := range s.Streams() {
		input := ch.Input()
		go func(input chan<- *common.Envelope) {
			for i := 0; i < msgsPerGo; i++ {
				message := make([]byte, c.MessageSize)
				n := atomic.AddUint64(&ops, 1)
				binary.LittleEndian.PutUint32(message, uint32(n))
				envelopeCreator := sidecarclient.NewEnvelopeCreator(c.ChannelID, signer, c.SignedEnvelopes)
				env, _ := envelopeCreator.CreateEnvelope(message)
				// TODO send to all nodes?
				//input <- <-buffered
				_ = rl.Take() // our rate limiter
				input <- env
			}
			wg.Done()
		}(input)
	}

	wg.Wait()

	//for i := uint64(0); i < msgsPerGo; i++ {
	//	// TODO submit asynchronously
	//	for _, ch := range s.Streams() {
	//		ch.Input() <- env
	//	}
	//	m.Throughput.Add(len(s.Streams()))
	//}

	utils.Must(s.CloseStreamsAndWait())

	fmt.Printf("----------------------broadcast message finish-------------------------------")
}
