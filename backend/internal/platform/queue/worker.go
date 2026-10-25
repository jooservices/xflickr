package queue

import (
	"context"
	"errors"
	"time"
)

const workerStopTimeout = 30 * time.Second

type workerQueue interface {
	Start(context.Context) error
	Stop(context.Context) error
	Stopped() <-chan struct{}
}

// Worker is the worker-role bootstrap for a configured River adapter.
type Worker struct {
	Queue workerQueue
}

// ErrStoppedUnexpectedly means the queue client stopped while the process was
// still running, so the role must fail instead of idling without a worker.
var ErrStoppedUnexpectedly = errors.New("queue: River client stopped unexpectedly")

// Run starts River's subscribed queues and waits until the process context is
// canceled. It returns an error when the client fails to start or stops on its
// own, so the process exits instead of silently working nothing.
func (w Worker) Run(ctx context.Context) error {
	if w.Queue == nil {
		return errors.New("queue: worker queue is required")
	}
	if ctx == nil {
		return errors.New("queue: worker context is required")
	}
	if err := w.Queue.Start(ctx); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.Background(), workerStopTimeout)
		defer cancel()
		return w.Queue.Stop(stopCtx)
	case <-w.Queue.Stopped():
		if ctx.Err() != nil {
			return nil
		}
		return ErrStoppedUnexpectedly
	}
}
