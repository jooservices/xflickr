package domain_test

import (
	"testing"

	"github.com/jooservices/xflickr/internal/platform/metrics/domain"
)

func TestRetentionSettingBounds(t *testing.T) {
	cases := []struct {
		name    string
		days    int
		wantErr bool
		clamp   int
	}{
		{name: "default", days: 30, clamp: 30},
		{name: "minimum", days: 1, clamp: 1},
		{name: "maximum", days: 365, clamp: 365},
		{name: "below minimum", days: 0, wantErr: true, clamp: domain.DefaultRetentionDays},
		{name: "above maximum", days: 366, wantErr: true, clamp: domain.DefaultRetentionDays},
		{name: "negative", days: -1, wantErr: true, clamp: domain.DefaultRetentionDays},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateRetentionDays(tc.days)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := domain.ClampRetentionDays(tc.days); got != tc.clamp {
				t.Fatalf("ClampRetentionDays(%d)=%d want %d", tc.days, got, tc.clamp)
			}
		})
	}
}
