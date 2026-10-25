package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/settings/domain"
)

func TestMaxCacheAgeIsThirtySeconds(t *testing.T) {
	if MaxCacheAge != 30*time.Second {
		t.Fatalf("MaxCacheAge = %s, want 30s", MaxCacheAge)
	}
}

func TestRegistrationOpenReadsApplicationSetting(t *testing.T) {
	snapshot := domain.DefaultSnapshot()
	snapshot.Settings[domain.RegistrationOpen] = domain.Definition{Key: domain.RegistrationOpen, Value: json.RawMessage("true")}
	repository := &fakeRepository{snapshot: snapshot}
	open, err := New(repository).RegistrationOpen(t.Context())
	if err != nil || !open {
		t.Fatalf("RegistrationOpen() = %v, %v", open, err)
	}
	if repository.loadCount() != 1 {
		t.Fatalf("settings loads = %d, want 1", repository.loadCount())
	}
}

func TestRegistrationOpenRejectsMissingOrInvalidSetting(t *testing.T) {
	for _, snapshot := range []domain.Snapshot{
		{Settings: map[domain.Key]domain.Definition{}},
		{Settings: map[domain.Key]domain.Definition{domain.RegistrationOpen: {Key: domain.RegistrationOpen, Value: json.RawMessage(`"yes"`)}}},
	} {
		if _, err := New(&fakeRepository{snapshot: snapshot}).RegistrationOpen(t.Context()); err == nil {
			t.Fatal("RegistrationOpen() succeeded with invalid configuration")
		}
	}
}

func TestNewWithCacheAgeClampsToThirtySeconds(t *testing.T) {
	repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
	tests := []struct {
		name string
		age  time.Duration
		want time.Duration
	}{
		{name: "default constructor", want: MaxCacheAge},
		{name: "maximum", age: MaxCacheAge, want: MaxCacheAge},
		{name: "shorter", age: time.Second, want: time.Second},
		{name: "above maximum", age: MaxCacheAge + time.Nanosecond, want: MaxCacheAge},
		{name: "zero", age: 0, want: MaxCacheAge},
		{name: "negative", age: -time.Second, want: MaxCacheAge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var service *Service
			if test.name == "default constructor" {
				service = New(repository)
			} else {
				service = NewWithCacheAge(repository, test.age)
			}
			if service.cacheAge != test.want {
				t.Fatalf("cache age = %s, want %s", service.cacheAge, test.want)
			}
		})
	}
}

func TestEffectiveReturnsTypedDefaultsAndReusesThemUntilExpiry(t *testing.T) {
	repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
	service, clock := newService(t, repository, MaxCacheAge)

	got, err := service.Effective(t.Context(), 17)
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	want := domain.Effective{
		MaxDepth:            2,
		ChangeCheckInterval: 24 * time.Hour,
		DailyCallLimit:      0,
		QuotaReserve:        100,
		PerSecondRate:       4,
		Burst:               5,
		PersonLaneShare:     0.30,
		ZipTTL:              24 * time.Hour,
		RetryMax:            5,
		RetryBackoff:        30 * time.Second,
		NodeCooldown:        45 * time.Minute,
		GroupPoolsEnabled:   false,
		PerKeyReadyCap:      100,
		NodeReadyCap:        500,
		MediaReadyCap:       100,
		AIReadyCap:          100,
		SyncReadyCap:        100,
		LeaseTimeout:        2 * time.Minute,
	}
	if got != want {
		t.Fatalf("Effective() = %+v, want %+v", got, want)
	}

	clock.now = clock.now.Add(MaxCacheAge)
	if _, err := service.Effective(t.Context(), 17); err != nil {
		t.Fatalf("Effective() at expiry error = %v", err)
	}
	if repository.loadCount() != 1 {
		t.Fatalf("loads at expiry = %d, want the cached value", repository.loadCount())
	}

	clock.now = clock.now.Add(time.Nanosecond)
	if _, err := service.Effective(t.Context(), 17); err != nil {
		t.Fatalf("Effective() after expiry error = %v", err)
	}
	if repository.loadCount() != 2 {
		t.Fatalf("loads after expiry = %d, want a fresh read", repository.loadCount())
	}
}

