package roles

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBuildHomeHandlerNeedsNoConnectionUntilRequest(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://localhost:1/xflickr")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	handler, err := buildHomeHandler(pool)
	if err != nil {
		t.Fatal(err)
	}
	if handler.Snapshots == nil || handler.ActorID == nil {
		t.Fatal("Home handler did not receive its service and actor resolver")
	}
}
