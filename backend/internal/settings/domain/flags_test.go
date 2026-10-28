package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateFeatureFlag(t *testing.T) {
	valid := []FeatureFlag{
		{Name: "new_catalog", Enabled: true, Audience: AudienceAll},
		{Name: "admin-tools.v2", Enabled: false, Audience: AudienceAdmins},
		{Name: "rollout-1", Enabled: true, Audience: AudienceUserIDs, UserIDs: []int64{2, 10}},
	}
	for _, flag := range valid {
		if err := ValidateFeatureFlag(flag); err != nil {
			t.Errorf("ValidateFeatureFlag(%+v) = %v", flag, err)
		}
	}

	invalid := []struct {
		name string
		flag FeatureFlag
	}{
		{name: "empty name", flag: FeatureFlag{Audience: AudienceAll}},
		{name: "uppercase name", flag: FeatureFlag{Name: "New", Audience: AudienceAll}},
		{name: "invalid character", flag: FeatureFlag{Name: "flag/name", Audience: AudienceAll}},
		{name: "too long", flag: FeatureFlag{Name: strings.Repeat("a", MaxFlagNameLength+1), Audience: AudienceAll}},
		{name: "all with ids", flag: FeatureFlag{Name: "flag", Audience: AudienceAll, UserIDs: []int64{1}}},
		{name: "admins with ids", flag: FeatureFlag{Name: "flag", Audience: AudienceAdmins, UserIDs: []int64{1}}},
		{name: "no user ids", flag: FeatureFlag{Name: "flag", Audience: AudienceUserIDs}},
		{name: "nonpositive id", flag: FeatureFlag{Name: "flag", Audience: AudienceUserIDs, UserIDs: []int64{0}}},
		{name: "negative id", flag: FeatureFlag{Name: "flag", Audience: AudienceUserIDs, UserIDs: []int64{-1}}},
		{name: "duplicate id", flag: FeatureFlag{Name: "flag", Audience: AudienceUserIDs, UserIDs: []int64{1, 1}}},
		{name: "too many ids", flag: FeatureFlag{Name: "flag", Audience: AudienceUserIDs, UserIDs: make([]int64, MaxFlagAudienceUsers+1)}},
		{name: "unknown audience", flag: FeatureFlag{Name: "flag", Audience: "everyone"}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateFeatureFlag(test.flag)
			if !errors.Is(err, ErrInvalidFeatureFlag) {
				t.Fatalf("ValidateFeatureFlag() error = %v", err)
			}
		})
	}
}

func TestFeatureFlagEnabledFor(t *testing.T) {
	tests := []struct {
		name  string
		flag  FeatureFlag
		user  int64
		admin bool
		want  bool
	}{
		{name: "all", flag: FeatureFlag{Enabled: true, Audience: AudienceAll}, user: 0, want: true},
		{name: "disabled", flag: FeatureFlag{Enabled: false, Audience: AudienceAll}, user: 1, want: false},
		{name: "admins accepted", flag: FeatureFlag{Enabled: true, Audience: AudienceAdmins}, user: 2, admin: true, want: true},
		{name: "admins reject member", flag: FeatureFlag{Enabled: true, Audience: AudienceAdmins}, user: 2, want: false},
		{name: "admins reject system", flag: FeatureFlag{Enabled: true, Audience: AudienceAdmins}, admin: true, want: false},
		{name: "user included", flag: FeatureFlag{Enabled: true, Audience: AudienceUserIDs, UserIDs: []int64{2, 5}}, user: 5, want: true},
		{name: "user excluded", flag: FeatureFlag{Enabled: true, Audience: AudienceUserIDs, UserIDs: []int64{2, 5}}, user: 6, want: false},
		{name: "user list rejects system", flag: FeatureFlag{Enabled: true, Audience: AudienceUserIDs, UserIDs: []int64{2}}, want: false},
		{name: "unknown audience off", flag: FeatureFlag{Enabled: true, Audience: "unknown"}, user: 2, admin: true, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.flag.EnabledFor(test.user, test.admin); got != test.want {
				t.Fatalf("EnabledFor() = %v, want %v", got, test.want)
			}
		})
	}
}
