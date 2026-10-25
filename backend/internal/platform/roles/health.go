package roles

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/ops"
	opsopenobserve "github.com/jooservices/xflickr/internal/platform/ops/adapters/openobserve"
)

// newHealthSink ships crawl health to OpenObserve. It returns nil, so the
// health line is only logged, until OpenObserve credentials are configured.
func newHealthSink(cfg config.Config) ops.Sink {
	if cfg.Obs.Username == "" || cfg.Obs.URL == "" {
		return nil
	}
	return opsopenobserve.Sink{URL: cfg.Obs.URL, Org: cfg.Obs.Org, Stream: cfg.Obs.Stream, Username: cfg.Obs.Username, Password: cfg.Obs.Password, Client: &http.Client{Timeout: 10 * time.Second}}
}

// startCrawlHealth logs the "crawl health" line every minute until the
// returned stop function is called, which waits for the loop to end.
func startCrawlHealth(ctx context.Context, store ops.Store, cfg config.Config, logger *slog.Logger) (stop func()) {
	monitor := &ops.Monitor{
		Service: ops.Service{Store: store},
		Sink:    newHealthSink(cfg),
		Logger:  logger,
		Node:    nodeID(cfg),
	}
	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		monitor.Run(runCtx)
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
		})
	}
}
