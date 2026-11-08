package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/quota/app"
	"github.com/jooservices/xflickr/internal/quota/domain"
)

func TestAcquireRefusesWhenLedgerNotReady(t *testing.T) {
	g, server, _ := setupGovernor(t, generousBudget())
	g.Rebuilder = noopRebuilder{}
	server.FlushAll()

	permit, wait, err := acquire(t, g, domain.Interactive)
	if permit != nil || err != nil || wait != domain.LedgerNotReadyRetry {
		t.Fatalf("acquire = permit=%v wait=%s err=%v, want refuse with %s", permit, wait, err, domain.LedgerNotReadyRetry)
	}
	if server.Exists(g.keys(1)[5]) {
		t.Fatal("ready marker must stay absent when rebuild does not restore")
	}
}

func TestGrantNeverCreatesReadyMarker(t *testing.T) {
	g, server, _ := setupGovernor(t, generousBudget())
	g.Rebuilder = noopRebuilder{}
	ready := g.keys(1)[5]
	server.Del(ready)
	if permit, wait, err := acquire(t, g, domain.Interactive); err != nil || permit != nil || wait != domain.LedgerNotReadyRetry {
		t.Fatalf("without ready, grant must refuse: permit=%v wait=%s err=%v", permit, wait, err)
	}
	if server.Exists(ready) {
		t.Fatal("a refused acquire must not create the ready marker")
	}
}

func TestGrantExtendsExistingReadyMarkerTTL(t *testing.T) {
	g, server, _ := setupGovernor(t, generousBudget())
	g.Rebuilder = noopRebuilder{}
	ready := g.keys(1)[5]
	server.Set(ready, "1")
	server.SetTTL(ready, 500*time.Millisecond)

	if permit, _, err := acquire(t, g, domain.Interactive); err != nil || permit == nil {
		t.Fatalf("grant with ready marker = %v %v", permit, err)
	}
	ttl := server.TTL(ready)
	if ttl < time.Minute {
		t.Fatalf("grant must extend existing ready marker TTL, got %s", ttl)
	}
}

func TestRestoreIsIdempotentAndAtomic(t *testing.T) {
	g, _, client := setupGovernor(t, generousBudget())
	c := Counters{Client: client, Prefix: testPrefix}
	at := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
	usage := app.Usage{
		KeyID: 1,
		Grants: []app.Grant{
			{Class: "interactive", At: at, Member: "log:1"},
			{Class: "person", At: at, Member: "log:2"},
			{Class: "photo", At: at, Member: "log:3"},
		},
	}
	wrote, err := c.Restore(context.Background(), usage)
	if err != nil || !wrote {
		t.Fatalf("first restore = %v %v", wrote, err)
	}
	wrote, err = c.Restore(context.Background(), usage)
	if err != nil || wrote {
		t.Fatalf("second restore must be a no-op once ready: wrote=%v err=%v", wrote, err)
	}
	if err := client.Del(context.Background(), g.keys(1)[5]).Err(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Restore(context.Background(), usage); err != nil {
				t.Errorf("concurrent restore: %v", err)
			}
		}()
	}
	wg.Wait()
	snap, ok, err := c.Snapshot(context.Background(), 1)
	if err != nil || !ok || snap.Used != 3 {
		t.Fatalf("restored ledger = %+v ok=%v err=%v", snap, ok, err)
	}
}

func TestConcurrentRolesRebuildOnce(t *testing.T) {
	budget := generousBudget()
	budget.HourlyLimit = 50
	budget.Reserve = 0
	server := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	at := time.Now().Add(-2 * time.Minute).Truncate(time.Millisecond)
	const prior = 20
	calls := memCalls{usage: app.CallUsage{
		First:   at,
		ByClass: map[string]int64{"interactive": prior},
		Grants:  testGrants(map[string]int64{"interactive": prior}, at),
	}}
	store := memStore{}
	var rebuilds atomic.Int64
	newRole := func() Governor {
		base := Governor{Client: client, Prefix: testPrefix}
		inner := rebuilderFor(base, store, calls)
		counting := countingRebuilder{inner: inner, n: &rebuilds}
		g, err := NewGovernor(client, testPrefix, StaticBudgets{1: budget}, counting, nil)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	crawl := newRole()
	scheduler := newRole()
	for _, key := range crawl.keys(1) {
		server.Del(key)
	}

	const callers = 40
	var wg sync.WaitGroup
	granted := make(chan bool, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g := crawl
			if i%2 == 1 {
				g = scheduler
			}
			permit, _, err := g.Acquire(context.Background(), 1, domain.Interactive)
			if err != nil {
				t.Errorf("Acquire: %v", err)
			}
			granted <- permit != nil
		}(i)
	}
	wg.Wait()
	close(granted)
	count := 0
	for ok := range granted {
		if ok {
			count++
		}
	}
	if rebuilds.Load() != 1 {
		t.Fatalf("rebuilds = %d, want 1 across both roles", rebuilds.Load())
	}
	if count != int(budget.HourlyLimit)-prior {
		t.Fatalf("granted %d after rebuild, want %d", count, int(budget.HourlyLimit)-prior)
	}
	used, err := client.ZCard(context.Background(), crawl.keys(1)[1]).Result()
	if err != nil || used != budget.HourlyLimit {
		t.Fatalf("ledger size = %d err=%v, want %d", used, err, budget.HourlyLimit)
	}
}

func TestNewGovernorRequiresRebuilder(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	if _, err := NewGovernor(client, testPrefix, StaticBudgets{1: generousBudget()}, nil, nil); err == nil {
		t.Fatal("nil rebuilder accepted")
	}
	if _, err := NewGovernor(nil, testPrefix, StaticBudgets{1: generousBudget()}, noopRebuilder{}, nil); err == nil {
		t.Fatal("nil client accepted")
	}
	if _, err := NewGovernor(client, testPrefix, nil, noopRebuilder{}, nil); err == nil {
		t.Fatal("nil budgets accepted")
	}
}

