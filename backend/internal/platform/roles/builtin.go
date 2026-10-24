package roles

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/jooservices/xflickr/internal/platform/httpx"
)

const apiAddress = ":8080"

var builtinRoleNames = []string{"api", "scheduler", "indexer", "node-agent", "crawl", "media", "sync", "ai-worker"}

// NewBuiltinRegistry creates the role set supported by the xflickr binary.
func NewBuiltinRegistry(version string) *Registry {
	registry := NewRegistry()
	registerBuiltin(registry, NewAPIRole(apiAddress, version))
	for _, name := range builtinRoleNames[1:] {
		registerBuiltin(registry, idleRole{name: name})
	}
	return registry
}

func registerBuiltin(registry *Registry, role Role) {
	if err := registry.Register(role); err != nil {
		panic(err)
	}
}

type idleRole struct {
	name string
}

func (r idleRole) Name() string { return r.name }

func (r idleRole) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// APIRole serves the health endpoint for the API process role.
type APIRole struct {
	address string
	version string
}

// NewAPIRole creates an API role bound to address.
func NewAPIRole(address, version string) APIRole {
	return APIRole{address: address, version: version}
}

func (APIRole) Name() string { return "api" }

func (r APIRole) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", r.address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: httpx.NewHealthHandler(r.version)}
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
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
