package roles

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/platform/config"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
)

// buildTestAPIHandler builds the public API handler with a fresh event hub.
func buildTestAPIHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, version string) (http.Handler, error) {
	handlers, err := buildAPIHandlerSet(apiBuildOptions{Pool: pool, Redis: redisClient, Config: cfg, Version: version, Hub: platformsse.NewHub(256)})
	return handlers.public, err
}

// newTestAPIRouter mounts the public routes for the handlers a test sets.
func newTestAPIRouter(opts apiRouterOptions) http.Handler {
	return newAPIRouterSet(opts).public
}
