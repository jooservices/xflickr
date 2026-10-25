package roles

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	redisv9 "github.com/redis/go-redis/v9"

	accountsflickr "github.com/jooservices/xflickr/internal/accounts/adapters/flickr"
	accountshttp "github.com/jooservices/xflickr/internal/accounts/adapters/http"
	accountspostgres "github.com/jooservices/xflickr/internal/accounts/adapters/postgres"
	accountsredis "github.com/jooservices/xflickr/internal/accounts/adapters/redis"
	accountsettings "github.com/jooservices/xflickr/internal/accounts/adapters/settings"
	accountsapp "github.com/jooservices/xflickr/internal/accounts/app"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	identitypostgres "github.com/jooservices/xflickr/internal/identity/adapters/postgres"
	identityredis "github.com/jooservices/xflickr/internal/identity/adapters/redis"
	identityapp "github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	auditpostgres "github.com/jooservices/xflickr/internal/platform/audit/adapters/postgres"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/httpx"
	schedulehttp "github.com/jooservices/xflickr/internal/schedule/adapters/http"
	schedulepostgres "github.com/jooservices/xflickr/internal/schedule/adapters/postgres"
	scheduleapp "github.com/jooservices/xflickr/internal/schedule/app"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
)

const apiAddress = ":8080"

var builtinRoleNames = []string{"api", "scheduler", "indexer", "node-agent", "crawl", "media", "sync", "ai-worker"}

// NewBuiltinRegistry creates the role set supported by the xflickr binary.
func NewBuiltinRegistry(version string) *Registry {
	registry := NewRegistry()
	registerBuiltin(registry, newConfiguredAPIRole(apiAddress, version))
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
	address         string
	version         string
	handler         http.Handler
	listenerFactory func(string) (net.Listener, error)
}

// NewAPIRole creates an API role bound to address.
func NewAPIRole(address, version string) APIRole {
	return APIRole{address: address, version: version, handler: httpx.NewHealthHandler(version)}
}

func newConfiguredAPIRole(address, version string) APIRole {
	role := NewAPIRole(address, version)
	role.handler = nil
	return role
}

func (APIRole) Name() string { return "api" }

func (r APIRole) Run(ctx context.Context) error {
	if r.handler != nil {
		return r.runServer(ctx, r.handler)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	redisClient := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port)), Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	defer redisClient.Close()
	handler, err := buildConfiguredAPIHandler(pool, redisClient, cfg, r.version)
	if err != nil {
		return err
	}
	return r.runServer(ctx, handler)
}

func buildConfiguredAPIHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, version string) (http.Handler, error) {
	if pool == nil {
		return nil, errors.New("api: postgres database is required")
	}
	identity, err := buildIdentityHandler(pool, redisClient, cfg, version)
	if err != nil {
		return nil, err
	}
	keys, err := buildKeyHandler(pool, cfg)
	if err != nil {
		return nil, err
	}
	connect, err := buildConnectHandler(pool, redisClient, cfg)
	if err != nil {
		return nil, err
	}
	plans, err := buildPlanHandler(pool)
	if err != nil {
		return nil, err
	}
	return newAPIRouter(identity, keys, connect, plans), nil
}

// apiServer composes identity and API key handlers. The two types expose
// different methods, so both promote onto the generated server interface.
type apiServer struct {
	identityhttp.Handler
	accountshttp.KeyHandler
	accountshttp.ConnectHandler
	planHandler
}

type planHandler struct{ schedulehttp.Handler }

var _ gen.StrictServerInterface = apiServer{}

func newAPIRouter(identity identityhttp.Handler, keys accountshttp.KeyHandler, connect accountshttp.ConnectHandler, plans schedulehttp.Handler) http.Handler {
	router := chi.NewRouter()
	router.Use(identityhttp.RequestBodyLimit)
	router.Use(identityhttp.CSRF)
	router.Use(identityhttp.ProtectRoutes(identity.Sessions))
	return gen.HandlerFromMux(gen.NewStrictHandler(apiServer{Handler: identity, KeyHandler: keys, ConnectHandler: connect, planHandler: planHandler{Handler: plans}}, []gen.StrictMiddlewareFunc{
		identityhttp.Middleware,
		identityhttp.MaintainSessionCookies(identity.Sessions),
	}), router)
}

