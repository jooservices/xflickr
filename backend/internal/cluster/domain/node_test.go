package domain

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"
)

func TestNodeTokenIsOpaqueAndHashed(t *testing.T) {
	token, err := NewNodeToken()
	if err != nil {
		t.Fatal(err)
	}
	if !ValidNodeToken(token) || len(HashNodeToken(token)) != 32 {
		t.Fatalf("generated token is invalid: length=%d", len(token))
	}
	if bytes.Equal([]byte(token), HashNodeToken(token)) {
		t.Fatal("token hash contains the raw token")
	}
	other, err := NewNodeToken()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(HashNodeToken(token), HashNodeToken(other)) {
		t.Fatal("different tokens produced the same hash")
	}
}

func TestValidNodeTokenRejectsMalformedValues(t *testing.T) {
	valid := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, NodeTokenBytes))
	for _, value := range []string{"", "short", valid + "=", "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!"} {
		if ValidNodeToken(value) {
			t.Errorf("ValidNodeToken(%q) = true", value)
		}
	}
	if !ValidNodeToken(valid) {
		t.Fatal("valid encoded token was rejected")
	}
}

func TestEffectiveStateFromHeartbeatAge(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		age  *time.Duration
		base State
		cool *time.Time
		want State
	}{
		{name: "never heard from", base: StateOffline, want: StateOffline},
		{name: "fresh", age: duration(time.Second), base: StateOnline, want: StateOnline},
		{name: "exact warning boundary", age: duration(WarningAfter), base: StateOnline, want: StateOnline},
		{name: "warning", age: duration(WarningAfter + time.Second), base: StateOnline, want: StateWarning},
		{name: "exact offline boundary", age: duration(OfflineAfter), base: StateOnline, want: StateWarning},
		{name: "offline", age: duration(OfflineAfter + time.Second), base: StateOnline, want: StateOffline},
		{name: "cooling", age: duration(time.Hour), base: StateOnline, cool: timePointer(now.Add(time.Minute)), want: StateCooling},
		{name: "disabled", age: duration(time.Hour), base: StateDisabled, cool: timePointer(now.Add(time.Minute)), want: StateDisabled},
	} {
		t.Run(test.name, func(t *testing.T) {
			var heartbeat *time.Time
			if test.age != nil {
				value := now.Add(-*test.age)
				heartbeat = &value
			}
			got := (Node{State: test.base, LastHeartbeatAt: heartbeat, CoolingUntil: test.cool}).EffectiveState(now)
			if got != test.want {
				t.Fatalf("EffectiveState() = %q, want %q", got, test.want)
			}
		})
	}
}

func duration(value time.Duration) *time.Duration { return &value }

func timePointer(value time.Time) *time.Time { return &value }
