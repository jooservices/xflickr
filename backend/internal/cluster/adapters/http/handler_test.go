package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/cluster/app"
	"github.com/jooservices/xflickr/internal/cluster/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

func TestCreateAdminNodeReturnsGeneratedTokenOnce(t *testing.T) {
	token := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	service := &serviceFake{node: domain.Node{Name: "node-1", Roles: []string{"crawl"}, State: domain.StateOffline}, token: token}
	handler := Handler{Service: service, Now: func() time.Time { return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) }}
	response, err := handler.CreateAdminNode(t.Context(), gen.CreateAdminNodeRequestObject{Body: &gen.CreateNodeRequest{Name: "node-1", Roles: []string{"crawl"}}})
	if err != nil {
		t.Fatal(err)
	}
	created, ok := response.(gen.CreateAdminNode201JSONResponse)
	if !ok || created.Token != token || created.Name != "node-1" || string(created.State) != "offline" {
		t.Fatalf("create response = %#v", response)
	}
	if service.createdName != "node-1" || len(service.createdRoles) != 1 {
		t.Fatalf("create input = %q %v", service.createdName, service.createdRoles)
	}
}

func TestCreateAdminNodeMapsValidationAndDuplicate(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid", err: app.ErrInvalidNode, want: 400},
		{name: "duplicate", err: app.ErrNodeExists, want: 409},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := Handler{Service: &serviceFake{err: test.err}}
			response, err := handler.CreateAdminNode(t.Context(), gen.CreateAdminNodeRequestObject{Body: &gen.CreateNodeRequest{Name: "node-1", Roles: []string{"crawl"}}})
			if err != nil {
				t.Fatal(err)
			}
			switch test.want {
			case 400:
				if _, ok := response.(gen.CreateAdminNode400JSONResponse); !ok {
					t.Fatalf("response = %#v", response)
				}
			case 409:
				if _, ok := response.(gen.CreateAdminNode409JSONResponse); !ok {
					t.Fatalf("response = %#v", response)
				}
			}
		})
	}
}

func TestHeartbeatRequiresAuthenticatedNodeAndEmitsSafeEvent(t *testing.T) {
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	var logs bytes.Buffer
	service := &serviceFake{node: domain.Node{Name: "node-1", State: domain.StateOnline, Version: "v1", LastHeartbeatAt: &at}}
	handler := Handler{Service: service, Logger: slog.New(slog.NewTextHandler(&logs, nil)), Now: func() time.Time { return at }}
	request := gen.HeartbeatNodeRequestObject{Body: &gen.NodeHeartbeatRequest{Version: stringPointer("v1"), EgressIP: stringPointer("192.0.2.5"), Metrics: &map[string]interface{}{"load": 0.25}}}
	ctx := context.WithValue(t.Context(), nodeContextKey{}, domain.Node{Name: "node-1"})
	if _, err := handler.HeartbeatNode(ctx, request); err != nil {
		t.Fatal(err)
	}
	if service.heartbeatName != "node-1" || service.heartbeatInput.EgressIP != "192.0.2.5" || !strings.Contains(string(service.heartbeatInput.Metrics), "load") {
		t.Fatalf("heartbeat input = %+v for %q", service.heartbeatInput, service.heartbeatName)
	}
	if !strings.Contains(logs.String(), "node.heartbeat") || !strings.Contains(logs.String(), "node-1") || strings.Contains(logs.String(), "token") || strings.Contains(logs.String(), "192.0.2.5") {
		t.Fatalf("heartbeat event leaked or was missing: %s", logs.String())
	}
	if _, err := handler.HeartbeatNode(ctx, request); err != nil {
		t.Fatal(err)
	}
}

