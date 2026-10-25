package redis

import (
	"context"
	"errors"
	"strconv"
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
	return Governor{Client: client, Prefix: testPrefix, Budgets: StaticBudgets{1: budget}}, server, client
}

func generousBudget() domain.Budget {
	return domain.Budget{HourlyLimit: 3600, PerSecondRate: 10000, Burst: 10000, Reserve: 100, PersonLaneShare: 0.3}
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
	permit, _, err := acquire(t, governor, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("acquire: %v %v", permit, err)
	}
	if err := permit.Done(domain.Meta{Err: &flickr.CallError{Kind: flickr.ErrQuotaExceeded}}); err != nil {
		t.Fatalf("Done(): %v", err)
	}
	keys := governor.keys(1)
	if got, ok := server.Get(keys[1]); ok != nil || got != "20" {
		t.Fatalf("calibrated hourly count = %q, present %v", got, ok)
	}
	if permit, wait, err := acquire(t, governor, domain.Interactive); err != nil || permit != nil || wait <= 0 {
		t.Fatalf("during cooldown = %v %s %v", permit, wait, err)
	}
	if err := permit.Done(domain.Meta{Err: flickr.ErrQuotaExceeded}); err != nil {
		t.Fatalf("duplicate Done(): %v", err)
	}
	keys = governor.keys(1)
	if err := server.Set(keys[2], "1"); err != nil {
		t.Fatal(err)
	}
	server.Del(keys[3])
	if permit, _, err := acquire(t, governor, domain.Interactive); err != nil || permit == nil {
		t.Fatalf("after window = %v %v", permit, err)
	}
}

func TestDoneNonQuotaDoesNotCalibrate(t *testing.T) {
	governor, server, _ := setupGovernor(t, generousBudget())
	permit, _, err := acquire(t, governor, domain.Interactive)
	if err != nil || permit == nil {
		t.Fatalf("acquire: %v %v", permit, err)
	}
	if err := permit.Done(domain.Meta{Err: errors.New("temporary")}); err != nil {
		t.Fatalf("Done(): %v", err)
	}
	keys := governor.keys(1)
	if got, ok := server.Get(keys[1]); ok != nil || got != "1" {
		t.Fatalf("hourly count = %q, present %v, want 1", got, ok)
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
	governor, server, client := setupGovernor(t, budget)
	credits, err := governor.Capacity(context.Background(), 1, 2*time.Minute)
	if err != nil || credits != 1200 {
		t.Fatalf("Capacity() = %d, %v; want 1200", credits, err)
	}
	keys := governor.keys(1)
	server.Set(keys[1], "3400")
	server.Set(keys[2], strconv.FormatInt(time.Now().UnixMilli(), 10))
	credits, err = governor.Capacity(context.Background(), 1, 2*time.Minute)
	if err != nil || credits != 100 {
		t.Fatalf("Capacity(near hourly limit) = %d, %v; want 100", credits, err)
	}
	server.Set(keys[1], "3600")
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
	governor, _, _ := setupGovernor(t, budget)
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
