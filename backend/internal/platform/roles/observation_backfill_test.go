package roles

import (
	"context"
	"testing"
	"time"
)

func TestConfiguredBackfillRejectsBadDatabase(t *testing.T) {
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_NAME", "xflickr_wtjooxf330")
	t.Setenv("DB_USER", "unused")
	t.Setenv("DB_PASSWORD", "unused")
	t.Setenv("DB_PORT", "invalid")
	if _, err := BackfillObservations(t.Context(), ObservationBackfillOptions{}); err == nil {
		t.Fatal("invalid database accepted")
	}
	t.Setenv("DB_PORT", "1")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := BackfillObservations(ctx, ObservationBackfillOptions{}); err == nil {
		t.Fatal("unreachable database accepted")
	}
}
