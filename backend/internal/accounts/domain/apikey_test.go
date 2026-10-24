package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStatusTransitions(t *testing.T) {
	allowed := []struct {
		from Status
		to   Status
	}{
		{StatusValid, StatusValid},
		{StatusValid, StatusInvalid},
		{StatusValid, StatusRevoked},
		{StatusInvalid, StatusInvalid},
		{StatusInvalid, StatusValid},
		{StatusInvalid, StatusRevoked},
		{StatusRevoked, StatusRevoked},
	}
	for _, test := range allowed {
		if err := test.from.Transition(test.to); err != nil {
			t.Errorf("%s -> %s: %v", test.from, test.to, err)
		}
		key := APIKey{Status: test.from, PublicKey: "0123456789abcdef"}
		next, err := key.WithStatus(test.to)
		if err != nil || next.Status != test.to || key.Status != test.from {
			t.Errorf("WithStatus(%s -> %s) = %+v, %v", test.from, test.to, next, err)
		}
	}

	rejected := []struct {
		from Status
		to   Status
	}{
		{StatusRevoked, StatusValid},
		{StatusRevoked, StatusInvalid},
		{Status("unknown"), StatusValid},
		{StatusValid, Status("paused")},
		{Status(""), StatusValid},
	}
	for _, test := range rejected {
		if err := test.from.Transition(test.to); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("%s -> %s error = %v", test.from, test.to, err)
		}
		if _, err := (APIKey{Status: test.from}).WithStatus(test.to); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("WithStatus(%s -> %s) error = %v", test.from, test.to, err)
		}
	}
	if StatusValid.Valid() != true || Status("paused").Valid() {
		t.Fatal("Valid() mismatch")
	}
}

func TestMaskPublicKey(t *testing.T) {
	key := APIKey{PublicKey: "0123456789abcdef0123456789abcdef"}
	masked := key.MaskedPublicKey()
	if masked != MaskPublicKey(key.PublicKey) {
		t.Fatalf("MaskedPublicKey() = %q", masked)
	}
	if !strings.HasPrefix(masked, "012345") || strings.Contains(masked, "6789abcdef") {
		t.Fatalf("mask = %q", masked)
	}
	if utf8.RuneCountInString(masked) != len(key.PublicKey) {
		t.Fatalf("mask length = %d", utf8.RuneCountInString(masked))
	}
	if got := MaskPublicKey("abc"); got != "•••" {
		t.Fatalf("short mask = %q", got)
	}
	if got := MaskPublicKey("abcdef"); got != "••••••" {
		t.Fatalf("exact prefix mask = %q", got)
	}
	if got := MaskPublicKey(""); got != "" {
		t.Fatalf("empty mask = %q", got)
	}
	if got := MaskPublicKey("0123456"); got != "012345•" {
		t.Fatalf("seven character mask = %q", got)
	}
}

func TestSecretAAD(t *testing.T) {
	if got := string(SecretAAD(42)); got != "api_keys:42" {
		t.Fatalf("SecretAAD(42) = %q", got)
	}
	if string(SecretAAD(7)) == string(SecretAAD(17)) {
		t.Fatal("different ids produced the same associated data")
	}
}

