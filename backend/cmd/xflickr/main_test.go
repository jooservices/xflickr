package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/roles"
)

type waitingRole struct {
	name string
	got  chan struct{}
}

func (r waitingRole) Name() string { return r.name }
func (r waitingRole) Run(ctx context.Context) error {
	if r.got != nil {
		close(r.got)
	}
	<-ctx.Done()
	return nil
}

type errorRole struct{ err error }

func (errorRole) Name() string                { return "failure" }
func (r errorRole) Run(context.Context) error { return r.err }

func TestParseRolesUsesEnvironmentWhenFlagIsAbsent(t *testing.T) {
	registry := roles.NewBuiltinRegistry("test")
	selected, err := parseRoles(nil, "api,scheduler", registry)
	if err != nil {
		t.Fatalf("parseRoles() error = %v", err)
	}
	if got := roleNamesForTest(selected); got != "api,scheduler" {
		t.Fatalf("selected roles = %q, want api,scheduler", got)
	}
}

func TestParseRolesFlagOverridesEnvironment(t *testing.T) {
	registry := roles.NewBuiltinRegistry("test")
	selected, err := parseRoles([]string{"--role=media"}, "api", registry)
	if err != nil {
		t.Fatalf("parseRoles() error = %v", err)
	}
	if got := roleNamesForTest(selected); got != "media" {
		t.Fatalf("selected roles = %q, want media", got)
	}
}

func TestExecuteUnknownRoleReturnsTwoAndListsValidRoles(t *testing.T) {
	var stderr bytes.Buffer
	code := execute(context.Background(), []string{"--role=unknown"}, "", roles.NewBuiltinRegistry("test"), &stderr)
	if code != 2 {
		t.Fatalf("execute() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown role \"unknown\"") || !strings.Contains(stderr.String(), "ai-worker, api, crawl, indexer, media, node-agent, scheduler, sync") {
		t.Fatalf("stderr = %q, want unknown role and valid role list", stderr.String())
	}
}

func TestExecuteWithMigrationDispatchesSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	registry := roles.NewBuiltinRegistry("test")
	code := executeWithMigration(context.Background(), []string{"migrate", "status"}, "", registry, &stdout, &stderr, func(_ context.Context, action string, output io.Writer) error {
		called = true
		if action != "status" || output != &stdout {
			t.Fatalf("migration runner got (%q, %T), want (status, stdout)", action, output)
		}
		return nil
	})
	if code != 0 || !called || stderr.Len() != 0 {
		t.Fatalf("executeWithMigration() = (%d, called=%t, stderr=%q)", code, called, stderr.String())
	}
}

func TestExecuteRoleFlagAndEnvironmentSelection(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		env  string
	}{
		{name: "flag", args: []string{"--role=api"}},
		{name: "environment", env: "api"},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			registry := roles.NewRegistry()
			if err := registry.Register(waitingRole{name: "api", got: started}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan int, 1)
			go func() { done <- execute(ctx, test.args, test.env, registry, &bytes.Buffer{}) }()
			select {
			case <-started:
			case <-contextTimeout(t):
				cancel()
				t.Fatal("selected role did not start")
			}
			cancel()
			select {
			case code := <-done:
				if code != 0 {
					t.Fatalf("execute() code = %d, want 0", code)
				}
			case <-contextTimeout(t):
				t.Fatal("execute() did not stop after cancellation")
			}
		})
	}
}

func TestExecuteReturnsOneWhenRoleFails(t *testing.T) {
	wantErr := errors.New("runner failed")
	registry := roles.NewRegistry()
	if err := registry.Register(errorRole{err: wantErr}); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := execute(context.Background(), []string{"--role=failure"}, "", registry, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), wantErr.Error()) {
		t.Fatalf("execute() = (%d, %q), want (1, error message)", code, stderr.String())
	}
}

func TestExecuteInvalidArgumentsReturnTwo(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"api"}} {
		var stderr bytes.Buffer
		code := execute(context.Background(), args, "", roles.NewBuiltinRegistry("test"), &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "valid roles:") {
			t.Errorf("execute(%v) = (%d, %q), want code 2 and valid roles", args, code, stderr.String())
		}
	}
}

func TestMainExitUsesSignalContextAndReturnsArgumentError(t *testing.T) {
	originalArgs, originalStderr := os.Args, os.Stderr
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Args = []string{"xflickr", "--role=missing"}
	os.Stderr = writeEnd
	t.Cleanup(func() {
		os.Args = originalArgs
		os.Stderr = originalStderr
		_ = readEnd.Close()
	})

	code := mainExit()
	if err := writeEnd.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	os.Stderr = originalStderr
	output, err := io.ReadAll(readEnd)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if code != 2 || !strings.Contains(string(output), "valid roles:") {
		t.Fatalf("mainExit() = (%d, %q), want code 2 with valid roles", code, output)
	}
}

func roleNamesForTest(selected []roles.Role) string {
	names := make([]string, len(selected))
	for index, role := range selected {
		names[index] = role.Name()
	}
	return strings.Join(names, ",")
}

func contextTimeout(t *testing.T) <-chan time.Time {
	t.Helper()
	return time.After(time.Second)
}
