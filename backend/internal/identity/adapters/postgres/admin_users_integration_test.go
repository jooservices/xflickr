package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/identity/adapters/postgres"
	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/audit"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
)

type recordedAudit struct {
	mu      sync.Mutex
	entries []string
}

func (r *recordedAudit) Record(_ context.Context, actor, action, target string, _ map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, actor+" "+action+" "+target)
	return nil
}

var _ audit.Recorder = (*recordedAudit)(nil)

func TestAdminUsersListAndUpdate(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, sourceFile, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.NewPool(ctx, dbConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := postgres.New(pool)

	// Make the two test administrators the only active ones, then restore the rest.
	rows, err := pool.Query(ctx, `SELECT id FROM users WHERE role='admin' AND disabled_at IS NULL`)
	if err != nil {
		t.Fatal(err)
	}
	var others []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		others = append(others, id)
	}
	rows.Close()
	if _, err := pool.Exec(ctx, `UPDATE users SET disabled_at=now() WHERE id = ANY($1)`, others); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("jooxf96-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM users WHERE email LIKE $1`, prefix+"%")
		_, _ = pool.Exec(bg, `UPDATE users SET disabled_at=NULL WHERE id = ANY($1)`, others)
	})
	insert := func(name, role string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'x',$2) RETURNING id`, prefix+name+"@example.invalid", role).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	adminA, adminB, member := insert("-a", "admin"), insert("-b", "admin"), insert("-m", "user")

	rec := &recordedAudit{}
	svc := app.AdminUsers{Store: repository, Audit: rec}

	page, err := svc.List(ctx, app.UserQuery{Search: strings.ToUpper(prefix), PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Items[0].ID != member {
		t.Fatalf("page 1 = total %d items %+v", page.Total, page.Items)
	}
	page, err = svc.List(ctx, app.UserQuery{Search: prefix, Page: 2, PageSize: 2})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != adminA {
		t.Fatalf("page 2 = %+v, %v", page, err)
	}
	if page, err = svc.List(ctx, app.UserQuery{Search: "%"}); err != nil || page.Total != 0 && len(page.Items) == 0 {
		t.Fatalf("wildcard search leaked: %+v, %v", page, err)
	}

	on, off := true, false
	user := domain.RoleUser
	got, err := svc.Update(ctx, adminA, member, domain.UserChange{Disabled: &on})
	if err != nil || !got.Disabled || got.Role != domain.RoleUser {
		t.Fatalf("disable member = %+v, %v", got, err)
	}
	if got, err = svc.Update(ctx, adminA, member, domain.UserChange{Disabled: &off, Role: ptrRole(domain.RoleAdmin)}); err != nil || got.Disabled || got.Role != domain.RoleAdmin {
		t.Fatalf("enable and promote member = %+v, %v", got, err)
	}
	// Three active administrators now; demote the member again, then each admin in turn.
	if _, err = svc.Update(ctx, adminA, member, domain.UserChange{Role: &user}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Update(ctx, adminA, adminB, domain.UserChange{Role: &user}); err != nil {
		t.Fatalf("demote one of two admins: %v", err)
	}
	if _, err = svc.Update(ctx, adminA, adminA, domain.UserChange{Disabled: &on}); !errors.Is(err, domain.ErrLastAdmin) {
		t.Fatalf("disable last admin error = %v", err)
	}
	if _, err = svc.Update(ctx, adminA, adminA, domain.UserChange{Role: &user}); !errors.Is(err, domain.ErrLastAdmin) {
		t.Fatalf("demote last admin error = %v", err)
	}
	if _, err = svc.Update(ctx, adminA, adminA+1_000_000, domain.UserChange{Disabled: &on}); !errors.Is(err, app.ErrUserNotFound) {
		t.Fatalf("unknown user error = %v", err)
	}
	stored, err := repository.FindByID(ctx, adminA)
	if err != nil || stored.Disabled || stored.Role != domain.RoleAdmin {
		t.Fatalf("last admin changed: %+v, %v", stored, err)
	}
	if len(rec.entries) != 4 {
		t.Fatalf("audit entries = %v", rec.entries)
	}

	// Two admins demoting each other at once must leave one.
	if _, err = svc.Update(ctx, adminA, adminB, domain.UserChange{Role: ptrRole(domain.RoleAdmin)}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i, id := range []int64{adminA, adminB} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = svc.Update(ctx, id, id, domain.UserChange{Role: &user})
		}()
	}
	wg.Wait()
	failures := 0
	for _, e := range results {
		if errors.Is(e, domain.ErrLastAdmin) {
			failures++
		} else if e != nil {
			t.Fatal(e)
		}
	}
	var active int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin' AND disabled_at IS NULL`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if failures != 1 || active != 1 {
		t.Fatalf("concurrent demotion: failures=%d active admins=%d", failures, active)
	}
}

func ptrRole(r domain.Role) *domain.Role { return &r }
