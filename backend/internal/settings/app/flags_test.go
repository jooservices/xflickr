package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/settings/domain"
)

type featureFlagRepoStub struct {
	flags     []domain.FeatureFlag
	admins    map[int64]bool
	listErr   error
	adminErr  error
	writeErr  error
	listCalls int
	adminCall int
	writes    int
	updated   []domain.FeatureFlag
}

func (repository *featureFlagRepoStub) ListFlags(context.Context) ([]domain.FeatureFlag, error) {
	repository.listCalls++
	return cloneFeatureFlags(repository.flags), repository.listErr
}

func (repository *featureFlagRepoStub) UserIsAdmin(_ context.Context, userID int64) (bool, error) {
	repository.adminCall++
	return repository.admins[userID], repository.adminErr
}

func (repository *featureFlagRepoStub) UpsertFlags(_ context.Context, flags []domain.FeatureFlag) error {
	repository.writes++
	if repository.writeErr != nil {
		return repository.writeErr
	}
	repository.updated = cloneFeatureFlags(flags)
	for _, update := range flags {
		found := false
		for i := range repository.flags {
			if repository.flags[i].Name == update.Name {
				repository.flags[i], found = update, true
				break
			}
		}
		if !found {
			repository.flags = append(repository.flags, update)
		}
	}
	return nil
}

func TestFeatureFlagsEnabledUsesAudienceAndThirtySecondCache(t *testing.T) {
	all := domain.FeatureFlag{Name: "all", Enabled: true, Audience: domain.AudienceAll}
	disabled := domain.FeatureFlag{Name: "disabled", Enabled: false, Audience: domain.AudienceAll}
	admins := domain.FeatureFlag{Name: "admins", Enabled: true, Audience: domain.AudienceAdmins}
	users := domain.FeatureFlag{Name: "users", Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{5, 9}}
	repository := &featureFlagRepoStub{flags: []domain.FeatureFlag{users, disabled, admins, all}, admins: map[int64]bool{5: true}}
	service := NewFeatureFlags(repository)
	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	cases := []struct {
		label  string
		flag   string
		userID int64
		want   bool
	}{
		{"unknown", "unknown", 5, false},
		{"all includes system context", "all", 0, true},
		{"disabled", "disabled", 5, false},
		{"admin", "admins", 5, true},
		{"member is not admin", "admins", 9, false},
		{"user id included", "users", 9, true},
		{"user id excluded", "users", 10, false},
		{"admin audience rejects system", "admins", 0, false},
	}
	for _, test := range cases {
		got, err := service.Enabled(t.Context(), test.flag, test.userID)
		if err != nil || got != test.want {
			t.Errorf("Enabled(%q, %d) = %v, %v; want %v", test.label, test.userID, got, err, test.want)
		}
	}
	if repository.adminCall != 2 {
		t.Fatalf("administrator lookups = %d, want one positive and one member check", repository.adminCall)
	}
	if repository.listCalls != 1 {
		t.Fatalf("list calls = %d, want one cached read", repository.listCalls)
	}

	listed, err := service.List(t.Context())
	if err != nil || listed[0].Name != "admins" {
		t.Fatalf("List() = %+v, %v; want name-sorted flags", listed, err)
	}
	listed[0].Name = "mutated"
	listed[3].UserIDs[0] = 500
	if got, err := service.Enabled(t.Context(), "users", 5); err != nil || !got {
		t.Fatalf("mutating List() changed cached audiences: Enabled() = %v, %v", got, err)
	}

	for i := range repository.flags {
		if repository.flags[i].Name == "all" {
			repository.flags[i].Enabled = false
		}
	}
	if got, err := service.Enabled(t.Context(), "all", 1); err != nil || !got {
		t.Fatalf("flag changed before cache expiry: %v, %v", got, err)
	}
	now = now.Add(MaxFeatureFlagCacheAge)
	if got, err := service.Enabled(t.Context(), "all", 1); err != nil || got {
		t.Fatalf("flag did not refresh at cache expiry: %v, %v", got, err)
	}
	if repository.listCalls != 2 {
		t.Fatalf("list calls after expiry = %d, want 2", repository.listCalls)
	}
}

