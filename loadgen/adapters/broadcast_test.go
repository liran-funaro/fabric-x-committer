/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package adapters

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	commontypes "github.com/hyperledger/fabric-x-common/api/types"
	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/loadgen/workload"
	"github.com/hyperledger/fabric-x-committer/mock"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
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

type broadcastTestEnv struct {
	config    ordererconn.Config
	orderer   *mock.Orderer
	servers   []test.GrpcServers
	nextBlock uint64
}

func TestBroadcast(t *testing.T) {
	t.Parallel()
	for _, mode := range test.ServerModes {
		t.Run(fmt.Sprintf("tls-mode:%s", mode), func(t *testing.T) {
			t.Parallel()
			// Create the credentials for both server and client using the same CA.
			// In this test, we are using the same server TLS config for all the orderer instances for simplicity.
			serverTLSConfig, clientTLSConfig := test.CreateServerAndClientTLSConfig(t, mode)

			// We use a short retry grpc-config to shorten the test time.
			e := newBroadcastTestEnv(t, &serverTLSConfig)

			// Set the orderer client credentials.
			e.config.Connection.TLS = clientTLSConfig
			allEndpoints := e.config.Connection.Endpoints

			// We only take the bottom endpoints for now.
			// Later we take the other endpoints and update the client.
			e.config.Connection.Endpoints = allEndpoints[:6]

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
			t.Cleanup(cancel)

			t.Log("Read config block")
			b := e.getNextBlock(t)
			require.Len(t, b.Data.Data, 1)

			t.Log("All good")
			e.submit(t, expectedSubmit{
				success: 3,
			})

			t.Log("One server down")
			e.servers[2].Servers[0].Stop()
			listener1 := holdPort(ctx, t, e.servers[2].Configs[0])
			e.submit(t, expectedSubmit{
				success: 3,
			})

			t.Log("Two servers down")
			e.servers[2].Servers[1].Stop()
			listener2 := holdPort(ctx, t, e.servers[2].Configs[1])
			e.submit(t, expectedSubmit{
				success:     2,
				unavailable: 1,
			})

			t.Log("One incorrect server")
			_ = listener1.Close()
			fakeServer := test.RunGrpcServerForTest(ctx, t, e.servers[2].Configs[0], nil)
			waitUntilGrpcServerIsReady(ctx, t, &e.servers[2].Configs[0].Endpoint, clientTLSConfig)
			e.submit(t, expectedSubmit{
				success:       2,
				unimplemented: 1,
			})
			t.Log("All good again")
			fakeServer.Stop()
			e.servers[2].Servers[0] = test.RunGrpcServerForTest(
				ctx, t, e.servers[2].Configs[0], e.orderer.RegisterService,
			)
			waitUntilGrpcServerIsReady(ctx, t, &e.servers[2].Configs[0].Endpoint, clientTLSConfig)
			e.submit(t, expectedSubmit{
				success: 3,
			})

			t.Log("Insufficient quorum")
			_ = listener2.Close()
			for _, gs := range e.servers[:2] {
				for _, s := range gs.Servers[:2] {
					s.Stop()
				}
			}
			for _, gs := range e.servers[:2] {
				for _, c := range gs.Configs[:2] {
					holdPort(ctx, t, c)
				}
			}
			e.submit(t, expectedSubmit{
				success:     1,
				unavailable: 2,
			})

			t.Log("Update endpoints")
			e.config.Connection.Endpoints = allEndpoints[6:]
			e.submit(t, expectedSubmit{
				success: 3,
			})
		})
	}
}

type expectedSubmit struct {
	success       int
	unavailable   int
	unimplemented int
}

