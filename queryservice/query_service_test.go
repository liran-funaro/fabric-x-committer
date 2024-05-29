package queryservice

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v4/pgxpool"
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
	"google.golang.org/grpc/status"
)

type queryServiceTestEnv struct {
	ctx        context.Context
	qs         *QueryService
	ns         []int
	grpcServer *grpc.Server
	clientConn *grpc.ClientConn
	pool       *pgxpool.Pool
}

func TestQuery(t *testing.T) {
	env := newQueryServiceTestEnv(t)

	requiredItems := make([]*items, len(env.ns))
	for i, ns := range env.ns {
		requiredItems[i] = &items{
			ns:       uint32(ns),
			keys:     strToBytes("item1", "item2", "item3", "item4"),
			values:   strToBytes("value1", "value2", "value3", "value4"),
			versions: verToBytes(0, 1, 2, 3),
		}
		env.insert(t, requiredItems[i])
	}

	query := &protoqueryservice.Query{}
	querySize := 0
	for _, item := range requiredItems {
		qNs := item.asQuery()
		query.Namespaces = append(query.Namespaces, qNs)
		querySize += len(qNs.Keys)
	}

	csParams := &protoqueryservice.ViewParameters{
		IsoLevel:      protoqueryservice.IsoLevel_RepeatableRead,
		NonDeferrable: false,
	}

	for i, qNs := range query.Namespaces {
		t.Run(fmt.Sprintf("Query internal NS %d", qNs.NsId), func(t *testing.T) {
			ret, err := queryRowsIfPresent(env.ctx, env.qs.pool, qNs)
			require.NoError(t, err)
			requireRow(t, requiredItems[i], ret)
		})
	}

	expectedMetricsSize := 0

	t.Run("Query GetRows interface", func(t *testing.T) {
		ret, err := env.qs.GetRows(env.ctx, query)
		require.NoError(t, err)
		expectedMetricsSize++
		requireResults(t, requiredItems, ret.Namespaces)
	})

	t.Run("Query GetRows client", func(t *testing.T) {
		client := protoqueryservice.NewQueryServiceClient(env.clientConn)
		ret, err := client.GetRows(env.ctx, query)
		require.NoError(t, err)
		expectedMetricsSize++
		requireResults(t, requiredItems, ret.Namespaces)
	})

	t.Run("Query GetRows client with consistentView", func(t *testing.T) {
		client := protoqueryservice.NewQueryServiceClient(env.clientConn)
		ret, err := client.GetRows(env.ctx, &protoqueryservice.Query{
			View:       env.beginView(t, client, csParams),
			Namespaces: query.Namespaces,
		})
		require.NoError(t, err)
		expectedMetricsSize++
		requireResults(t, requiredItems, ret.Namespaces)
	})

	require.Equal(t, expectedMetricsSize, int(test.GetMetricValue(t, env.qs.metrics.queriesReceivedTotal)))
	require.Equal(t, expectedMetricsSize*querySize, int(test.GetMetricValue(t, env.qs.metrics.keyQueriedTotal)))

	t.Run("Consistency with repeated GetRows", func(t *testing.T) {
		// This test will query key1, then update key2.
		// We expect that the updated key2 will not be visible to GetRows() with the initial view.
		// But it will be visible to a new view.
		client := protoqueryservice.NewQueryServiceClient(env.clientConn)

		t1 := requiredItems[0]
		testItem1 := items{0, t1.keys[:1], t1.values[:1], t1.versions[:1]}

		view := env.beginView(t, client, csParams)
		ret, err := client.GetRows(env.ctx, &protoqueryservice.Query{
			View: view,
			Namespaces: []*protoqueryservice.QueryNamespace{{
				NsId: testItem1.ns,
				Keys: testItem1.keys,
			}},
		})
		require.NoError(t, err)

		requireResults(t, []*items{&testItem1}, ret.Namespaces)

		testItem2 := items{testItem1.ns, t1.keys[1:2], t1.values[1:2], t1.versions[1:2]}
		testItem2Mod := testItem2
		testItem2Mod.values = strToBytes("value2.1")
		testItem2Mod.versions = verToBytes(2)
		env.update(t, &testItem2Mod)

		key2Query := &protoqueryservice.Query{
			View: view,
			Namespaces: []*protoqueryservice.QueryNamespace{{
				NsId: testItem2.ns,
				Keys: testItem2.keys,
			}},
		}
		ret2, err := client.GetRows(env.ctx, key2Query)
		require.NoError(t, err)
		// This is the same view, so we expect the old version of item 2.
		requireResults(t, []*items{&testItem2}, ret2.Namespaces)

		key2Query.View = env.beginView(t, client, csParams)
		ret3, err := client.GetRows(env.ctx, key2Query)
		require.NoError(t, err)
		// This is a new view, so we expect the new version of item 2.
		requireResults(t, []*items{&testItem2Mod}, ret3.Namespaces)
	})

	t.Run("Nil view parameters", func(t *testing.T) {
		client := protoqueryservice.NewQueryServiceClient(env.clientConn)
		env.beginView(t, client, nil)
	})

	t.Run("Bad view ID", func(t *testing.T) {
		client := protoqueryservice.NewQueryServiceClient(env.clientConn)
		_, err := client.EndView(env.ctx, &protoqueryservice.View{Id: "bad"})
		require.Equal(t, status.Convert(err).Message(), ErrInvalidViewID.Error())
	})

	t.Run("Stale view ID", func(t *testing.T) {
		client := protoqueryservice.NewQueryServiceClient(env.clientConn)
		view, err := client.BeginView(env.ctx, csParams)
		require.NoError(t, err)
		_, err = client.EndView(env.ctx, view)
		require.NoError(t, err)
		_, err = client.EndView(env.ctx, view)
		require.Equal(t, status.Convert(err).Message(), ErrInvalidViewID.Error())
	})
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

	pool, err := vcservice.NewDatabasePool(config.Database)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// We set a default context with timeout to make sure the test never halts progress.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*10)
	t.Cleanup(cancel)

	return &queryServiceTestEnv{
		ctx:        ctx,
		qs:         qs,
		ns:         ns,
		grpcServer: grpcSrv,
		clientConn: clientConn,
		pool:       pool,
	}
}

