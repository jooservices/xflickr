package roles

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
)

type sizesEventTx struct {
	pgx.Tx
	err        error
	topic, key string
	payload    []byte
}

func (tx *sizesEventTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	tx.topic = args[0].(string)
	tx.key = args[1].(string)
	tx.payload = args[2].([]byte)
	return pgconn.CommandTag{}, tx.err
}

func TestPublishSizesResolved(t *testing.T) {
	event := catalogapp.SizesResolved{PhotoID: 42, Status: "confirmed"}
	tx := &sizesEventTx{}
	if err := publishSizesResolved(t.Context(), tx, event); err != nil {
		t.Fatal(err)
	}
	var got catalogapp.SizesResolved
	if err := json.Unmarshal(tx.payload, &got); err != nil {
		t.Fatal(err)
	}
	if tx.topic != catalogapp.SizesResolvedTopic || tx.key != "42" || got != event {
		t.Fatalf("event=%+v tx=%+v", got, tx)
	}
	tx.err = errors.New("database")
	if err := publishSizesResolved(t.Context(), tx, event); !errors.Is(err, tx.err) {
		t.Fatal(err)
	}
}
