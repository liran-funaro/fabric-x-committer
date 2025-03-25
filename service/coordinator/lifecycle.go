package coordinator

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/cockroachdb/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/promutil"
)

// RemoteServiceLifecycle manages the lifecycle of connection and interaction with a remote service.
type RemoteServiceLifecycle struct {
	Name         string
	Retry        *connection.RetryProfile
	ConnStatus   *prometheus.GaugeVec
	FailureTotal *prometheus.CounterVec

	backoff       *backoff.ExponentialBackOff
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	criticalError atomic.Bool
}

var (
	// ErrLifecycleCritical should be returned (or wrapped/joined) when the lifecycle should end due to critical error.
	ErrLifecycleCritical = errors.New("cannot recover from error with remote service")

	// ErrLifecycleTimeout is returned if the retry attempts were exhausted due to timeout.
	ErrLifecycleTimeout = errors.New("lifecycle timed out")
)

// RunLifecycle runs the remote service lifecycle flow.
// connectAndWork should return the connection error. For successful connection it should submit background activities
// and return nil.
// recovery should run a recovery flow to ensure correct execution.
func (r *RemoteServiceLifecycle) RunLifecycle(
	ctx context.Context,
	connectAndWork func(context.Context) error,
	recovery func() error,
) error {
	// We initialized according to the current status.
	promutil.SetGaugeVec(r.ConnStatus, []string{r.Name}, connection.Disconnected)

	// We call recovery once before starting in case previous interaction stopped incorrectly.
	initialRecoveryErr := errors.Wrapf(recovery(), "[%s] failed to recover", r.Name)
	if r.logErrorIsCritical(initialRecoveryErr) {
		return initialRecoveryErr
	}

	r.backoff = r.Retry.NewBackoff()
	for {
		sCtx, sCancel := context.WithCancel(ctx)
		r.cancel = sCancel

		r.criticalError.Store(false)
		connectionErr := errors.Wrapf(connectAndWork(sCtx), "[%s] failed to connect", r.Name)
		r.logErrorIsCritical(connectionErr)
		if connectionErr == nil {
			logger.Infof("[%s] connected", r.Name)
			promutil.SetGaugeVec(r.ConnStatus, []string{r.Name}, connection.Connected)
		}

		// We continue to the next phase only after the background activities are finished.
		// And cancel the context in case no background activities were used.
		r.wg.Wait()
		sCancel()

		if connectionErr == nil {
			// We count failures only if they happen after a successful connection attempt.
			promutil.AddToCounterVec(r.FailureTotal, []string{r.Name}, 1)
			promutil.SetGaugeVec(r.ConnStatus, []string{r.Name}, connection.Disconnected)
		}

		// We always run recovery, regardless of the work outcome.
		recoveryErr := errors.Wrapf(recovery(), "[%s] failed to recover", r.Name)
		r.logErrorIsCritical(recoveryErr)

		if r.criticalError.Load() {
			// We log the errors when they happen. Here we only return that a critical error occurred.
			return ErrLifecycleCritical
		}

		waitTime := r.backoff.NextBackOff()
		if waitTime == r.backoff.Stop {
			return ErrLifecycleTimeout
		}

		logger.Infof("[%s] Waiting %v before retrying", r.Name, waitTime)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(waitTime):
		}
	}
}

// ReportInteraction is used to indicate to the RemoteServiceLifecycle that a successful interaction
// with the remote service is established.
func (r *RemoteServiceLifecycle) ReportInteraction() {
	r.backoff.Reset()
}

// Go runs a background activity.
// All background activities must end before processing to the next step in the lifecycle.
// The context of the current connection is cancelled if any of these activities ends.
func (r *RemoteServiceLifecycle) Go(f func() error) {
	r.wg.Add(1)
	go func() {
		// We used defer to ensure correct flow even under panics.
		defer func() {
			r.wg.Done()
			// If other background activities stops for a reason that do not stop the other activities,
			// we need to stop them manually by cancelling the context.
			r.cancel()
		}()
		r.logErrorIsCritical(f())
	}()
}

func (r *RemoteServiceLifecycle) logErrorIsCritical(err error) bool {
	logger.ErrorStackTrace(err)
	isCritical := errors.Is(err, ErrLifecycleCritical)
	if isCritical {
		r.criticalError.Store(true)
	}
	return isCritical
}
