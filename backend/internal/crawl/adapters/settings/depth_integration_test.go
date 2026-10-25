package settings_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/crawl/adapters/settings"
	"github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
)

const integrationUser = int64(860101)

func TestDepthAgainstTheWorktreeDatabase(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use this worktree's configured PostgreSQL database")
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source file")
	}
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../")), ".env"))
	if err != nil {
		t.Fatalf("load test PostgreSQL settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	pool := dbtest.Open(t, dbConfig)
	clean := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id = $1`, integrationUser); err != nil {
			t.Errorf("delete user settings: %v", err)
		}
	}
	clean()
	t.Cleanup(clean)

	repository, err := settingspostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	service := app.Depth{Store: settings.Depth{Settings: settingsapp.New(repository)}}

	policy, err := service.Get(t.Context(), integrationUser)
	if err != nil || policy.Depth != policy.Default || policy.Max < policy.Default {
		t.Fatalf("Get() = %+v, %v", policy, err)
	}
	changed, err := service.Set(t.Context(), integrationUser, 1)
	if err != nil || changed.Depth != 1 || changed.Default != policy.Default {
		t.Fatalf("Set(1) = %+v, %v", changed, err)
	}
	if _, err := service.Set(t.Context(), integrationUser, policy.Max+1); !errors.Is(err, app.ErrDepthAboveCap) {
		t.Fatalf("Set(above cap) error = %v", err)
	}
}
