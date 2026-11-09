package app

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/crawl/facets"
	"github.com/jooservices/xflickr/pkg/flickr"
)

func TestGalleriesFirstPageNotRepeatedOnRetry(t *testing.T) {
	r := newRig(t)
	r.facet.name = "galleries"
	r.facet.result = domain.Result{NextCursor: "opaque-page-three", HasMore: true}
	r.store.item.Kind = "galleries"
	r.store.item.Cursor = domain.Cursor{Page: 2, Method: "flickr.galleries.getList", Continuation: "opaque-page-two", Params: map[string]string{"user_id": "100@N01"}}
	r.tx.current = domain.Item{State: domain.StateRunning, Cursor: r.store.item.Cursor}
	r.engine.Registry = NewRegistry()
	if err := r.engine.Registry.Register(r.facet); err != nil {
		t.Fatal(err)
	}
	r.flickr.err = flickr.ErrRateLimited
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	r.flickr.err = nil
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if len(r.flickr.requests) != 2 {
		t.Fatalf("Flickr calls = %d, want retry plus committed page", len(r.flickr.requests))
	}
	for index, request := range r.flickr.requests {
		if request.Params["continuation"] != "opaque-page-two" || request.Params["page"] != "" {
			t.Fatalf("request %d params = %#v", index+1, request.Params)
		}
	}
	if r.tx.advanced == nil || r.tx.advanced.Continuation != "opaque-page-three" {
		t.Fatalf("advanced cursor = %#v", r.tx.advanced)
	}
}

func TestEngineLogsGalleryCursorWarning(t *testing.T) {
	r := newRig(t)
	r.facet.result = domain.Result{CursorWarning: "repeated continuation", NextCursor: "same-token"}
	var logs bytes.Buffer
	r.engine.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "repeated continuation") {
		t.Fatalf("warning log = %q", logs.String())
	}
	if !r.tx.finished || r.tx.advanced != nil {
		t.Fatalf("cursor was not committed and finished: %+v", r.tx)
	}
}

func TestGalleriesLegacyResponseFallsBackToPageNumber(t *testing.T) {
	r := newRig(t)
	r.store.item.Kind = "galleries"
	r.store.item.Cursor = domain.Cursor{Page: 1, Method: "flickr.galleries.getList", Continuation: "0", Params: map[string]string{"user_id": "100@N01"}}
	r.tx.current = domain.Item{State: domain.StateRunning, Cursor: r.store.item.Cursor}
	r.flickr.payload = json.RawMessage(`{"galleries":{"pages":2,"gallery":[{"id":"gallery-legacy","owner":"100@N01"}]}}`)
	r.engine.Registry = NewRegistry()
	if err := r.engine.Registry.Register(facets.Galleries{}); err != nil {
		t.Fatal(err)
	}

	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	first := r.flickr.requests[0]
	if first.Params["continuation"] != "0" || first.Params["page"] != "" || r.tx.advanced == nil || r.tx.advanced.Page != 2 || r.tx.advanced.Continuation != "" {
		t.Fatalf("first request=%#v advanced=%+v", first, r.tx.advanced)
	}

	r.store.item.Cursor = *r.tx.advanced
	r.tx.current = domain.Item{State: domain.StateRunning, Cursor: r.store.item.Cursor}
	r.tx.advanced = nil
	r.tx.finished = false
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	second := r.flickr.requests[1]
	if second.Params["continuation"] != "" || second.Params["page"] != "2" || !r.tx.finished {
		t.Fatalf("second request=%#v finished=%v", second, r.tx.finished)
	}
}
