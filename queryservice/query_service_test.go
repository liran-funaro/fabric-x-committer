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
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
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
		ret[i] = types.VersionNumber(v).Bytes()
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
		);`, vcservice.TableName(types.NamespaceID(i)))
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
	query := &protoqueryservice.Query{}
	expected := protoqueryservice.Rows{}
	querySize := 0
	for _, ns := range env.ns {
		qNs := &protoqueryservice.QueryNamespace{
			NsId: uint32(ns),
			Keys: make([][]byte, 0, len(env.keys)+1),
		}
		qNs.Keys = append(qNs.Keys, env.keys...)
		qNs.Keys = append(qNs.Keys, strToBytes("non-tx")...)
		query.Namespaces = append(query.Namespaces, qNs)
		querySize += len(qNs.Keys)

		eNs := &protoqueryservice.RowsNamespace{
			NsId: uint32(ns),
			Rows: make([]*protoqueryservice.Row, 0, len(env.keys)),
		}
		for i, k := range env.keys {
			eNs.Rows = append(eNs.Rows, &protoqueryservice.Row{
				Key:     k,
				Value:   env.values[i],
				Version: env.versions[i],
			})
		}
		expected.Namespaces = append(expected.Namespaces, eNs)
	}

	for i, qNs := range query.Namespaces {
		t.Run(fmt.Sprintf("Query internal NS %d", qNs.NsId), func(t *testing.T) {
			ret, err := env.qs.queryRowsIfPresent(context.Background(), qNs)
			require.NoError(t, err)
			require.Equal(t, expected.Namespaces[i].NsId, ret.NsId)
			require.ElementsMatch(t, expected.Namespaces[i].Rows, ret.Rows)
		})
	}

	require.Zero(t, test.GetMetricValue(t, env.qs.metrics.queriesReceivedTotal))
	require.Zero(t, test.GetMetricValue(t, env.qs.metrics.keyQueriedTotal))

	t.Run("Query interface", func(t *testing.T) {
		ret, err := env.qs.GetRows(context.Background(), query)
		require.NoError(t, err)
		for i, eNs := range expected.Namespaces {
			rNs := ret.Namespaces[i]
			require.Equal(t, eNs.NsId, rNs.NsId)
			require.ElementsMatch(t, eNs.Rows, rNs.Rows)
		}

		require.Equal(t, 1, int(test.GetMetricValue(t, env.qs.metrics.queriesReceivedTotal)))
		require.Equal(t, querySize, int(test.GetMetricValue(t, env.qs.metrics.keyQueriedTotal)))
	})

	t.Run("Query client", func(t *testing.T) {
		client := protoqueryservice.NewQueryServiceClient(env.clientConn)
		ret, err := client.GetRows(context.Background(), query)
		require.NoError(t, err)
		for i, eNs := range expected.Namespaces {
			rNs := ret.Namespaces[i]
			require.Equal(t, eNs.NsId, rNs.NsId)
			require.ElementsMatch(t, eNs.Rows, rNs.Rows)
		}

		require.Equal(t, 2, int(test.GetMetricValue(t, env.qs.metrics.queriesReceivedTotal)))
		require.Equal(t, querySize*2, int(test.GetMetricValue(t, env.qs.metrics.keyQueriedTotal)))
	})
}
