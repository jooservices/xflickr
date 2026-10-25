package openobserve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/ops"
)

func TestSendPostsEvents(t *testing.T) {
	var path, user string
	var body []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		user, _, _ = r.BasicAuth()
		_ = json.NewDecoder(r.Body).Decode(&body)
	}))
	defer server.Close()
	sink := Sink{URL: server.URL + "/", Org: "default", Stream: "xflickr", Username: "u", Password: "p"}
	if err := sink.Send(t.Context(), []ops.Event{{"event": "crawl.health"}}); err != nil {
		t.Fatal(err)
	}
	if path != "/api/default/xflickr/_json" || user != "u" || len(body) != 1 || body[0]["event"] != "crawl.health" {
		t.Fatalf("path=%s user=%s body=%v", path, user, body)
	}
}

func TestSendErrors(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer failing.Close()
	events := []ops.Event{{"event": "x"}}
	if err := (Sink{URL: failing.URL, Org: "o", Stream: "s"}).Send(t.Context(), events); err == nil {
		t.Fatal("expected non-2xx error")
	}
	if err := (Sink{Org: "o", Stream: "s"}).Send(t.Context(), events); err == nil {
		t.Fatal("expected unconfigured error")
	}
	if err := (Sink{URL: failing.URL, Org: "o", Stream: "s"}).Send(t.Context(), nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if err := (Sink{URL: "http://127.0.0.1:1", Org: "o", Stream: "s"}).Send(t.Context(), events); err == nil {
		t.Fatal("expected transport error")
	}
	if err := (Sink{URL: failing.URL, Org: "o", Stream: "s"}).Send(t.Context(), []ops.Event{{"bad": make(chan int)}}); err == nil {
		t.Fatal("expected encode error")
	}
}
