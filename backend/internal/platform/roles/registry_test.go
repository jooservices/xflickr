package roles

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type testRole struct {
	name string
	run  func(context.Context) error
}

func (r testRole) Name() string { return r.name }
func (r testRole) Run(ctx context.Context) error {
	if r.run != nil {
		return r.run(ctx)
	}
	<-ctx.Done()
	return nil
}

func TestParseRoleListSingleAndCommaSeparated(t *testing.T) {
	registry := testRegistry(t, "api", "scheduler")
	for _, test := range []struct {
		name string
		raw  string
		want []string
	}{
		{name: "single", raw: "api", want: []string{"api"}},
		{name: "comma separated with whitespace", raw: " api, scheduler ", want: []string{"api", "scheduler"}},
		{name: "duplicate role", raw: "api,api", want: []string{"api"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseRoleList(test.raw, registry)
			if err != nil {
				t.Fatalf("ParseRoleList() error = %v", err)
			}
			if names := roleNames(got); strings.Join(names, ",") != strings.Join(test.want, ",") {
				t.Fatalf("role names = %v, want %v", names, test.want)
			}
		})
	}
}

func TestParseRoleListUnknownShowsValidNames(t *testing.T) {
	registry := testRegistry(t, "scheduler", "api")
	_, err := ParseRoleList("missing", registry)
	if err == nil || !strings.Contains(err.Error(), "unknown role \"missing\"") || !strings.Contains(err.Error(), "valid roles: api, scheduler") {
		t.Fatalf("error = %v, want unknown role and sorted valid roles", err)
	}
}

func TestParseRoleListRequiresAtLeastOneRole(t *testing.T) {
	registry := testRegistry(t, "api")
	for _, raw := range []string{"", "  ", ", ,"} {
		if _, err := ParseRoleList(raw, registry); err == nil || !strings.Contains(err.Error(), "a role is required") {
			t.Errorf("ParseRoleList(%q) error = %v, want role required", raw, err)
		}
	}
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(nil); err == nil {
		t.Fatal("Register(nil) error = nil")
	}
	role := testRole{name: "node-agent"}
	if err := registry.Register(role); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(role); err == nil {
		t.Fatal("Register() duplicate error = nil")
	}
	if _, ok := registry.Get("node-agent"); !ok {
		t.Fatal("Get() did not find registered role")
	}
	if _, ok := registry.Get("missing"); ok {
		t.Fatal("Get() found an unregistered role")
	}
	if got := strings.Join(registry.Names(), ","); got != "node-agent" {
		t.Fatalf("Names() = %q, want node-agent", got)
	}
	if got := registry.ValidRoles(); got != "node-agent" {
		t.Fatalf("ValidRoles() = %q, want node-agent", got)
	}
}

func TestRegistryRejectsInvalidNames(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"", "  ", "bad,name", "two words", "tab\tname"} {
		if err := registry.Register(testRole{name: name}); err == nil {
			t.Errorf("Register(%q) error = nil, want invalid name", name)
		}
	}
}

func TestRunCancelsEverySelectedRole(t *testing.T) {
	started := make(chan struct{}, 2)
	finished := make(chan struct{}, 2)
	block := func(ctx context.Context) error {
		started <- struct{}{}
		<-ctx.Done()
		finished <- struct{}{}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, []Role{
			testRole{name: "one", run: block},
			testRole{name: "two", run: block},
		})
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("roles did not start")
		}
	}
	cancel()
	for range 2 {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("role did not observe cancellation")
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
}

func TestRunCancelsOtherRolesAfterFailure(t *testing.T) {
	failed := errors.New("role failed")
	started := make(chan struct{})
	finished := make(chan struct{})
	block := testRole{name: "blocking", run: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(finished)
		return nil
	}}
	bad := testRole{name: "failing", run: func(context.Context) error {
		<-started
		return failed
	}}
	if err := Run(context.Background(), []Role{block, bad}); !errors.Is(err, failed) {
		t.Fatalf("Run() error = %v, want %v", err, failed)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("blocking role did not observe failure cancellation")
	}
}

func TestRunRejectsEmptyOrNilRoles(t *testing.T) {
	if err := Run(context.Background(), nil); err == nil {
		t.Fatal("Run(nil) error = nil")
	}
	if err := Run(context.Background(), []Role{nil}); err == nil {
		t.Fatal("Run([nil]) error = nil")
	}
}

func TestBuiltinRegistryHasAllSupportedRoles(t *testing.T) {
	registry := NewBuiltinRegistry("test")
	want := "ai-worker,api,crawl,indexer,media,node-agent,scheduler,sync"
	if got := strings.Join(registry.Names(), ","); got != want {
		t.Fatalf("Names() = %q, want %q", got, want)
	}
	scheduler, ok := registry.Get("scheduler")
	if !ok || scheduler.Name() != "scheduler" {
		t.Fatal("registry did not return scheduler role")
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- scheduler.Run(ctx)
	}()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("scheduler Run() error = %v", err)
	}
}

func TestAPIRoleServesHealthAndStops(t *testing.T) {
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	address := reservation.Addr().String()
	if err := reservation.Close(); err != nil {
		t.Fatalf("release address: %v", err)
	}

	role := NewAPIRole(address, "abc123")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- role.Run(ctx) }()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	var response *http.Response
	for attempt := 0; attempt < 20; attempt++ {
		response, err = client.Get("http://" + address + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("GET /healthz: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil {
		cancel()
		t.Fatalf("read health response: %v", readErr)
	}
	if response.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("GET /healthz status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(body), `"version":"abc123"`) {
		cancel()
		t.Fatalf("GET /healthz body = %q, want version abc123", body)
	}
	if role.Name() != "api" {
		cancel()
		t.Fatalf("Name() = %q, want api", role.Name())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("API role did not stop after cancellation")
	}
}

func TestAPIRoleReturnsListenError(t *testing.T) {
	if err := NewAPIRole("missing-port", "test").Run(context.Background()); err == nil {
		t.Fatal("Run() error = nil, want invalid listen address error")
	}
}

func testRegistry(t *testing.T, names ...string) *Registry {
	t.Helper()
	registry := NewRegistry()
	for _, name := range names {
		if err := registry.Register(testRole{name: name}); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}
	return registry
}

func roleNames(roles []Role) []string {
	names := make([]string, len(roles))
	for index, role := range roles {
		names[index] = role.Name()
	}
	return names
}
