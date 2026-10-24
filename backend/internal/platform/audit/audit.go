// Package audit defines the port that records administrative actions.
package audit

import "context"

// Recorder stores one audit entry. The real adapter arrives with the audit
// trail ticket (JOOXF-14); until then Discard is the stub.
type Recorder interface {
	// Record stores that actor performed action on target. Detail holds
	// JSON-serialisable context such as the values before and after a change.
	Record(ctx context.Context, actor, action, target string, detail map[string]any) error
}

type discard struct{}

func (discard) Record(context.Context, string, string, string, map[string]any) error { return nil }

// Discard is a Recorder that drops every entry. It stands in until the audit
// trail adapter exists.
var Discard Recorder = discard{}
