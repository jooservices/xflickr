package ops

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	snapshot Snapshot
	err      error
}

func (f fakeStore) Snapshot(context.Context) (Snapshot, error) { return f.snapshot, f.err }

var base = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func statusFor(t *testing.T, snapshot Snapshot) Status {
	t.Helper()
	status, err := Service{Store: fakeStore{snapshot: snapshot}, Now: func() time.Time { return base }}.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func TestStatusDerivesTotalsAndAge(t *testing.T) {
	status := statusFor(t, Snapshot{
		WorkItems: map[string]int64{"ready": 3, "pending": 2, "failed": 4},
		RiverJobs: map[string]int64{"discarded": 5},
		Keys: []KeyStatus{
			{KeyID: 2, Label: "b", Status: "valid", CallsLastHour: 50, Limit: 100},
			{KeyID: 1, Label: "a", Status: "valid", CallsLastHour: 150, Limit: 100},
		},
		Pending:         Pending{Total: 2, Oldest: base.Add(-90 * time.Second)},
		FailedWorkItems: 4,
		DeadJobs:        5,
		Tokens:          map[string]int64{"ok": 1, "revoked": 1},
	})
	if status.ReadyJobs != 3 || status.CallsLastHour != 200 || status.OldestPendingAge != 90*time.Second {
		t.Fatalf("status=%+v", status)
	}
	if status.Keys[0].KeyID != 1 || status.Keys[0].Remaining() != 0 || status.Keys[1].Remaining() != 50 || status.Keys[1].Used() != 50 {
		t.Fatalf("keys=%+v", status.Keys)
	}
	if status.TokenStatus != TokenRevoked || status.Idle() || status.IdleReason != "" {
		t.Fatalf("token=%s idle=%v reason=%q", status.TokenStatus, status.Idle(), status.IdleReason)
	}
	if status.String() == "" {
		t.Fatal("empty summary")
	}
}

func TestIdleReason(t *testing.T) {
	valid := KeyStatus{KeyID: 1, Status: "valid", CallsLastHour: 1, Limit: 100}
	spent := KeyStatus{KeyID: 1, Status: "valid", CallsLastHour: 100, Limit: 100}
	tests := []struct {
		name    string
		pending Pending
		keys    []KeyStatus
		want    string
	}{
		{"no keys", Pending{Total: 2, WithoutKey: 0}, nil, ReasonNoKey},
		{"invalid key only", Pending{Total: 2}, []KeyStatus{{KeyID: 1, Status: "revoked", Limit: 10}}, ReasonNoKey},
		{"items without key", Pending{Total: 2, WithoutKey: 2}, []KeyStatus{valid}, ReasonNoKey},
		{"quota spent", Pending{Total: 2}, []KeyStatus{spent}, ReasonQuotaExhausted},
		{"not before later", Pending{Total: 2, NotBeforeLater: 2}, []KeyStatus{valid}, ReasonNotBeforeLater},
		{"unknown", Pending{Total: 2, NotBeforeLater: 1}, []KeyStatus{valid}, ReasonUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := statusFor(t, Snapshot{Pending: test.pending, Keys: test.keys})
			if !status.Idle() || status.IdleReason != test.want {
				t.Fatalf("idle=%v reason=%q want %q", status.Idle(), status.IdleReason, test.want)
			}
		})
	}
}

func TestTokenStatusOrder(t *testing.T) {
	cases := map[string]map[string]int64{
		TokenNone:      nil,
		TokenOK:        {"ok": 2},
		TokenReconnect: {"ok": 1, "reconnect_required": 1},
		TokenRevoked:   {"reconnect_required": 1, "revoked": 1},
	}
	for want, tokens := range cases {
		if got := tokenStatus(tokens); got != want {
			t.Fatalf("tokens=%v got %s want %s", tokens, got, want)
		}
	}
}

func TestStatusErrors(t *testing.T) {
	if _, err := (Service{}).Status(t.Context()); err == nil {
		t.Fatal("expected error without store")
	}
	want := errors.New("boom")
	if _, err := (Service{Store: fakeStore{err: want}}).Status(t.Context()); !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}

func TestStatusDefaultsClock(t *testing.T) {
	status, err := Service{Store: fakeStore{}}.Status(t.Context())
	if err != nil || status.GeneratedAt.IsZero() {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
