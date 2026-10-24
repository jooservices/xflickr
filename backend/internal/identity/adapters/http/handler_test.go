package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

type repo struct {
	user domain.User
	err  error
}

func (r repo) Register(_ context.Context, u domain.User) (domain.User, error) {
	if r.err != nil {
		return domain.User{}, r.err
	}
	u.ID = 1
	u.Role = domain.RoleAdmin
	u.CreatedAt = time.Now()
	return u, nil
}
func (r repo) FindByEmail(context.Context, string) (domain.User, error) { return r.user, r.err }
func (r repo) FindByID(context.Context, int64) (domain.User, error)     { return r.user, r.err }

type rateLimit struct {
	allowed bool
	err     error
}

func (r rateLimit) Allow(context.Context, string, string) (bool, error) { return r.allowed, r.err }

func TestRegisterResponses(t *testing.T) {
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	good := Handler{RegisterUseCase: app.Register{Repository: repo{}, Params: params}}
	email := openapi_types.Email("user@example.com")
	password := "secret"
	for _, tc := range []struct {
		name string
		body *gen.RegisterJSONRequestBody
		want any
	}{{"created", &gen.AuthRequest{Email: email, Password: &password}, gen.Register201JSONResponse{}}, {"missing body", nil, gen.Register400Response{}}, {"missing password", &gen.AuthRequest{Email: email}, gen.Register400Response{}}, {"closed", &gen.AuthRequest{Email: email, Password: &password}, gen.Register409Response{}}} {
		h := good
		if tc.name == "closed" {
			h.RegisterUseCase.Repository = repo{err: app.ErrRegistrationClosed}
		}
		got, err := h.Register(context.Background(), gen.RegisterRequestObject{Body: tc.body})
		if err != nil {
			t.Fatal(err)
		}
		switch tc.want.(type) {
		case gen.Register201JSONResponse:
			if _, ok := got.(gen.Register201JSONResponse); !ok {
				t.Fatalf("%s response %T", tc.name, got)
			}
		case gen.Register400Response:
			if _, ok := got.(gen.Register400Response); !ok {
				t.Fatalf("%s response %T", tc.name, got)
			}
		case gen.Register409Response:
			if _, ok := got.(gen.Register409Response); !ok {
				t.Fatalf("%s response %T", tc.name, got)
			}
		}
	}
}

func TestLoginResponses(t *testing.T) {
	params := domain.Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, _ := domain.HashPassword("secret", params)
	password := "secret"
	request := gen.LoginRequestObject{Body: &gen.AuthRequest{Email: openapi_types.Email("u@example.com"), Password: &password}}
	for _, tc := range []struct {
		name    string
		repo    repo
		limiter rateLimit
		body    *gen.LoginJSONRequestBody
		want    string
	}{{"success", repo{user: domain.User{ID: 1, PasswordHash: hash, CreatedAt: time.Now()}}, rateLimit{allowed: true}, request.Body, "ok"}, {"bad password", repo{user: domain.User{PasswordHash: hash}}, rateLimit{allowed: true}, func() *gen.AuthRequest { p := "bad"; return &gen.AuthRequest{Password: &p} }(), "unauthorized"}, {"missing body", repo{}, rateLimit{}, nil, "unauthorized"}, {"limited", repo{}, rateLimit{allowed: false}, request.Body, "limited"}, {"limiter error", repo{}, rateLimit{err: errors.New("redis")}, request.Body, "unauthorized"}} {
		h := Handler{LoginUseCase: app.Login{Repository: tc.repo, Limiter: tc.limiter}}
		req := request
		req.Body = tc.body
		ctx := WithRemoteAddress(context.Background(), "127.0.0.1:1234")
		got, err := h.Login(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		switch tc.want {
		case "ok":
			if _, ok := got.(gen.Login200JSONResponse); !ok {
				t.Fatalf("%s response %T", tc.name, got)
			}
		case "limited":
			if _, ok := got.(gen.Login429Response); !ok {
				t.Fatalf("%s response %T", tc.name, got)
			}
		default:
			if _, ok := got.(gen.Login401Response); !ok {
				t.Fatalf("%s response %T", tc.name, got)
			}
		}
	}
}

func TestLogoutGetMeHealthAndMiddleware(t *testing.T) {
	h := Handler{Users: repo{user: domain.User{ID: 7, Role: domain.RoleUser, CreatedAt: time.Now()}}, Version: "test"}
	if _, err := h.Logout(context.Background(), gen.LogoutRequestObject{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.GetMe(context.Background(), gen.GetMeRequestObject{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.GetMe(WithUserID(context.Background(), 7), gen.GetMeRequestObject{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.GetMe(WithUserID(context.Background(), 7), gen.GetMeRequestObject{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.GetHealth(context.Background(), gen.GetHealthRequestObject{}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/", nil)
	wrapped := Middleware(func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		if requestIP(ctx) == "" {
			t.Fatal("remote address missing")
		}
		return nil, nil
	}, "test")
	_, _ = wrapped(context.Background(), httptest.NewRecorder(), request, nil)
}
