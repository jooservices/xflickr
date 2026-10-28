package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadinessAggregatesRoleDependenciesWithoutLeakingErrors(t *testing.T) {
	readiness := NewReadiness("api", "scheduler")
	readiness.ConfigureRole("scheduler", DependencyCheck{
		Name:  "redis",
		Check: func(context.Context) error { return nil },
	})
	readiness.ConfigureRole("api",
		DependencyCheck{
			Name:  "postgres",
			Check: func(context.Context) error { return errors.New("secret connection detail") },
		},
		DependencyCheck{
			Name:  "redis",
			Check: func(context.Context) error { return nil },
		},
	)

	result, ready := readiness.Evaluate(context.Background())
	if ready || result.Status != "error" {
		t.Fatalf("readiness = %+v, ready=%v; want error", result, ready)
	}
	if len(result.Dependencies) != 3 {
		t.Fatalf("dependencies = %+v; want three", result.Dependencies)
	}
	want := []DependencyStatus{
		{Name: "api.postgres", Status: "error"},
		{Name: "api.redis", Status: "ok"},
		{Name: "scheduler.redis", Status: "ok"},
	}
	for index := range want {
		if result.Dependencies[index] != want[index] {
			t.Fatalf("dependencies = %+v, want %+v", result.Dependencies, want)
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret connection detail") {
		t.Fatalf("readiness response leaked check error: %s", encoded)
	}
}

func TestReadinessReportsStartupAndStoppedRoles(t *testing.T) {
	readiness := NewReadiness("api")
	result, ready := readiness.Evaluate(context.Background())
	if ready || result.Status != "error" || len(result.Dependencies) != 1 || result.Dependencies[0].Name != "api.startup" {
		t.Fatalf("startup readiness = %+v, ready=%v", result, ready)
	}

	readiness.ConfigureRole("api")
	readiness.MarkStopped("api")
	result, ready = readiness.Evaluate(context.Background())
	if ready || result.Status != "error" || len(result.Dependencies) != 1 || result.Dependencies[0].Name != "api.process" {
		t.Fatalf("stopped readiness = %+v, ready=%v", result, ready)
	}
}

func TestProbeHandlerSeparatesLivenessFromReadiness(t *testing.T) {
	readiness := NewReadiness("api")
	handler := NewProbeHandler("abc123", readiness)

	live := httptest.NewRecorder()
	handler.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if live.Code != http.StatusOK || !strings.Contains(live.Body.String(), `"version":"abc123"`) {
		t.Fatalf("liveness = %d %s", live.Code, live.Body.String())
	}

	notReady := httptest.NewRecorder()
	handler.ServeHTTP(notReady, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if notReady.Code != http.StatusServiceUnavailable || !strings.Contains(notReady.Body.String(), `"api.startup"`) {
		t.Fatalf("readiness before startup = %d %s", notReady.Code, notReady.Body.String())
	}

	readiness.ConfigureRole("api", DependencyCheck{
		Name:  "postgres",
		Check: func(context.Context) error { return nil },
	})
	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), `"api.postgres"`) {
		t.Fatalf("readiness after startup = %d %s", ready.Code, ready.Body.String())
	}

	method := httptest.NewRecorder()
	handler.ServeHTTP(method, httptest.NewRequest(http.MethodPost, "/readyz", nil))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /readyz status = %d, want %d", method.Code, http.StatusMethodNotAllowed)
	}
}
