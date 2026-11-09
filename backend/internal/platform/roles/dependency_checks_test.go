package roles

import (
	"context"
	"errors"
	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
	"net/http"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/config"
)

func TestDependencyChecksReportMissingClients(t *testing.T) {
	for _, check := range []struct {
		name string
		call func() error
	}{
		{"postgres", func() error { return postgresDependency(nil).Check(t.Context()) }},
		{"redis", func() error { return redisDependency(nil).Check(t.Context()) }},
		{"blob_store", func() error { return blobStoreDependency(nil).Check(t.Context()) }},
	} {
		if err := check.call(); err == nil {
			t.Fatalf("%s dependency accepted a missing client", check.name)
		}
	}
}

func TestNewFlickrClientBuildsWithoutObserver(t *testing.T) {
	if client := newFlickrClient(config.Config{}); client == nil {
		t.Fatal("newFlickrClient() returned nil")
	}
}

func TestNewRiverWithSettingsRequiresPool(t *testing.T) {
	if _, err := newRiverWithSettings(t.Context(), nil, map[string]int{"media": 1}); err == nil {
		t.Fatal("newRiverWithSettings() accepted a nil pool")
	}
}

func TestShutdownAPIServersClosesIdleServers(t *testing.T) {
	if err := shutdownAPIServers([]*http.Server{{}, {}}); err != nil {
		t.Fatalf("shutdownAPIServers() = %v", err)
	}
	if err := shutdownAPIServers(nil); err != nil {
		t.Fatalf("shutdownAPIServers(nil) = %v", err)
	}
}

type statStore struct {
	mediaapp.BlobStore
	err error
}

func (s statStore) Stat(context.Context, string) (mediaapp.BlobInfo, error) {
	return mediaapp.BlobInfo{}, s.err
}

func TestBlobStoreDependencyTreatsMissingProbeAsHealthy(t *testing.T) {
	if err := blobStoreDependency(statStore{err: mediaapp.ErrNotFound}).Check(t.Context()); err != nil {
		t.Fatalf("missing probe object = %v, want healthy", err)
	}
	boom := errors.New("storage down")
	if err := blobStoreDependency(statStore{err: boom}).Check(t.Context()); !errors.Is(err, boom) {
		t.Fatalf("storage error = %v, want %v", err, boom)
	}
}

func TestNewCrawlEngineRequiresMasterKeys(t *testing.T) {
	if _, err := newCrawlEngine(nil, nil, config.Config{}, nil); err == nil {
		t.Fatal("newCrawlEngine() accepted a configuration without master keys")
	}
}

func TestRunMetricsWithRetryDefaultsAndStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	runMetricsWithRetry(ctx, nil, "crawl", 0, func(context.Context) (func(), error) {
		cancel()
		return nil, nil
	})
	ctx, cancel = context.WithCancel(t.Context())
	runMetricsWithRetry(ctx, nil, "crawl", 0, func(context.Context) (func(), error) {
		cancel()
		return nil, errors.New("setup failed")
	})
	if ctx.Err() == nil {
		t.Fatal("context was not cancelled")
	}
}

func TestServeProbeServerReportsListenErrors(t *testing.T) {
	if err := serveProbeServer(t.Context(), "256.256.256.256:0", http.NotFoundHandler()); err == nil {
		t.Fatal("serveProbeServer() accepted an invalid address")
	}
}

func TestNewFlickrClientAcceptsCallObserver(t *testing.T) {
	observer := flickrclient.CallObserverFunc(func(context.Context, flickrclient.CallRecord) {})
	if client := newFlickrClient(config.Config{}, observer); client == nil {
		t.Fatal("newFlickrClient() returned nil with an observer")
	}
}
