package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRangeStartRejectsSpanWithoutDash(t *testing.T) {
	for _, value := range []string{"bytes 5/10", "bytes 5"} {
		if got, ok := rangeStart(value); ok || got != 0 {
			t.Fatalf("rangeStart(%q) = %d, %t", value, got, ok)
		}
	}
}

func TestFetchWaitUsesTimerWithoutHook(t *testing.T) {
	if err := (&Fetcher{}).wait(context.Background(), time.Nanosecond); err != nil {
		t.Fatalf("wait() = %v", err)
	}
}

func TestFetchStopsWhileWaitingForBusyHostSlot(t *testing.T) {
	fetcher := &Fetcher{
		Blobs: missingBlobStore{},
		HTTP: fetchHTTPFunc(func(context.Context, CDNRequest) (*CDNResponse, error) {
			return &CDNResponse{StatusCode: 200, Body: io.NopCloser(strings.NewReader("x"))}, nil
		}),
		Limits: FetchLimits{PerHostConcurrency: 1},
	}
	fetcher.hostLimit("cdn.test").semaphore <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, _, _, err := fetcher.Fetch(ctx, "https://cdn.test/original"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestFetchRefreshStopsWhileWaitingForBusyNewHostSlot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fetcher := &Fetcher{
		Blobs: missingBlobStore{},
		HTTP: fetchHTTPFunc(func(context.Context, CDNRequest) (*CDNResponse, error) {
			return &CDNResponse{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
		Limits: FetchLimits{PerHostConcurrency: 1},
		Refresh: func(context.Context, string) (string, int64, error) {
			cancel()
			return "https://busy.test/original", 3, nil
		},
	}
	fetcher.hostLimit("busy.test").semaphore <- struct{}{}
	if _, _, _, err := fetcher.Fetch(ctx, "https://cdn.test/original"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestFetchStopsWhileWaitingForBusyKeySlot(t *testing.T) {
	const source = "https://cdn.test/original"
	fetcher := &Fetcher{Blobs: missingBlobStore{}, HTTP: fetchHTTPFunc(func(context.Context, CDNRequest) (*CDNResponse, error) {
		t.Fatal("HTTP called while the key was busy")
		return nil, nil
	})}
	key := fmt.Sprintf("originals/%x", sha256.Sum256([]byte(source)))
	release, err := fetcher.acquireKey(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := fetcher.Fetch(ctx, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestAcquireKeyCancelledLastWaiterRemovesEntry(t *testing.T) {
	fetcher := &Fetcher{keys: map[string]*keyLimit{"blob": {semaphore: make(chan struct{}, 1)}}}
	fetcher.keys["blob"].semaphore <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetcher.acquireKey(ctx, "blob"); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquireKey() error = %v", err)
	}
	if len(fetcher.keys) != 0 {
		t.Fatalf("key lock map retained entries: %d", len(fetcher.keys))
	}
}
