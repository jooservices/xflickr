package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestHandlerRedactsGroupsAndAddsNestedContext(t *testing.T) {
	var output bytes.Buffer
	ctx := WithFields(context.Background(), map[string]any{"role": "api"})
	logger := slog.New(NewHandler(&output, slog.LevelDebug, nil)).WithGroup("request")
	logger.InfoContext(ctx, "credentials loaded", slog.Group("credentials",
		"api_token", "hidden-token",
		"username", "member",
	))
	got := output.String()
	if strings.Contains(got, "hidden-token") || !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, `"role":"api"`) {
		t.Fatalf("grouped log was not redacted/enriched: %s", got)
	}
}

func TestSlogAttrsSkipsNonStringKeys(t *testing.T) {
	attrs := slogAttrs([]any{42, "ignored", "accepted", true})
	if len(attrs) != 1 || attrs[0].Key != "accepted" || attrs[0].Value.Bool() != true {
		t.Fatalf("slogAttrs() = %#v", attrs)
	}
}

func TestRedactAnyHandlesStringMapsSlicesAndUnmarshalableValues(t *testing.T) {
	value := map[string]any{
		"api_secret": "hidden",
		"labels":     map[string]string{"access_token": "hidden-token", "safe": "ok"},
		"items":      []any{map[string]any{"client_password": "hidden-password"}},
		"credentials": struct {
			RefreshToken string `json:"refresh_token"`
		}{RefreshToken: "hidden-refresh"},
	}
	redacted := redactAny(value).(map[string]any)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("marshal redacted value: %v", err)
	}
	got := string(encoded)
	for _, secret := range []string{"hidden\"", "hidden-token", "hidden-password", "hidden-refresh"} {
		if strings.Contains(got, secret) {
			t.Errorf("redacted value retained %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") || !strings.Contains(got, `"safe":"ok"`) {
		t.Fatalf("unexpected redacted value: %s", got)
	}
	channel := make(chan int)
	if got := redactAny(channel); got != channel {
		t.Fatalf("unmarshalable value changed from %#v to %#v", channel, got)
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	redactedCycle, ok := redactAny(cyclic).(map[string]any)
	if !ok {
		t.Fatal("redactAny() did not copy a cyclic map")
	}
	if _, err := json.Marshal(redactedCycle); err != nil {
		t.Fatalf("redactAny() left a cycle in the log value: %v", err)
	}
}

func TestMultiHandlerEnabledAndErrorAggregation(t *testing.T) {
	wantErr := errors.New("handler failure")
	enabled := testSlogHandler{enabled: true, err: wantErr}
	disabled := testSlogHandler{}
	handler := multiHandler{handlers: []slog.Handler{disabled, enabled}}
	if !handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Enabled() = false, want true")
	}
	if err := handler.Handle(context.Background(), slog.NewRecord(time.Time{}, slog.LevelInfo, "test", 0)); !errors.Is(err, wantErr) {
		t.Fatalf("Handle() error = %v, want %v", err, wantErr)
	}
	if handler.WithAttrs(nil) == nil || handler.WithGroup("group") == nil {
		t.Fatal("handler cloning returned nil")
	}
	if (multiHandler{handlers: []slog.Handler{disabled}}).Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Enabled() = true when all handlers are disabled")
	}
}

type testSlogHandler struct {
	enabled bool
	err     error
}

func (h testSlogHandler) Enabled(context.Context, slog.Level) bool  { return h.enabled }
func (h testSlogHandler) Handle(context.Context, slog.Record) error { return h.err }
func (h testSlogHandler) WithAttrs([]slog.Attr) slog.Handler        { return h }
func (h testSlogHandler) WithGroup(string) slog.Handler             { return h }