func TestFeatureFlagsUpdateValidatesNormalizesAndInvalidatesCache(t *testing.T) {
	repository := &featureFlagRepoStub{flags: []domain.FeatureFlag{{Name: "alpha", Enabled: false, Audience: domain.AudienceAll}}}
	service := NewFeatureFlags(repository)
	if got, err := service.Enabled(t.Context(), "alpha", 1); err != nil || got {
		t.Fatalf("initial alpha = %v, %v", got, err)
	}
	updates := []domain.FeatureFlag{
		{Name: "beta", Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{9, 3}},
		{Name: "alpha", Enabled: true, Audience: domain.AudienceAll},
	}
	if err := service.Update(t.Context(), updates); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !reflect.DeepEqual(repository.updated, []domain.FeatureFlag{
		{Name: "alpha", Enabled: true, Audience: domain.AudienceAll, UserIDs: nil},
		{Name: "beta", Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{3, 9}},
	}) {
		t.Fatalf("persisted update = %+v", repository.updated)
	}
	if got, err := service.Enabled(t.Context(), "alpha", 1); err != nil || !got {
		t.Fatalf("alpha after update = %v, %v", got, err)
	}
	if got, err := service.Enabled(t.Context(), "beta", 9); err != nil || !got {
		t.Fatalf("beta for included user = %v, %v", got, err)
	}
	if got, err := service.Enabled(t.Context(), "beta", 10); err != nil || got {
		t.Fatalf("beta for excluded user = %v, %v", got, err)
	}

	beforeWrites := repository.writes
	for _, test := range []struct {
		name  string
		flags []domain.FeatureFlag
		want  error
	}{
		{name: "empty", want: ErrNoFeatureFlags},
		{name: "duplicate", flags: []domain.FeatureFlag{{Name: "dup", Audience: domain.AudienceAll}, {Name: "dup", Audience: domain.AudienceAdmins}}, want: ErrDuplicateFeatureFlag},
		{name: "invalid", flags: []domain.FeatureFlag{{Name: "INVALID", Audience: domain.AudienceAll}}, want: domain.ErrInvalidFeatureFlag},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := service.Update(t.Context(), test.flags); !errors.Is(err, test.want) {
				t.Fatalf("Update() error = %v, want %v", err, test.want)
			}
		})
	}
	if repository.writes != beforeWrites {
		t.Fatalf("invalid updates wrote %d times", repository.writes-beforeWrites)
	}
}

func TestFeatureFlagsPropagateRepositoryErrorsAndDoNotEvictOnFailedWrite(t *testing.T) {
	databaseErr := errors.New("database unavailable")
	repository := &featureFlagRepoStub{flags: []domain.FeatureFlag{{Name: "alpha", Enabled: false, Audience: domain.AudienceAll}}}
	service := NewFeatureFlags(repository)
	if _, err := service.List(t.Context()); err != nil {
		t.Fatal(err)
	}
	repository.writeErr = databaseErr
	if err := service.Update(t.Context(), []domain.FeatureFlag{{Name: "alpha", Enabled: true, Audience: domain.AudienceAll}}); !errors.Is(err, databaseErr) {
		t.Fatalf("Update() error = %v", err)
	}
	repository.flags[0].Enabled = true
	if got, err := service.Enabled(t.Context(), "alpha", 1); err != nil || got {
		t.Fatalf("failed write invalidated cache: got %v, err %v", got, err)
	}

	repository.listErr = databaseErr
	service = NewFeatureFlags(repository)
	if _, err := service.List(t.Context()); !errors.Is(err, databaseErr) || !strings.Contains(err.Error(), "load feature flags") {
		t.Fatalf("List() error = %v", err)
	}
	repository.listErr = nil
	repository.flags = []domain.FeatureFlag{{Name: "admin", Enabled: true, Audience: domain.AudienceAdmins}}
	repository.adminErr = databaseErr
	service = NewFeatureFlags(repository)
	if got, err := service.Enabled(t.Context(), "admin", 5); got || !errors.Is(err, databaseErr) || !strings.Contains(err.Error(), "check administrator") {
		t.Fatalf("Enabled() = %v, %v", got, err)
	}
}

