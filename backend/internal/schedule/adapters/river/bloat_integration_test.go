package river_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	scheduleriver "github.com/jooservices/xflickr/internal/schedule/adapters/river"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

func TestPumpClaimUsesIndexOnlyNoSeqScan(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	if os.Getenv("XFLICKR_BLOAT_TEST") != "1" {
		t.Skip("set XFLICKR_BLOAT_TEST=1 after docker/scripts/synthetic-work-items.sh")
	}
	pool := integrationPool(t)
	ctx := t.Context()
	var keyID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM api_keys WHERE key_public='synthetic-public'`).Scan(&keyID); err != nil {
		t.Fatalf("synthetic key missing; run synthetic-work-items.sh: %v", err)
	}
	for _, query := range []struct {
		name string
		sql  string
	}{
		{"pendingKeys", `EXPLAIN (FORMAT TEXT) SELECT DISTINCT k.id FROM api_keys k JOIN flickr_accounts a ON a.key_id=k.id AND a.status='ok' WHERE k.status='valid' ORDER BY k.id`},
		{"claim", fmt.Sprintf(`EXPLAIN (FORMAT TEXT) SELECT w.id FROM work_items w WHERE w.state='pending' AND w.key_id=%d AND w.worker_type='sync' AND w.not_before<=now() ORDER BY w.score DESC, w.not_before, w.id LIMIT 10`, keyID)},
		{"inflight", fmt.Sprintf(`EXPLAIN (FORMAT TEXT) SELECT count(*) FROM work_items WHERE state IN ('ready','running') AND key_id=%d`, keyID)},
		{"rescore", `EXPLAIN (FORMAT TEXT) SELECT w2.id FROM work_items w2 LEFT JOIN users u ON u.id=w2.user_id LEFT JOIN plans p ON p.id=u.plan_id LEFT JOIN crawl_interests ci ON ci.user_id=w2.user_id AND ci.person_nsid=w2.subject_key WHERE w2.state='pending' ORDER BY w2.id LIMIT 500`},
	} {
		rows, err := pool.Query(ctx, query.sql)
		if err != nil {
			t.Fatalf("%s: %v", query.name, err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		rows.Close()
		text := plan.String()
		t.Logf("%s plan:\n%s", query.name, text)
		if strings.Contains(text, "Seq Scan on work_items") {
			t.Fatalf("%s used Seq Scan on work_items:\n%s", query.name, text)
		}
	}
}

func TestSteadyStateHOTRatioAtLeast70Percent(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	if os.Getenv("XFLICKR_BLOAT_TEST") != "1" {
		t.Skip("set XFLICKR_BLOAT_TEST=1 for the HOT steady-state loop")
	}
	pool := integrationPool(t)
	ctx := t.Context()
	suffix := fmt.Sprintf("hot-%d", time.Now().UnixNano())
	keyID := seedKey(t, ctx, pool, suffix)
	t.Cleanup(func() { cleanupPumpFixture(t, pool, suffix) })
	var userID int64
	if err := pool.QueryRow(ctx, `SELECT user_id FROM api_keys WHERE id=$1`, keyID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,key_id,state,worker_type,score,not_before)
		SELECT 'sync_person',$1||'-'||g, $2, $3, 'pending', 'sync', g, now()-interval '1 minute'
		FROM generate_series(1,200) g`, suffix, userID, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `SELECT pg_stat_reset_single_table_counters('work_items'::regclass)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		tag, err := pool.Exec(ctx, `WITH picked AS (
			SELECT id FROM work_items WHERE state='pending' AND key_id=$1 ORDER BY score DESC, id LIMIT 5 FOR UPDATE SKIP LOCKED
		) UPDATE work_items SET state='ready', lease_expires_at=now()+interval '2 minutes', updated_at=now() WHERE id IN (SELECT id FROM picked)`, keyID)
		if err != nil {
			t.Fatal(err)
		}
		if tag.RowsAffected() == 0 {
			break
		}
		if _, err := pool.Exec(ctx, `UPDATE work_items SET state='pending', lease_expires_at=NULL, attempts=attempts+1, updated_at=now()
			WHERE state='ready' AND key_id=$1`, keyID); err != nil {
			t.Fatal(err)
		}
	}
	var updates, hot float64
	if err := pool.QueryRow(ctx, `SELECT n_tup_upd::float8, n_tup_hot_upd::float8 FROM pg_stat_user_tables WHERE relname='work_items'`).Scan(&updates, &hot); err != nil {
		t.Fatal(err)
	}
	if updates == 0 {
		t.Fatal("no updates recorded for work_items")
	}
	ratio := hot / updates
	t.Logf("HOT ratio = %.2f (%v / %v)", ratio, hot, updates)
	if ratio < 0.70 {
		t.Fatalf("HOT ratio = %.2f, want >= 0.70", ratio)
	}
}

func TestWeightsFingerprintRescoreIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	pool := integrationPool(t)
	ctx := t.Context()
	store := scheduleriver.Store{Pool: pool}
	weights := domain.ScoreWeights{PlanWeight: 10, DirectAction: 100, Starred: 20, Depth: 5, AgeMinutes: .1}
	fp, err := store.ComputeWeightsFingerprint(ctx, weights, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StoreWeightsFingerprint(ctx, "stale-for-test"); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadWeightsFingerprint(ctx)
	if err != nil || got != "stale-for-test" {
		t.Fatalf("LoadWeightsFingerprint = %q, %v", got, err)
	}
	if fp == got {
		t.Fatal("computed fingerprint unexpectedly matched stale marker")
	}
	_ = store.StoreWeightsFingerprint(ctx, fp)
}