func TestEffectiveHonorsShorterCacheAndNeverExceedsThirtySeconds(t *testing.T) {
	t.Run("shorter cache", func(t *testing.T) {
		repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
		service, clock := newService(t, repository, time.Second)
		if _, err := service.Effective(t.Context(), 1); err != nil {
			t.Fatalf("Effective() error = %v", err)
		}
		clock.now = clock.now.Add(time.Second + time.Nanosecond)
		if _, err := service.Effective(t.Context(), 1); err != nil {
			t.Fatalf("Effective() error = %v", err)
		}
		if repository.loadCount() != 2 {
			t.Fatalf("loads = %d, want a reload after one second", repository.loadCount())
		}
	})

	t.Run("requested age above thirty seconds", func(t *testing.T) {
		repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
		service, clock := newService(t, repository, time.Minute)
		if _, err := service.Effective(t.Context(), 1); err != nil {
			t.Fatalf("Effective() error = %v", err)
		}
		clock.now = clock.now.Add(MaxCacheAge)
		if _, err := service.Effective(t.Context(), 1); err != nil {
			t.Fatalf("Effective() error = %v", err)
		}
		if repository.loadCount() != 1 {
			t.Fatalf("loads at 30s = %d, want a cache hit", repository.loadCount())
		}
		clock.now = clock.now.Add(time.Nanosecond)
		if _, err := service.Effective(t.Context(), 1); err != nil {
			t.Fatalf("Effective() error = %v", err)
		}
		if repository.loadCount() != 2 {
			t.Fatalf("loads after 30s = %d, want a reload", repository.loadCount())
		}
	})
}

func TestEffectiveKeepsUsersSeparateAndHidesUncommittedRepositoryChanges(t *testing.T) {
	repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
	service, clock := newService(t, repository, MaxCacheAge)
	if _, err := service.Effective(t.Context(), 1); err != nil {
		t.Fatalf("Effective(user 1) error = %v", err)
	}
	if _, err := service.Effective(t.Context(), 2); err != nil {
		t.Fatalf("Effective(user 2) error = %v", err)
	}

	repository.putOverride(1, domain.MaxDepth, json.RawMessage("4"))

	stale, err := service.Effective(t.Context(), 1)
	if err != nil {
		t.Fatalf("cached Effective() error = %v", err)
	}
	if stale.MaxDepth != 2 || repository.loadCount() != 2 {
		t.Fatalf("cached depth/loads = %d/%d, want depth 2 without another load", stale.MaxDepth, repository.loadCount())
	}

	clock.now = clock.now.Add(MaxCacheAge + time.Nanosecond)
	fresh, err := service.Effective(t.Context(), 1)
	if err != nil {
		t.Fatalf("reloaded Effective() error = %v", err)
	}
	if fresh.MaxDepth != 4 {
		t.Fatalf("reloaded depth = %d, want 4", fresh.MaxDepth)
	}
}

func TestEffectiveReloadsWhenCacheEpochOrRevisionDoesNotMatch(t *testing.T) {
	repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
	service, clock := newService(t, repository, MaxCacheAge)
	entry := cacheEntry{value: domain.Effective{MaxDepth: 9}, expiresAt: clock.now.Add(MaxCacheAge), epoch: 1, revision: 4}
	service.cache[3] = entry
	service.epoch = 2
	if _, err := service.Effective(t.Context(), 3); err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	if repository.loadCount() != 1 || service.cache[3].value.MaxDepth != 2 {
		t.Fatalf("epoch mismatch was served from the cache")
	}

	service.cache[3] = cacheEntry{value: domain.Effective{MaxDepth: 9}, expiresAt: clock.now.Add(MaxCacheAge), epoch: service.epoch, revision: 1}
	service.revision[3] = 2
	if _, err := service.Effective(t.Context(), 3); err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	if repository.loadCount() != 2 {
		t.Fatalf("loads = %d, want a reload after a revision mismatch", repository.loadCount())
	}
}

