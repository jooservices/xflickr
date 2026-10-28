package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSeverityValid(t *testing.T) {
	for _, severity := range []Severity{SeverityInfo, SeverityWarning, SeverityCritical} {
		if !severity.Valid() {
			t.Errorf("%q should be valid", severity)
		}
	}
	if Severity("urgent").Valid() {
		t.Fatal("unsupported severity should be invalid")
	}
}

func TestValidateOpen(t *testing.T) {
	valid := Incident{
		Kind: "crawl.stalled", Scope: "node-1", Severity: SeverityWarning,
		Title: "Crawl is stalled", Detail: json.RawMessage(`{"pending":3}`),
	}
	if err := valid.ValidateOpen(); err != nil {
		t.Fatalf("ValidateOpen() error = %v", err)
	}
	cases := []struct {
		name string
		edit func(*Incident)
	}{
		{name: "missing kind", edit: func(i *Incident) { i.Kind = "" }},
		{name: "trimmed kind", edit: func(i *Incident) { i.Kind = " crawl.stalled" }},
		{name: "long scope", edit: func(i *Incident) { i.Scope = string(make([]byte, 513)) }},
		{name: "invalid severity", edit: func(i *Incident) { i.Severity = "urgent" }},
		{name: "missing title", edit: func(i *Incident) { i.Title = " " }},
		{name: "invalid detail", edit: func(i *Incident) { i.Detail = json.RawMessage(`{"bad"`) }},
		{name: "non-object detail", edit: func(i *Incident) { i.Detail = json.RawMessage(`[]`) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			incident := valid
			test.edit(&incident)
			if err := incident.ValidateOpen(); !errors.Is(err, ErrInvalidIncident) {
				t.Fatalf("ValidateOpen() error = %v, want ErrInvalidIncident", err)
			}
		})
	}
}

func TestAcknowledgeAndCloseTransitions(t *testing.T) {
	open := Incident{ID: 7, State: IncidentOpen}
	acked, err := open.Acknowledge(42)
	if err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}
	if acked.State != IncidentAcked || acked.AckedBy == nil || *acked.AckedBy != 42 {
		t.Fatalf("acknowledged incident = %+v", acked)
	}
	if _, err := open.Acknowledge(0); !errors.Is(err, ErrInvalidIncident) {
		t.Fatalf("Acknowledge(invalid user) error = %v", err)
	}
	if _, err := acked.Acknowledge(43); !errors.Is(err, ErrInvalidIncidentTransition) {
		t.Fatalf("Acknowledge(acked incident) error = %v", err)
	}

	at := time.Date(2026, 10, 28, 14, 0, 0, 0, time.FixedZone("offset", -7*60*60))
	closed, err := acked.Close(at)
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if closed.State != IncidentClosed || closed.ClosedAt == nil || !closed.ClosedAt.Equal(at.UTC()) {
		t.Fatalf("closed incident = %+v", closed)
	}
	if _, err := open.Close(time.Time{}); !errors.Is(err, ErrInvalidIncident) {
		t.Fatalf("Close(zero time) error = %v", err)
	}
	if _, err := closed.Close(at); !errors.Is(err, ErrInvalidIncidentTransition) {
		t.Fatalf("Close(closed incident) error = %v", err)
	}
}
