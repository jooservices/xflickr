package roles

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/httpx"
)

type probeTestRole struct {
	name  string
	ready *atomic.Bool
}

func (r probeTestRole) Name() string { return r.name }

func (r probeTestRole) Run(ctx context.Context) error {
	configureRoleReadiness(ctx, r.name, httpx.DependencyCheck{
		Name: "dependency",
		Check: func(context.Context) error {
			if !r.ready.Load() {
				return errors.New("private test dependency detail")
			}
			return nil
		},
	})
	<-ctx.Done()
	return nil
}

func TestRunWithHealthSharesReadinessAcrossRoles(t *testing.T) {
	port := reserveProbePort(t)
	t.Setenv("OBS_HTTP_PORT", strconv.Itoa(port))
	var firstReady, secondReady atomic.Bool
	secondReady.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	runErr := make(chan error, 1)
	go func() {
		runErr <- RunWithHealth(ctx, []Role{
			probeTestRole{name: "first", ready: &firstReady},
			probeTestRole{name: "second", ready: &secondReady},
		}, "test")
	}()

	status, body := awaitProbeStatus(t, port, "/readyz", http.StatusServiceUnavailable)
	if !strings.Contains(body, "\"name\":\"first.dependency\",\"status\":\"error\"") {
		t.Fatalf("readiness response omitted the failing role: %s", body)
	}
	if strings.Contains(body, "private test dependency detail") {
		t.Fatalf("readiness response leaked dependency detail: %s", body)
	}
	if status != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d", status)
	}
	if status, _ := awaitProbeStatus(t, port, "/healthz", http.StatusOK); status != http.StatusOK {
		t.Fatalf("liveness status = %d", status)
	}

	firstReady.Store(true)
	if status, body = awaitProbeStatus(t, port, "/readyz", http.StatusOK); status != http.StatusOK {
		t.Fatalf("readiness status after recovery = %d, body=%s", status, body)
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("RunWithHealth() after cancellation = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithHealth() did not stop after cancellation")
	}
}

func TestAPIServerReadinessUsesContractStatusResponses(t *testing.T) {
	readiness := httpx.NewReadiness("api")
	readiness.ConfigureRole("api", httpx.DependencyCheck{
		Name:  "postgres",
		Check: func(context.Context) error { return nil },
	})
	server := apiServer{Readiness: readiness}
	response, err := server.GetReadiness(t.Context(), gen.GetReadinessRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	ready, ok := response.(gen.GetReadiness200JSONResponse)
	if !ok || ready.Status != gen.ReadinessStatus("ok") || len(ready.Dependencies) != 1 || ready.Dependencies[0].Status != gen.DependencyStatusStatus("ok") {
		t.Fatalf("ready response = %#v", response)
	}

	readiness.MarkStopped("api")
	response, err = server.GetReadiness(t.Context(), gen.GetReadinessRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	notReady, ok := response.(gen.GetReadiness503JSONResponse)
	if !ok || notReady.Status != gen.ReadinessStatus("error") {
		t.Fatalf("not-ready response = %#v", response)
	}
}

func TestRunWithHealthRejectsInvalidProbePort(t *testing.T) {
	t.Setenv("OBS_HTTP_PORT", "0")
	var ready atomic.Bool
	if err := RunWithHealth(t.Context(), []Role{probeTestRole{name: "test", ready: &ready}}, "test"); err == nil || !strings.Contains(err.Error(), "OBS_HTTP_PORT") {
		t.Fatalf("RunWithHealth() error = %v, want invalid OBS_HTTP_PORT", err)
	}
}

func TestRunWithHealthReturnsProbeListenerError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	t.Setenv("OBS_HTTP_PORT", strconv.Itoa(port))
	var ready atomic.Bool
	err = RunWithHealth(t.Context(), []Role{probeTestRole{name: "test", ready: &ready}}, "test")
	if err == nil || !strings.Contains(err.Error(), "listen for health probes") {
		t.Fatalf("RunWithHealth() error = %v, want probe listener error", err)
	}
}

func TestRunWithHealthRequiresSeparateAPIAndProbePorts(t *testing.T) {
	port := reserveProbePort(t)
	portText := strconv.Itoa(port)
	t.Setenv("OBS_HTTP_PORT", portText)
	t.Setenv("HTTP_PORT", portText)
	if err := RunWithHealth(t.Context(), []Role{NewAPIRole(":"+portText, "test")}, "test"); err == nil || !strings.Contains(err.Error(), "differ from HTTP_PORT") {
		t.Fatalf("RunWithHealth() error = %v, want separate API and probe ports", err)
	}
}

func reserveProbePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func awaitProbeStatus(t *testing.T, port int, path string, want int) (int, string) {
	t.Helper()
	client := &http.Client{Timeout: 250 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	var lastStatus int
	var lastBody string
	for time.Now().Before(deadline) {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err == nil {
			var body [4096]byte
			n, _ := response.Body.Read(body[:])
			_ = response.Body.Close()
			lastStatus, lastBody = response.StatusCode, string(body[:n])
			if lastStatus == want {
				return lastStatus, lastBody
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("probe %s status = %d, body=%s; want %d", path, lastStatus, lastBody, want)
	return lastStatus, lastBody
}