type items struct {
	ns                     uint32
	keys, values, versions [][]byte
}

func (it *items) asRows() []*protoqueryservice.Row {
	rows := make([]*protoqueryservice.Row, len(it.keys))
	for i, k := range it.keys {
		rows[i] = &protoqueryservice.Row{
			Key:     k,
			Value:   it.values[i],
			Version: it.versions[i],
		}
	}
	return rows
}

func (it *items) asQuery() *protoqueryservice.QueryNamespace {
	q := &protoqueryservice.QueryNamespace{
		NsId: it.ns,
		// Add additional item that does not exist.
		Keys: append(it.keys, strToBytes("non-tx")...),
	}
	return q
}

func (q *queryServiceTestEnv) insert(t *testing.T, i *items) {
	query := fmt.Sprintf(
		`insert into %s values (
			UNNEST($1::bytea[]), UNNEST($2::bytea[]), UNNEST($3::bytea[])
		);`,
		vcservice.TableName(types.NamespaceID(i.ns)),
	)
	_, err := q.pool.Exec(q.ctx, query, i.keys, i.values, i.versions)
	require.NoError(t, err)
}

func (q *queryServiceTestEnv) update(t *testing.T, i *items) {
	query := fmt.Sprintf(`
		UPDATE %[1]s
			SET value = t.value,
				version = t.version
		FROM (
			SELECT * FROM UNNEST($1::bytea[], $2::bytea[], $3::bytea[]) AS t(key, value, version)
		) AS t
		WHERE %[1]s.key = t.key;
		`,
		vcservice.TableName(types.NamespaceID(i.ns)),
	)
	_, err := q.pool.Exec(q.ctx, query, i.keys, i.values, i.versions)
	require.NoError(t, err)
}

func requireResults(
	t *testing.T,
	expected []*items,
	ret []*protoqueryservice.RowsNamespace,
) {
	require.Len(t, ret, len(expected))
	for i, item := range expected {
		requireRow(t, item, ret[i])
	}
}

func requireRow(
	t *testing.T,
	expected *items,
	ret *protoqueryservice.RowsNamespace,
) {
	require.Equal(t, expected.ns, ret.NsId)
	require.ElementsMatch(t, expected.asRows(), ret.Rows)
}

func (q *queryServiceTestEnv) beginView(
	t *testing.T,
	client protoqueryservice.QueryServiceClient,
	params *protoqueryservice.ViewParameters,
) *protoqueryservice.View {
	view, err := client.BeginView(q.ctx, params)
	require.NoError(t, err)
	require.NotNil(t, view)
	require.NotEmpty(t, view.Id)
	t.Cleanup(func() {
		_, err = client.EndView(q.ctx, view)
		require.NoError(t, err)
	})
	return view
}