func TestDeletedPayloadHasNoSecret(t *testing.T) {
	encoded, err := json.Marshal(Deleted{ID: 4, UserID: 9})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"id":4,"user_id":9}` {
		t.Fatalf("payload = %s", encoded)
	}
}

func TestPolicyFromSettings(t *testing.T) {
	policy, err := PolicyFromSettings(0, 4, 5)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := policy.Resolve(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if limits != (Limits{PerHour: FlickrHourlyCeiling, PerSecond: 4, Burst: 5}) {
		t.Fatalf("defaults = %+v", limits)
	}
	hour := 100
	second := 2
	burst := 1
	limits, err = policy.Resolve(&hour, &second, &burst)
	if err != nil || limits != (Limits{PerHour: 100, PerSecond: 2, Burst: 1}) {
		t.Fatalf("within caps = %+v, %v", limits, err)
	}
	over := 5
	if _, err := policy.Resolve(nil, &over, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("per second over cap error = %v", err)
	}
	burstOver := 6
	if _, err := policy.Resolve(nil, nil, &burstOver); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("burst over cap error = %v", err)
	}
	tooHigh := FlickrHourlyCeiling + 1
	if _, err := policy.Resolve(&tooHigh, nil, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("hourly over cap error = %v", err)
	}
	zero := 0
	if _, err := policy.Resolve(&zero, nil, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("zero hourly error = %v", err)
	}
	negative := -1
	if _, err := policy.Resolve(nil, &negative, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("negative per second error = %v", err)
	}

	tight, err := PolicyFromSettings(100, 4, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tight.Resolve(&hour, nil, nil); err != nil {
		t.Fatal(err)
	}
	aboveDaily := 101
	if _, err := tight.Resolve(&aboveDaily, nil, nil); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("above daily budget error = %v", err)
	}
	wide, err := PolicyFromSettings(10_000, 4, 5)
	if err != nil {
		t.Fatal(err)
	}
	atCeiling := FlickrHourlyCeiling
	limits, err = wide.Resolve(&atCeiling, nil, nil)
	if err != nil || limits.PerHour != FlickrHourlyCeiling {
		t.Fatalf("wide daily policy = %+v, %v", limits, err)
	}

	for _, test := range []struct {
		daily     int64
		perSecond int
		burst     int
	}{
		{daily: -1, perSecond: 4, burst: 5},
		{daily: 0, perSecond: 0, burst: 5},
		{daily: 0, perSecond: 4, burst: -2},
	} {
		if _, err := PolicyFromSettings(test.daily, test.perSecond, test.burst); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("PolicyFromSettings(%d, %d, %d) error = %v", test.daily, test.perSecond, test.burst, err)
		}
	}
	if err := (Bound{Default: 0, Max: 4}).Enforce(1); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("invalid bound error = %v", err)
	}
	if err := (Bound{Default: 8, Max: 4}).Enforce(1); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("default above max error = %v", err)
	}
	if err := policy.PerSecond.Enforce(4); err != nil {
		t.Fatal(err)
	}
	if err := policy.Burst.Enforce(6); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("burst enforce error = %v", err)
	}
}

func TestNormalizeLabelAndCredential(t *testing.T) {
	label, err := NormalizeLabel("  Personal key  ")
	if err != nil || label != "Personal key" {
		t.Fatalf("label = %q, %v", label, err)
	}
	if _, err := NormalizeLabel("   "); !errors.Is(err, ErrInvalidLabel) {
		t.Fatalf("blank label error = %v", err)
	}
	if _, err := NormalizeLabel(strings.Repeat("a", LabelMaxLength+1)); !errors.Is(err, ErrInvalidLabel) {
		t.Fatalf("long label error = %v", err)
	}
	long, err := NormalizeLabel(strings.Repeat("a", LabelMaxLength))
	if err != nil || len([]rune(long)) != LabelMaxLength {
		t.Fatalf("max label = %q, %v", long, err)
	}
	if _, err := NormalizeLabel("bad\nlabel"); !errors.Is(err, ErrInvalidLabel) {
		t.Fatalf("control label error = %v", err)
	}

	secret, err := NormalizeCredential("  abcdef0123456789  ")
	if err != nil || secret != "abcdef0123456789" {
		t.Fatalf("secret = %q, %v", secret, err)
	}
	for _, value := range []string{"", "   ", "has space", "line\nbreak", strings.Repeat("a", CredentialMaxLength+1)} {
		if _, err := NormalizeCredential(value); !errors.Is(err, ErrInvalidCredential) {
			t.Fatalf("NormalizeCredential(%q) error = %v", value, err)
		}
	}
	if _, err := NormalizeCredential(strings.Repeat("b", CredentialMaxLength)); err != nil {
		t.Fatal(err)
	}
}
