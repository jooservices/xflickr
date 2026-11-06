package redis

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joho/godotenv"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/quota/domain"
)

// TestRealRedisClockSecondStaysWithinBurst runs the acquire script on the
// worktree Redis. Miniredis covers the fast-forward hour; this checks Redis's
// own TIME and Lua against the live limits (4/s, burst 5).
func TestRealRedisClockSecondStaysWithinBurst(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the worktree Redis")
	}
	values, err := godotenv.Read(filepath.Join(integrationRoot(t), ".env"))
	if err != nil {
		t.Fatal("read shared worktree environment")
	}
	prefix := values["REDIS_PREFIX"]
	db, _ := strconv.Atoi(values["REDIS_DB"])
	if prefix == "" || prefix == "xflickr:" || db == 7 {
		t.Fatal("refusing the live Redis index")
	}
	port, _ := strconv.Atoi(values["REDIS_PORT"])
	if port == 0 {
		port = 16382
	}
	host := values["REDIS_HOST"]
	if host == "" {
		host = "127.0.0.1"
	}
	client := redisv9.NewClient(&redisv9.Options{
		Addr:     net.JoinHostPort(host, strconv.Itoa(port)),
		Password: values["REDIS_PASSWORD"],
		DB:       db,
	})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping worktree Redis: %v", err)
	}

	const keyID int64 = 312312312
	budget := productionSecondBudget()
	budgets := StaticBudgets{keyID: budget}
	crawl := Governor{Client: client, Prefix: prefix + "jooxf312:", Budgets: budgets}
	scheduler := Governor{Client: client, Prefix: crawl.Prefix, Budgets: budgets}
	if crawl.keys(keyID)[0] != scheduler.keys(keyID)[0] {
		t.Fatal("roles do not share one bucket key")
	}
	t.Cleanup(func() {
		for _, key := range crawl.keys(keyID) {
			_ = client.Del(context.Background(), key).Err()
		}
	})
	for _, key := range crawl.keys(keyID) {
		if err := client.Del(ctx, key).Err(); err != nil {
			t.Fatal(err)
		}
	}

	grants, slipped := grantsNearClockSecond(t, []Governor{crawl, scheduler}, keyID)
	if slipped {
		t.Fatal("could not start within 100ms of a clock second")
	}
	if grants > int(budget.Burst) {
		t.Fatalf("one clock second granted %d calls, burst is %d", grants, budget.Burst)
	}
	if grants < int(budget.Burst) {
		t.Fatalf("one clock second granted %d calls, want the full burst of %d", grants, budget.Burst)
	}
	t.Logf("worktree Redis granted %d calls in the measured second, burst %d", grants, budget.Burst)
}

// grantsNearClockSecond hammers two governors from the start of a clock second
// through 800ms. Retries a later second when the process wakes up late.
func grantsNearClockSecond(t *testing.T, roles []Governor, keyID int64) (int, bool) {
	t.Helper()
	classes := []domain.Class{domain.Person, domain.Photo, domain.Interactive}
	for attempt := 0; attempt < 3; attempt++ {
		boundary := time.Now().Truncate(time.Second).Add(time.Second)
		time.Sleep(time.Until(boundary))
		if time.Since(boundary) > 100*time.Millisecond {
			continue
		}
		deadline := boundary.Add(800 * time.Millisecond)
		var grants atomic.Int64
		var acquireErr error
		var errOnce sync.Once
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				role := roles[i%len(roles)]
				class := classes[i%len(classes)]
				for time.Now().Before(deadline) {
					permit, wait, err := role.Acquire(context.Background(), keyID, class)
					if err != nil {
						errOnce.Do(func() { acquireErr = err })
						return
					}
					if permit != nil {
						grants.Add(1)
						continue
					}
					pause := wait
					remaining := time.Until(deadline)
					if pause <= 0 || pause > remaining {
						pause = remaining
					}
					if pause <= 0 {
						return
					}
					time.Sleep(pause)
				}
			}(i)
		}
		wg.Wait()
		if acquireErr != nil {
			t.Fatalf("Acquire: %v", acquireErr)
		}
		return int(grants.Load()), false
	}
	return 0, true
}
