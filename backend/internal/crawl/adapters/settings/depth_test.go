package settings

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/crawl/app"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

type fakeSettings struct {
	policy settingsapp.DepthPolicy
	err    error
	key    settingsdomain.Key
	value  string
}

func (f *fakeSettings) DepthPolicy(context.Context, int64) (settingsapp.DepthPolicy, error) {
	return f.policy, f.err
}

func (f *fakeSettings) SetUser(_ context.Context, _ int64, key settingsdomain.Key, value json.RawMessage) error {
	f.key, f.value = key, string(value)
	return f.err
}

func TestDepthMapsThePolicyAndWritesTheOverride(t *testing.T) {
	fake := &fakeSettings{policy: settingsapp.DepthPolicy{Depth: 3, Default: 2, Max: 4}}
	depth := Depth{Settings: fake}
	policy, err := depth.Policy(t.Context(), 7)
	if err != nil || policy != (app.DepthPolicy{Depth: 3, Default: 2, Max: 4}) {
		t.Fatalf("Policy() = %+v, %v", policy, err)
	}
	if err := depth.SetDepth(t.Context(), 7, 3); err != nil || fake.key != settingsdomain.MaxDepth || fake.value != "3" {
		t.Fatalf("SetDepth() = %v (%s=%s)", err, fake.key, fake.value)
	}
}

func TestDepthMapsFailures(t *testing.T) {
	if _, err := (Depth{}).Policy(t.Context(), 1); err == nil {
		t.Fatal("Policy accepted a missing service")
	}
	if err := (Depth{}).SetDepth(t.Context(), 1, 2); err == nil {
		t.Fatal("SetDepth accepted a missing service")
	}
	fake := &fakeSettings{err: &settingsdomain.ValidationError{Key: settingsdomain.MaxDepth, Cap: "4"}}
	if err := (Depth{Settings: fake}).SetDepth(t.Context(), 1, 9); !errors.Is(err, app.ErrDepthAboveCap) {
		t.Fatalf("validation error = %v", err)
	}
	if _, err := (Depth{Settings: fake}).Policy(t.Context(), 1); err == nil {
		t.Fatal("Policy swallowed a failure")
	}
	other := errors.New("down")
	if err := (Depth{Settings: &fakeSettings{err: other}}).SetDepth(t.Context(), 1, 2); !errors.Is(err, other) {
		t.Fatalf("other error = %v", err)
	}
}
