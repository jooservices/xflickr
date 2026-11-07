package redis

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/quota/app"
	"github.com/jooservices/xflickr/internal/quota/domain"
	flickr "github.com/jooservices/xflickr/pkg/flickr"
)

const testPrefix = "xflickr_wt106:"

func setupGovernor(t *testing.T, budget domain.Budget) (Governor, *miniredis.Miniredis, *redisv9.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	base := Governor{Client: client, Prefix: testPrefix}
	rebuilder := rebuilderFor(base, memStore{}, memCalls{})
	governor, err := NewGovernor(client, testPrefix, StaticBudgets{1: budget}, rebuilder, nil)
	if err != nil {
		t.Fatal(err)
	}
	return governor, server, client
}

func generousBudget() domain.Budget {
	return domain.Budget{HourlyLimit: 3600, PerSecondRate: 10000, Burst: 10000, Reserve: 100, PersonLaneShare: 0.3, PhotoLaneFloorShare: 0.1}
}

func acquire(t *testing.T, governor Governor, class domain.Class) (domain.Permit, time.Duration, error) {
	t.Helper()
	return governor.Acquire(context.Background(), 1, class)
}

func TestAcquireAtomicallyEnforcesHourlyLimitUnderConcurrency(t *testing.T) {
	budget := generousBudget()
	budget.HourlyLimit = 73
	governor, _, _ := setupGovernor(t, budget)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			permit, _, err := governor.Acquire(context.Background(), 1, domain.Interactive)
			if err != nil {
				t.Errorf("Acquire(): %v", err)
				return
			}
			if permit != nil {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != int(budget.HourlyLimit) {
		t.Fatalf("allowed = %d, want %d", allowed, budget.HourlyLimit)
	}
}

func TestReserveAndPersonLaneShare(t *testing.T) {
	budget := domain.Budget{HourlyLimit: 100, PerSecondRate: 1000, Burst: 1000, Reserve: 10, PersonLaneShare: 0.3}
	governor, _, _ := setupGovernor(t, budget)
	for i := 0; i < 70; i++ {
		permit, wait, err := acquire(t, governor, domain.Photo)
		if err != nil || permit == nil || wait != 0 {
			t.Fatalf("photo %d = permit %v wait %s err %v", i, permit != nil, wait, err)
		}
	}
	if permit, wait, err := acquire(t, governor, domain.Photo); err != nil || permit != nil || wait <= 0 {
		t.Fatalf("photo crossed lane share: %v %s %v", permit, wait, err)
	}
	for i := 0; i < 20; i++ {
		permit, wait, err := acquire(t, governor, domain.Person)
		if err != nil || permit == nil || wait != 0 {
			t.Fatalf("person %d = permit %v wait %s err %v", i, permit != nil, wait, err)
		}
	}
	if permit, _, err := acquire(t, governor, domain.Person); err != nil || permit != nil {
		t.Fatalf("person used interactive reserve: %v %v", permit, err)
	}
	permit, _, err := acquire(t, governor, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("interactive could not use reserve: %v %v", permit, err)
	}
}

func TestPhotoAlsoPreservesInteractiveReserve(t *testing.T) {
	budget := domain.Budget{HourlyLimit: 10, PerSecondRate: 1000, Burst: 1000, Reserve: 3, PersonLaneShare: 0}
	governor, _, _ := setupGovernor(t, budget)
	for i := 0; i < 7; i++ {
		permit, _, err := acquire(t, governor, domain.Photo)
		if err != nil || permit == nil {
			t.Fatalf("photo %d: %v %v", i, permit, err)
		}
	}
	if permit, _, err := acquire(t, governor, domain.Photo); err != nil || permit != nil {
		t.Fatalf("photo consumed reserve: %v %v", permit, err)
	}
}

func TestPerSecondRateReturnsWait(t *testing.T) {
	budget := domain.Budget{HourlyLimit: 100, PerSecondRate: 1, Burst: 1, Reserve: 0}
	governor, _, _ := setupGovernor(t, budget)
	first, _, err := acquire(t, governor, domain.Interactive)
	if err != nil || first == nil {
		t.Fatalf("first permit: %v %v", first, err)
	}
	if permit, wait, err := acquire(t, governor, domain.Interactive); err != nil || permit != nil || wait <= 0 {
		t.Fatalf("second acquire = %v %s %v", permit, wait, err)
	}
	time.Sleep(1100 * time.Millisecond)
	if permit, _, err := acquire(t, governor, domain.Interactive); err != nil || permit == nil {
		t.Fatalf("after refill = %v %v", permit, err)
	}
}

func TestDoneQuotaErrorCalibratesCounterAndCooldown(t *testing.T) {
	budget := generousBudget()
	budget.HourlyLimit = 20
	governor, server, _ := setupGovernor(t, budget)
	server.SetTime(time.Unix(1800000000, 0))
	permit, _, err := acquire(t, governor, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("acquire: %v %v", permit, err)
	}
	if err := permit.Done(domain.Meta{Err: &flickr.CallError{Kind: flickr.ErrQuotaExceeded}}); err != nil {
		t.Fatalf("Done(): %v", err)
	}
	keys := governor.keys(1)
	if got, err := governor.Client.ZCard(t.Context(), keys[1]).Result(); err != nil || got != 20 {
		t.Fatalf("calibrated hourly count = %d, error %v", got, err)
	}
	if permit, wait, err := acquire(t, governor, domain.Interactive); err != nil || permit != nil || wait <= 0 {
		t.Fatalf("during cooldown = %v %s %v", permit, wait, err)
	}
	if err := permit.Done(domain.Meta{Err: flickr.ErrQuotaExceeded}); err != nil {
		t.Fatalf("duplicate Done(): %v", err)
	}
	server.SetTime(time.Unix(1800000000, 0).Add(time.Hour))
	if permit, _, err := acquire(t, governor, domain.Interactive); err != nil || permit == nil {
		t.Fatalf("after window = %v %v", permit, err)
	}
}

func TestDoneNonQuotaDoesNotCalibrate(t *testing.T) {
	governor, _, _ := setupGovernor(t, generousBudget())
	permit, _, err := acquire(t, governor, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("acquire: %v %v", permit, err)
	}
	if err := permit.Done(domain.Meta{Err: errors.New("temporary")}); err != nil {
		t.Fatalf("Done(): %v", err)
	}
	keys := governor.keys(1)
	if got, err := governor.Client.ZCard(t.Context(), keys[1]).Result(); err != nil || got != 1 {
		t.Fatalf("hourly count = %d, error %v, want 1", got, err)
	}
}

func TestAcquireFailsClosedForInvalidInputsAndUnavailableRedis(t *testing.T) {
	governor, server, client := setupGovernor(t, generousBudget())
	if permit, _, err := governor.Acquire(context.Background(), 0, domain.Interactive); permit != nil || !errors.Is(err, domain.ErrGovernorOffline) {
		t.Fatalf("invalid key result = %v %v", permit, err)
	}
	if permit, _, err := governor.Acquire(context.Background(), 1, domain.Class("bad")); permit != nil || !errors.Is(err, domain.ErrGovernorOffline) {
		t.Fatalf("invalid class result = %v %v", permit, err)
	}
	if permit, _, err := (Governor{}).Acquire(context.Background(), 1, domain.Interactive); permit != nil || !errors.Is(err, domain.ErrGovernorOffline) {
		t.Fatalf("missing dependencies result = %v %v", permit, err)
	}
	if permit, _, err := governor.Acquire(nil, 1, domain.Interactive); permit != nil || !errors.Is(err, domain.ErrGovernorOffline) {
		t.Fatalf("nil context result = %v %v", permit, err)
	}
	server.Close()
	if permit, _, err := governor.Acquire(context.Background(), 1, domain.Interactive); permit != nil || !errors.Is(err, domain.ErrGovernorOffline) {
		t.Fatalf("offline Redis result = %v %v", permit, err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireFailsClosedForMissingOrInvalidBudget(t *testing.T) {
	governor, _, _ := setupGovernor(t, generousBudget())
	governor.Budgets = StaticBudgets{}
	if permit, _, err := acquire(t, governor, domain.Interactive); permit != nil || !errors.Is(err, domain.ErrGovernorOffline) {
		t.Fatalf("missing budget result = %v %v", permit, err)
	}
	governor.Budgets = StaticBudgets{1: {HourlyLimit: 0}}
	if permit, _, err := acquire(t, governor, domain.Interactive); permit != nil || !errors.Is(err, domain.ErrGovernorOffline) {
		t.Fatalf("invalid budget result = %v %v", permit, err)
	}
}

func TestCapacityUsesHourlyRemainingAndTwoMinuteRate(t *testing.T) {
	budget := generousBudget()
	budget.PerSecondRate = 10
	governor, _, client := setupGovernor(t, budget)
	credits, err := governor.Capacity(context.Background(), 1, 2*time.Minute)
	if err != nil || credits != 1200 {
		t.Fatalf("Capacity() = %d, %v; want 1200", credits, err)
	}
	seedGrants(t, governor, domain.Interactive, 3400, time.Now())
	credits, err = governor.Capacity(context.Background(), 1, 2*time.Minute)
	if err != nil || credits != 100 {
		t.Fatalf("Capacity(near hourly limit) = %d, %v; want 100", credits, err)
	}
	seedGrants(t, governor, domain.Person, 200, time.Now())
	credits, err = governor.Capacity(context.Background(), 1, 2*time.Minute)
	if err != nil || credits != 0 {
		t.Fatalf("Capacity(exhausted) = %d, %v; want 0", credits, err)
	}
	_ = client.Close()
}

func TestCapacityFailsClosedWhenRedisIsOffline(t *testing.T) {
	governor, server, _ := setupGovernor(t, generousBudget())
	server.Close()
	if _, err := governor.Capacity(context.Background(), 7, 2*time.Minute); !errors.Is(err, domain.ErrGovernorOffline) {
		t.Fatalf("Capacity() error = %v, want offline", err)
	}
}

func TestCapacityPreservesUnavailableKeyError(t *testing.T) {
	governor, _, _ := setupGovernor(t, generousBudget())
	governor.Budgets = missingBudget{}
	if _, err := governor.Capacity(context.Background(), 1, 2*time.Minute); !errors.Is(err, app.ErrKeyUnavailable) {
		t.Fatalf("Capacity() error = %v, want key unavailable", err)
	}
}

type missingBudget struct{}

func (missingBudget) Budget(context.Context, int64) (domain.Budget, error) {
	return domain.Budget{}, app.ErrKeyUnavailable
}

func TestDoneFailsClosedWhenRedisIsUnavailable(t *testing.T) {
	governor, server, _ := setupGovernor(t, generousBudget())
	permit, _, err := acquire(t, governor, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("acquire: %v %v", permit, err)
	}
	server.Close()
	if err := permit.Done(domain.Meta{Err: flickr.ErrQuotaExceeded}); !errors.Is(err, domain.ErrGovernorOffline) {
		t.Fatalf("Done() error = %v", err)
	}
}

func TestAsInt64Conversions(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  int64
		valid bool
	}{
		{name: "int64", input: int64(12), want: 12, valid: true},
		{name: "int", input: int(13), want: 13, valid: true},
		{name: "uint64", input: uint64(14), want: 14, valid: true},
		{name: "uint64 overflow", input: uint64(^uint64(0)), valid: false},
		{name: "numeric string", input: "15", want: 15, valid: true},
		{name: "bad string", input: "x", valid: false},
		{name: "unsupported", input: true, valid: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, valid := asInt64(test.input)
			if got != test.want || valid != test.valid {
				t.Fatalf("asInt64(%v) = %d, %v; want %d, %v", test.input, got, valid, test.want, test.valid)
			}
		})
	}
}

func TestPersonLaneCannotStarvePhotoShare(t *testing.T) {
	budget := domain.Budget{HourlyLimit: 100, PerSecondRate: 1000, Burst: 1000, Reserve: 10, PersonLaneShare: 0.3}
	governor, server, _ := setupGovernor(t, budget)
	server.Set(governor.keys(1)[6], "1")
	person := 0
	for {
		permit, _, err := acquire(t, governor, domain.Person)
		if err != nil {
			t.Fatal(err)
		}
		if permit == nil {
			break
		}
		person++
	}
	if person != 20 {
		t.Fatalf("person used %d calls, want 20 while photo share is unused", person)
	}
	for i := 0; i < 70; i++ {
		if permit, _, err := acquire(t, governor, domain.Photo); err != nil || permit == nil {
			t.Fatalf("photo %d starved: %v %v", i, permit, err)
		}
	}
}

// productionSecondBudget is the live key: 4/s sustained, burst 5. Tests must
// not raise either limit to make the overshoot disappear.
func productionSecondBudget() domain.Budget {
	return domain.Budget{HourlyLimit: 100_000, PerSecondRate: 4, Burst: 5, Reserve: 0, PersonLaneShare: 0}
}

// TestConcurrentRolesShareOneBucketAndStayWithinBurst is the reproduction for
// the live overshoot: several roles hammer one key through one clock second.
// A full bucket plus in-second refill used to grant burst+rate-1 (8) calls.
func TestConcurrentRolesShareOneBucketAndStayWithinBurst(t *testing.T) {
	budget := productionSecondBudget()
	budgets := StaticBudgets{1: budget, 2: budget}
	crawl, server, client := setupGovernor(t, budget)
	crawl.Budgets = budgets
	scheduler, err := NewGovernor(client, crawl.Prefix, budgets, crawl.Rebuilder, nil)
	if err != nil {
		t.Fatal(err)
	}
	apiRole, err := NewGovernor(client, crawl.Prefix, budgets, crawl.Rebuilder, nil)
	if err != nil {
		t.Fatal(err)
	}
	roles := []Governor{crawl, scheduler, apiRole}
	bucketKey := crawl.keys(1)[0]
	if scheduler.keys(1)[0] != bucketKey || apiRole.keys(1)[0] != bucketKey {
		t.Fatal("roles do not share one bucket key for the same prefix and key")
	}

	start := time.Unix(1_700_000_000, 0)
	grants := grantsInClockSecond(t, server, roles, 1, start)
	if grants > int(budget.Burst) {
		t.Fatalf("one clock second granted %d calls, burst is %d", grants, budget.Burst)
	}
	if grants < int(budget.Burst) {
		t.Fatalf("one clock second granted %d calls, want the full burst of %d from a full bucket", grants, budget.Burst)
	}
	exists, err := client.Exists(context.Background(), bucketKey).Result()
	if err != nil || exists != 1 {
		t.Fatalf("shared bucket key exists = %d, %v", exists, err)
	}
	// Key 2 has its own bucket, so key 1's exhausted second does not block it.
	permit, _, err := apiRole.Acquire(context.Background(), 2, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("other key during key 1's full second = %v, %v", permit, err)
	}

	// Ten more seconds, still one bucket, must not relax the sustained rate.
	total := grants
	for sec := 1; sec <= 10; sec++ {
		got := grantsInClockSecond(t, server, roles, 1, start.Add(time.Duration(sec)*time.Second))
		if got > int(budget.Burst) {
			t.Fatalf("second %d granted %d calls, burst is %d", sec, got, budget.Burst)
		}
		total += got
	}
	rateLimit := int(budget.Burst + budget.PerSecondRate*11)
	if total > rateLimit {
		t.Fatalf("11 clock seconds granted %d calls, want at most %d so the rate of %d stays in force", total, rateLimit, budget.PerSecondRate)
	}
	t.Logf("11 clock seconds granted %d calls, ceiling %d", total, rateLimit)
}

// TestNoClockSecondExceedsBurstOverAFullHour walks every second of an hour at
// the live 4/s and burst 5 limits. AC1 is that none of those seconds exceeds
// the burst, and the hour must not spend faster than the sustained rate.
func TestNoClockSecondExceedsBurstOverAFullHour(t *testing.T) {
	budget := productionSecondBudget()
	governor, server, _ := setupGovernor(t, budget)
	start := time.Unix(1_800_000_000, 0)
	total := 0
	for sec := 0; sec < 3600; sec++ {
		grants := drainClockSecond(t, server, governor, start.Add(time.Duration(sec)*time.Second))
		if grants > int(budget.Burst) {
			t.Fatalf("second %d granted %d calls, burst is %d", sec, grants, budget.Burst)
		}
		total += grants
	}
	rateLimit := int(budget.Burst + budget.PerSecondRate*3600)
	if total > rateLimit {
		t.Fatalf("hour granted %d calls, want at most %d so the rate of %d stays in force", total, rateLimit, budget.PerSecondRate)
	}
	t.Logf("hour granted %d calls, ceiling %d", total, rateLimit)
}

func grantsInClockSecond(t *testing.T, server *miniredis.Miniredis, roles []Governor, keyID int64, start time.Time) int {
	t.Helper()
	total := 0
	for step := 0; step < 20; step++ {
		server.SetTime(start.Add(time.Duration(step) * 50 * time.Millisecond))
		total += hammerRoles(t, roles, keyID, 12)
	}
	return total
}

func hammerRoles(t *testing.T, roles []Governor, keyID int64, workers int) int {
	t.Helper()
	classes := []domain.Class{domain.Person, domain.Photo, domain.Interactive}
	var wg sync.WaitGroup
	var mu sync.Mutex
	grants := 0
	var acquireErr error
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			permit, _, err := roles[i%len(roles)].Acquire(context.Background(), keyID, classes[i%len(classes)])
			mu.Lock()
			defer mu.Unlock()
			if err != nil && acquireErr == nil {
				acquireErr = err
			}
			if permit != nil {
				grants++
			}
		}(i)
	}
	wg.Wait()
	if acquireErr != nil {
		t.Fatalf("Acquire: %v", acquireErr)
	}
	return grants
}

func drainClockSecond(t *testing.T, server *miniredis.Miniredis, governor Governor, start time.Time) int {
	t.Helper()
	grants := 0
	for _, ms := range []int{0, 250, 500, 750, 999} {
		server.SetTime(start.Add(time.Duration(ms) * time.Millisecond))
		for {
			permit, _, err := governor.Acquire(context.Background(), 1, domain.Interactive)
			if err != nil {
				t.Fatalf("Acquire: %v", err)
			}
			if permit == nil {
				break
			}
			grants++
			if grants > 32 {
				t.Fatalf("runaway grants at %s", start)
			}
		}
	}
	return grants
}
