package connection

import (
	"sync"
	"sync/atomic"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("request tracker")

type RequestTracker struct {
	requestsSubmitted chan int
	stopSending       chan struct{}
	done              chan struct{}
	outputReceived    chan int
	startTime         time.Time
	startedSubmitting sync.WaitGroup
	once              sync.Once
	totalRequests     int
}

func NewRequestTracker() *RequestTracker {
	return &RequestTracker{
		requestsSubmitted: make(chan int),
		stopSending:       make(chan struct{}),
		done:              make(chan struct{}),
	}
}

// Start starts the tracker and creates an output channel.
// We can register responses only by calling ReceivedResponses.
func (t *RequestTracker) Start() {
	t.StartWithOutputReceived(make(chan int))
}

// StartWithOutputReceived starts the tracker with an existing output channel.
// We can register responses either by sending the lengths to the channel or by calling ReceivedResponses.
func (t *RequestTracker) StartWithOutputReceived(outputReceived chan int) {
	if t.outputReceived != nil {
		panic("tracker already started")
	}
	t.startedSubmitting.Add(1)
	t.outputReceived = outputReceived
	t.totalRequests = 0
	t.startTime = time.Now()

	go t.trackRequests()
}

func (t *RequestTracker) trackRequests() {
	logger.Infof("Started tracker at %v", time.Now())
	var pending int64
	stillSubmitting := true
	for {
		select {
		case <-t.stopSending:
			logger.Infof("Requested to stop submitting at %v.", time.Now())
			stillSubmitting = false
			if pending == 0 {
				t.done <- struct{}{}
				return
			}
		case inputBatchSize := <-t.requestsSubmitted:
			atomic.AddInt64(&pending, int64(inputBatchSize))
			t.totalRequests += inputBatchSize
			logger.Debugf("%d requests submitted. Pending: %d, Total: %d", inputBatchSize, pending, t.totalRequests)
		case outputBatch := <-t.outputReceived:
			atomic.AddInt64(&pending, -int64(outputBatch))
			logger.Debugf("%d responses received. Pending: %d, Total: %d", outputBatch, pending, t.totalRequests)
			if pending < 0 {
				panic("negative pending")
			}
			if pending == 0 && !stillSubmitting {
				t.done <- struct{}{}
				return
			}
		}
	}
}

// SubmitRequests registers the requests sent
func (t *RequestTracker) SubmitRequests(requests int) {
	t.requestsSubmitted <- requests
	t.once.Do(func() {
		t.startedSubmitting.Done()
	})
}

// ReceivedResponses registers the responses received
func (t *RequestTracker) ReceivedResponses(responses int) {
	t.outputReceived <- responses
}

// WaitUntilDone registers that we have finished sending requests, and we want to wait until we receive all responses
func (t *RequestTracker) WaitUntilDone() {
	t.startedSubmitting.Wait()
	t.stopSending <- struct{}{}
	<-t.done
	stats := t.CurrentStats()
	logger.Infof("Finished execution: %v (rate %d TPS)", stats, stats.RequestsPer(time.Second))
	close(t.stopSending)
	close(t.requestsSubmitted)
}

// CurrentStats returns a current summary of the tracker since we called Start.
func (t *RequestTracker) CurrentStats() *RequestTrackerStats {
	return &RequestTrackerStats{
		TotalRequests: t.totalRequests,
		TotalTime:     time.Now().Sub(t.startTime),
	}
}

type RequestTrackerStats struct {
	TotalTime     time.Duration
	TotalRequests int
}

func (s *RequestTrackerStats) RequestsPer(unit time.Duration) int {
	return int(float64(unit) * float64(s.TotalRequests) / float64(s.TotalTime))
}