func TestHeartbeatMapsInvalidInputAndRepositoryErrors(t *testing.T) {
	ctx := context.WithValue(t.Context(), nodeContextKey{}, domain.Node{Name: "node-1"})
	for _, test := range []struct {
		name string
		err  error
		want any
	}{
		{name: "invalid input", err: app.ErrInvalidNode, want: gen.HeartbeatNode400JSONResponse{}},
		{name: "disabled node", err: app.ErrInvalidToken, want: gen.HeartbeatNode401JSONResponse{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := Handler{Service: &serviceFake{err: test.err}}
			response, err := handler.HeartbeatNode(ctx, gen.HeartbeatNodeRequestObject{Body: &gen.NodeHeartbeatRequest{}})
			if err != nil {
				t.Fatal(err)
			}
			switch test.want.(type) {
			case gen.HeartbeatNode400JSONResponse:
				if _, ok := response.(gen.HeartbeatNode400JSONResponse); !ok {
					t.Fatalf("response = %#v", response)
				}
			case gen.HeartbeatNode401JSONResponse:
				if _, ok := response.(gen.HeartbeatNode401JSONResponse); !ok {
					t.Fatalf("response = %#v", response)
				}
			}
		})
	}
	service := &serviceFake{err: errors.New("storage down")}
	if _, err := (Handler{Service: service}).HeartbeatNode(ctx, gen.HeartbeatNodeRequestObject{Body: &gen.NodeHeartbeatRequest{}}); err == nil {
		t.Fatal("storage failure was hidden")
	}
}

func TestCreateAdminNodeMissingDependenciesAndStorageError(t *testing.T) {
	request := gen.CreateAdminNodeRequestObject{Body: &gen.CreateNodeRequest{Name: "node-1", Roles: []string{"crawl"}}}
	if response, err := (Handler{}).CreateAdminNode(t.Context(), request); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.CreateAdminNode400JSONResponse); !ok {
		t.Fatalf("missing service response = %#v", response)
	}
	if response, err := (Handler{Service: &serviceFake{}}).CreateAdminNode(t.Context(), gen.CreateAdminNodeRequestObject{}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.CreateAdminNode400JSONResponse); !ok {
		t.Fatalf("missing body response = %#v", response)
	}
	if _, err := (Handler{Service: &serviceFake{err: errors.New("storage down")}}).CreateAdminNode(t.Context(), request); err == nil {
		t.Fatal("storage failure was hidden")
	}
}

func TestHeartbeatNodeMissingContextAndInvalidResponse(t *testing.T) {
	request := gen.HeartbeatNodeRequestObject{Body: &gen.NodeHeartbeatRequest{}}
	if response, err := (Handler{Service: &serviceFake{}}).HeartbeatNode(t.Context(), request); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.HeartbeatNode401JSONResponse); !ok {
		t.Fatalf("missing node context response = %#v", response)
	}
	ctx := context.WithValue(t.Context(), nodeContextKey{}, domain.Node{Name: "node-1"})
	if response, err := (Handler{}).HeartbeatNode(ctx, request); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.HeartbeatNode400JSONResponse); !ok {
		t.Fatalf("missing service response = %#v", response)
	}
	unsupported := map[string]interface{}{"channel": make(chan int)}
	badMetrics := gen.HeartbeatNodeRequestObject{Body: &gen.NodeHeartbeatRequest{Metrics: &unsupported}}
	if response, err := (Handler{Service: &serviceFake{}}).HeartbeatNode(ctx, badMetrics); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.HeartbeatNode400JSONResponse); !ok {
		t.Fatalf("unsupported metrics response = %#v", response)
	}
	if _, err := (Handler{Service: &serviceFake{node: domain.Node{Name: "node-1"}}}).HeartbeatNode(ctx, request); err == nil {
		t.Fatal("missing heartbeat timestamp was accepted")
	}
}

func TestHandlerNowUsesUTCWhenNoClockIsInjected(t *testing.T) {
	value := (Handler{}).now()
	if value.IsZero() || value.Location() != time.UTC {
		t.Fatalf("default clock = %v (%v)", value, value.Location())
	}
}

