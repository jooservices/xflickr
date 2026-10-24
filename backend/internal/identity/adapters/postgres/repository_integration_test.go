package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/identity/adapters/postgres"
	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
)

func TestRepositoryRegistrationAndLookup(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
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
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	email := fmt.Sprintf("jooxf93-%d@example.invalid", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `UPDATE auth_settings SET value=true WHERE key='registration_open'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email=$1`, email)
		_, _ = pool.Exec(context.Background(), `UPDATE auth_settings SET value=false WHERE key='registration_open'`)
	})
	user, err := repository.Register(ctx, domain.User{Email: email, PasswordHash: "$argon2id$test", Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	wantRole := domain.RoleUser
	if total == 0 {
		wantRole = domain.RoleAdmin
	}
	if user.Role != wantRole {
		t.Fatalf("first-user role = %q; want %q", user.Role, wantRole)
	}
	byEmail, err := repository.FindByEmail(ctx, email)
	if err != nil || byEmail.ID != user.ID || byEmail.PasswordHash != user.PasswordHash {
		t.Fatalf("FindByEmail() = %#v, %v", byEmail, err)
	}
	byID, err := repository.FindByID(ctx, user.ID)
	if err != nil || byID.Email != email {
		t.Fatalf("FindByID() = %#v, %v", byID, err)
	}
	if _, err := repository.Register(ctx, domain.User{Email: email, PasswordHash: "duplicate", Locale: "en"}); !errors.Is(err, app.ErrEmailExists) {
		t.Fatalf("duplicate Register() error = %v", err)
	}
	if _, err := repository.FindByEmail(ctx, email+"-missing"); err == nil {
		t.Fatal("FindByEmail() succeeded for missing user")
	}
	if _, err := repository.FindByID(ctx, user.ID+999999); err == nil {
		t.Fatal("FindByID() succeeded for missing user")
	}
	if _, err := pool.Exec(ctx, `UPDATE auth_settings SET value=false WHERE key='registration_open'`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Register(ctx, domain.User{Email: email + "-closed", PasswordHash: "hash", Locale: "en"}); !errors.Is(err, app.ErrRegistrationClosed) {
		t.Fatalf("closed registration error = %v", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := repository.Register(canceled, domain.User{}); err == nil {
		t.Fatal("Register() succeeded with canceled context")
	}
	if _, err := repository.FindByEmail(canceled, email); err == nil {
		t.Fatal("FindByEmail() succeeded with canceled context")
	}
	if _, err := repository.FindByID(canceled, user.ID); err == nil {
		t.Fatal("FindByID() succeeded with canceled context")
	}
}
