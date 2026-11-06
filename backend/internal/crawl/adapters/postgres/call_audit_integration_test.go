package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	catalogpg "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/crawl/facets"
)

type auditFlickr struct{ calls map[string]int }

func (f auditFlickr) Call(_ context.Context, r crawlapp.Request) (json.RawMessage, error) {
	f.calls[r.Facet]++
	var payload string
	switch r.Facet {
	case "albums":
		payload = fmt.Sprintf(`{"photosets":{"pages":1,"photoset":[{"id":%q,"photos":"3"}]}}`, r.Params["user_id"])
	case "galleries":
		payload = fmt.Sprintf(`{"galleries":{"pages":1,"gallery":[{"id":%q,"count_photos":"4"}]}}`, r.Params["user_id"])
	case "groups":
		payload = fmt.Sprintf(`{"groups":{"group":[{"nsid":%q,"name":"Group","members":"150","pool_count":2500}]}}`, r.Params["user_id"])
	default:
		return nil, fmt.Errorf("unexpected audit facet %s", r.Facet)
	}
	return json.RawMessage(payload), nil
}

// TestCatalogCallAuditHour replays one virtual hour against PostgreSQL, with
// twelve arrivals per facet and the engine's real retry policy. HTTP is fake;
// this is not a live quota measurement or a scheduler throughput benchmark.
func TestCatalogCallAuditHour(t *testing.T) {
	pool := openEngineTestPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	user := insertInterestTestUser(t, ctx, pool)
	prefix := fmt.Sprintf("call-audit-%d-", time.Now().UnixNano())
	t.Cleanup(func() {
		for _, table := range []string{"person_groups", "groups", "albums", "galleries", "people"} {
			column := map[string]string{"person_groups": "person_nsid", "groups": "nsid", "albums": "id", "galleries": "id", "people": "nsid"}[table]
			_, _ = pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE "+column+" LIKE $1", prefix+"%")
		}
		_, _ = pool.Exec(context.Background(), `DELETE FROM work_items WHERE user_id=$1`, user)
	})
	registry := crawlapp.NewRegistry()
	facetList := []domain.Facet{facets.Albums{}, facets.Galleries{}, facets.Groups{}}
	for _, facet := range facetList {
		if err := registry.Register(facet); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewEngine(pool, func(conn Conn) crawlapp.CatalogWriter {
		repo, err := catalogpg.New(conn)
		if err != nil {
			t.Fatal(err)
		}
		return repo
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	start := now
	calls := map[string]int{}
	engine := &crawlapp.Engine{Registry: registry, Store: store, Units: store, Keys: intKeys{}, Governor: intGovernor{}, Flickr: auditFlickr{calls}, Settings: photosFavoritesSettings{maxDepth: 2}, Now: func() time.Time { return now }}
	var ids []int64
	for tick := 0; tick < 120; tick++ {
		now = start.Add(time.Duration(tick) * 30 * time.Second)
		if tick%8 == 0 && tick/8 < 12 {
			person := fmt.Sprintf("%s%d", prefix, tick/8)
			if _, err := pool.Exec(ctx, `INSERT INTO people(nsid,username) VALUES($1,'Audit')`, person); err != nil {
				t.Fatal(err)
			}
			for _, facet := range facetList {
				ids = append(ids, insertPhotosFavoritesWork(t, ctx, pool, user, facet.Name(), person, 2, facet))
			}
		}
		for _, id := range ids {
			// Emulate dispatch only after the stored retry deadline, preserving the
			// engine's exponential backoff and terminal-state behavior.
			if _, err := pool.Exec(ctx, `UPDATE work_items SET state='ready' WHERE id=$1 AND state='pending' AND not_before<=$2`, id, now); err != nil {
				t.Fatal(err)
			}
			if err := engine.Handle(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	done := count(t, ctx, pool, `SELECT count(*) FROM work_items WHERE user_id=$1 AND state='done'`, user)
	t.Logf("virtual hour: calls=%v completed=%d/36", calls, done)
	for _, facet := range facetList {
		if calls[facet.Name()] != 12 {
			t.Errorf("%s calls=%d, want 12 (one call per successful list)", facet.Name(), calls[facet.Name()])
		}
	}
	if done != 36 {
		t.Errorf("completed=%d, want 36", done)
	}
	for table, column := range map[string]string{"groups": "nsid", "person_groups": "person_nsid", "albums": "id", "galleries": "id"} {
		if n := count(t, ctx, pool, "SELECT count(*) FROM "+table+" WHERE "+column+" LIKE $1", prefix+"%"); n != 12 {
			t.Errorf("%s rows=%d, want 12", table, n)
		}
	}
}