func TestUnlockOnlyRemovesOwnLock(t *testing.T) {
	_, _, client := setupGovernor(t, generousBudget())
	ctx := t.Context()
	lockKey := fmt.Sprintf("%squota:key:1:rolling:rebuild_lock", testPrefix)
	if err := client.Set(ctx, lockKey, "holder-a", rebuildLockTTL).Err(); err != nil {
		t.Fatal(err)
	}
	// Simulate TTL expiry: another holder replaces the lock value.
	if err := client.Set(ctx, lockKey, "holder-b", rebuildLockTTL).Err(); err != nil {
		t.Fatal(err)
	}
	removed, err := unlockScript.Run(ctx, client, []string{lockKey}, "holder-a").Int64()
	if err != nil || removed != 0 {
		t.Fatalf("expired holder unlock = %d err=%v, want 0 (must not delete next holder)", removed, err)
	}
	val, err := client.Get(ctx, lockKey).Result()
	if err != nil || val != "holder-b" {
		t.Fatalf("lock after foreign unlock = %q err=%v, want holder-b", val, err)
	}
	removed, err = unlockScript.Run(ctx, client, []string{lockKey}, "holder-b").Int64()
	if err != nil || removed != 1 {
		t.Fatalf("own unlock = %d err=%v, want 1", removed, err)
	}
	if n, err := client.Exists(ctx, lockKey).Result(); err != nil || n != 0 {
		t.Fatalf("lock must be gone after own unlock: exists=%d err=%v", n, err)
	}
}

func TestWaiterTakesOverWhenHolderFails(t *testing.T) {
	budget := generousBudget()
	budget.HourlyLimit = 50
	budget.Reserve = 0
	server := miniredis.RunT(t)
	client := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	at := time.Now().Add(-2 * time.Minute).Truncate(time.Millisecond)
	const prior = 5
	inner := rebuilderFor(Governor{Client: client, Prefix: testPrefix}, memStore{}, memCalls{usage: app.CallUsage{
		First:   at,
		ByClass: map[string]int64{"interactive": prior},
		Grants:  testGrants(map[string]int64{"interactive": prior}, at),
	}})
	entered := make(chan struct{})
	release := make(chan struct{})
	holderReb := &holdThenFailRebuilder{entered: entered, release: release, err: errors.New("rebuild failed")}
	holder, err := NewGovernor(client, testPrefix, StaticBudgets{1: budget}, holderReb, nil)
	if err != nil {
		t.Fatal(err)
	}
	waiter, err := NewGovernor(client, testPrefix, StaticBudgets{1: budget}, inner, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range holder.keys(1) {
		server.Del(key)
	}

	holderDone := make(chan error, 1)
	go func() {
		_, _, err := holder.Acquire(context.Background(), 1, domain.Interactive)
		holderDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("holder did not enter Rebuild")
	}

	type outcome struct {
		permit domain.Permit
		err    error
		took   time.Duration
	}
	waiterDone := make(chan outcome, 1)
	go func() {
		start := time.Now()
		permit, _, err := waiter.Acquire(context.Background(), 1, domain.Interactive)
		waiterDone <- outcome{permit: permit, err: err, took: time.Since(start)}
	}()

	// Waiter must lose SetNX and enter waitReady before the holder unlocks.
	settle := time.NewTimer(2 * rebuildPollEvery)
	defer settle.Stop()
	select {
	case <-settle.C:
	case o := <-waiterDone:
		t.Fatalf("waiter finished before holder released: permit=%v err=%v", o.permit, o.err)
	}
	close(release)

	o := <-waiterDone
	if o.err != nil || o.permit == nil {
		t.Fatalf("waiter acquire = permit=%v err=%v", o.permit, o.err)
	}
	if o.took > time.Second {
		t.Fatalf("waiter waited %s; want takeover within 1s when holder fails", o.took)
	}
	select {
	case err := <-holderDone:
		if !errors.Is(err, domain.ErrGovernorOffline) {
			t.Fatalf("holder err = %v, want ErrGovernorOffline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("holder Acquire did not finish")
	}
}

type holdThenFailRebuilder struct {
	entered chan struct{}
	release chan struct{}
	err     error
	once    sync.Once
}

func (r *holdThenFailRebuilder) Rebuild(context.Context, int64) error {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	return r.err
}

type countingRebuilder struct {
	inner *app.Rebuilder
	n     *atomic.Int64
}

func (c countingRebuilder) Rebuild(ctx context.Context, keyID int64) error {
	c.n.Add(1)
	return c.inner.Rebuild(ctx, keyID)
}

// recordingCallLog is both the Governor recorder and the Rebuilder call log.
// Live acquire members use <now>:<seq>; recovered members use log:<n>.
type recordingCallLog struct {
	mu     sync.Mutex
	grants []app.Grant
}

func (r *recordingCallLog) Record(_ context.Context, _ int64, class string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.grants = append(r.grants, app.Grant{
		Class:  class,
		At:     at,
		Member: fmt.Sprintf("log:%d", len(r.grants)+1),
	})
	return nil
}

func (r *recordingCallLog) Usage(_ context.Context, _ int64, since time.Time) (app.CallUsage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := app.CallUsage{ByClass: map[string]int64{}}
	for _, grant := range r.grants {
		if grant.At.Before(since) {
			continue
		}
		out.Grants = append(out.Grants, grant)
		out.ByClass[grant.Class]++
		if out.First.IsZero() || grant.At.Before(out.First) {
			out.First = grant.At
		}
	}
	return out, nil
}

func (r *recordingCallLog) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.grants)
}