func TestEffectiveDropsInFlightResultWhenSettingsChange(t *testing.T) {
	t.Run("user write", func(t *testing.T) {
		repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
		service, _ := newService(t, repository, MaxCacheAge)
		started, release := blockFirstLoad(repository, 7)
		defer release()

		result := make(chan error, 1)
		go func() {
			_, err := service.Effective(context.Background(), 7)
			result <- err
		}()
		waitForSignal(t, started, "load did not block")
		if err := service.SetUser(context.Background(), 7, domain.MaxDepth, json.RawMessage("3")); err != nil {
			t.Fatalf("SetUser() error = %v", err)
		}
		release()
		if err := wait(t, result); err != nil {
			t.Fatalf("in-flight Effective() error = %v", err)
		}

		before := repository.loadCount()
		got, err := service.Effective(context.Background(), 7)
		if err != nil {
			t.Fatalf("Effective() error = %v", err)
		}
		if got.MaxDepth != 3 || repository.loadCount() == before {
			t.Fatalf("depth/loads = %d/%d, want depth 3 from a new load (before %d)", got.MaxDepth, repository.loadCount(), before)
		}
	})

	t.Run("application write", func(t *testing.T) {
		repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
		service, _ := newService(t, repository, MaxCacheAge)
		started, release := blockFirstLoad(repository, 8)
		defer release()

		result := make(chan error, 1)
		go func() {
			_, err := service.Effective(context.Background(), 8)
			result <- err
		}()
		waitForSignal(t, started, "load did not block")
		setting := repository.definition(domain.QuotaReserve)
		setting.Value = json.RawMessage("250")
		if err := service.SetApplicationSetting(context.Background(), setting); err != nil {
			t.Fatalf("SetApplicationSetting() error = %v", err)
		}
		release()
		if err := wait(t, result); err != nil {
			t.Fatalf("in-flight Effective() error = %v", err)
		}

		before := repository.loadCount()
		got, err := service.Effective(context.Background(), 8)
		if err != nil {
			t.Fatalf("Effective() error = %v", err)
		}
		if got.QuotaReserve != 250 || repository.loadCount() == before {
			t.Fatalf("quota/loads = %d/%d, want 250 from a new load (before %d)", got.QuotaReserve, repository.loadCount(), before)
		}
	})
}

func TestEffectiveReportsLoadAndValidationFailuresWithoutCachingThem(t *testing.T) {
	repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
	service, clock := newService(t, repository, MaxCacheAge)
	if _, err := service.Effective(t.Context(), 4); err != nil {
		t.Fatalf("Effective() error = %v", err)
	}

	clock.now = clock.now.Add(MaxCacheAge + time.Nanosecond)
	repository.loadErr = errors.New("database unavailable")
	_, err := service.Effective(t.Context(), 4)
	if err == nil || !errors.Is(err, repository.loadErr) {
		t.Fatalf("Effective() error = %v, want the load error", err)
	}

	repository.loadErr = nil
	repository.snapshot = domain.Snapshot{}
	before, found := service.cache[4]
	if !found {
		t.Fatal("successful read was not cached")
	}
	if _, err := service.Effective(t.Context(), 4); err == nil {
		t.Fatal("Effective() accepted an incomplete snapshot")
	}
	after, found := service.cache[4]
	if !found || after != before {
		t.Fatal("failed read changed the cache")
	}
}

func TestEffectiveRejectsAMissingServiceOrContext(t *testing.T) {
	repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
	service, _ := newService(t, repository, MaxCacheAge)
	if _, err := (*Service)(nil).Effective(t.Context(), 1); !errors.Is(err, ErrRepositoryRequired) {
		t.Fatalf("nil service error = %v", err)
	}
	if _, err := New(nil).Effective(t.Context(), 1); !errors.Is(err, ErrRepositoryRequired) {
		t.Fatalf("nil repository error = %v", err)
	}
	if _, err := service.Effective(nil, 1); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil context error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	if _, err := service.Effective(ctx, 1); err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	cancel()
	if _, err := service.Effective(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Effective() error = %v", err)
	}
	if repository.loadCount() != 1 {
		t.Fatalf("loads = %d, want the cached load only", repository.loadCount())
	}

	cancelDuringLoad := &fakeRepository{snapshot: domain.DefaultSnapshot()}
	loading, _ := newService(t, cancelDuringLoad, MaxCacheAge)
	loadCtx, cancelLoad := context.WithCancel(context.Background())
	cancelDuringLoad.onLoad = func(int64) { cancelLoad() }
	if _, err := loading.Effective(loadCtx, 5); !errors.Is(err, context.Canceled) {
		t.Fatalf("Effective() canceled during load error = %v", err)
	}
	if _, found := loading.cache[5]; found {
		t.Fatal("canceled load was cached")
	}
}

