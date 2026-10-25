package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestCrawlControlsIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("crawl-control-%d", time.Now().UnixNano())
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID) })
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES($1,$2,2,'contact')`, userID, suffix); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"person", "favorites", "photo_sizes"} {
		if _, err := pool.Exec(ctx, `INSERT INTO work_items(kind,subject_key,user_id,state,direct_action,starred,score,score_updated_at) VALUES($1,$2,$3,'pending',false,false,3,now())`, kind, suffix, userID); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := NewControls(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetStarred(ctx, userID, suffix, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.Select(ctx, userID, suffix, time.Now().UTC().Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var starred, direct bool
	var updated *time.Time
	if err := pool.QueryRow(ctx, `SELECT starred,direct_action,score_updated_at FROM work_items WHERE user_id=$1 AND subject_key=$2 LIMIT 1`, userID, suffix).Scan(&starred, &direct, &updated); err != nil {
		t.Fatal(err)
	}
	if !starred || !direct || updated != nil {
		t.Fatalf("backlog score fields starred=%t direct=%t score_updated=%v", starred, direct, updated)
	}
	if err := repo.SetPaused(ctx, userID, suffix, true); err != nil {
		t.Fatal(err)
	}
	status, err := repo.Status(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	paused := int64(0)
	for _, lane := range status.Lanes {
		paused += lane.Paused
	}
	if len(status.Depths) != 1 || status.Depths[0].Depth != 2 || len(status.Facets) != 3 || len(status.Lanes) != 2 || paused != 3 {
		t.Fatalf("crawl status=%+v", status)
	}
	if err := repo.SetPaused(ctx, userID, suffix, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetStarred(ctx, userID, suffix, false); err != nil {
		t.Fatal(err)
	}
	// Unstarring must not clear direct_action, which selection owns.
	if err := pool.QueryRow(ctx, `SELECT starred,direct_action FROM work_items WHERE user_id=$1 AND subject_key=$2 LIMIT 1`, userID, suffix).Scan(&starred, &direct); err != nil {
		t.Fatal(err)
	}
	if starred || !direct {
		t.Fatalf("after unstar starred=%t direct=%t", starred, direct)
	}
}
