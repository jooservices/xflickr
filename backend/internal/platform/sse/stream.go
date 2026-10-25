package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Stream opens a resumable SSE body for an already authenticated user.
func (h Handler) Stream(ctx context.Context, userID int64, lastID string) (io.ReadCloser, error) {
	if ctx == nil || h.Hub == nil || userID <= 0 {
		return nil, errors.New("sse: stream requires context, hub, and user")
	}
	events, cancel, err := h.Hub.Subscribe(userID, lastID)
	if err != nil {
		return nil, err
	}
	heartbeat := h.Heartbeat
	if heartbeat <= 0 {
		heartbeat = 25 * time.Second
	}
	return &eventReader{ctx: ctx, events: events, cancel: cancel, ticker: time.NewTicker(heartbeat)}, nil
}

type eventReader struct {
	ctx     context.Context
	events  <-chan Event
	cancel  func()
	ticker  *time.Ticker
	current []byte
	closed  bool
}

func (r *eventReader) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if r.closed {
		return 0, io.EOF
	}
	if len(r.current) == 0 {
		select {
		case <-r.ctx.Done():
			r.close()
			return 0, io.EOF
		case event, open := <-r.events:
			if !open {
				r.close()
				return 0, io.EOF
			}
			payload, err := json.Marshal(event.Data)
			if err != nil {
				return 0, fmt.Errorf("sse: encode event frame: %w", err)
			}
			r.current = []byte(fmt.Sprintf("id: %s\nevent: %s\ndata: %s\n\n", safeField(event.ID), safeField(event.Type), payload))
		case <-r.ticker.C:
			r.current = []byte(": keep-alive\n\n")
		}
	}
	n := copy(buffer, r.current)
	r.current = r.current[n:]
	return n, nil
}

func (r *eventReader) Close() error {
	r.close()
	return nil
}

func (r *eventReader) close() {
	if !r.closed {
		r.closed = true
		r.ticker.Stop()
		r.cancel()
	}
}
