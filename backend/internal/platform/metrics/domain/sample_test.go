package domain_test

import (
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

func TestBucketFloorTruncatesToUTCMinute(t *testing.T) {
	at := time.Date(2026, 11, 9, 6, 21, 45, 123, time.FixedZone("ICT", 7*3600))
	got := domain.BucketFloor(at)
	want := time.Date(2026, 11, 8, 23, 21, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("BucketFloor=%v want %v", got, want)
	}
}

func TestLabelsHashIsOrderIndependent(t *testing.T) {
	a, err := domain.LabelsHash(map[string]string{"role": "api", "node": "n1"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := domain.LabelsHash(map[string]string{"node": "n1", "role": "api"})
	if err != nil {
		t.Fatal(err)
	}
	if a != b || len(a) != 64 {
		t.Fatalf("hash a=%q b=%q", a, b)
	}
	c, err := domain.LabelsHash(map[string]string{"role": "crawl", "node": "n1"})
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatal("different labels produced the same hash")
	}
}
