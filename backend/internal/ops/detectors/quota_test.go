package detectors

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/ops/domain"
)

func TestQuotaDetectorExhausted(t *testing.T) {
	tests := []struct {
		name  string
		used  int64
		limit int64
	}{
		{name: "at limit", used: 10, limit: 10},
		{name: "over limit", used: 12, limit: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			incidents := &incidentOpenerFake{}
			detector, err := NewQuotaDetector(incidents)
			if err != nil {
				t.Fatalf("NewQuotaDetector() error = %v", err)
			}

			if err := detector.Detect(t.Context(), QuotaSnapshot{KeyID: 42, Used: test.used, Limit: test.limit}); err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if incidents.calls != 1 {
				t.Fatalf("Open calls = %d, want 1", incidents.calls)
			}
			wantDetail := json.RawMessage(`{"detection":"Key quota reached its limit.","automatic_action":"No automatic remediation is defined.","human_step":"Check the key and reset time in API quota; follow key policy.","used":` + strconv.FormatInt(test.used, 10) + `,"limit":` + strconv.FormatInt(test.limit, 10) + `}`)
			got := incidents.incident
			if got.Kind != "quota_exhausted" || got.Scope != "key:42" || got.Severity != domain.SeverityWarning ||
				got.Title != "API key quota exhausted" || string(got.Detail) != string(wantDetail) {
				t.Fatalf("opened incident = %+v, want detail %s", got, wantDetail)
			}
		})
	}
}

func TestQuotaDetectorBelowLimit(t *testing.T) {
	tests := []struct {
		name string
		used int64
	}{
		{name: "unused quota", used: 0},
		{name: "remaining quota", used: 9},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			incidents := &incidentOpenerFake{}
			detector, err := NewQuotaDetector(incidents)
			if err != nil {
				t.Fatalf("NewQuotaDetector() error = %v", err)
			}
			if err := detector.Detect(t.Context(), QuotaSnapshot{KeyID: 42, Used: test.used, Limit: 10}); err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if incidents.calls != 0 {
				t.Fatalf("Open calls = %d, want 0", incidents.calls)
			}
		})
	}
}

func TestQuotaDetectorInvalidSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		snapshot QuotaSnapshot
	}{
		{name: "zero key", snapshot: QuotaSnapshot{Used: 1, Limit: 2}},
		{name: "negative key", snapshot: QuotaSnapshot{KeyID: -1, Used: 1, Limit: 2}},
		{name: "zero limit", snapshot: QuotaSnapshot{KeyID: 1, Used: 0}},
		{name: "negative limit", snapshot: QuotaSnapshot{KeyID: 1, Used: 0, Limit: -1}},
		{name: "negative usage", snapshot: QuotaSnapshot{KeyID: 1, Used: -1, Limit: 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			incidents := &incidentOpenerFake{}
			detector, err := NewQuotaDetector(incidents)
			if err != nil {
				t.Fatalf("NewQuotaDetector() error = %v", err)
			}
			if err := detector.Detect(t.Context(), test.snapshot); !errors.Is(err, ErrInvalidQuotaSnapshot) {
				t.Fatalf("Detect() error = %v, want %v", err, ErrInvalidQuotaSnapshot)
			}
			if incidents.calls != 0 {
				t.Fatalf("Open calls = %d, want 0", incidents.calls)
			}
		})
	}

	if _, err := NewQuotaDetector(nil); !errors.Is(err, ErrIncidentOpenerRequired) {
		t.Fatalf("NewQuotaDetector(nil) error = %v, want %v", err, ErrIncidentOpenerRequired)
	}
}

func TestQuotaDetectorOpenError(t *testing.T) {
	wantErr := errors.New("database unavailable")
	incidents := &incidentOpenerFake{err: wantErr}
	detector, err := NewQuotaDetector(incidents)
	if err != nil {
		t.Fatalf("NewQuotaDetector() error = %v", err)
	}

	err = detector.Detect(t.Context(), QuotaSnapshot{KeyID: 42, Used: 10, Limit: 10})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Detect() error = %v, want wrapped database error", err)
	}
	if !strings.HasPrefix(err.Error(), "ops: detect quota incident:") {
		t.Fatalf("Detect() error = %v, want ops context", err)
	}
	if incidents.calls != 1 {
		t.Fatalf("Open calls = %d, want 1", incidents.calls)
	}
}

type incidentOpenerFake struct {
	incident domain.Incident
	calls    int
	err      error
}

func (fake *incidentOpenerFake) Open(_ context.Context, incident domain.Incident) (domain.Incident, error) {
	fake.calls++
	fake.incident = incident
	if fake.err != nil {
		return domain.Incident{}, fake.err
	}
	return incident, nil
}
