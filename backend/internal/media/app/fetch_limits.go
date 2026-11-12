package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

type keyLimit struct {
	semaphore chan struct{}
	refs      int
}

type hostLimit struct {
	semaphore chan struct{}
	mu        sync.Mutex
	next      time.Time
}

func (f *Fetcher) acquireKey(ctx context.Context, key string) (func(), error) {
	f.mu.Lock()
	if f.keys == nil {
		f.keys = make(map[string]*keyLimit)
	}
	limit := f.keys[key]
	if limit == nil {
		limit = &keyLimit{semaphore: make(chan struct{}, 1)}
		f.keys[key] = limit
	}
	limit.refs++
	f.mu.Unlock()
	select {
	case limit.semaphore <- struct{}{}:
		return func() {
			<-limit.semaphore
			f.mu.Lock()
			limit.refs--
			if limit.refs == 0 {
				delete(f.keys, key)
			}
			f.mu.Unlock()
		}, nil
	case <-ctx.Done():
		f.mu.Lock()
		limit.refs--
		if limit.refs == 0 {
			delete(f.keys, key)
		}
		f.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (f *Fetcher) hostLimit(host string) *hostLimit {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hosts == nil {
		f.hosts = make(map[string]*hostLimit)
	}
	limit := f.hosts[host]
	if limit == nil {
		limit = &hostLimit{}
		if f.Limits.PerHostConcurrency > 0 {
			limit.semaphore = make(chan struct{}, f.Limits.PerHostConcurrency)
		}
		f.hosts[host] = limit
	}
	return limit
}

func (f *Fetcher) acquire(ctx context.Context, limit *hostLimit) error {
	if limit.semaphore == nil {
		return nil
	}
	select {
	case limit.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("media: wait for CDN host: %w", ctx.Err())
	}
}

func (f *Fetcher) release(limit *hostLimit) {
	if limit != nil && limit.semaphore != nil {
		<-limit.semaphore
	}
}

// swapHost releases the host slot the job holds, then waits for next. When
// the wait fails the job holds no slot.
func (f *Fetcher) swapHost(ctx context.Context, job *fetchJob, next *hostLimit) error {
	f.releaseJobHost(job)
	if err := f.acquire(ctx, next); err != nil {
		return err
	}
	job.limit, job.hostHeld = next, true
	return nil
}

// releaseJobHost gives back the job's host slot when it holds one.
func (f *Fetcher) releaseJobHost(job *fetchJob) {
	if job.hostHeld {
		f.release(job.limit)
		job.hostHeld = false
	}
}

func (f *Fetcher) wait(ctx context.Context, duration time.Duration) error {
	if f.Wait != nil {
		return f.Wait(ctx, duration)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type limitedBody struct {
	ctx  context.Context
	body io.Reader
	host *hostLimit
	rate int64
	wait func(context.Context, time.Duration) error
}

func (r *limitedBody) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.body.Read(p)
	if n > 0 && r.rate > 0 {
		if waitErr := r.throttle(n); waitErr != nil {
			return n, fmt.Errorf("media: bandwidth wait: %w", waitErr)
		}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("media: CDN body read: %w", err)
	}
	if err != nil {
		return n, io.EOF
	}
	return n, nil
}

// throttle books the time n bytes take at the configured rate on the host's
// shared schedule and waits until that time has passed.
func (r *limitedBody) throttle(n int) error {
	delay := time.Duration(float64(time.Second) * float64(n) / float64(r.rate))
	r.host.mu.Lock()
	now := time.Now()
	start := now
	if r.host.next.After(start) {
		start = r.host.next
	}
	r.host.next = start.Add(delay)
	waitFor := start.Sub(now) + delay
	r.host.mu.Unlock()
	if waitFor <= 0 {
		return nil
	}
	return r.wait(r.ctx, waitFor)
}
