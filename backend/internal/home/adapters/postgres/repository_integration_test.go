package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/home/app"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
)

func TestContactDepthsIntegration(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the worktree database")
	}
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, cfg)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	suffix := fmt.Sprintf("home-%d", time.Now().UnixNano())
	var userID, otherUserID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"@example.invalid").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'integration','user') RETURNING id`, suffix+"-other@example.invalid").Scan(&otherUserID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=ANY($1)`, []int64{userID, otherUserID})
	})
	if _, err := pool.Exec(ctx, `INSERT INTO crawl_interests(user_id,person_nsid,depth,via) VALUES
		($1,$2,0,'owner'),($1,$3,1,'direct'),($1,$4,1,'direct'),($1,$5,2,'discovered'),($1,$6,3,'discovered'),($1,$7,0,'owner'),
		($8,$3,1,'other-user')`, userID, suffix+"-root", suffix+"-direct-a", suffix+"-direct-b", suffix+"-depth-two", suffix+"-depth-three", suffix+"-root-two", otherUserID); err != nil {
		t.Fatal(err)
	}
	repository, err := New(pool)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repository.ContactDepths(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if got != (app.ContactDepthCounts{Direct: 2, Discovered: 2}) {
		t.Fatalf("ContactDepths() = %+v, want 2 direct and 2 discovered", got)
	}
}