func TestFeatureFlagsContextAndRepositoryValidation(t *testing.T) {
	if _, err := NewFeatureFlags(nil).List(t.Context()); !errors.Is(err, ErrFlagRepositoryRequired) {
		t.Fatalf("missing repository error = %v", err)
	}
	if _, err := (*FeatureFlags)(nil).Enabled(t.Context(), "x", 1); !errors.Is(err, ErrFlagRepositoryRequired) {
		t.Fatalf("nil service error = %v", err)
	}
	service := NewFeatureFlags(&featureFlagRepoStub{})
	if _, err := service.List(nil); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil context error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.List(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
	if err := service.Update(nil, []domain.FeatureFlag{{Name: "x", Audience: domain.AudienceAll}}); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Update(nil context) error = %v", err)
	}
	if err := service.Update(t.Context(), nil); !errors.Is(err, ErrNoFeatureFlags) {
		t.Fatalf("empty update error = %v", err)
	}

	for _, age := range []time.Duration{0, -time.Second, MaxFeatureFlagCacheAge + time.Second} {
		if got := NewFeatureFlagsWithCacheAge(&featureFlagRepoStub{}, age).cacheAge; got != MaxFeatureFlagCacheAge {
			t.Errorf("cache age for %s = %s", age, got)
		}
	}
	if got := NewFeatureFlagsWithCacheAge(&featureFlagRepoStub{}, time.Second).cacheAge; got != time.Second {
		t.Errorf("custom cache age = %s", got)
	}
}

type inFlightFlagRepo struct {
	mu      sync.Mutex
	flags   []domain.FeatureFlag
	started chan struct{}
	release chan struct{}
	calls   int
}

func (repository *inFlightFlagRepo) ListFlags(context.Context) ([]domain.FeatureFlag, error) {
	repository.mu.Lock()
	repository.calls++
	call := repository.calls
	flags := cloneFeatureFlags(repository.flags)
	repository.mu.Unlock()
	if call == 1 {
		close(repository.started)
		<-repository.release
	}
	return flags, nil
}

func (repository *inFlightFlagRepo) UserIsAdmin(context.Context, int64) (bool, error) {
	return false, nil
}

func (repository *inFlightFlagRepo) UpsertFlags(_ context.Context, flags []domain.FeatureFlag) error {
	repository.mu.Lock()
	repository.flags = cloneFeatureFlags(flags)
	repository.mu.Unlock()
	return nil
}

func TestFeatureFlagsDoNotCacheAReadThatOverlapsAWrite(t *testing.T) {
	repository := &inFlightFlagRepo{
		flags:   []domain.FeatureFlag{{Name: "alpha", Enabled: false, Audience: domain.AudienceAll}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := NewFeatureFlags(repository)
	loaded := make(chan []domain.FeatureFlag, 1)
	go func() {
		flags, _ := service.List(context.Background())
		loaded <- flags
	}()
	<-repository.started
	if err := service.Update(context.Background(), []domain.FeatureFlag{{Name: "alpha", Enabled: true, Audience: domain.AudienceAll}}); err != nil {
		t.Fatal(err)
	}
	close(repository.release)
	if flags := <-loaded; flags[0].Enabled {
		t.Fatal("the in-flight reader unexpectedly observed the write")
	}
	if got, err := service.Enabled(context.Background(), "alpha", 1); err != nil || !got {
		t.Fatalf("post-write Enabled() = %v, %v; want freshly loaded true", got, err)
	}
	repository.mu.Lock()
	calls := repository.calls
	repository.mu.Unlock()
	if calls != 2 {
		t.Fatalf("repository reads = %d, want old in-flight read plus a fresh read", calls)
	}
}
