package flickr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestGalleriesGetListForwardsContinuation(t *testing.T) {
	fixture, err := os.ReadFile("testdata/galleries.getList.continuation.json")
	if err != nil {
		t.Fatal(err)
	}
	var pages []json.RawMessage
	if err := json.Unmarshal(fixture, &pages); err != nil {
		t.Fatal(err)
	}
	var continuation, page string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse request: %v", err)
		}
		continuation, page = r.Form.Get("continuation"), r.Form.Get("page")
		_, _ = w.Write(pages[0])
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())
	got, err := client.GalleriesGetList(t.Context(), Credentials{APIKey: "test-key"}, "owner@N01", ListOptions{Continuation: "opaque:request/2?token=x"})
	if err != nil {
		t.Fatal(err)
	}
	if continuation != "opaque:request/2?token=x" || page != "" || got.Continuation != "opaque:page/2?token=a" {
		t.Fatalf("request continuation=%q page=%q response=%q", continuation, page, got.Continuation)
	}
}

func TestGalleriesGetListStartsWithZeroContinuation(t *testing.T) {
	var continuation string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse request: %v", err)
		}
		continuation = r.Form.Get("continuation")
		_, _ = w.Write([]byte(`{"galleries":{"continuation":"-1","gallery":[]}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client())
	page, err := client.GalleriesGetList(t.Context(), Credentials{APIKey: "test-key"}, "owner@N01", ListOptions{})
	if err != nil || continuation != "0" || page.Continuation != "-1" {
		t.Fatalf("continuation=%q page=%+v error=%v", continuation, page, err)
	}
}
