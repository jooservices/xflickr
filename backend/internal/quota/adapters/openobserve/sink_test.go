package openobserve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jooservices/xflickr/internal/quota/app"
)

func TestSendPostsJSONWithBasicAuth(t *testing.T) {
	var path, user, pass, contentType string
	var body []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, contentType = r.URL.Path, r.Header.Get("Content-Type")
		user, pass, _ = r.BasicAuth()
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	sink := Sink{URL: server.URL + "/", Org: "default", Stream: "xflickr", Username: "u", Password: "p"}
	if err := sink.Send(t.Context(), []app.Event{{"event": "quota.state", "key_id": 1}}); err != nil {
		t.Fatal(err)
	}
	if path != "/api/default/xflickr/_json" || user != "u" || pass != "p" || contentType != "application/json" {
		t.Fatalf("request path=%q user=%q type=%q", path, user, contentType)
	}
	if len(body) != 1 || body[0]["event"] != "quota.state" {
		t.Fatalf("body = %v", body)
	}
}

func TestSendErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	events := []app.Event{{"event": "quota.state"}}
	if err := (Sink{URL: server.URL, Org: "o", Stream: "s"}).Send(t.Context(), events); err == nil {
		t.Fatal("401 accepted")
	}
	if err := (Sink{Org: "o", Stream: "s"}).Send(t.Context(), events); err == nil {
		t.Fatal("missing URL accepted")
	}
	if err := (Sink{URL: server.URL, Org: "o", Stream: "s"}).Send(t.Context(), nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if err := (Sink{URL: server.URL, Org: "o", Stream: "s"}).Send(t.Context(), []app.Event{{"bad": make(chan int)}}); err == nil {
		t.Fatal("unencodable event accepted")
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	url := closed.URL
	closed.Close()
	if err := (Sink{URL: url, Org: "o", Stream: "s"}).Send(t.Context(), events); err == nil {
		t.Fatal("connection failure accepted")
	}
	if err := (Sink{URL: "http://bad host", Org: "o", Stream: "s"}).Send(t.Context(), events); err == nil {
		t.Fatal("invalid URL accepted")
	}
}