func buildKeyHandler(pool *pgxpool.Pool, cfg config.Config) (accountshttp.KeyHandler, error) {
	if pool == nil {
		return accountshttp.KeyHandler{}, errors.New("accounts: postgres database is required")
	}
	ring, err := keyringFromConfig(cfg)
	if err != nil {
		return accountshttp.KeyHandler{}, err
	}
	repository, err := accountspostgres.New(pool)
	if err != nil {
		return accountshttp.KeyHandler{}, err
	}
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return accountshttp.KeyHandler{}, err
	}
	return accountshttp.KeyHandler{
		Keys: accountsapp.Keys{
			Repository: repository,
			Probe:      accountsflickr.NewProbe("", nil),
			Sealer:     ring,
			Settings:   accountsettings.Reader{Settings: settingsapp.New(settingsRepository)},
		},
		Principal: userIDFromPrincipal,
	}, nil
}

func buildConnectHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config) (accountshttp.ConnectHandler, error) {
	if pool == nil {
		return accountshttp.ConnectHandler{}, errors.New("accounts: postgres database is required")
	}
	ring, err := keyringFromConfig(cfg)
	if err != nil {
		return accountshttp.ConnectHandler{}, err
	}
	repository, err := accountspostgres.New(pool)
	if err != nil {
		return accountshttp.ConnectHandler{}, err
	}
	return accountshttp.ConnectHandler{
		Accounts: accountsapp.Connect{
			Repository:  repository,
			Authorizer:  accountsflickr.NewAuthorizer(nil, accountsflickr.Endpoints{}),
			States:      accountsredis.States{Client: redisClient, Prefix: cfg.Redis.Prefix},
			Cipher:      ring,
			CallbackURL: cfg.Flickr.CallbackURL,
		},
		Principal: userIDFromPrincipal,
	}, nil
}

func buildPlanHandler(pool *pgxpool.Pool) (schedulehttp.Handler, error) {
	if pool == nil {
		return schedulehttp.Handler{}, errors.New("schedule: postgres database is required")
	}
	store := schedulepostgres.New(pool)
	return schedulehttp.Handler{
		Plans: &scheduleapp.Plans{Store: store},
		ActorID: func(ctx context.Context) (int64, bool) {
			principal, ok := identityhttp.PrincipalFrom(ctx)
			if !ok || principal.User.ID <= 0 {
				return 0, false
			}
			return principal.User.ID, true
		},
	}, nil
}

func keyringFromConfig(cfg config.Config) (*crypto.Keyring, error) {
	if strings.TrimSpace(cfg.Crypto.MasterKeys) == "" || strings.TrimSpace(cfg.Crypto.ActiveMasterKeyID) == "" {
		return nil, errors.New("accounts: master key configuration is required")
	}
	keys, err := crypto.ParseMasterKeys(cfg.Crypto.MasterKeys)
	if err != nil {
		return nil, err
	}
	return crypto.NewKeyring(keys, strings.TrimSpace(cfg.Crypto.ActiveMasterKeyID))
}

func userIDFromPrincipal(ctx context.Context) (int64, bool) {
	principal, ok := identityhttp.PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 {
		return 0, false
	}
	return principal.User.ID, true
}

func buildIdentityHandler(pool *pgxpool.Pool, redisClient redisv9.UniversalClient, cfg config.Config, version string) (identityhttp.Handler, error) {
	users := identitypostgres.New(pool)
	params := domain.Argon2Params{Memory: cfg.Identity.Argon2Memory, Iterations: cfg.Identity.Argon2Iterations, Parallelism: cfg.Identity.Argon2Parallelism, SaltLength: cfg.Identity.Argon2SaltLength, KeyLength: cfg.Identity.Argon2KeyLength}
	dummyHash, err := domain.HashPassword("xflickr-invalid-login-dummy", params)
	if err != nil {
		return identityhttp.Handler{}, err
	}
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		return identityhttp.Handler{}, err
	}
	settings := settingsapp.New(settingsRepository)
	return identityhttp.Handler{
		RegisterUseCase: identityapp.Register{Repository: users, Policy: settings, Params: params},
		LoginUseCase:    identityapp.Login{Repository: users, Limiter: identityredis.Limiter{Client: redisClient, Prefix: cfg.Redis.Prefix}, DummyPasswordHash: dummyHash},
		Users:           users,
		Profile:         identityapp.UpdateProfile{Repository: users},
		Password:        identityapp.ChangePassword{Repository: users, Sessions: users, Limiter: identityredis.Limiter{Client: redisClient, Prefix: cfg.Redis.Prefix}, Params: params},
		Sessions:        identityapp.Sessions{Repository: users, Users: users},
		AdminUsers:      identityapp.AdminUsers{Store: users, Audit: auditpostgres.New(pool)},
		Version:         version,
	}, nil
}

func (r APIRole) runServer(ctx context.Context, handler http.Handler) error {
	listen := r.listenerFactory
	if listen == nil {
		listen = func(address string) (net.Listener, error) { return net.Listen("tcp", address) }
	}
	listener, err := listen(r.address)
	if err != nil {
		return err
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
