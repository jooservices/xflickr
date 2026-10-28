package roles

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	"github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/platform/httpx"
)

type readinessContextKey struct{}

func withReadiness(ctx context.Context, readiness *httpx.Readiness) context.Context {
	return context.WithValue(ctx, readinessContextKey{}, readiness)
}

func readinessFromContext(ctx context.Context) *httpx.Readiness {
	if ctx == nil {
		return nil
	}
	readiness, _ := ctx.Value(readinessContextKey{}).(*httpx.Readiness)
	return readiness
}

func configureRoleReadiness(ctx context.Context, role string, checks ...httpx.DependencyCheck) {
	if readiness := readinessFromContext(ctx); readiness != nil {
		readiness.ConfigureRole(role, checks...)
	}
}

func postgresDependency(pool *pgxpool.Pool) httpx.DependencyCheck {
	return httpx.DependencyCheck{
		Name: "postgres",
		Check: func(ctx context.Context) error {
			if pool == nil {
				return errors.New("postgres pool is unavailable")
			}
			return pool.Ping(ctx)
		},
	}
}

func redisDependency(client redisv9.UniversalClient) httpx.DependencyCheck {
	return httpx.DependencyCheck{
		Name: "redis",
		Check: func(ctx context.Context) error {
			if client == nil {
				return errors.New("redis client is unavailable")
			}
			return client.Ping(ctx).Err()
		},
	}
}

func blobStoreDependency(store app.BlobStore) httpx.DependencyCheck {
	return httpx.DependencyCheck{
		Name: "blob_store",
		Check: func(ctx context.Context) error {
			if store == nil {
				return errors.New("blob store is unavailable")
			}
			_, err := store.Stat(ctx, "__xflickr_health_probe__")
			if errors.Is(err, app.ErrNotFound) {
				return nil
			}
			return err
		},
	}
}

func healthProbeAddress() (string, error) {
	rawPort := strings.TrimSpace(os.Getenv("OBS_HTTP_PORT"))
	if rawPort == "" {
		rawPort = "8081"
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("OBS_HTTP_PORT must be between 1 and 65535")
	}
	return net.JoinHostPort("", strconv.Itoa(port)), nil
}

// RunWithHealth starts one probe listener for all selected roles in this process.
func RunWithHealth(ctx context.Context, selected []Role, version string) error {
	if ctx == nil {
		return errors.New("health probes require a context")
	}
	if err := validateSelectedRoles(selected); err != nil {
		return err
	}
	address, err := healthProbeAddress()
	if err != nil {
		return err
	}
	for _, role := range selected {
		if role.Name() == "api" && address == apiAddress() {
			return errors.New("OBS_HTTP_PORT must differ from HTTP_PORT when the api role is selected")
		}
	}

	roleNames := make([]string, 0, len(selected))
	for _, role := range selected {
		roleNames = append(roleNames, role.Name())
	}
	readiness := httpx.NewReadiness(roleNames...)
	group, groupCtx := errgroup.WithContext(ctx)
	roleCtx := withReadiness(groupCtx, readiness)

	group.Go(func() error {
		return serveProbeServer(groupCtx, address, httpx.NewProbeHandler(version, readiness))
	})
	for _, role := range selected {
		role := role
		group.Go(func() error {
			err := role.Run(roleCtx)
			if ctx.Err() != nil || groupCtx.Err() != nil {
				return err
			}
			readiness.MarkStopped(role.Name())
			if err == nil {
				return fmt.Errorf("role %s stopped unexpectedly", role.Name())
			}
			return err
		})
	}
	return group.Wait()
}

func serveProbeServer(ctx context.Context, address string, handler http.Handler) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen for health probes: %w", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