func TestSetUserAppliesOverridesInsideCapsAndRejectsTheRest(t *testing.T) {
	repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
	service, _ := newService(t, repository, MaxCacheAge)
	overrides := []struct {
		key   domain.Key
		value string
	}{
		{domain.MaxDepth, "4"},
		{domain.ChangeCheckInterval, "3600"},
		{domain.DailyCallLimit, "12"},
		{domain.QuotaReserve, "40"},
		{domain.PerSecondRate, "2"},
		{domain.Burst, "6"},
		{domain.PersonLaneShare, "0.5"},
		{domain.ZipTTL, "7200"},
		{domain.RetryMax, "1"},
		{domain.RetryBackoff, "0"},
		{domain.NodeCooldown, "120"},
		{domain.GroupPoolsEnabled, "true"},
	}
	for _, override := range overrides {
		if err := service.SetUser(t.Context(), 11, override.key, json.RawMessage(override.value)); err != nil {
			t.Fatalf("SetUser(%s) error = %v", override.key, err)
		}
	}
	got, err := service.Effective(t.Context(), 11)
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	want := domain.Effective{
		MaxDepth:            4,
		ChangeCheckInterval: time.Hour,
		DailyCallLimit:      12,
		QuotaReserve:        40,
		PerSecondRate:       2,
		Burst:               6,
		PersonLaneShare:     0.5,
		ZipTTL:              2 * time.Hour,
		RetryMax:            1,
		RetryBackoff:        0,
		NodeCooldown:        2 * time.Minute,
		GroupPoolsEnabled:   true,
		PerKeyReadyCap:      100,
		NodeReadyCap:        500,
		MediaReadyCap:       100,
		AIReadyCap:          100,
		SyncReadyCap:        100,
		LeaseTimeout:        2 * time.Minute,
	}
	if got != want {
		t.Fatalf("Effective() = %+v, want %+v", got, want)
	}
	if repository.userSaves() != len(overrides) {
		t.Fatalf("saves = %d, want %d", repository.userSaves(), len(overrides))
	}

	other, err := service.Effective(t.Context(), 12)
	if err != nil {
		t.Fatalf("Effective(other) error = %v", err)
	}
	if other.MaxDepth != 2 || other.GroupPoolsEnabled {
		t.Fatalf("other user = %+v, want defaults", other)
	}
}

func TestSetUserNamesTheKeyAndCapWhenRejectingAnOverride(t *testing.T) {
	tests := []struct {
		name       string
		key        domain.Key
		raw        string
		prepare    func(domain.Snapshot)
		want       string
		wantCap    string
		checkCache bool
	}{
		{name: "above admin cap", key: domain.MaxDepth, raw: "5", want: "exceeds cap 4", wantCap: "4", checkCache: true},
		{name: "below admin cap", key: domain.MaxDepth, raw: "0", want: "is below cap 1", wantCap: "1", checkCache: true},
		{name: "unknown key", key: "nope", raw: "1", want: "not a registered setting key", checkCache: true},
		{
			name: "missing setting",
			key:  domain.Burst,
			raw:  "2",
			prepare: func(snapshot domain.Snapshot) {
				delete(snapshot.Settings, domain.Burst)
			},
			want: "is not configured",
		},
		{
			name: "not overridable",
			key:  domain.GroupPoolsEnabled,
			raw:  "true",
			prepare: func(snapshot domain.Snapshot) {
				setting := snapshot.Settings[domain.GroupPoolsEnabled]
				setting.UserOverridable = false
				snapshot.Settings[domain.GroupPoolsEnabled] = setting
			},
			want:       "not user-overridable",
			checkCache: true,
		},
		{name: "negative daily limit", key: domain.DailyCallLimit, raw: "-1", want: "schema cap 0", wantCap: "0", checkCache: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
			if test.prepare != nil {
				test.prepare(repository.snapshot)
			}
			service, _ := newService(t, repository, MaxCacheAge)
			if test.checkCache {
				if _, err := service.Effective(t.Context(), 1); err != nil {
					t.Fatalf("Effective() error = %v", err)
				}
			}
			err := service.SetUser(t.Context(), 1, test.key, json.RawMessage(test.raw))
			var validation *domain.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("SetUser() error = %v, want ValidationError", err)
			}
			if validation.Key != test.key || validation.Cap != test.wantCap || !containsAll(err.Error(), string(test.key), test.want) {
				t.Fatalf("SetUser() error = %v, want key %s cap %q and %q", err, test.key, test.wantCap, test.want)
			}
			if repository.userSaves() != 0 {
				t.Fatal("rejected override was persisted")
			}
			if test.checkCache {
				if _, err := service.Effective(t.Context(), 1); err != nil {
					t.Fatalf("Effective() error = %v", err)
				}
				if repository.loadCount() != 1 {
					t.Fatalf("loads = %d, want the rejected override to leave the cached read in place", repository.loadCount())
				}
			}
		})
	}
}

