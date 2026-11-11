package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/crawl/facets"
)

func TestGetGroupsLargeListUsesOneFlickrCall(t *testing.T) {
	const total = 5000
	payload := largeGroupsPayload(t, total)
	registry := NewRegistry()
	if err := registry.Register(facets.Groups{}); err != nil {
		t.Fatal(err)
	}
	item := domain.Item{
		ID: 42, Kind: "groups", Subject: "p@N01", UserID: 3, Depth: 1, State: domain.StateReady, Attempts: 1,
		Cursor: domain.Cursor{Page: 1, Method: "flickr.people.getGroups", Params: map[string]string{"user_id": "p@N01"}},
	}
	tx := &fakeTx{current: domain.Item{State: domain.StateRunning, Cursor: item.Cursor}}
	flickr := &fakeFlickr{payload: payload}
	store := &fakeStore{item: item, started: true}
	engine := &Engine{
		Registry: registry, Store: store, Units: tx, Keys: fakeKeys{id: 9},
		Governor: fakeGovernor{permit: &fakePermit{}}, Flickr: flickr, Settings: fakeSettings{max: 3},
		Now: func() time.Time { return epoch },
	}
	if err := engine.Handle(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	if len(flickr.requests) != 1 {
		t.Fatalf("flickr calls = %d, want 1", len(flickr.requests))
	}
	if flickr.requests[0].Method != "flickr.people.getGroups" {
		t.Fatalf("method = %q", flickr.requests[0].Method)
	}
	if len(tx.groups) != total || len(tx.personGroups) != total {
		t.Fatalf("committed groups=%d memberships=%d, want %d each", len(tx.groups), len(tx.personGroups), total)
	}
	if !tx.finished {
		t.Fatal("page did not finish after the single unpaged response")
	}
}

func TestGroupsEmptyEnvelopeFinishesWithoutMemberships(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(facets.Groups{}); err != nil {
		t.Fatal(err)
	}
	item := domain.Item{
		ID: 44, Kind: "groups", Subject: "p@N01", UserID: 3, Depth: 1, State: domain.StateReady, Attempts: 1,
		Cursor: domain.Cursor{Page: 1, Method: "flickr.people.getGroups", Params: map[string]string{"user_id": "p@N01"}},
	}
	tx := &fakeTx{current: domain.Item{State: domain.StateRunning, Cursor: item.Cursor}}
	flickr := &fakeFlickr{payload: json.RawMessage(`{"groups":{}}`)}
	store := &fakeStore{item: item, started: true}
	engine := &Engine{
		Registry: registry, Store: store, Units: tx, Keys: fakeKeys{id: 9},
		Governor: fakeGovernor{permit: &fakePermit{}}, Flickr: flickr, Settings: fakeSettings{max: 3},
		Now: func() time.Time { return epoch },
	}
	if err := engine.Handle(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	if len(tx.groups) != 0 || len(tx.personGroups) != 0 {
		t.Fatalf("empty response wrote groups=%d memberships=%d", len(tx.groups), len(tx.personGroups))
	}
	if !tx.finished || tx.advanced != nil {
		t.Fatalf("empty response progress: finished=%v advanced=%v", tx.finished, tx.advanced)
	}
	if len(tx.log) != 1 || tx.log[0] != "finish" {
		t.Fatalf("empty response transaction writes = %#v, want finish only", tx.log)
	}
}

func TestGroupsInvalidEnvelopeDoesNotCommit(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(facets.Groups{}); err != nil {
		t.Fatal(err)
	}
	item := domain.Item{
		ID: 43, Kind: "groups", Subject: "p@N01", UserID: 3, Depth: 1, State: domain.StateReady, Attempts: 1,
		Cursor: domain.Cursor{Page: 1, Method: "flickr.people.getGroups", Params: map[string]string{"user_id": "p@N01"}},
	}
	tx := &fakeTx{current: domain.Item{State: domain.StateRunning, Cursor: item.Cursor}}
	flickr := &fakeFlickr{payload: json.RawMessage(`{}`)}
	store := &fakeStore{item: item, started: true}
	engine := &Engine{
		Registry: registry, Store: store, Units: tx, Keys: fakeKeys{id: 9},
		Governor: fakeGovernor{permit: &fakePermit{}}, Flickr: flickr, Settings: fakeSettings{max: 3},
		Now: func() time.Time { return epoch },
	}
	if err := engine.Handle(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	if len(flickr.requests) != 1 || flickr.requests[0].Method != "flickr.people.getGroups" {
		t.Fatalf("Flickr requests = %#v, want one getGroups call", flickr.requests)
	}
	if len(tx.groups) != 0 || len(tx.personGroups) != 0 || len(tx.log) != 0 {
		t.Fatalf("invalid response wrote catalog data: groups=%d memberships=%d writes=%v", len(tx.groups), len(tx.personGroups), tx.log)
	}
	if tx.finished || tx.advanced != nil {
		t.Fatalf("invalid response committed page progress: finished=%v advanced=%v", tx.finished, tx.advanced)
	}
	if len(store.calls) != 2 || store.calls[0] != "start" || store.calls[1] != "defer" {
		t.Fatalf("work item calls = %#v, want start and existing failure deferral", store.calls)
	}
}

func TestGroupsEmptyIntermediatePageDoesNotFinishWork(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(facets.Groups{}); err != nil {
		t.Fatal(err)
	}
	item := domain.Item{
		ID: 45, Kind: "groups", Subject: "p@N01", UserID: 3, Depth: 1, State: domain.StateReady, Attempts: 1,
		Cursor: domain.Cursor{Page: 1, Method: "flickr.people.getGroups", Params: map[string]string{"user_id": "p@N01"}},
	}
	tx := &fakeTx{current: domain.Item{State: domain.StateRunning, Cursor: item.Cursor}}
	flickr := &fakeFlickr{payload: json.RawMessage(`{"groups":{"pages":2,"group":[]}}`)}
	store := &fakeStore{item: item, started: true}
	engine := &Engine{
		Registry: registry, Store: store, Units: tx, Keys: fakeKeys{id: 9},
		Governor: fakeGovernor{permit: &fakePermit{}}, Flickr: flickr, Settings: fakeSettings{max: 3},
		Now: func() time.Time { return epoch },
	}
	if err := engine.Handle(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	if len(tx.groups) != 0 || len(tx.personGroups) != 0 || len(tx.log) != 0 {
		t.Fatalf("empty intermediate page wrote groups=%d memberships=%d writes=%v", len(tx.groups), len(tx.personGroups), tx.log)
	}
	if tx.finished || tx.advanced != nil {
		t.Fatalf("empty intermediate page committed progress: finished=%v advanced=%v", tx.finished, tx.advanced)
	}
	if len(store.calls) != 2 || store.calls[0] != "start" || store.calls[1] != "defer" {
		t.Fatalf("work item calls = %#v, want start and existing failure deferral", store.calls)
	}
}

func largeGroupsPayload(t *testing.T, n int) json.RawMessage {
	t.Helper()
	var b strings.Builder
	b.Grow(n * 64)
	b.WriteString(`{"groups":{"pages":1,"group":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"nsid":"g%d@N00","name":"G%d","members":1,"pool_count":1}`, i, i)
	}
	b.WriteString(`]}}`)
	raw := json.RawMessage(b.String())
	if !json.Valid(raw) {
		t.Fatal("generated groups payload is invalid JSON")
	}
	return raw
}
