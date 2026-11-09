package domain

import (
	"testing"
	"time"
)

func TestRetentionCutoffChoosesOnlyOldDailyPartitions(t *testing.T) {
	now := time.Date(2026, time.November, 9, 15, 30, 0, 0, time.UTC)
	cutoff := RetentionCutoff(now, 30)
	wantCutoff := time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC)
	if !cutoff.Equal(wantCutoff) {
		t.Fatalf("cutoff = %s, want %s", cutoff, wantCutoff)
	}

	parts := []DailyPartition{
		mustPart(t, "api_call_log_20261109"), // today
		mustPart(t, "api_call_log_20261110"), // future
		mustPart(t, "api_call_log_20261010"), // day == cutoff; keep (upper after cutoff)
		mustPart(t, "api_call_log_20261009"), // upper == cutoff; expire (day < cutoff)
		mustPart(t, "api_call_log_20261001"), // older
		mustPart(t, "api_call_log_20261011"), // still inside window
	}
	got := ChooseExpired(parts, cutoff)
	want := []string{"api_call_log_20261001", "api_call_log_20261009"}
	if len(got) != len(want) {
		t.Fatalf("expired = %#v, want %v", names(got), want)
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Fatalf("expired[%d] = %s, want %s (all=%v)", i, got[i].Name, want[i], names(got))
		}
	}
}

func TestRetentionSettingBounds(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{0, DefaultRetentionDays},
		{-3, DefaultRetentionDays},
		{6, MinRetentionDays},
		{7, 7},
		{30, 30},
		{365, 365},
		{366, MaxRetentionDays},
		{1000, MaxRetentionDays},
	}
	for _, tc := range cases {
		if got := ClampRetentionDays(tc.in); got != tc.want {
			t.Fatalf("ClampRetentionDays(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseDailyPartitionName(t *testing.T) {
	part, err := ParseDailyPartitionName("api_call_log_20260301")
	if err != nil {
		t.Fatal(err)
	}
	if !part.Day.Equal(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("day = %s", part.Day)
	}
	if !part.UpperBound().Equal(time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("upper = %s", part.UpperBound())
	}
	for _, bad := range []string{"", "api_call_log", "api_call_log_2026030", "photos_p00", "api_call_log_20261301"} {
		if _, err := ParseDailyPartitionName(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func mustPart(t *testing.T, name string) DailyPartition {
	t.Helper()
	part, err := ParseDailyPartitionName(name)
	if err != nil {
		t.Fatal(err)
	}
	return part
}

func names(parts []DailyPartition) []string {
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = part.Name
	}
	return out
}
