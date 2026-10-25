// Package queue defines the application's durable background job queue port.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Job is an application job. Args must be JSON-serializable.
type Job struct {
	Kind string          `json:"kind"`
	Args json.RawMessage `json:"args"`
}

// Options configure an individual enqueued job. Queue names are deployment
// configuration and may be generated dynamically, for example crawl@node-1.
type Options struct {
	Queue        string
	MaxAttempts  int
	LeaseTimeout time.Duration
	Backoff      time.Duration
}

// Handler processes one application job.
type Handler func(context.Context, Job) error

// JobQueue is the durable queue boundary used by application services.
type JobQueue interface {
	Enqueue(context.Context, pgx.Tx, Job, Options) error
	Register(kind string, handler Handler) error
}

func (j Job) validate() error {
	if strings.TrimSpace(j.Kind) == "" {
		return errors.New("queue: job kind is required")
	}
	if len(j.Args) == 0 || !json.Valid(j.Args) {
		return errors.New("queue: job args must be valid JSON")
	}
	return nil
}

func (o Options) validate() error {
	if strings.TrimSpace(o.Queue) == "" {
		return errors.New("queue: queue name is required")
	}
	if o.MaxAttempts < 0 {
		return errors.New("queue: max attempts cannot be negative")
	}
	if o.LeaseTimeout < 0 {
		return errors.New("queue: lease timeout cannot be negative")
	}
	if o.Backoff < 0 {
		return errors.New("queue: backoff cannot be negative")
	}
	return nil
}
