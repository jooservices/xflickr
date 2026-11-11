// Package detectors turns operational snapshots into incidents.
package detectors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jooservices/xflickr/internal/ops/domain"
)

var (
	// ErrIncidentOpenerRequired reports a missing incident application port.
	ErrIncidentOpenerRequired = errors.New("ops: quota detector incident opener is required")
	// ErrInvalidQuotaSnapshot reports invalid key usage or limit values.
	ErrInvalidQuotaSnapshot = errors.New("ops: invalid quota snapshot")
)

// IncidentOpener opens an incident through the operations application layer.
type IncidentOpener interface {
	Open(context.Context, domain.Incident) (domain.Incident, error)
}

// QuotaSnapshot contains the observed usage and limit for one API key.
type QuotaSnapshot struct {
	KeyID int64
	Used  int64
	Limit int64
}

// QuotaDetector classifies exhausted key quota snapshots.
type QuotaDetector struct {
	incidents IncidentOpener
}

// NewQuotaDetector creates a quota detector using the incident application port.
func NewQuotaDetector(incidents IncidentOpener) (*QuotaDetector, error) {
	if incidents == nil {
		return nil, ErrIncidentOpenerRequired
	}
	return &QuotaDetector{incidents: incidents}, nil
}

// Detect opens a warning incident when a key has reached its quota limit.
func (detector *QuotaDetector) Detect(ctx context.Context, snapshot QuotaSnapshot) error {
	if snapshot.KeyID <= 0 || snapshot.Limit <= 0 || snapshot.Used < 0 {
		return ErrInvalidQuotaSnapshot
	}
	if snapshot.Used < snapshot.Limit {
		return nil
	}

	detail, err := json.Marshal(struct {
		Detection       string `json:"detection"`
		AutomaticAction string `json:"automatic_action"`
		HumanStep       string `json:"human_step"`
		Used            int64  `json:"used"`
		Limit           int64  `json:"limit"`
	}{
		Detection:       "Key quota reached its limit.",
		AutomaticAction: "No automatic remediation is defined.",
		HumanStep:       "Check the key and reset time in API quota; follow key policy.",
		Used:            snapshot.Used,
		Limit:           snapshot.Limit,
	})
	if err != nil {
		return fmt.Errorf("ops: encode quota incident detail: %w", err)
	}

	_, err = detector.incidents.Open(ctx, domain.Incident{
		Kind:     "quota_exhausted",
		Scope:    fmt.Sprintf("key:%d", snapshot.KeyID),
		Severity: domain.SeverityWarning,
		Title:    "API key quota exhausted",
		Detail:   detail,
	})
	if err != nil {
		return fmt.Errorf("ops: detect quota incident: %w", err)
	}
	return nil
}
