package roles

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/platform/queue"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
)

func TestLaneClass(t *testing.T) {
	if laneClass(crawldomain.LanePhoto) != quotadomain.Photo || laneClass(crawldomain.LanePerson) != quotadomain.Person {
		t.Fatal("lanes map to the wrong quota classes")
	}
}

type stubPermit struct{ got error }

func (p *stubPermit) Done(meta quotadomain.Meta) error { p.got = meta.Err; return nil }

type stubGovernor struct {
	permit quotadomain.Permit
	wait   time.Duration
	err    error
	class  quotadomain.Class
}

func (g *stubGovernor) Acquire(_ context.Context, _ int64, class quotadomain.Class) (quotadomain.Permit, time.Duration, error) {
	g.class = class
	return g.permit, g.wait, g.err
}

func TestCrawlGovernorWrapsPermitAndPassesWait(t *testing.T) {
	permit := &stubPermit{}
	governor := &stubGovernor{permit: permit}
	got, _, err := crawlGovernor{governor: governor}.Acquire(context.Background(), 1, crawldomain.LanePhoto)
	if err != nil || got == nil || governor.class != quotadomain.Photo {
		t.Fatalf("Acquire() = %v, %v, class %q", got, err, governor.class)
	}
	callErr := errors.New("flickr down")
	if err := got.Done(callErr); err != nil || !errors.Is(permit.got, callErr) {
		t.Fatalf("Done() = %v, reported %v", err, permit.got)
	}
	none, wait, err := crawlGovernor{governor: &stubGovernor{wait: time.Second}}.Acquire(context.Background(), 1, crawldomain.LanePerson)
	if none != nil || wait != time.Second || err != nil {
		t.Fatalf("denied Acquire() = %v, %v, %v; want nil permit with the wait", none, wait, err)
	}
	failure := errors.New("offline")
	if _, _, err := (crawlGovernor{governor: &stubGovernor{err: failure}}).Acquire(context.Background(), 1, crawldomain.LanePerson); !errors.Is(err, failure) {
		t.Fatalf("Acquire() error = %v", err)
	}
}

type stubInterests []int64

func (s stubInterests) InterestedUsers(context.Context, string) ([]int64, error) { return s, nil }

type stubKeys struct{}

func (stubKeys) KeysOf(context.Context, int64) ([]quotaapp.PoolKey, error) {
	return []quotaapp.PoolKey{{ID: 7, Valid: true}}, nil
}

type stubQuota struct{ left int }

func (q stubQuota) Capacity(context.Context, int64, time.Duration) (int, error) { return q.left, nil }

func TestCrawlKeysChoosesKeyAndMapsNoKey(t *testing.T) {
	pool := quotaapp.KeyPool{Interests: stubInterests{1}, Keys: stubKeys{}, Quota: stubQuota{left: 5}}
	if id, err := (crawlKeys{pool: pool}).Choose(context.Background(), "n", crawldomain.LanePerson); err != nil || id != 7 {
		t.Fatalf("Choose() = %d, %v; want 7", id, err)
	}
	pool.Quota = stubQuota{}
	if _, err := (crawlKeys{pool: pool}).Choose(context.Background(), "n", crawldomain.LanePerson); !errors.Is(err, crawlapp.ErrNoKey) {
		t.Fatalf("Choose() error = %v, want crawl ErrNoKey", err)
	}
}

func TestRegisterCrawlJobsRegistersEveryFacetOnce(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://user:password@127.0.0.1:1/xflickr_wt251?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	adapter, err := queue.NewRiver(pool, map[string]int{"sync": 1})
	if err != nil {
		t.Fatal(err)
	}
	registry := crawlapp.NewRegistry()
	if err := registry.Register(crawlapp.ChangeCheck{}); err != nil {
		t.Fatal(err)
	}
	engine := &crawlapp.Engine{Registry: registry}
	if err := registerCrawlJobs(adapter, engine); err != nil {
		t.Fatal(err)
	}
	if err := registerCrawlJobs(adapter, engine); err == nil {
		t.Fatal("registering the same facet kind twice succeeded")
	}
}
