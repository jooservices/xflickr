package queue

import (
	"context"
	"errors"
)

// Worker is the worker-role bootstrap for a configured River adapter.
type Worker struct {
	Queue *River
}

// Run starts River's subscribed queues and waits until the process context is
// canceled or the client stops.
func (w Worker) Run(ctx context.Context) error {
	if w.Queue == nil {
		return errors.New("queue: worker queue is required")
	}
	return w.Queue.Start(ctx)
}
