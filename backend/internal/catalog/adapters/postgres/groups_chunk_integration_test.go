package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestGroupsCommitUpTo500Unchanged(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	repo, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("grp500-%d", time.Now().UnixNano())
	person := suffix + "@example.invalid"
	seen := time.Now().UTC().Truncate(time.Microsecond)
	groups := make([]postgres.Group, 500)
	memberships := make([]postgres.PersonGroup, 500)
	nsids := make([]string, 500)
	for i := 0; i < 500; i++ {
		nsid := fmt.Sprintf("%s-g-%d", suffix, i)
		nsids[i] = nsid
		groups[i] = postgres.Group{NSID: nsid, Name: fmt.Sprintf("Group %d", i), MembersCount: int64(i), PoolCount: int64(i * 2), SeenAt: seen}
		memberships[i] = postgres.PersonGroup{PersonNSID: person, GroupNSID: nsid, SeenAt: seen}
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM person_groups WHERE person_nsid=$1`, person)
		_, _ = pool.Exec(context.Background(), `DELETE FROM groups WHERE nsid=ANY($1)`, nsids)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, person)
	}()
	if err := repo.UpsertPeople(ctx, []domain.Person{{NSID: person, Username: "groups-500"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertGroupsBatch(ctx, groups); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertPersonGroupsBatch(ctx, memberships); err != nil {
		t.Fatal(err)
	}
	var groupCount, membershipCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM groups WHERE nsid=ANY($1)`, nsids).Scan(&groupCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM person_groups WHERE person_nsid=$1`, person).Scan(&membershipCount); err != nil {
		t.Fatal(err)
	}
	if groupCount != 500 || membershipCount != 500 {
		t.Fatalf("counts groups=%d memberships=%d, want 500 each", groupCount, membershipCount)
	}
	var name string
	var members, poolCount int64
	if err := pool.QueryRow(ctx, `SELECT name, members_count, pool_count FROM groups WHERE nsid=$1`, nsids[42]).Scan(&name, &members, &poolCount); err != nil {
		t.Fatal(err)
	}
	if name != "Group 42" || members != 42 || poolCount != 84 {
		t.Fatalf("row 42 = %q members=%d pool=%d", name, members, poolCount)
	}
}

func TestGroupsUpsertPlanUsesPrimaryKeys(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	cases := []struct {
		name          string
		conflictIndex string
		query         string
	}{
		{
			name:          "groups",
			conflictIndex: "groups_pkey",
			query: `INSERT INTO groups (nsid,name,description,members_count,pool_count,seen_at)
				VALUES ('plan-group@N00','Plan Group','',0,0,CURRENT_TIMESTAMP)
				ON CONFLICT (nsid) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,
				members_count=EXCLUDED.members_count,pool_count=EXCLUDED.pool_count,seen_at=EXCLUDED.seen_at
				WHERE (groups.name,groups.description,groups.members_count,groups.pool_count,groups.seen_at)
				IS DISTINCT FROM (EXCLUDED.name,EXCLUDED.description,EXCLUDED.members_count,EXCLUDED.pool_count,EXCLUDED.seen_at)`,
		},
		{
			name:          "person groups",
			conflictIndex: "person_groups_pkey",
			query: `INSERT INTO person_groups (person_nsid,group_nsid,seen_at)
				VALUES ('plan-person@N00','plan-group@N00',CURRENT_TIMESTAMP)
				ON CONFLICT (person_nsid,group_nsid) DO UPDATE SET seen_at=EXCLUDED.seen_at
				WHERE person_groups.seen_at IS DISTINCT FROM EXCLUDED.seen_at`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw []byte
			if err := pool.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+tc.query).Scan(&raw); err != nil {
				t.Fatalf("explain upsert: %v", err)
			}
			var plan any
			if err := json.Unmarshal(raw, &plan); err != nil {
				t.Fatalf("decode plan: %v", err)
			}
			hasSeqScan, hasConflictIndex := inspectGroupsUpsertPlan(plan, tc.conflictIndex)
			if hasSeqScan {
				t.Fatal("upsert plan contains a Seq Scan")
			}
			if !hasConflictIndex {
				t.Fatalf("upsert plan does not use conflict arbiter %q: %s", tc.conflictIndex, raw)
			}
		})
	}
}

func inspectGroupsUpsertPlan(value any, conflictIndex string) (bool, bool) {
	hasSeqScan, hasConflictIndex := false, false
	switch node := value.(type) {
	case map[string]any:
		if nodeType, ok := node["Node Type"].(string); ok && (nodeType == "Seq Scan" || nodeType == "Parallel Seq Scan") {
			hasSeqScan = true
		}
		if arbiters, ok := node["Conflict Arbiter Indexes"].([]any); ok {
			for _, arbiter := range arbiters {
				if arbiter == conflictIndex {
					hasConflictIndex = true
				}
			}
		}
		for _, child := range node {
			childSeqScan, childConflictIndex := inspectGroupsUpsertPlan(child, conflictIndex)
			hasSeqScan = hasSeqScan || childSeqScan
			hasConflictIndex = hasConflictIndex || childConflictIndex
		}
	case []any:
		for _, child := range node {
			childSeqScan, childConflictIndex := inspectGroupsUpsertPlan(child, conflictIndex)
			hasSeqScan = hasSeqScan || childSeqScan
			hasConflictIndex = hasConflictIndex || childConflictIndex
		}
	}
	return hasSeqScan, hasConflictIndex
}

func TestGroupsCommit5000MembershipsInOneTransaction(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	pool := dbtest.Open(t, loadCatalogDatabase(t))
	suffix := fmt.Sprintf("grp5k-%d", time.Now().UnixNano())
	person := suffix + "@example.invalid"
	seen := time.Now().UTC().Truncate(time.Microsecond)
	const total = 5000
	groups := make([]postgres.Group, total)
	memberships := make([]postgres.PersonGroup, total)
	nsids := make([]string, total)
	for i := 0; i < total; i++ {
		nsid := fmt.Sprintf("%s-g-%d", suffix, i)
		nsids[i] = nsid
		groups[i] = postgres.Group{NSID: nsid, Name: fmt.Sprintf("Group %d", i), MembersCount: 1, PoolCount: 1, SeenAt: seen}
		memberships[i] = postgres.PersonGroup{PersonNSID: person, GroupNSID: nsid, SeenAt: seen}
	}
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM person_groups WHERE person_nsid=$1`, person)
		_, _ = pool.Exec(context.Background(), `DELETE FROM groups WHERE nsid LIKE $1`, suffix+"-g-%")
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, person)
	}
	defer cleanup()

	seed, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.UpsertPeople(ctx, []domain.Person{{NSID: person, Username: "groups-5000"}}); err != nil {
		t.Fatal(err)
	}

	t.Run("all rows commit", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		repo, err := postgres.New(tx)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertGroupsBatch(ctx, groups); err != nil {
			t.Fatal(err)
		}
		if err := repo.UpsertPersonGroupsBatch(ctx, memberships); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		var groupCount, membershipCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM groups WHERE nsid LIKE $1`, suffix+"-g-%").Scan(&groupCount); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM person_groups WHERE person_nsid=$1`, person).Scan(&membershipCount); err != nil {
			t.Fatal(err)
		}
		if groupCount != total || membershipCount != total {
			t.Fatalf("counts groups=%d memberships=%d, want %d each", groupCount, membershipCount, total)
		}
	})

	t.Run("last chunk failure rolls back", func(t *testing.T) {
		cleanup()
		if err := seed.UpsertPeople(ctx, []domain.Person{{NSID: person, Username: "groups-5000"}}); err != nil {
			t.Fatal(err)
		}
		broken := append([]postgres.Group(nil), groups...)
		broken[total-1] = postgres.Group{NSID: "", Name: "bad", SeenAt: seen}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		repo, err := postgres.New(tx)
		if err != nil {
			t.Fatal(err)
		}
		err = repo.UpsertGroupsBatch(ctx, broken)
		if err == nil {
			_ = tx.Rollback(ctx)
			t.Fatal("expected invalid last chunk to fail")
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var groupCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM groups WHERE nsid LIKE $1`, suffix+"-g-%").Scan(&groupCount); err != nil {
			t.Fatal(err)
		}
		if groupCount != 0 {
			t.Fatalf("rolled-back transaction left %d groups", groupCount)
		}
	})
}
