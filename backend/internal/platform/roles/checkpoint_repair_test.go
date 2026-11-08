package roles

import (
	"context"
	"testing"
	"time"
)

func TestConfiguredCheckpointRepairRejectsBadDatabase(t *testing.T) {
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_NAME", "xflickr_wtjooxf345")
	t.Setenv("DB_USER", "unused")
	t.Setenv("DB_PASSWORD", "unused")
	t.Setenv("DB_PORT", "invalid")
	if _, err := RepairZeroCheckpoints(t.Context(), CheckpointRepairOptions{BatchSize: 10}); err == nil {
		t.Fatal("invalid database accepted")
	}
	t.Setenv("DB_PORT", "1")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := RepairZeroCheckpoints(ctx, CheckpointRepairOptions{BatchSize: 10}); err == nil {
		t.Fatal("unreachable database accepted")
	}
}