func TestSetUserReportsDependencyFailures(t *testing.T) {
	repository := &fakeRepository{snapshot: domain.DefaultSnapshot(), loadErr: errors.New("database unavailable")}
	service, _ := newService(t, repository, MaxCacheAge)
	err := service.SetUser(t.Context(), 1, domain.MaxDepth, json.RawMessage("3"))
	if err == nil || !errors.Is(err, repository.loadErr) || !containsAll(err.Error(), "persist user override") {
		t.Fatalf("SetUser() error = %v", err)
	}

	repository.loadErr = nil
	repository.saveUserErr = errors.New("disk full")
	if _, err := service.Effective(t.Context(), 1); err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	err = service.SetUser(t.Context(), 1, domain.MaxDepth, json.RawMessage("3"))
	if err == nil || !errors.Is(err, repository.saveUserErr) || !containsAll(err.Error(), "persist user override") {
		t.Fatalf("SetUser() error = %v", err)
	}
	if repository.loadCount() != 1 {
		t.Fatalf("loads = %d, want the failed write to leave the cache in place", repository.loadCount())
	}
	cached, err := service.Effective(t.Context(), 1)
	if err != nil || cached.MaxDepth != 2 || repository.loadCount() != 1 {
		t.Fatalf("cached depth/loads/error = %d/%d/%v", cached.MaxDepth, repository.loadCount(), err)
	}

	if err := service.SetUser(nil, 1, domain.MaxDepth, json.RawMessage("3")); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil context error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := service.SetUser(ctx, 1, domain.MaxDepth, json.RawMessage("3")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled SetUser() error = %v", err)
	}
	if err := (*Service)(nil).SetUser(t.Context(), 1, domain.MaxDepth, json.RawMessage("3")); !errors.Is(err, ErrRepositoryRequired) {
		t.Fatalf("nil service error = %v", err)
	}
	if err := New(nil).SetUser(t.Context(), 1, domain.MaxDepth, json.RawMessage("3")); !errors.Is(err, ErrRepositoryRequired) {
		t.Fatalf("nil repository error = %v", err)
	}
}

func TestSetApplicationSettingUpdatesCapsAndDropsEveryCachedReader(t *testing.T) {
	repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
	service, _ := newService(t, repository, MaxCacheAge)
	if _, err := service.Effective(t.Context(), 1); err != nil {
		t.Fatalf("Effective(user 1) error = %v", err)
	}
	if _, err := service.Effective(t.Context(), 2); err != nil {
		t.Fatalf("Effective(user 2) error = %v", err)
	}

	setting := repository.definition(domain.MaxDepth)
	limit := 3.0
	setting.Max = &limit
	setting.Value = json.RawMessage("3")
	if err := service.SetApplicationSetting(t.Context(), setting); err != nil {
		t.Fatalf("SetApplicationSetting() error = %v", err)
	}
	if service.epoch != 1 || len(service.cache) != 0 {
		t.Fatalf("epoch/cache = %d/%d, want epoch 1 and an empty cache", service.epoch, len(service.cache))
	}

	err := service.SetUser(t.Context(), 1, domain.MaxDepth, json.RawMessage("4"))
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Key != domain.MaxDepth || validation.Cap != "3" || !containsAll(err.Error(), "max_depth", "3") {
		t.Fatalf("SetUser() error = %v, want max_depth cap 3", err)
	}
	if err := service.SetUser(t.Context(), 1, domain.MaxDepth, json.RawMessage("3")); err != nil {
		t.Fatalf("SetUser() inside the new cap error = %v", err)
	}
	got, err := service.Effective(t.Context(), 2)
	if err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	if got.MaxDepth != 3 {
		t.Fatalf("default depth = %d, want 3", got.MaxDepth)
	}
}

