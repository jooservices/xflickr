// Package outbox provides transactional event publication and at-least-once
// event consumption backed by PostgreSQL.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Tx is the transaction surface needed to publish an event.
type Tx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// Publish appends payload to the transactional outbox. The event is durable
// only if the caller commits tx, keeping it atomic with the caller's changes.
func Publish(tx Tx, topic, key string, payload any) error {
	return PublishContext(context.Background(), tx, topic, key, payload)
}

// PublishContext is Publish with an explicit context for cancelable database
// calls.
func PublishContext(ctx context.Context, tx Tx, topic, key string, payload any) error {
	if ctx == nil {
		return errors.New("outbox: publish context is required")
	}
	if tx == nil {
		return errors.New("outbox: publish transaction is required")
	}
	if strings.TrimSpace(topic) == "" {
		return errors.New("outbox: event topic is required")
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outbox: encode event payload: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO outbox_events (topic, key, payload) VALUES ($1, $2, $3)`,
		topic, key, encoded,
	); err != nil {
		return fmt.Errorf("outbox: insert event: %w", err)
	}
	return nil
}
