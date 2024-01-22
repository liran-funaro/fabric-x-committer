package queryservice

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
	"google.golang.org/grpc"
)

type queryServiceTestEnv struct {
	qs         *QueryService
	ns         []int
	keys       [][]byte
	values     [][]byte
	versions   [][]byte
	grpcServer *grpc.Server
	clientConn *grpc.ClientConn
}

func strToBytes(str ...string) [][]byte {
	ret := make([][]byte, len(str))
	for i, s := range str {
		ret[i] = []byte(s)
	}
	return ret
}

func verToBytes(ver ...int) [][]byte {
	ret := make([][]byte, len(ver))
	for i, v := range ver {
		ret[i] = vcservice.VersionNumber(v).Bytes()
	}
	return ret
}

func newQueryServiceTestEnv(t *testing.T) *queryServiceTestEnv {
	c := &logging.Config{
		Enabled:     true,
		Level:       logging.Debug,
		Caller:      true,
		Development: true,
	}
	logging.SetupWithConfig(c)

	cs := yuga.PrepareYugaTestEnv(t)
	port, err := strconv.Atoi(cs.Port)
	require.NoError(t, err)

	config := &Config{
		Server: &connection.ServerConfig{
			Endpoint: connection.Endpoint{
				Host: "localhost",
				Port: 0,
			},
		},
		Database: &vcservice.DatabaseConfig{
			Host:           cs.Host,
			Port:           port,
			Username:       cs.User,
			Password:       cs.Password,
			Database:       cs.Database,
			MaxConnections: 10,
			MinConnections: 1,
		},
		Monitoring: &monitoring.Config{
			Metrics: &metrics.Config{
				Enable: true,
				Endpoint: &connection.Endpoint{
					Host: "localhost",
					Port: 0,
				},
			},
		},
	}
	ns := []int{0, 1, 2}
	require.NoError(t, vcservice.InitDatabase(config.Database, ns))
	t.Cleanup(func() {
		assert.NoError(t, vcservice.ClearDatabase(config.Database, ns))
	})

	keys := strToBytes("tx1", "tx2", "tx3", "tx4")
	values := strToBytes("value1", "value2", "value3", "value4")
	versions := verToBytes(0, 1, 2, 3)
	pool, err := vcservice.NewDatabasePool(config.Database)
	require.NoError(t, err)
	defer pool.Close()
	ctx := context.Background()
	for _, i := range ns {
		query := fmt.Sprintf(`
		insert into %s values (
			UNNEST($1::bytea[]), UNNEST($2::bytea[]), UNNEST($3::bytea[])
		);`, vcservice.NamespaceID(i).TableName())
		_, err = pool.Exec(ctx, query, keys, values, versions)
		require.NoError(t, err)
	}

	qs, err := NewQueryService(config)
	require.NoError(t, err)
	t.Cleanup(qs.Close)

	var grpcSrv *grpc.Server
	var wg sync.WaitGroup
	wg.Add(1)
	sConfig := connection.ServerConfig{
		Endpoint: connection.Endpoint{Host: "localhost", Port: 0},
	}
	go func() {
		connection.RunServerMain(&sConfig, func(grpcServer *grpc.Server, actualListeningPort int) {
			grpcSrv = grpcServer
			sConfig.Endpoint.Port = actualListeningPort
			protoqueryservice.RegisterQueryServiceServer(grpcServer, qs)
			wg.Done()
		})
	}()
	wg.Wait()
	t.Cleanup(grpcSrv.Stop)

	clientConn, err := connection.Connect(connection.NewDialConfig(sConfig.Endpoint))
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, clientConn.Close())
	})

	return &queryServiceTestEnv{
		qs:         qs,
		ns:         ns,
		keys:       keys,
		values:     values,
		versions:   versions,
		grpcServer: grpcSrv,
		clientConn: clientConn,
	}
}

func TestQuery(t *testing.T) {
	env := newQueryServiceTestEnv(t)
	query := &protoqueryservice.TokenIDs{
		Keys: make([][]byte, 0, len(env.keys)+1),
	}
	query.Keys = append(query.Keys, env.keys...)
	query.Keys = append(query.Keys, strToBytes("non-tx")...)

	for _, ns := range env.ns {
		t.Run(fmt.Sprintf("Query internal NS %d", ns), func(t *testing.T) {
			ret, err := env.qs.queryRowsIfPresent(context.Background(), vcservice.NamespaceID(ns), query.Keys)
			require.NoError(t, err)
			require.Len(t, ret, len(env.keys))
			for i, k := range env.keys {
				v, ok := ret[string(k)]
				require.True(t, ok)
				require.Equal(t, env.values[i], v.Value)
				require.Equal(t, env.versions[i], v.Version)
			}
		})
	}

	require.Zero(t, test.GetMetricValue(t, env.qs.metrics.queriesReceivedTotal))
	require.Zero(t, test.GetMetricValue(t, env.qs.metrics.keyQueriedTotal))

	t.Run("Query interface", func(t *testing.T) {
		ret, err := env.qs.GetCommitments(context.Background(), query)
		require.NoError(t, err)
		env.verifyCommitments(t, ret)

		require.Equal(t, 1, int(test.GetMetricValue(t, env.qs.metrics.queriesReceivedTotal)))
		require.Equal(t, len(query.Keys), int(test.GetMetricValue(t, env.qs.metrics.keyQueriedTotal)))
	})

	t.Run("Query client", func(t *testing.T) {
		client := protoqueryservice.NewQueryServiceClient(env.clientConn)
		ret, err := client.GetCommitments(context.Background(), query)
		require.NoError(t, err)
		env.verifyCommitments(t, ret)

		require.Equal(t, 2, int(test.GetMetricValue(t, env.qs.metrics.queriesReceivedTotal)))
		require.Equal(t, len(query.Keys)*2, int(test.GetMetricValue(t, env.qs.metrics.keyQueriedTotal)))
	})
}

func (e *queryServiceTestEnv) verifyCommitments(t *testing.T, c *protoqueryservice.Commitments) {
	require.Len(t, c.Output, len(e.keys)+1)
	for i := range e.keys {
		require.Equal(t, e.values[i], c.Output[i].Value)
		require.Equal(t, e.versions[i], c.Output[i].Version)
	}
	nonExisting := c.Output[len(e.keys)]
	if nonExisting != nil {
		require.Nil(t, nonExisting.Value)
		require.Nil(t, nonExisting.Version)
	}
}
