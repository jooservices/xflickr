package domain_test

import (
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

func TestCatalogueIsTheOnlySourceOfMetricNames(t *testing.T) {
	known := domain.KnownNames()
	if len(known) == 0 {
		t.Fatal("catalogue is empty")
	}
	if len(known) != len(domain.Catalogue()) {
		t.Fatalf("KnownNames=%d Catalogue=%d", len(known), len(domain.Catalogue()))
	}
	seen := map[string]struct{}{}
	for _, entry := range domain.Catalogue() {
		if entry.Name == "" || entry.Unit == "" || entry.Description == "" {
			t.Fatalf("incomplete catalogue entry: %+v", entry)
		}
		if _, dup := seen[entry.Name]; dup {
			t.Fatalf("duplicate catalogue name %q", entry.Name)
		}
		if entry.Name == domain.MetricGrowthMediaUsedBytes && entry.Description != "Used bytes of the filesystem that holds the media directory" {
			t.Fatalf("media used description=%q", entry.Description)
		}
		seen[entry.Name] = struct{}{}
	}
	samples := []domain.Sample{{Name: domain.MetricProcessCPUPercent, At: time.Unix(0, 0), Value: 1}}
	if err := domain.ValidateSamples(samples); err != nil {
		t.Fatalf("catalogue sample rejected: %v", err)
	}
	if err := domain.ValidateSamples([]domain.Sample{{Name: "not.in.catalogue", Value: 1}}); err == nil {
		t.Fatal("unknown metric was accepted")
	}
}
