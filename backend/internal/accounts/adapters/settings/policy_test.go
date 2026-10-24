package settings

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/accounts/domain"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

func TestPolicyUsesApplicationSettings(t *testing.T) {
	reader := Reader{Settings: fakeEffective{value: settingsdomain.Effective{
		DailyCallLimit: 100,
		PerSecondRate:  4,
		Burst:          5,
	}}}
	policy, err := reader.Policy(t.Context(), 9)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := policy.Resolve(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if limits.PerHour != 100 || limits.PerSecond != 4 || limits.Burst != 5 {
		t.Fatalf("limits = %+v", limits)
	}
	over := 101
	if _, err := policy.Resolve(&over, nil, nil); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("over daily budget error = %v", err)
	}
}

func TestPolicyReportsSettingsFailures(t *testing.T) {
	if _, err := (Reader{}).Policy(t.Context(), 1); err == nil || !strings.Contains(err.Error(), "settings service") {
		t.Fatalf("nil settings error = %v", err)
	}
	reader := Reader{Settings: fakeEffective{err: errors.New("settings down")}}
	if _, err := reader.Policy(nil, 1); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := reader.Policy(t.Context(), 1); err == nil || !strings.Contains(err.Error(), "settings down") {
		t.Fatalf("load error = %v", err)
	}
	reader.Settings = fakeEffective{value: settingsdomain.Effective{PerSecondRate: 0, Burst: 1}}
	if _, err := reader.Policy(t.Context(), 1); !errors.Is(err, domain.ErrInvalidPolicy) {
		t.Fatalf("invalid policy error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	reader.Settings = fakeEffective{err: context.Canceled}
	if _, err := reader.Policy(canceled, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
}

type fakeEffective struct {
	value settingsdomain.Effective
	err   error
}

func (f fakeEffective) Effective(context.Context, int64) (settingsdomain.Effective, error) {
	if f.err != nil {
		return settingsdomain.Effective{}, f.err
	}
	return f.value, nil
}
