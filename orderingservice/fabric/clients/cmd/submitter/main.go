// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/orderingservice/fabric"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/sidecarclient"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"go.uber.org/ratelimit"
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
		OnAck: func(err error) {
			if err == nil && bar != nil {
				bar.Add(1)
			}
		},
	}

	s, err := sidecarclient.NewFabricOrdererBroadcaster(opts)
	utils.Must(err)
	if roundMsgs != c.Messages {
		fmt.Println("Rounding messages to", roundMsgs)
	}

	var ops uint64

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

	envelopeCreator := sidecarclient.NewEnvelopeCreator(c.ChannelID, signer, c.SignedEnvelopes)

	envelopesToSend := make(chan *common.Envelope, 10000)

	for workerID := 0; workerID < c.GoRoutines; workerID++ {
		go func() {
			for i := 0; i < msgsPerGo; i++ {
				message := make([]byte, c.MessageSize)
				n := atomic.AddUint64(&ops, 1)
				binary.LittleEndian.PutUint32(message, uint32(n))
				env, _ := envelopeCreator.CreateEnvelope(message)

				envelopesToSend <- env
			}
		}()
	}

	var sendtoAll bool

	bftEnvVarValue := os.Getenv("BFT")
	if bftEnvVarValue != "" {
		fmt.Println("BFT =", bftEnvVarValue, ", will send to all orderers")
	}

	streams := s.Streams()

	var wg sync.WaitGroup
	wg.Add(c.GoRoutines)

	for workerID := 0; workerID < c.GoRoutines; workerID++ {

		go func() {

			defer wg.Done()

			for sendCounter := 0; sendCounter < msgsPerGo; sendCounter++ {

				// Get a transaction to send to the ordering service
				env := <-envelopesToSend

				if sendtoAll {
					// Enqueue envelope into some stream for each orderer
					for _, streamsForOrderer := range s.StreamsByOrderer() {
						streamsForOrderer[sendCounter%len(streamsForOrderer)].Input() <- env
					}

					// Rate limit
					_ = rl.Take()

				} else {
					// Pick some orderer to send the envelope to
					streams[sendCounter%len(streams)].Input() <- env

					// Rate limit
					_ = rl.Take()
				}
			} // send iteration for worker

		}()
	} // for all workers

	wg.Wait()

	utils.Must(s.CloseStreamsAndWait())

	fmt.Printf("----------------------broadcast message finish-------------------------------")
}
