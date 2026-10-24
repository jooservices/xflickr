package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/db"
)

const (
	defaultRetryDelay = 100 * time.Millisecond
	defaultMaxDelay   = 5 * time.Second
	defaultPollDelay  = 100 * time.Millisecond
)

// Event is a committed outbox row delivered to a handler.
type Event struct {
	ID        int64
	Topic     string
	Key       string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// Handler processes one event. Handlers must be idempotent because a process
// failure after the side effect and before transaction commit can redeliver it.
type Handler func(context.Context, Event) error

// RetryPolicy defines exponential backoff for handler and database failures.
type RetryPolicy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// DefaultRetryPolicy returns a policy that starts at 100ms and caps at 5s.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{InitialDelay: defaultRetryDelay, MaxDelay: defaultMaxDelay}
}

// Delay returns the delay before retry attempt. Attempts are one-based.
func (policy RetryPolicy) Delay(attempt int) time.Duration {
	if attempt < 1 {
		return 0
	}
	initial := policy.InitialDelay
	if initial <= 0 {
		initial = defaultRetryDelay
	}
	maximum := policy.MaxDelay
	if maximum < initial {
		maximum = initial
	}
	delay := initial
	for retry := 1; retry < attempt && delay < maximum; retry++ {
		if delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

// Consumer polls and processes pending events for one topic.
type Consumer struct {
	database     db.Beginner
	retryPolicy  RetryPolicy
	pollInterval time.Duration
}

// NewConsumer creates a consumer with the default retry policy.
func NewConsumer(database db.Beginner) *Consumer {
	return NewConsumerWithRetryPolicy(database, DefaultRetryPolicy())
}

// NewConsumerWithRetryPolicy creates a consumer with a caller-supplied retry
// policy. Non-positive policy values use the default delay values.
func NewConsumerWithRetryPolicy(database db.Beginner, policy RetryPolicy) *Consumer {
	return &Consumer{database: database, retryPolicy: policy, pollInterval: defaultPollDelay}
}

// Consume repeatedly handles pending events for topic until ctx is canceled.
// A handler failure rolls back the row lock and retries with backoff. Event
// handling and processed_at are committed in one transaction, so an
// interruption before commit leaves the event pending for redelivery.
func (consumer *Consumer) Consume(ctx context.Context, topic string, handler Handler) error {
	if consumer == nil || consumer.database == nil {
		return errors.New("outbox: consumer database is required")
	}
	if ctx == nil {
		return errors.New("outbox: consume context is required")
	}
	if strings.TrimSpace(topic) == "" {
		return errors.New("outbox: consume topic is required")
	}
	if handler == nil {
		return errors.New("outbox: consume handler is required")
	}

	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		processed, err := consumer.consumeOne(ctx, topic, handler)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			failures++
			if err := wait(ctx, consumer.retryPolicy.Delay(failures)); err != nil {
				return err
			}
			continue
		}
		failures = 0
		if !processed {
			if err := wait(ctx, consumer.pollInterval); err != nil {
				return err
			}
		}
	}
}

func (consumer *Consumer) consumeOne(ctx context.Context, topic string, handler Handler) (bool, error) {
	tx, err := consumer.database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, fmt.Errorf("outbox: begin consume transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var event Event
	err = tx.QueryRow(ctx,
		`SELECT id, topic, key, payload, created_at
		 FROM outbox_events
		 WHERE topic = $1 AND processed_at IS NULL
		 ORDER BY id
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED`,
		topic,
	).Scan(&event.ID, &event.Topic, &event.Key, &event.Payload, &event.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("outbox: read pending event: %w", err)
	}

	if err := handler(ctx, event); err != nil {
		return false, fmt.Errorf("outbox: handle event %d: %w", event.ID, err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE outbox_events SET processed_at = now() WHERE id = $1`,
		event.ID,
	); err != nil {
		return false, fmt.Errorf("outbox: mark event %d processed: %w", event.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("outbox: commit event %d: %w", event.ID, err)
	}
	return true, nil
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
