package heartbeat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "node-token-with-secret-value"

func TestClientSendHeartbeat(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/internal/nodes/heartbeat" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var body struct {
			Version  string          `json:"version"`
			EgressIP string          `json:"egress_ip"`
			Metrics  json.RawMessage `json:"metrics"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Version != "v1.2.3" || body.EgressIP != "192.0.2.7" || string(body.Metrics) != `{"cpu":0.5}` {
			t.Errorf("request body = %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"node-a","state":"online","last_heartbeat_at":"2026-11-11T04:10:00Z"}`)
	}))
	defer server.Close()

	client, err := New(server.URL, testToken, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Send(t.Context(), Input{
		Version: "v1.2.3", EgressIP: "192.0.2.7", Metrics: json.RawMessage(`{"cpu":0.5}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "node-a" || got.State != "online" || !got.LastHeartbeatAt.Equal(time.Date(2026, 11, 11, 4, 10, 0, 0, time.UTC)) {
		t.Fatalf("result = %+v", got)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want exactly one", requests.Load())
	}
}

func TestClientDefaultsEmptyMetricsObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if string(body["metrics"]) != `{}` {
			t.Errorf("metrics = %s, want empty object", body["metrics"])
		}
		_, _ = io.WriteString(w, `{"name":"node-a","state":"warning","last_heartbeat_at":"2026-11-11T04:10:00Z"}`)
	}))
	defer server.Close()

	client, err := New(server.URL, testToken, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Send(t.Context(), Input{}); err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		url    string
		token  string
		client *http.Client
	}{
		{name: "empty URL", url: "", token: testToken, client: &http.Client{}},
		{name: "relative URL", url: "control.internal", token: testToken, client: &http.Client{}},
		{name: "unsupported scheme", url: "ftp://control.internal", token: testToken, client: &http.Client{}},
		{name: "missing host", url: "http:///heartbeat", token: testToken, client: &http.Client{}},
		{name: "remote HTTP", url: "http://control.internal", token: testToken, client: &http.Client{}},
		{name: "remote HTTP IP", url: "http://192.0.2.10", token: testToken, client: &http.Client{}},
		{name: "userinfo", url: "https://user:pass@control.internal", token: testToken, client: &http.Client{}},
		{name: "query", url: "https://control.internal?key=value", token: testToken, client: &http.Client{}},
		{name: "empty query", url: "https://control.internal?", token: testToken, client: &http.Client{}},
		{name: "fragment", url: "https://control.internal#part", token: testToken, client: &http.Client{}},
		{name: "empty fragment", url: "https://control.internal#", token: testToken, client: &http.Client{}},
		{name: "path", url: "https://control.internal/prefix", token: testToken, client: &http.Client{}},
		{name: "empty token", url: "https://control.internal", token: "", client: &http.Client{}},
		{name: "token whitespace", url: "https://control.internal", token: "node token", client: &http.Client{}},
		{name: "nil HTTP client", url: "https://control.internal", token: testToken},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.url, test.token, test.client); err == nil {
				t.Fatal("New accepted invalid constructor input")
			}
		})
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client, err := New(server.URL, testToken, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	var nilClient *Client
	for _, test := range []struct {
		name   string
		client *Client
		ctx    context.Context
		input  Input
	}{
		{name: "version too long", client: client, ctx: t.Context(), input: Input{Version: strings.Repeat("v", 65)}},
		{name: "invalid IP", client: client, ctx: t.Context(), input: Input{EgressIP: "not-an-ip"}},
		{name: "invalid metrics JSON", client: client, ctx: t.Context(), input: Input{Metrics: json.RawMessage(`{"broken"`)}},
		{name: "metrics must be an object", client: client, ctx: t.Context(), input: Input{Metrics: json.RawMessage(`[]`)}},
		{name: "metrics cannot be null", client: client, ctx: t.Context(), input: Input{Metrics: json.RawMessage(`null`)}},
		{name: "metrics too large", client: client, ctx: t.Context(), input: Input{Metrics: json.RawMessage(strings.Repeat(" ", 64*1024) + `{}`)}},
		{name: "nil context", client: client, input: Input{}},
		{name: "nil client", client: nilClient, ctx: t.Context(), input: Input{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.client.Send(test.ctx, test.input); err == nil {
				t.Fatal("Send accepted invalid input")
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid inputs sent %d requests", requests.Load())
	}
}

func TestClientHandlesFailures(t *testing.T) {
	responseMarker := "private response body marker"
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "bad request", status: http.StatusBadRequest, body: testToken + " " + responseMarker},
		{name: "unauthorized", status: http.StatusUnauthorized, body: testToken + " " + responseMarker},
		{name: "server error", status: http.StatusInternalServerError, body: testToken + " " + responseMarker},
		{name: "invalid JSON", status: http.StatusOK, body: testToken + " " + responseMarker},
		{name: "missing fields", status: http.StatusOK, body: `{"name":"node-a","state":"online"}`},
		{name: "invalid state", status: http.StatusOK, body: `{"name":"node-a","state":"unknown","last_heartbeat_at":"2026-11-11T04:10:00Z"}`},
		{name: "invalid timestamp", status: http.StatusOK, body: `{"name":"node-a","state":"online","last_heartbeat_at":"yesterday"}`},
		{name: "oversized response", status: http.StatusOK, body: strings.Repeat("x", 64*1024+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &trackedBody{reader: strings.NewReader(test.body)}
			var requests atomic.Int32
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				requests.Add(1)
				return &http.Response{StatusCode: test.status, Header: make(http.Header), Body: body}, nil
			})
			client, err := New("https://control.internal", testToken, &http.Client{Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Send(t.Context(), Input{})
			if err == nil {
				t.Fatal("Send accepted invalid response")
			}
			if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), responseMarker) || strings.Contains(err.Error(), "yesterday") {
				t.Fatalf("error exposes sensitive response data: %v", err)
			}
			if test.status != http.StatusOK && !strings.Contains(err.Error(), strconv.Itoa(test.status)) {
				t.Fatalf("error %q does not include status %d", err, test.status)
			}
			if requests.Load() != 1 {
				t.Fatalf("requests = %d, want one", requests.Load())
			}
			if body.closed != 1 {
				t.Fatalf("response body closed %d times, want once", body.closed)
			}
		})
	}

	transportError := errors.New("transport failed: " + testToken + " " + responseMarker)
	var requests atomic.Int32
	client, err := New("https://control.internal", testToken, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, transportError
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Send(t.Context(), Input{}); err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), responseMarker) {
		t.Fatalf("network error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("network requests = %d, want one", requests.Load())
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	client, err = New("https://control.internal", testToken, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	type sendResult struct{ err error }
	finished := make(chan sendResult, 1)
	go func() {
		_, sendErr := client.Send(ctx, Input{})
		finished <- sendResult{err: sendErr}
	}()
	select {
	case <-started:
		cancel()
	case <-time.After(3 * time.Second):
		t.Fatal("server did not receive canceled request")
	}
	select {
	case result := <-finished:
		if result.err == nil || strings.Contains(result.err.Error(), testToken) || strings.Contains(result.err.Error(), responseMarker) {
			t.Fatalf("canceled request error = %v", result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Send did not return after context cancellation")
	}
}

func TestClientDoesNotFollowRedirect(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()
	var sourceRequests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sourceRequests.Add(1)
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = io.WriteString(w, "redirect response body")
	}))
	defer source.Close()
	var redirectCallbacks atomic.Int32
	callerClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		redirectCallbacks.Add(1)
		return nil
	}}
	client, err := New(source.URL, testToken, callerClient)
	if err != nil {
		t.Fatal(err)
	}
	if callerClient.CheckRedirect == nil {
		t.Fatal("New changed the caller's HTTP client")
	}
	_, err = client.Send(t.Context(), Input{})
	if err == nil || !strings.Contains(err.Error(), "307") || strings.Contains(err.Error(), testToken) {
		t.Fatalf("redirect error = %v", err)
	}
	if sourceRequests.Load() != 1 || targetRequests.Load() != 0 || redirectCallbacks.Load() != 0 {
		t.Fatalf("source, target, redirect callback counts = %d, %d, %d", sourceRequests.Load(), targetRequests.Load(), redirectCallbacks.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type trackedBody struct {
	reader io.Reader
	closed int
}

func (body *trackedBody) Read(p []byte) (int, error) { return body.reader.Read(p) }

func (body *trackedBody) Close() error {
	body.closed++
	return nil
}
