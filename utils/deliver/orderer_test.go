/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package deliver_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	commontypes "github.com/hyperledger/fabric-x-common/api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-committer/loadgen/workload"
	"github.com/hyperledger/fabric-x-committer/mock"
	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/deliver"
	"github.com/hyperledger/fabric-x-committer/utils/ordererconn"
	"github.com/hyperledger/fabric-x-committer/utils/serialization"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

var testGrpcRetryProfile = connection.RetryProfile{
	InitialInterval: 10 * time.Millisecond,
	MaxInterval:     100 * time.Millisecond,
	Multiplier:      2,
	MaxElapsedTime:  time.Second,
}

const channelForTest = "mychannel"

func TestOrdererDeliver(t *testing.T) {
	t.Parallel()
	for _, mode := range test.ServerModes {
		t.Run(fmt.Sprintf("tls-mode:%s", mode), func(t *testing.T) {
			t.Parallel()
			// Create the credentials for both server and client using the same CA.
			// In this test, we are using the same server TLS config for all the orderer instances for simplicity.
			serverTLSConfig, clientTLSConfig := test.CreateServerAndClientTLSConfig(t, mode)

			// We use a short retry grpc-config to shorten the test time.
			e := newOrdererDeliverTestEnv(t, serverTLSConfig)

			// Set the orderer client credentials.
			e.ordererConnConfig.Connection.TLS = clientTLSConfig

			wg := sync.WaitGroup{}
			t.Cleanup(wg.Wait)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			t.Cleanup(cancel)

			outputBlocksChan := make(chan *deliver.BlockWithSourceID, 100)
			wg.Go(func() {
				deliverErr := deliver.OrdererToChannel(ctx, &deliver.OrdererDeliveryParameters{
					FaultToleranceLevel:     ordererconn.BFT,
					TLS:                     serverTLSConfig,
					Identity:                e.ordererConnConfig.Identity,
					OutputBlockWithSourceID: outputBlocksChan,
					LastestKnownConfig:      e.crypto.ConfigBlock,
				})
				assert.ErrorIs(t, deliverErr, context.Canceled)
			})

			t.Log("Reading config block")
			b, ok := channel.NewReader(t.Context(), outputBlocksChan).ReadWithTimeout(3 * time.Second)
			require.True(t, ok)
			require.NotNil(t, b)
			require.NotNil(t, b.Block)
			require.Len(t, b.Block.Data.Data, 1)

			t.Log("All good")
			e.submit(t, outputBlocksChan)

			require.Len(t, e.partyStates, 3)
			p0 := e.partyStates[0]
			p1 := e.partyStates[1]
			p2 := e.partyStates[2]

			t.Log("Switch data source to party 2 (holding)")
			expectedBlockNum := e.prevBlock.Header.Number + 1
			p0.HoldFromBlock.Store(expectedBlockNum)
			p1.HoldFromBlock.Store(expectedBlockNum)
			p2.HoldFromBlock.Store(expectedBlockNum + 1)
			b = e.submit(t, outputBlocksChan)
			require.EqualValues(t, 2, b.SourceID)
			require.EqualValues(t, 2, e.getDataStream(t).PartyID)

			t.Log("Switch data source to party 1 (holding)")
			expectedBlockNum = e.prevBlock.Header.Number + 1
			p0.HoldFromBlock.Store(expectedBlockNum)
			p1.HoldFromBlock.Store(expectedBlockNum + 1)
			p2.HoldFromBlock.Store(expectedBlockNum)
			b = e.submit(t, outputBlocksChan)
			require.EqualValues(t, 1, b.SourceID)
			require.EqualValues(t, 1, e.getDataStream(t).PartyID)

			t.Log("Switch data source to party 0 (holding)")
			expectedBlockNum = e.prevBlock.Header.Number + 1
			p0.HoldFromBlock.Store(expectedBlockNum + 1)
			p1.HoldFromBlock.Store(expectedBlockNum)
			p2.HoldFromBlock.Store(expectedBlockNum)
			b = e.submit(t, outputBlocksChan)
			require.EqualValues(t, 0, b.SourceID)
			require.EqualValues(t, 0, e.getDataStream(t).PartyID)

			t.Log("Switch data source to party 2 (malformed block)")
			expectedBlockNum = e.prevBlock.Header.Number + 1
			p0.HoldFromBlock.Store(expectedBlockNum + 1)
			p1.HoldFromBlock.Store(expectedBlockNum)
			p2.HoldFromBlock.Store(expectedBlockNum + 1)
			p0.ReplaceBlock.Store(expectedBlockNum, &common.Block{
				Header: &common.BlockHeader{Number: expectedBlockNum},
			})
			b = e.submit(t, outputBlocksChan)
			require.EqualValues(t, 2, b.SourceID)
			require.EqualValues(t, 2, e.getDataStream(t).PartyID)

			t.Log("Switch data source to party 1 (bad signatures)")
			expectedBlockNum = e.prevBlock.Header.Number + 1
			p0.HoldFromBlock.Store(expectedBlockNum)
			p1.HoldFromBlock.Store(expectedBlockNum + 1)
			p2.HoldFromBlock.Store(expectedBlockNum + 1)
			badSigBlock := &common.Block{}
			mock.PrepareBlockHeaderAndMetadata(badSigBlock, mock.BlockPrepareParameters{
				PrevBlock: e.prevBlock,
			})
			p2.ReplaceBlock.Store(expectedBlockNum, badSigBlock)
			b = e.submit(t, outputBlocksChan)
			require.EqualValues(t, 1, b.SourceID)
			require.EqualValues(t, 1, e.getDataStream(t).PartyID)
		})
	}
}

