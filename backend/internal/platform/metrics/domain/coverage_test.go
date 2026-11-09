package domain_test

import (
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

func TestHourFloorAndCloneAndChunk(t *testing.T) {
	at := time.Date(2026, 11, 9, 6, 45, 10, 0, time.UTC)
	if got := domain.HourFloor(at); !got.Equal(time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)) {
		t.Fatalf("HourFloor=%v", got)
	}
	if got := domain.CloneLabels(nil); len(got) != 0 {
		t.Fatalf("nil clone=%v", got)
	}
	src := map[string]string{"a": "1"}
	cloned := domain.CloneLabels(src)
	cloned["a"] = "2"
	if src["a"] != "1" {
		t.Fatal("CloneLabels mutated source")
	}
	chunks := domain.ChunkSamples([]int{1, 2, 3, 4, 5}, 2)
	if len(chunks) != 3 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	if domain.ChunkSamples([]int{}, 2) != nil {
		t.Fatal("expected nil for empty")
	}
	err := domain.ValidateSamples([]domain.Sample{{Name: "missing.metric"}})
	if err == nil || err.Error() == "" {
		t.Fatalf("err=%v", err)
	}
	encoded, err := domain.CanonicalLabelsJSON(nil)
	if err != nil || string(encoded) != "{}" {
		t.Fatalf("canonical=%s err=%v", encoded, err)
	}
}