func (e *broadcastTestEnv) submit(
	t *testing.T,
	expected expectedSubmit,
) {
	t.Helper()
	txb := workload.TxBuilder{ChannelID: channelForTest}
	tx := txb.MakeTx(&applicationpb.Tx{})

	// We create a new stream for each request to ensure GRPC does not cache the latest state.
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	stream, err := NewBroadcastStream(ctx, &e.config)
	require.NoError(t, err)
	defer stream.CloseConnections()

	err = stream.SendBatch(workload.MapToEnvelopeBatch(0, []*servicepb.LoadGenTx{tx}))
	if err != nil {
		t.Logf("Response error:\n%s", err)
	}
	if expected.unavailable > 0 {
		require.Error(t, err)
	} else if expected.unimplemented == 0 {
		// Unimplemented sometimes not responding with error, so we cannot enforce it.
		require.NoError(t, err)
	}

	if err != nil {
		errStr := err.Error()
		// We verify that we didn't get more Unavailable or Unimplemented than expected.
		// Unimplemented sometimes returns Unavailable or no error at all, so we might not get exact value.
		unavailableCount := strings.Count(errStr, "Unavailable")
		unimplementedCount := strings.Count(errStr, "Unimplemented")
		require.LessOrEqual(t, unavailableCount, expected.unavailable+expected.unimplemented, err)
		require.LessOrEqual(t, unimplementedCount, expected.unimplemented, err)
	}

	b := e.getNextBlock(t)
	require.Len(t, b.Data.Data, 1)
	_, hdr, err := serialization.UnwrapEnvelope(b.Data.Data[0])
	require.NoError(t, err)
	require.Equal(t, tx.Id, hdr.TxId)
}

func (e *broadcastTestEnv) getNextBlock(t *testing.T) *common.Block {
	t.Helper()
	b, err := e.orderer.GetBlock(t.Context(), e.nextBlock)
	require.NoError(t, err)
	require.NotNil(t, b)
	e.nextBlock++
	return b
}

func newBroadcastTestEnv(t *testing.T, tlsConfig *connection.TLSConfig) *broadcastTestEnv {
	t.Helper()

	idCount := 3
	serverPerID := 4
	instanceCount := idCount * serverPerID
	t.Logf("Instance count: %d; idCount: %d", instanceCount, idCount)

	config := &mock.OrdererConfig{
		NumService:      instanceCount,
		BlockSize:       1,
		SendConfigBlock: true,
	}
	if tlsConfig != nil {
		sc := make([]*connection.ServerConfig, instanceCount)
		for i := range sc {
			creds := *tlsConfig
			sc[i] = connection.NewLocalHostServer(creds)
		}
		config.ServerConfigs = sc
	}
	ordererService, ordererServer := mock.StartMockOrderingServices(t, config)
	require.Len(t, ordererServer.Servers, instanceCount)

	conf := ordererconn.Config{
		ChannelID:           channelForTest,
		FaultToleranceLevel: ordererconn.BFT,
		Connection:          ordererconn.ConnectionConfig{Retry: &testGrpcRetryProfile},
	}
	servers := make([]test.GrpcServers, idCount)
	for i, c := range ordererServer.Configs {
		id := uint32(i % idCount) //nolint:gosec // integer overflow conversion int -> uint32
		conf.Connection.Endpoints = append(conf.Connection.Endpoints, &commontypes.OrdererEndpoint{
			ID:   id,
			Host: c.Endpoint.Host,
			Port: c.Endpoint.Port,
		})
		s := &servers[id]
		s.Configs = append(s.Configs, c)
		s.Servers = append(s.Servers, ordererServer.Servers[i])
	}
	for i, s := range servers {
		require.Lenf(t, s.Configs, serverPerID, "id: %d", i)
		require.Lenf(t, s.Servers, serverPerID, "id: %d", i)
	}
	for i, e := range conf.Connection.Endpoints {
		t.Logf("ENDPOINT [%02d] %s", i, e.String())
	}
	return &broadcastTestEnv{
		config:  conf,
		servers: servers,
		orderer: ordererService,
	}
}

func waitUntilGrpcServerIsReady(
	ctx context.Context, t *testing.T, endpoint *connection.Endpoint, clientTLSConfig connection.TLSConfig,
) {
	t.Helper()
	newConn := test.NewSecuredConnection(t, endpoint, clientTLSConfig)
	defer connection.CloseConnectionsLog(newConn)
	test.WaitUntilGrpcServerIsReady(ctx, t, newConn)
	t.Logf("%v is ready", endpoint)
}

// holdPort attempts to bind to the specified server port and holds it until the listener is closed.
// It serves two purposes:
//  1. A successful bind indicates the port is free, meaning the server previously using it is down.
//  2. It prevents other tests from binding to the same port, ensuring this test correctly detects the server as
//     unavailable.
func holdPort(ctx context.Context, t *testing.T, c *connection.ServerConfig) net.Listener {
	t.Helper()
	listener, err := c.Listener(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return listener
}
