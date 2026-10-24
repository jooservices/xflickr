package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	"github.com/jooservices/xflickr/internal/platform/outbox"
)

func TestPublishRollsBackWithUnitOfWork(t *testing.T) {
	pool := integrationPool(t)
	topic := uniqueTopic("rollback")
	cleanupEvents(t, pool, topic)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	wantErr := errors.New("abort unit of work")

	err := db.NewUnitOfWork(pool).Do(ctx, func(tx db.Tx) error {
		if err := outbox.Publish(tx, topic, "photo-rollback", map[string]any{"id": 1}); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("UnitOfWork.Do() error = %v, want callback error", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic = $1`, topic).Scan(&count); err != nil {
		t.Fatalf("count rollback events: %v", err)
	}
	if count != 0 {
		t.Fatalf("outbox event count after rollback = %d, want 0", count)
	}
}

func TestConsumeRedeliversWhenInterruptedBeforeMark(t *testing.T) {
	pool := integrationPool(t)
	topic := uniqueTopic("redelivery")
	cleanupEvents(t, pool, topic)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	if err := db.NewUnitOfWork(pool).Do(ctx, func(tx db.Tx) error {
		return outbox.Publish(tx, topic, "photo-redelivery", map[string]any{"id": 2})
	}); err != nil {
		t.Fatalf("publish test event: %v", err)
	}

	var handled atomic.Int32
	consumer := outbox.NewConsumer(pool)
	crashCtx, crash := context.WithCancel(ctx)
	err := consumer.Consume(crashCtx, topic, func(_ context.Context, event outbox.Event) error {
		if event.Key != "photo-redelivery" {
			t.Errorf("first handler key = %q", event.Key)
		}
		handled.Add(1)
		crash()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first Consume() error = %v, want context canceled", err)
	}

	var pending bool
	if err := pool.QueryRow(ctx, `SELECT processed_at IS NULL FROM outbox_events WHERE topic = $1`, topic).Scan(&pending); err != nil {
		t.Fatalf("read event after interrupted consume: %v", err)
	}
	if !pending {
		t.Fatal("event was marked processed after interruption")
	}

	consumeCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- consumer.Consume(consumeCtx, topic, func(_ context.Context, event outbox.Event) error {
			if event.Key != "photo-redelivery" {
				t.Errorf("redelivery handler key = %q", event.Key)
			}
			handled.Add(1)
			return nil
		})
	}()

	deadline := time.NewTimer(5 * time.Second)
	poll := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	defer poll.Stop()
	for {
		var processed bool
		if err := pool.QueryRow(ctx, `SELECT processed_at IS NOT NULL FROM outbox_events WHERE topic = $1`, topic).Scan(&processed); err != nil {
			stop()
			t.Fatalf("read redelivered event: %v", err)
		}
		if processed {
			break
		}
		select {
		case <-deadline.C:
			stop()
			t.Fatal("redelivered event was not marked processed")
		case <-poll.C:
		case <-ctx.Done():
			stop()
			t.Fatalf("wait for redelivery: %v", ctx.Err())
		}
	}
	stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("second Consume() error = %v, want context canceled", err)
	}
	if got := handled.Load(); got != 2 {
		t.Fatalf("handler calls = %d, want first delivery plus redelivery", got)
	}
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("XFLICKR_OUTBOX_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_OUTBOX_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source file")
	}
	worktreeRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(worktreeRoot, ".env"))
	if err != nil {
		t.Fatalf("load test PostgreSQL settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbtest.Open(t, dbConfig)
}

func cleanupEvents(t *testing.T, pool *pgxpool.Pool, topic string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE topic = $1`, topic)
	})
}

func uniqueTopic(prefix string) string {
	return fmt.Sprintf("test.outbox.%s.%d", prefix, time.Now().UnixNano())
}