type ordererDeliverTestEnv struct {
	idCount           int
	ordererConfig     *mock.OrdererConfig
	ordererConnConfig ordererconn.Config
	orderer           *mock.Orderer
	grpc              []test.GrpcServers
	crypto            *workload.CryptoMaterial
	prevBlock         *common.Block
	partyStates       []*mock.PartyState
}

func newOrdererDeliverTestEnv(t *testing.T, tlsConfig connection.TLSConfig) *ordererDeliverTestEnv {
	t.Helper()

	idCount := 3
	serverPerID := 4
	instanceCount := idCount * serverPerID
	t.Logf("Instance count: %d; idCount: %d", instanceCount, idCount)

	partyStates := make([]*mock.PartyState, idCount)
	for i := range uint32(idCount) {
		partyStates[i] = &mock.PartyState{PartyID: i}
	}

	ordererConfig := &mock.OrdererConfig{
		NumService:         instanceCount,
		BlockSize:          10,
		SendConfigBlock:    true,
		CryptoMaterialPath: t.TempDir(),
	}
	sc := make([]*connection.ServerConfig, instanceCount)
	for i := range sc {
		sc[i] = connection.NewLocalHostServer(tlsConfig)
		listener, err := sc[i].PreAllocateListener()
		t.Cleanup(func() {
			_ = listener.Close()
		})
		require.NoError(t, err)
	}
	ordererConfig.ServerConfigs = sc
	ordererConnConfig := ordererconn.Config{
		ChannelID:           channelForTest,
		FaultToleranceLevel: ordererconn.BFT,
		Connection:          ordererconn.ConnectionConfig{Retry: &testGrpcRetryProfile},
	}
	conf := &ordererConnConfig.Connection
	for i, c := range ordererConfig.ServerConfigs {
		conf.Endpoints = append(conf.Endpoints, &commontypes.OrdererEndpoint{
			ID:   partyStates[i%idCount].PartyID,
			Host: c.Endpoint.Host,
			Port: c.Endpoint.Port,
		})
	}
	for i, e := range conf.Endpoints {
		t.Logf("ENDPOINT [%02d] %s", i, e.String())
	}

	_, err := workload.CreateDefaultConfigBlockWithCrypto(ordererConfig.CryptoMaterialPath, &workload.ConfigBlock{
		ChannelID:        "my-chan",
		OrdererEndpoints: conf.Endpoints,
	})
	require.NoError(t, err)
	cryptoMaterial, err := workload.LoadCrypto(ordererConfig.CryptoMaterialPath)
	require.NoError(t, err)

	ordererService, ordererServer := mock.StartMockOrderingServices(t, ordererConfig)
	require.Len(t, ordererServer.Servers, instanceCount)

	servers := make([]test.GrpcServers, idCount)
	for i, c := range ordererServer.Configs {
		partyState := partyStates[i%idCount]
		s := &servers[partyState.PartyID]
		s.Configs = append(s.Configs, c)
		s.Servers = append(s.Servers, ordererServer.Servers[i])
		ordererService.RegisterSharedState(c.Endpoint.Address(), partyState)
	}
	for i, s := range servers {
		require.Lenf(t, s.Configs, serverPerID, "idx: %d", i)
		require.Lenf(t, s.Servers, serverPerID, "idx: %d", i)
	}

	return &ordererDeliverTestEnv{
		idCount:           idCount,
		ordererConfig:     ordererConfig,
		ordererConnConfig: ordererConnConfig,
		orderer:           ordererService,
		grpc:              servers,
		crypto:            cryptoMaterial,
		partyStates:       partyStates,
	}
}

func (e *ordererDeliverTestEnv) getDataStream(t *testing.T) *mock.OrdererStreamState {
	t.Helper()
	streams := mock.RequireStreams(t, e.orderer, e.idCount)
	var dataStream *mock.OrdererStreamState
	for _, s := range streams {
		if s.DataBlockStream {
			require.Nil(t, dataStream)
			dataStream = s
		}
	}
	require.NotNil(t, dataStream)
	return dataStream
}

func (e *ordererDeliverTestEnv) submit(
	t *testing.T, outputBlocks <-chan *deliver.BlockWithSourceID,
) *deliver.BlockWithSourceID {
	t.Helper()
	size := 10
	txs := workload.GenerateTransactions(t, workload.DefaultProfile(1), size)

	block := workload.MapToOrdererBlock(0, txs)
	err := e.orderer.SubmitBlock(t.Context(), block)
	require.NoError(t, err)

	b, ok := channel.NewReader(t.Context(), outputBlocks).ReadWithTimeout(3 * time.Second)
	require.True(t, ok)
	require.NotNil(t, b)
	require.NotNil(t, b.Block)
	if e.prevBlock != nil {
		require.Equal(t, e.prevBlock.Header.Number+1, b.Block.Header.Number)
	}
	t.Logf("Received block #%d", b.Block.Header.Number)
	e.prevBlock = b.Block

	require.Len(t, b.Block.Data.Data, size)
	for i, d := range b.Block.Data.Data {
		_, hdr, marshalErr := serialization.UnwrapEnvelope(d)
		require.NoError(t, marshalErr)
		require.Equal(t, txs[i].Id, hdr.TxId)
	}
	return b
}
