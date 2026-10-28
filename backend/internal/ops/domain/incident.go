package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Severity is the urgency level of an incident.
type Severity string

// State is the lifecycle status of an incident.
type State string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"

	IncidentOpen   State = "open"
	IncidentAcked  State = "acked"
	IncidentClosed State = "closed"

	// NotificationRequestedTopic is the transactional outbox topic consumed by
	// notification delivery.
	NotificationRequestedTopic = "notification.requested"
	NotificationIncident       = "incident"
)

var (
	ErrInvalidIncident           = errors.New("ops: invalid incident")
	ErrInvalidIncidentTransition = errors.New("ops: invalid incident state transition")
)

// Incident is one durable operational problem. Repeated opens for an active
// kind and scope increment Count and LastSeen without replacing its first evidence.
type Incident struct {
	ID        int64
	Kind      string
	Scope     string
	Severity  Severity
	Title     string
	Detail    json.RawMessage
	State     State
	FirstSeen time.Time
	LastSeen  time.Time
	Count     int64
	AckedBy   *int64
	ClosedAt  *time.Time
}

// ValidateOpen checks fields required to create or deduplicate an open incident.
func (incident Incident) ValidateOpen() error {
	if err := ValidateIdentity(incident.Kind, incident.Scope); err != nil {
		return err
	}
	if !incident.Severity.Valid() {
		return fmt.Errorf("%w: unsupported severity %q", ErrInvalidIncident, incident.Severity)
	}
	if !validToken(incident.Title, 512) {
		return fmt.Errorf("%w: title is required", ErrInvalidIncident)
	}
	detail := strings.TrimSpace(string(incident.Detail))
	if detail != "" && (!json.Valid([]byte(detail)) || !strings.HasPrefix(detail, "{")) {
		return fmt.Errorf("%w: detail must be a JSON object", ErrInvalidIncident)
	}
	return nil
}

// Valid reports whether severity is a supported incident level.
func (severity Severity) Valid() bool {
	switch severity {
	case SeverityInfo, SeverityWarning, SeverityCritical:
		return true
	default:
		return false
	}
}

// Acknowledge moves an open incident to acked and records the responsible user.
func (incident Incident) Acknowledge(userID int64) (Incident, error) {
	if incident.State != IncidentOpen {
		return Incident{}, ErrInvalidIncidentTransition
	}
	if userID <= 0 {
		return Incident{}, fmt.Errorf("%w: acknowledging user is required", ErrInvalidIncident)
	}
	incident.State = IncidentAcked
	incident.AckedBy = &userID
	return incident, nil
}

// Close moves an open or acknowledged incident to closed.
func (incident Incident) Close(at time.Time) (Incident, error) {
	if incident.State != IncidentOpen && incident.State != IncidentAcked {
		return Incident{}, ErrInvalidIncidentTransition
	}
	if at.IsZero() {
		return Incident{}, fmt.Errorf("%w: close time is required", ErrInvalidIncident)
	}
	closedAt := at.UTC()
	incident.State = IncidentClosed
	incident.ClosedAt = &closedAt
	return incident, nil
}

// ValidateIdentity checks the fields used to group repeated incidents.
func ValidateIdentity(kind, scope string) error {
	if !validToken(kind, 128) || !validToken(scope, 512) {
		return fmt.Errorf("%w: kind and scope are required", ErrInvalidIncident)
	}
	return nil
}

func validToken(value string, maxLength int) bool {
	return value != "" && value == strings.TrimSpace(value) && utf8.RuneCountInString(value) <= maxLength
}

// NotificationRequested asks delivery to notify administrators through the
// configured alert channel. It deliberately excludes incident detail payloads.
type NotificationRequested struct {
	Kind       string   `json:"kind"`
	IncidentID int64    `json:"incident_id"`
	Severity   Severity `json:"severity"`
	Title      string   `json:"title"`
	Recipients []string `json:"recipients"`
	Channels   []string `json:"channels"`
}
