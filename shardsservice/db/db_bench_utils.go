package db

import (
	"fmt"
	"os"
	"testing"

	sigverification_test "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

var (
	defaultInitDbSizes = []int{10_000}
	defaultBatchSizes  = []int{100}
	//dbSize             = prometheus.NewCounter(prometheus.CounterOpts{
	//	Name: "db_size",
	//	Help: "How many SNs are in the DB",
	//})
	//dbRequestLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	//	Name:    "db_request_latency",
	//	Help:    "Latency for read/write in the shard DB (ns)",
	//	Buckets: metrics.UniformBuckets(10000, 0, float64(1*time.Second)),
	//}, []string{"operation"})
)

type BenchmarkCommand string

const (
	Read       BenchmarkCommand = "read"
	Commit                      = "commit"
	ReadCommit                  = "read-commit"
)

func Benchmark(commandType BenchmarkCommand, opener DbOpener, b *testing.B) {
	RunTest(b, opener, defaultInitDbSizes, defaultBatchSizes, test.NoDelay, commandType)
}

func RunTest(b *testing.B, opener DbOpener, initDbSizes []int, batchSizes []int, delay test.Distribution, commandType BenchmarkCommand) {
	command := getCommand(commandType)
	delayGenerator := test.NewDelayGenerator(delay, 1)
	for _, initDbSize := range initDbSizes {
		for _, batchSize := range batchSizes {

			b.Run(fmt.Sprintf("batch:%d, init_db_size:%d", batchSize, initDbSizes), func(b *testing.B) {
				g := sigverification_test.NewLinearTxInputGenerator([]test.DiscreteValue{{0, 1}})

				// Populate the DB with RowCount entries (approx) and store all existing in a slice for further use
				db, _ := opener(path)
				defer func() {
					db.Close()
					os.RemoveAll(path)
				}()
				for i := 0; i <= initDbSize/batchSize; i++ {
					utils.Must(db.Commit(g.Next()))
					//dbSize.Add(float64(batchSize))
				}

				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						command(db, g)
						delayGenerator.Next()
					}
				})
			})
		}
	}

}

func getCommand(commandType BenchmarkCommand) func(Database, *sigverification_test.LinearTxInputGenerator) {
	switch commandType {
	case Commit:
		return func(db Database, g *sigverification_test.LinearTxInputGenerator) {
			serialNumbers := g.Next()
			//startWrite := time.Now()
			utils.Must(db.Commit(serialNumbers))
			//dbRequestLatency.WithLabelValues("write").Observe(float64(time.Now().Sub(startWrite)))
			//dbSize.Add(float64(len(serialNumbers)))
		}
	case Read:
		return func(db Database, g *sigverification_test.LinearTxInputGenerator) {
			serialNumbers := g.Next()
			//startRead := time.Now()
			db.DoNotExist(serialNumbers)
			//dbRequestLatency.WithLabelValues("read").Observe(float64(time.Now().Sub(startRead)))
		}
	case ReadCommit:
		return func(db Database, g *sigverification_test.LinearTxInputGenerator) {
			commitSerialNumbers := g.Next()
			//startWrite := time.Now()
			utils.Must(db.Commit(commitSerialNumbers))
			//dbRequestLatency.WithLabelValues("write").Observe(float64(time.Now().Sub(startWrite)))
			//dbSize.Add(float64(len(commitSerialNumbers)))
			readSerialNumbers := g.Next()
			//startRead := time.Now()
			db.DoNotExist(readSerialNumbers)
			//dbRequestLatency.WithLabelValues("read").Observe(float64(time.Now().Sub(startRead)))
		}
	default:
		panic("unknown command")
	}
}