func TestSetApplicationSettingRejectsInvalidDefinitions(t *testing.T) {
	repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
	service, _ := newService(t, repository, MaxCacheAge)
	if _, err := service.Effective(t.Context(), 1); err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	setting := repository.definition(domain.MaxDepth)
	minimum, maximum := 5.0, 1.0
	setting.Min = &minimum
	setting.Max = &maximum
	err := service.SetApplicationSetting(t.Context(), setting)
	if err == nil || !containsAll(err.Error(), "max_depth", "minimum cap exceeds maximum cap") {
		t.Fatalf("SetApplicationSetting() error = %v", err)
	}
	if repository.appSaves() != 0 || service.epoch != 0 {
		t.Fatalf("saves/epoch = %d/%d, want no write", repository.appSaves(), service.epoch)
	}
	if _, err := service.Effective(t.Context(), 1); err != nil {
		t.Fatalf("Effective() error = %v", err)
	}
	if repository.loadCount() != 1 {
		t.Fatalf("loads = %d, want the original cache entry", repository.loadCount())
	}

	repository.saveAppErr = errors.New("disk full")
	setting = repository.definition(domain.Burst)
	setting.Value = json.RawMessage("8")
	err = service.SetApplicationSetting(t.Context(), setting)
	if err == nil || !errors.Is(err, repository.saveAppErr) || !containsAll(err.Error(), "persist application setting") {
		t.Fatalf("SetApplicationSetting() error = %v", err)
	}
	if service.epoch != 0 || repository.loadCount() != 1 {
		t.Fatalf("epoch/loads = %d/%d, want the cache preserved", service.epoch, repository.loadCount())
	}

	if err := service.SetApplicationSetting(nil, setting); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil context error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := service.SetApplicationSetting(ctx, setting); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled SetApplicationSetting() error = %v", err)
	}
	if err := (*Service)(nil).SetApplicationSetting(t.Context(), setting); !errors.Is(err, ErrRepositoryRequired) {
		t.Fatalf("nil service error = %v", err)
	}
	if err := New(nil).SetApplicationSetting(t.Context(), setting); !errors.Is(err, ErrRepositoryRequired) {
		t.Fatalf("nil repository error = %v", err)
	}
}

func TestEffectiveDropsStaleOverrides(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(domain.Snapshot)
		key     domain.Key
		raw     string
		want    string
		assert  func(*testing.T, domain.Effective)
	}{
		{
			name: "cap tightened",
			prepare: func(snapshot domain.Snapshot) {
				setting := snapshot.Settings[domain.MaxDepth]
				maximum := 3.0
				setting.Max = &maximum
				snapshot.Settings[domain.MaxDepth] = setting
			},
			key:  domain.MaxDepth,
			raw:  "4",
			want: "exceeds cap 3",
			assert: func(t *testing.T, got domain.Effective) {
				t.Helper()
				if got.MaxDepth != 2 {
					t.Fatalf("depth = %d, want the application default", got.MaxDepth)
				}
			},
		},
		{
			name: "overridable flipped",
			prepare: func(snapshot domain.Snapshot) {
				setting := snapshot.Settings[domain.GroupPoolsEnabled]
				setting.UserOverridable = false
				snapshot.Settings[domain.GroupPoolsEnabled] = setting
			},
			key:  domain.GroupPoolsEnabled,
			raw:  "true",
			want: "not user-overridable",
			assert: func(t *testing.T, got domain.Effective) {
				t.Helper()
				if got.GroupPoolsEnabled {
					t.Fatal("group pools stayed enabled after overriding was turned off")
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{snapshot: domain.DefaultSnapshot()}
			test.prepare(repository.snapshot)
			repository.putOverride(9, test.key, json.RawMessage(test.raw))
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })

			service, _ := newService(t, repository, MaxCacheAge)
			got, err := service.Effective(t.Context(), 9)
			if err != nil {
				t.Fatalf("Effective() error = %v", err)
			}
			test.assert(t, got)
			if !containsAll(logs.String(), "dropped stale user override", string(test.key), test.want, "user_id=9") {
				t.Fatalf("log = %q", logs.String())
			}
		})
	}
}

func newService(t *testing.T, repository *fakeRepository, age time.Duration) (*Service, *clock) {
	t.Helper()
	service := NewWithCacheAge(repository, age)
	clock := &clock{now: time.Unix(1_700_000_000, 0).UTC()}
	service.now = clock.Now
	return service, clock
}

