package domain

import "testing"

func TestSettingValidation(t *testing.T) {
	if err := (Setting{UserID: 3}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Setting{}).Validate(); err == nil {
		t.Fatal("expected missing user error")
	}
	if ChangedTopic != "sharing.changed" {
		t.Fatalf("topic = %q", ChangedTopic)
	}
}
