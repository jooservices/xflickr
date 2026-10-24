package audit

import (
	"context"
	"testing"
)

func TestDiscardAcceptsEntries(t *testing.T) {
	if err := Discard.Record(context.Background(), "user:1", "user.update", "user:2", map[string]any{"role": "admin"}); err != nil {
		t.Fatal(err)
	}
}
