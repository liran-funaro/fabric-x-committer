package test

import (
	"sync/atomic"
	"time"
)

type AsyncRequestTracker struct {
	requestsSubmitted chan int
	stopSending       chan struct{}
	done              chan struct{}
	outputReceived    chan int
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

func (t *AsyncRequestTracker) Start() func(int) {
	outputReceived := make(chan int)
	t.StartWithOutputReceived(outputReceived)
	return func(length int) {
		outputReceived <- length
	}
}

func (t *AsyncRequestTracker) StartWithOutputReceived(outputReceived chan int) {
	t.outputReceived = outputReceived
	t.totalRequests = 0
	t.startTime = time.Now()

	go t.trackRequests()
}

func (t *AsyncRequestTracker) trackRequests() {
	var pending int64
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
			atomic.AddInt64(&pending, int64(inputBatchSize))
			t.totalRequests += inputBatchSize
		case outputBatch := <-t.outputReceived:
			atomic.AddInt64(&pending, -int64(outputBatch))
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

func (s *AsyncRequestTracker) RequestsPer(unit time.Duration) int {
	return int(float64(unit) * float64(s.totalRequests) / float64(time.Now().Sub(s.startTime)))
}

func (s *AsyncTrackerStats) RequestsPer(unit time.Duration) int {
	return int(float64(unit) * float64(s.TotalRequests) / float64(s.TotalTime))
}
