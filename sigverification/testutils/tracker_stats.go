package testutils

import (
	"runtime"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

type SyncTracker struct {
	sampling time.Duration

	stop chan struct{}

	startTime     time.Time
	startMemory   uint64
	maxGoRoutines int
}

var NoSampling = 1 * time.Hour

func NewSyncTracker(sampling time.Duration) *SyncTracker {
	return &SyncTracker{
		sampling: sampling,
		stop:     make(chan struct{}),
	}
}

func (t *SyncTracker) Start() {
	t.startTime = time.Now()
	t.startMemory = utils.MemoryAllocation()
	go t.trackSystem()
}

func (t *SyncTracker) Stop() SyncTrackerStats {
	t.stop <- struct{}{}
	close(t.stop)
	return t.currentStats()
}

func (t *SyncTracker) currentStats() SyncTrackerStats {
	return SyncTrackerStats{
		TotalTime:     int(time.Now().Sub(t.startTime)),
		TotalMemory:   int(utils.MemoryAllocation() - t.startMemory),
		MaxGoRoutines: t.maxGoRoutines,
	}
}

func (t *SyncTracker) trackSystem() {
	for {
		select {
		case <-t.stop:
			return
		default:
			<-time.After(t.sampling)
			t.maxGoRoutines = utils.Max(t.maxGoRoutines, runtime.NumGoroutine())
		}
	}
}

type SyncTrackerStats struct {
	TotalTime     int
	TotalMemory   int
	MaxGoRoutines int
}

type AsyncRequestTracker struct {
	SyncTracker
	requestsSubmitted chan int
	stopSending       chan struct{}
	done              chan struct{}

	totalRequests int
}

func NewAsyncTracker(sampling time.Duration) *AsyncRequestTracker {
	return &AsyncRequestTracker{
		SyncTracker: *NewSyncTracker(sampling),

		requestsSubmitted: make(chan int),
		stopSending:       make(chan struct{}),
		done:              make(chan struct{}),
	}
}

func (t *AsyncRequestTracker) Start(outputReceived <-chan []*sigverification.Response) {
	t.totalRequests = 0
	t.SyncTracker.Start()
	go t.trackRequests(outputReceived)
}

func (t *AsyncRequestTracker) trackRequests(outputReceived <-chan []*sigverification.Response) {
	pending := 0
	stillSubmitting := true
	for {
		select {
		case <-t.stopSending:
			stillSubmitting = false
		case inputBatchSize := <-t.requestsSubmitted:
			pending += inputBatchSize
			t.totalRequests += inputBatchSize
		case outputBatch := <-outputReceived:
			pending -= len(outputBatch)
			if pending < 0 {
				panic("negative pending")
			}
			if pending == 0 && !stillSubmitting {
				t.done <- struct{}{}
				t.Stop()
				return
			}
		}
	}
}

func (t *AsyncRequestTracker) WaitUntilDone() AsyncTrackerStats {
	t.stopSending <- struct{}{}
	<-t.done
	currentStats := t.currentStats()
	close(t.stopSending)
	close(t.requestsSubmitted)
	//log.Printf("Throughput: %d requests/s, Memory: %d KB, Max additional GoRoutines: %d", currentStats.RequestsPer(time.Second), currentStats.TotalMemory/1000, currentStats.MaxGoRoutines)

	return currentStats
}

func (t *AsyncRequestTracker) currentStats() AsyncTrackerStats {
	return AsyncTrackerStats{
		SyncTrackerStats: t.SyncTracker.currentStats(),
		TotalRequests:    t.totalRequests,
	}
}

func (t *AsyncRequestTracker) SubmitRequests(requests int) {
	t.requestsSubmitted <- requests
}

type AsyncTrackerStats struct {
	SyncTrackerStats
	TotalRequests int
}

func (s *AsyncTrackerStats) RequestsPer(unit time.Duration) int {
	return int(float64(unit) * float64(s.TotalRequests) / float64(s.TotalTime))
}
