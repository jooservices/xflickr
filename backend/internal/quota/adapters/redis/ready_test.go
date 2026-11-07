package redis

import (
	"context"
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

func TestGrantNeverSetsReadyMarker(t *testing.T) {
	g, server, _ := setupGovernor(t, generousBudget())
	g.Rebuilder = noopRebuilder{}
	ready := g.keys(1)[5]
	server.Set(ready, "1")
	server.SetTTL(ready, time.Second)

	if permit, _, err := acquire(t, g, domain.Interactive); err != nil || permit == nil {
		t.Fatalf("grant with ready marker = %v %v", permit, err)
	}
	ttl := server.TTL(ready)
	if ttl > 2*time.Second {
		t.Fatalf("grant refreshed ready marker TTL to %s; only restore may set ready", ttl)
	}
	server.Del(ready)
	if permit, wait, err := acquire(t, g, domain.Interactive); err != nil || permit != nil || wait != domain.LedgerNotReadyRetry {
		t.Fatalf("after deleting ready, grant must refuse: permit=%v wait=%s err=%v", permit, wait, err)
	}
	if server.Exists(ready) {
		t.Fatal("a refused acquire must not set the ready marker")
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

type countingRebuilder struct {
	inner *app.Rebuilder
	n     *atomic.Int64
}

func (c countingRebuilder) Rebuild(ctx context.Context, keyID int64) error {
	c.n.Add(1)
	return c.inner.Rebuild(ctx, keyID)
}
