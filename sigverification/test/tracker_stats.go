package sigverification_test

import (
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
)

type Output = *sigverification.Response

type AsyncRequestTracker struct {
	requestsSubmitted chan int
	stopSending       chan struct{}
	done              chan struct{}
	startTime         time.Time

	totalRequests int
}

func NewAsyncTracker() *AsyncRequestTracker {
	return &AsyncRequestTracker{
		requestsSubmitted: make(chan int),
		stopSending:       make(chan struct{}),
		done:              make(chan struct{}),
	}
}

func (t *AsyncRequestTracker) Start(outputReceived <-chan []Output) {
	t.totalRequests = 0
	t.startTime = time.Now()

	go t.trackRequests(outputReceived)
}

func (t *AsyncRequestTracker) trackRequests(outputReceived <-chan []Output) {
	pending := 0
	stillSubmitting := true
	for {
		select {
		case <-t.stopSending:
			stillSubmitting = false
			if pending == 0 {
				t.done <- struct{}{}
				return
			}
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

	return currentStats
}

func (t *AsyncRequestTracker) currentStats() AsyncTrackerStats {
	return AsyncTrackerStats{
		TotalRequests: t.totalRequests,
		TotalTime:     time.Now().Sub(t.startTime),
	}
}

func (t *AsyncRequestTracker) SubmitRequests(requests int) {
	t.requestsSubmitted <- requests
}

type AsyncTrackerStats struct {
	TotalTime     time.Duration
	TotalRequests int
}

func (s *AsyncTrackerStats) RequestsPer(unit time.Duration) int {
	return int(float64(unit) * float64(s.TotalRequests) / float64(s.TotalTime))
}
