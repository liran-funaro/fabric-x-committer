package shardsservice_test

import (
	"testing"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	connectionUtils "github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/cmd/testclient/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

func BenchmarkShardsCoordinator(b *testing.B) {
	c := shardsservice.ReadConfig()

	connection.RunServerMainAndWait(&connection.ServerConfig{Endpoint: c.Endpoint}, func(server *grpc.Server) {
		shardsservice.RegisterShardsServer(server, shardsservice.NewShardsCoordinator(c.Database, c.Limits, metrics.New(c.Prometheus.IsEnabled())))
	})
	//monitoring.LaunchPrometheus(c.Prometheus, monitoring.ShardsService, metrics.New(c.Prometheus.Enabled).AllMetrics())

	client, err := connectionUtils.NewClient(connectionUtils.ClientConfig{
		Connections:        []*connection.DialConfig{connection.NewDialConfig(c.Endpoint)},
		NumShardsPerServer: 4,
		Input: connectionUtils.ClientInputConfig{
			BlockCount:     100_000,
			BlockSize:      100,
			TxSize:         1,
			SignatureBytes: false,
			InputDelay:     test.Constant(int64(100 * time.Microsecond)),
		},
	})
	utils.Must(err)
	utils.Every(time.Second, client.LogDebug)

	//for i := 0; i < b.N; i++ {
	b.Run("main", func(b *testing.B) {
		client.Start()
		client.WaitUntilDone()
		client.LogDebug()
		client.CleanUp()
	})
	//}
}
