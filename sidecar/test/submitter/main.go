// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	config2 "github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/sidecarservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"go.uber.org/ratelimit"
)

var logger = logging.New("submitter")

func main() {

	numOfWorker := 1
	totalTxCount := 100

	// we are just using the sidecar configuration :D
	config.ServerConfig("sidecar")
	config.ParseFlags()

	c := config2.ReadConfig()

	ctx := context.Background()

	// start orderer client that forwards blocks to aggregator
	fmt.Printf("Create orderer client and connect to %v\n", c.Orderer.Endpoint)
	creds, signer := connection.GetOrdererConnectionCreds(c.Orderer.OrdererConnectionProfile)
	ordererDialConfig := connection.NewDialConfigWithCreds(c.Orderer.Endpoint, creds)
	ordererConn, err := connection.Connect(ordererDialConfig)
	utils.Must(err)

	client, err := ab.NewAtomicBroadcastClient(ordererConn).Broadcast(ctx)
	utils.Must(err)

	creator := NewEnvelopeCreator(c.Orderer.ChannelID, signer, true)

	var ops uint64

	logger.Infof("Start producer ...")

	rl := ratelimit.New(10)

	envelopesToSend := make(chan *common.Envelope, 10000)
	for workerID := 0; workerID < numOfWorker; workerID++ {
		go func() {
			for i := 0; i < totalTxCount; i++ {
				n := atomic.AddUint64(&ops, 1)

				tx := &protoblocktx.Tx{
					Id: fmt.Sprintf("%d", n),
				}
				message := protoutil.MarshalOrPanic(tx)
				env, _ := creator.CreateEnvelope(message)

				_ = rl.Take()

				envelopesToSend <- env
			}
			logger.Infof("Sent all transactions!")
		}()
	}

	var wg sync.WaitGroup
	wg.Add(numOfWorker)

	logger.Infof("Sending transactions ...")
	for workerID := 0; workerID < numOfWorker; workerID++ {
		go func() {
			defer wg.Done()
			for env := range envelopesToSend {
				client.Send(env)
			}
		}()
	} // for all workers

	wg.Wait()
	utils.Must(client.CloseSend())

	fmt.Printf("----------------------broadcast message finish-------------------------------")
}
