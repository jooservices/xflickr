package roles

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/config"
	metricsprocess "github.com/jooservices/xflickr/internal/platform/metrics/adapters/process"
	metricsapp "github.com/jooservices/xflickr/internal/platform/metrics/app"
)

func TestBuildMetricsCollectorsWithAndWithoutGrowth(t *testing.T) {
	cfg := config.Config{
		Node:   config.NodeConfig{ID: "node-test"},
		Search: config.SearchConfig{URL: "http://search.example", Username: "u", Password: "p", IndexPrefix: "xf_"},
		Blob:   config.BlobConfig{Driver: "local", LocalDir: t.TempDir()},
	}
	clock := metricsapp.SystemClock{}
	network := &metricsprocess.ByteCounter{}
	pool := &pgxpool.Pool{}
	basic, err := buildMetricsCollectors(pool, nil, cfg, "api", "", false, network, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(basic) != 2 {
		t.Fatalf("basic collectors=%d", len(basic))
	}
	server := miniredis.RunT(t)
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	withGrowth, err := buildMetricsCollectors(pool, redisClient, cfg, "scheduler", "1.0.0", true, network, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(withGrowth) != 3 {
		t.Fatalf("growth collectors=%d", len(withGrowth))
	}
	if versionOrDev("") != "dev" || versionOrDev("v") != "v" {
		t.Fatal("versionOrDev mismatch")
	}
}

func TestStartMetricsCollectionWithCancelledContext(_ *testing.T) {
	cfg := config.Config{Node: config.NodeConfig{ID: "node-test"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stop := startMetricsCollection(ctx, &pgxpool.Pool{}, nil, cfg, "api", "test", false, nil)
	stop()
	stop()
}

func TestMetricsWrappedRoleNameAndCancelledRun(t *testing.T) {
	role := metricsWrappedRole{inner: idleRole{name: "indexer"}, roleName: "indexer"}
	if role.Name() != "indexer" {
		t.Fatalf("name=%q", role.Name())
	}
	if err := role.Run(nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := role.Run(ctx); err != nil {
		t.Fatal(err)
	}
	empty := metricsWrappedRole{roleName: "solo"}
	if empty.Name() != "solo" {
		t.Fatalf("empty name=%q", empty.Name())
	}
}

func TestMetricsWrappedRoleStartsInnerWhenDatabaseIsUnavailable(t *testing.T) {
	setRoleStartupEnvironment(t)
	withUnreachableRolePool(t)
	previousPing := pingRoleDatabase
	pingRoleDatabase = func(context.Context, *pgxpool.Pool) error { return errors.New("database unavailable") }
	t.Cleanup(func() { pingRoleDatabase = previousPing })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	inner := &stubRole{name: "indexer", ready: make(chan struct{})}
	role := metricsWrappedRole{inner: inner, roleName: "indexer"}
	done := make(chan error, 1)
	go func() { done <- role.Run(ctx) }()
	select {
	case <-inner.ready:
	case err := <-done:
		t.Fatalf("wrapped role returned before the inner role started: %v", err)
	case <-ctx.Done():
		t.Fatal("inner role did not start while metrics storage was unavailable")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wrapped role did not stop after cancellation")
	}
}

func TestMetricsWrappedRoleRunsInnerWhenPingSucceeds(t *testing.T) {
	setRoleStartupEnvironment(t)
	previousOpen := openRoleDatabase
	openRoleDatabase = func(ctx context.Context, _ config.DBConfig) (*pgxpool.Pool, error) {
		return pgxpool.New(ctx, "postgres://unused:unused@127.0.0.1:1/xflickr_loopback_test?sslmode=disable&connect_timeout=1")
	}
	t.Cleanup(func() { openRoleDatabase = previousOpen })
	previousPing := pingRoleDatabase
	pingRoleDatabase = func(context.Context, *pgxpool.Pool) error { return nil }
	t.Cleanup(func() { pingRoleDatabase = previousPing })

	ctx, cancel := context.WithCancel(context.Background())
	inner := &stubRole{name: "indexer", ready: make(chan struct{})}
	role := metricsWrappedRole{inner: inner, roleName: "indexer"}
	done := make(chan error, 1)
	go func() { done <- role.Run(ctx) }()
	select {
	case <-inner.ready:
	case <-time.After(2 * time.Second):
		t.Fatal("inner role did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wrapped role did not stop")
	}
	solo := metricsWrappedRole{roleName: "solo"}
	soloCtx, soloCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer soloCancel()
	if err := solo.Run(soloCtx); err != nil {
		t.Fatal(err)
	}
}

func TestMetricsSetupRetriesAfterUnavailableDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	attempts := 0
	runMetricsWithRetry(ctx, slog.Default(), "indexer", time.Millisecond, func(context.Context) (func(), error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("database unavailable")
		}
		cancel()
		return func() {}, nil
	})
	if attempts != 2 {
		t.Fatalf("setup attempts=%d want 2", attempts)
	}
}

func TestRunMetricsLoopsWaitsForRetention(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	retentionStarted := make(chan struct{})
	allowRetentionReturn := make(chan struct{})
	retentionDone := make(chan struct{})
	runnerReturned := make(chan struct{})
	runner := func(context.Context) {
		cancel()
		close(runnerReturned)
	}
	retention := func(context.Context) {
		close(retentionStarted)
		<-allowRetentionReturn
		close(retentionDone)
	}
	done := make(chan struct{})
	go func() {
		runMetricsLoops(ctx, runner, retention)
		close(done)
	}()
	<-retentionStarted
	<-runnerReturned
	select {
	case <-done:
		t.Fatal("metrics loops returned before retention stopped")
	case <-time.After(20 * time.Millisecond):
	}
	close(allowRetentionReturn)
	select {
	case <-retentionDone:
	case <-time.After(2 * time.Second):
		t.Fatal("retention did not return")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("metrics loops did not return after retention stopped")
	}
}

type stubRole struct {
	name  string
	ready chan struct{}
}

func (s *stubRole) Name() string { return s.name }
func (s *stubRole) Run(ctx context.Context) error {
	close(s.ready)
	<-ctx.Done()
	return nil
}

func TestRunMetricsReturnsOnCancelledContext(t *testing.T) {
	cfg := config.Config{
		Node:   config.NodeConfig{ID: "node-test"},
		Search: config.SearchConfig{URL: "http://search.example", Username: "u"},
		Blob:   config.BlobConfig{Driver: "local", LocalDir: t.TempDir()},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool := &pgxpool.Pool{}
	if err := runMetrics(ctx, pool, nil, cfg, "api", "v", false, nil); err != nil {
		t.Fatal(err)
	}
	server := miniredis.RunT(t)
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	if err := runMetrics(ctx, pool, redisClient, cfg, "scheduler", "v", true, nil); err != nil {
		t.Fatal(err)
	}
}
