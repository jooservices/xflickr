package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/joho/godotenv"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	"github.com/jooservices/xflickr/internal/identity/adapters/postgres"
	identityredis "github.com/jooservices/xflickr/internal/identity/adapters/redis"
	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
	redisv9 "github.com/redis/go-redis/v9"
)

type integrationRegistrationPolicy struct{}

func (integrationRegistrationPolicy) RegistrationOpen(context.Context) (bool, error) {
	return true, nil
}

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
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email=$1`, email)
	})
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	register := app.Register{Repository: repository, Policy: integrationRegistrationPolicy{}, Params: params}
	user, err := register.Execute(ctx, email, "integration-password", "")
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
	values, err := godotenv.Read(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal("read shared worktree environment")
	}
	redisPort, _ := strconv.Atoi(values["REDIS_PORT"])
	if redisPort == 0 {
		redisPort = 16382
	}
	redisDB, _ := strconv.Atoi(values["REDIS_DB"])
	redisHost := values["REDIS_HOST"]
	if redisHost == "" {
		redisHost = "127.0.0.1"
	}
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(redisHost, strconv.Itoa(redisPort)), Password: values["REDIS_PASSWORD"], DB: redisDB})
	defer redisClient.Close()
	login := app.Login{Repository: repository, Limiter: identityredis.Limiter{Client: redisClient, Prefix: values["REDIS_PREFIX"]}, DummyPasswordHash: user.PasswordHash}
	loggedIn, err := login.Execute(ctx, email, "integration-password", "127.0.0.1")
	if err != nil || loggedIn.ID != user.ID {
		t.Fatalf("Login.Execute() = %#v, %v", loggedIn, err)
	}
	handler := identityhttp.Handler{Users: repository}
	me, err := handler.GetMe(identityhttp.WithUserID(ctx, loggedIn.ID), gen.GetMeRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := me.(gen.GetMe200JSONResponse); !ok {
		t.Fatalf("GET /me response = %T", me)
	}
	if _, err := repository.Register(ctx, domain.User{Email: email, PasswordHash: "duplicate", Locale: "en"}, true); !errors.Is(err, app.ErrEmailExists) {
		t.Fatalf("duplicate Register() error = %v", err)
	}
	if _, err := repository.FindByEmail(ctx, email+"-missing"); err == nil {
		t.Fatal("FindByEmail() succeeded for missing user")
	}
	if _, err := repository.FindByID(ctx, user.ID+999999); err == nil {
		t.Fatal("FindByID() succeeded for missing user")
	}
	if _, err := repository.Register(ctx, domain.User{Email: email + "-closed", PasswordHash: "hash", Locale: "en"}, false); !errors.Is(err, app.ErrRegistrationClosed) {
		t.Fatalf("closed registration error = %v", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := repository.Register(canceled, domain.User{}, false); err == nil {
		t.Fatal("Register() succeeded with canceled context")
	}
	if _, err := repository.FindByEmail(canceled, email); err == nil {
		t.Fatal("FindByEmail() succeeded with canceled context")
	}
	if _, err := repository.FindByID(canceled, user.ID); err == nil {
		t.Fatal("FindByID() succeeded with canceled context")
	}
}
