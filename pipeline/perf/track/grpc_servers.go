package track

import (
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func StartGrpcServers(sigVerifiersAddresses, shardsServersAddresses []*connection.Endpoint) (s *GrpcServers, err error) {
	s = &GrpcServers{}
	defer func() {
		if err != nil {
			s.StopAll()
		}
	}()

	s.sigVerifierServers, err = testutil.StartsSigVerifierGrpcServers(testutil.DefaultSigVerifierBehavior, sigVerifiersAddresses)
	if err != nil {
		return
	}

	s.shardsServers, err = testutil.StartsShardsGrpcServers(testutil.DefaultPhaseOneBehavior, shardsServersAddresses)
	if err != nil {
		return
	}
	return
}

type GrpcServers struct {
	sigVerifierServers []*testutil.SigVerifierGrpcServer
	shardsServers      []*testutil.ShardsGrpcServer
}

func (s *GrpcServers) StopAll() {
	for _, s := range s.sigVerifierServers {
		s.Stop()
	}
	for _, s := range s.shardsServers {
		s.Stop()
	}
}

func StartProfiling() {
	// go tool pprof http://localhost:6060/debug/pprof/profile
	// go tool pprof http://localhost:6060/debug/pprof/heap
	// go tool pprof http://localhost:6060/debug/pprof/block

	go func() {
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			panic(fmt.Sprintf("Error while starting http server for profiling: %s", err))
		}
	}()

	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)
}

func TrackProgress(statusCh <-chan []*pipeline.TxStatus, numBlocks, numTxPerBlock int) {
	max := numBlocks * numTxPerBlock
	counter := 0
	printMark := 100000
	startTime := time.Now()
	for {
		status := <-statusCh
		counter += len(status)

		if printMark <= counter {
			totalTime := time.Since(startTime)
			fmt.Printf("time taken: %f sec. Total Status Recieved: %d. tps: %.2f \n",
				totalTime.Seconds(),
				counter,
				float64(counter)/totalTime.Seconds(),
			)
			printMark += 100000
		}

		if counter >= max {
			break
		}
	}

	totalTime := time.Since(startTime)
	workload.PrintStats(int64(counter), int64(numBlocks), totalTime)
}