func blockFirstLoad(repository *fakeRepository, userID int64) (<-chan struct{}, func()) {
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	var blocked atomic.Bool
	blocked.Store(true)
	repository.onLoad = func(loadedUserID int64) {
		if loadedUserID != userID || !blocked.CompareAndSwap(true, false) {
			return
		}
		close(started)
		<-release
	}
	return started, func() { once.Do(func() { close(release) }) }
}

func waitForSignal(t *testing.T, started <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func wait(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("settings read did not return")
	}
	return nil
}

func containsAll(message string, parts ...string) bool {
	for _, part := range parts {
		if part != "" && !strings.Contains(message, part) {
			return false
		}
	}
	return true
}

type clock struct {
	now time.Time
}

func (clock *clock) Now() time.Time {
	return clock.now
}

type fakeRepository struct {
	mu          sync.Mutex
	snapshot    domain.Snapshot
	overrides   map[int64]map[domain.Key]json.RawMessage
	loadErr     error
	saveUserErr error
	saveAppErr  error
	loads       int
	saves       int
	appWrites   int
	onLoad      func(int64)
}

func (repository *fakeRepository) Load(_ context.Context, userID int64) (domain.Snapshot, error) {
	repository.mu.Lock()
	repository.loads++
	onLoad := repository.onLoad
	err := repository.loadErr
	snapshot := cloneSnapshot(repository.snapshot)
	snapshot.Overrides = cloneOverrides(repository.overrides[userID])
	repository.mu.Unlock()
	if onLoad != nil {
		onLoad(userID)
	}
	if err != nil {
		return domain.Snapshot{}, err
	}
	return snapshot, nil
}

func (repository *fakeRepository) SetUserOverride(_ context.Context, userID int64, key domain.Key, value json.RawMessage) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.loadErr != nil {
		return repository.loadErr
	}
	setting, found := repository.snapshot.Settings[key]
	if err := domain.CheckUserOverride(key, setting, found, value); err != nil {
		return err
	}
	if repository.saveUserErr != nil {
		return repository.saveUserErr
	}
	repository.saves++
	repository.storeOverride(userID, key, value)
	return nil
}

func (repository *fakeRepository) putOverride(userID int64, key domain.Key, value json.RawMessage) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.storeOverride(userID, key, value)
}

func (repository *fakeRepository) storeOverride(userID int64, key domain.Key, value json.RawMessage) {
	if repository.overrides == nil {
		repository.overrides = map[int64]map[domain.Key]json.RawMessage{}
	}
	if repository.overrides[userID] == nil {
		repository.overrides[userID] = map[domain.Key]json.RawMessage{}
	}
	repository.overrides[userID][key] = append(json.RawMessage(nil), value...)
}

func (repository *fakeRepository) SetApplicationSetting(_ context.Context, setting domain.Definition) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.saveAppErr != nil {
		return repository.saveAppErr
	}
	repository.appWrites++
	if repository.snapshot.Settings == nil {
		repository.snapshot.Settings = map[domain.Key]domain.Definition{}
	}
	stored := setting
	stored.Value = append(json.RawMessage(nil), setting.Value...)
	stored.Min = cloneFloat(setting.Min)
	stored.Max = cloneFloat(setting.Max)
	repository.snapshot.Settings[setting.Key] = stored
	return nil
}

func (repository *fakeRepository) loadCount() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.loads
}

func (repository *fakeRepository) userSaves() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.saves
}

func (repository *fakeRepository) appSaves() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.appWrites
}

func (repository *fakeRepository) definition(key domain.Key) domain.Definition {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return cloneSnapshot(repository.snapshot).Settings[key]
}

func cloneSnapshot(snapshot domain.Snapshot) domain.Snapshot {
	settings := make(map[domain.Key]domain.Definition, len(snapshot.Settings))
	for key, setting := range snapshot.Settings {
		setting.Value = append(json.RawMessage(nil), setting.Value...)
		setting.Min = cloneFloat(setting.Min)
		setting.Max = cloneFloat(setting.Max)
		settings[key] = setting
	}
	return domain.Snapshot{Settings: settings, Overrides: cloneOverrides(snapshot.Overrides)}
}

func cloneOverrides(overrides map[domain.Key]json.RawMessage) map[domain.Key]json.RawMessage {
	if overrides == nil {
		return nil
	}
	cloned := make(map[domain.Key]json.RawMessage, len(overrides))
	for key, value := range overrides {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