func TestNodeAuthMiddlewareMapsServiceFailures(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unauthorized request reached handler") })
	tokenRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/internal/nodes/heartbeat", nil)
		request.Header.Set("Authorization", "Bearer AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		return request
	}
	for _, test := range []struct {
		name    string
		service Service
		want    int
	}{
		{name: "missing service", want: http.StatusInternalServerError},
		{name: "invalid token", service: &serviceFake{err: app.ErrInvalidToken}, want: http.StatusUnauthorized},
		{name: "repository failure", service: &serviceFake{err: errors.New("storage down")}, want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := httptest.NewRecorder()
			NodeAuthMiddleware(test.service)(next).ServeHTTP(record, tokenRequest())
			if record.Code != test.want || strings.Contains(record.Body.String(), "storage down") {
				t.Fatalf("response = %d %s", record.Code, record.Body.String())
			}
		})
	}
}

func TestNodeAuthMiddlewareNeverEchoesCredential(t *testing.T) {
	service := &serviceFake{node: domain.Node{Name: "node-1"}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node, ok := NodeFromContext(r.Context())
		if !ok || node.Name != "node-1" {
			t.Fatalf("node context = %+v, %v", node, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	middleware := NodeAuthMiddleware(service)
	for _, header := range []string{"", "Basic secret", "Bearer", "Bearer token with spaces"} {
		record := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/internal/nodes/heartbeat", nil)
		request.Header.Set("Authorization", header)
		middleware(next).ServeHTTP(record, request)
		if record.Code != http.StatusUnauthorized || strings.Contains(record.Body.String(), "secret") || strings.Contains(record.Body.String(), "token with") {
			t.Errorf("header %q response = %d %s", header, record.Code, record.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/nodes/heartbeat", nil)
	request.Header.Set("Authorization", "Bearer AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	record := httptest.NewRecorder()
	middleware(next).ServeHTTP(record, request)
	if record.Code != http.StatusNoContent {
		t.Fatalf("valid token status = %d body=%s", record.Code, record.Body.String())
	}
}

func TestInternalListenerMiddlewareOnlyAllowsHeartbeat(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, test := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/internal/nodes/heartbeat", 404},
		{http.MethodPost, "/internal/other", 404},
		{http.MethodPost, "/internal/nodes/heartbeat", 204},
	} {
		record := httptest.NewRecorder()
		InternalHeartbeatOnly(next).ServeHTTP(record, httptest.NewRequest(test.method, test.path, nil))
		if record.Code != test.want {
			t.Errorf("%s %s = %d, want %d", test.method, test.path, record.Code, test.want)
		}
	}
	public := PublicInternalPathBlocker(next)
	record := httptest.NewRecorder()
	public.ServeHTTP(record, httptest.NewRequest(http.MethodPost, "/internal/nodes/heartbeat", nil))
	if record.Code != http.StatusNotFound {
		t.Fatalf("public internal route status = %d", record.Code)
	}
	record = httptest.NewRecorder()
	public.ServeHTTP(record, httptest.NewRequest(http.MethodPost, "/internal", nil))
	if record.Code != http.StatusNotFound {
		t.Fatalf("public internal root status = %d", record.Code)
	}
}

type serviceFake struct {
	node           domain.Node
	token          string
	err            error
	createdName    string
	createdRoles   []string
	heartbeatName  string
	heartbeatInput app.HeartbeatInput
}

func (service *serviceFake) Create(_ context.Context, name string, roles []string) (domain.Node, string, error) {
	service.createdName, service.createdRoles = name, roles
	return service.node, service.token, service.err
}

func (service *serviceFake) AuthenticateToken(_ context.Context, _ string) (domain.Node, error) {
	return service.node, service.err
}

func (service *serviceFake) Heartbeat(_ context.Context, name string, input app.HeartbeatInput) (domain.Node, error) {
	service.heartbeatName, service.heartbeatInput = name, input
	return service.node, service.err
}
func stringPointer(value string) *string { return &value }

func TestHeartbeatBodyJSONMapping(t *testing.T) {
	body, err := json.Marshal(gen.NodeHeartbeatRequest{Version: stringPointer("test")})
	if err != nil || !strings.Contains(string(body), "version") {
		t.Fatalf("marshal heartbeat body = %s, %v", body, err)
	}
}
