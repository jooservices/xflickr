package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/domain"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

var epoch = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

type fakeFacet struct {
	name   string
	lane   domain.Lane
	plan   []domain.WorkSpec
	result domain.Result
	err    error
	pages  []domain.Page
}

func (f *fakeFacet) Name() string      { return f.name }
func (f *fakeFacet) Lane() domain.Lane { return f.lane }
func (f *fakeFacet) Plan(context.Context, domain.Person) []domain.WorkSpec {
	return f.plan
}

func (f *fakeFacet) Handle(_ context.Context, page domain.Page) (domain.Result, error) {
	f.pages = append(f.pages, page)
	return f.result, f.err
}

type fakeStore struct {
	item     domain.Item
	loadErr  error
	started  bool
	startErr error
	calls    []string
	deferral Deferral
	stop     StopRequest
	reason   string
}

func (s *fakeStore) Load(context.Context, int64) (domain.Item, error) { return s.item, s.loadErr }
func (s *fakeStore) Start(context.Context, int64) (bool, error) {
	s.calls = append(s.calls, "start")
	return s.started, s.startErr
}

func (s *fakeStore) Defer(_ context.Context, _ int64, d Deferral) error {
	s.calls = append(s.calls, "defer")
	s.deferral = d
	return nil
}

func (s *fakeStore) Finish(context.Context, int64) error {
	s.calls = append(s.calls, "finish")
	return nil
}

func (s *fakeStore) Fail(_ context.Context, _ int64, reason string) error {
	s.calls = append(s.calls, "fail")
	s.reason = reason
	return nil
}

func (s *fakeStore) Stop(_ context.Context, r StopRequest) error {
	s.calls = append(s.calls, "stop")
	s.stop = r
	return nil
}

// fakeTx records writes and can hold a current item for the lock check.
type fakeTx struct {
	current      domain.Item
	lockErr      error
	failOn       string
	log          []string
	enqueued     []NewWork
	interests    []domain.Interest
	observations []domain.Observation
	advanced     *domain.Cursor
	finished     bool
}

func (t *fakeTx) fail(op string) error {
	if t.failOn == op {
		return errors.New("boom " + op)
	}
	t.log = append(t.log, op)
	return nil
}

func (t *fakeTx) Lock(context.Context, int64) (domain.Item, error) { return t.current, t.lockErr }
func (t *fakeTx) Enqueue(_ context.Context, w NewWork) error {
	t.enqueued = append(t.enqueued, w)
	return t.fail("enqueue")
}

func (t *fakeTx) Advance(_ context.Context, _ int64, c domain.Cursor) error {
	t.advanced = &c
	return t.fail("advance")
}

func (t *fakeTx) Finish(context.Context, int64) error {
	t.finished = true
	return t.fail("finish")
}

func (t *fakeTx) UpsertPeople(context.Context, []catalog.Person) error { return t.fail("people") }
func (t *fakeTx) UpsertPhotos(context.Context, []catalog.Photo) error  { return t.fail("photos") }
func (t *fakeTx) Add(_ context.Context, i domain.Interest) error {
	t.interests = append(t.interests, i)
	return t.fail("interest")
}

func (t *fakeTx) Record(_ context.Context, _ int64, _ int, o []domain.Observation) error {
	t.observations = append(t.observations, o...)
	return t.fail("observe")
}

func (t *fakeTx) Do(_ context.Context, fn func(Writers) error) error {
	return fn(Writers{Work: t, Catalog: t, Interests: t, Observations: t})
}

type fakeKeys struct {
	id  int64
	err error
}

func (k fakeKeys) Choose(context.Context, string, domain.Lane) (int64, error) { return k.id, k.err }

type fakePermit struct{ done []error }

func (p *fakePermit) Done(err error) error { p.done = append(p.done, err); return nil }

type fakeGovernor struct {
	permit *fakePermit
	wait   time.Duration
	err    error
}

func (g fakeGovernor) Acquire(context.Context, int64, domain.Lane) (Permit, time.Duration, error) {
	if g.permit == nil {
		return nil, g.wait, g.err
	}
	return g.permit, 0, g.err
}

type fakeFlickr struct {
	payload  json.RawMessage
	err      error
	requests []Request
}

func (f *fakeFlickr) Call(_ context.Context, r Request) (json.RawMessage, error) {
	f.requests = append(f.requests, r)
	return f.payload, f.err
}

type fakeHealth struct {
	reported []int64
	err      error
}

func (h *fakeHealth) ReportIPBlocked(_ context.Context, keyID int64, _ error) error {
	h.reported = append(h.reported, keyID)
	return h.err
}

type fakeSettings struct {
	max int
	err error
}

func (s fakeSettings) Effective(context.Context, int64) (settingsdomain.Effective, error) {
	return settingsdomain.Effective{MaxDepth: s.max}, s.err
}

type rig struct {
	engine  *Engine
	store   *fakeStore
	tx      *fakeTx
	facet   *fakeFacet
	flickr  *fakeFlickr
	permit  *fakePermit
	health  *fakeHealth
	favFace *fakeFacet
}

func newRig(t *testing.T) *rig {
	t.Helper()
	item := domain.Item{
		ID: 7, Kind: "photos", Subject: "100@N01", UserID: 3, Depth: 1, State: domain.StateReady, Attempts: 1,
		Cursor: domain.Cursor{Page: 2, Method: "flickr.people.getPhotos", Params: map[string]string{"user_id": "100@N01"}},
	}
	facet := &fakeFacet{name: "photos", lane: domain.LanePerson}
	other := &fakeFacet{name: "contacts", lane: domain.LanePerson, plan: []domain.WorkSpec{{Subject: "200@N01", Method: "flickr.contacts.getList"}, {Subject: "", Method: "x"}}}
	registry := NewRegistry()
	for _, f := range []*fakeFacet{facet, other} {
		if err := registry.Register(f); err != nil {
			t.Fatal(err)
		}
	}
	tx := &fakeTx{current: domain.Item{State: domain.StateRunning, Cursor: item.Cursor}}
	permit := &fakePermit{}
	health := &fakeHealth{}
	flk := &fakeFlickr{payload: json.RawMessage(`{"ok":true}`)}
	store := &fakeStore{item: item, started: true}
	engine := &Engine{
		Registry: registry, Store: store, Units: tx, Keys: fakeKeys{id: 9}, Governor: fakeGovernor{permit: permit},
		Flickr: flk, Health: health, Settings: fakeSettings{max: 3}, Now: func() time.Time { return epoch },
	}
	return &rig{engine: engine, store: store, tx: tx, facet: facet, flickr: flk, permit: permit, health: health, favFace: other}
}

func TestHandleSuccessCommitsPageAndAdvances(t *testing.T) {
	r := newRig(t)
	r.facet.result = domain.Result{
		People:       []catalog.Person{{NSID: "100@N01"}},
		Photos:       []catalog.Photo{{ID: 1}},
		Observations: []domain.Observation{{Kind: "photo.changed", Subject: "1"}},
		Discoveries:  []domain.Discovery{{NSID: "200@N01", Via: "contact"}},
		Work:         []domain.WorkSpec{{Subject: "100@N01", Method: "flickr.photos.getSizes"}},
		HasMore:      true,
	}
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	req := r.flickr.requests[0]
	if req.KeyID != 9 || req.UserID != 3 || req.Method != "flickr.people.getPhotos" || req.Params["page"] != "2" || req.Params["user_id"] != "100@N01" {
		t.Fatalf("request = %+v", req)
	}
	if len(r.permit.done) != 1 || r.permit.done[0] != nil {
		t.Fatalf("permit done = %v", r.permit.done)
	}
	if len(r.facet.pages) != 1 || r.facet.pages[0].Number != 2 || string(r.facet.pages[0].Payload) != `{"ok":true}` {
		t.Fatalf("pages = %+v", r.facet.pages)
	}
	if len(r.tx.interests) != 1 || r.tx.interests[0].Depth != 2 || r.tx.interests[0].UserID != 3 || r.tx.interests[0].Via != "contact" {
		t.Fatalf("interests = %+v", r.tx.interests)
	}
	// One planned contact (depth 2, kind stamped) plus the explicit sizes work at depth 1.
	if len(r.tx.enqueued) != 2 {
		t.Fatalf("enqueued = %+v", r.tx.enqueued)
	}
	planned, explicit := r.tx.enqueued[0], r.tx.enqueued[1]
	if planned.Kind != "contacts" || planned.Depth != 2 || planned.Cursor.Page != 1 || planned.Subject != "200@N01" {
		t.Fatalf("planned = %+v", planned)
	}
	if explicit.Kind != "photos" || explicit.Depth != 1 {
		t.Fatalf("explicit = %+v", explicit)
	}
	if len(r.tx.observations) != 1 || r.tx.advanced == nil || r.tx.advanced.Page != 3 || r.tx.finished {
		t.Fatalf("tx = %+v", r.tx)
	}
}

func TestHandleLastPageFinishes(t *testing.T) {
	r := newRig(t)
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if !r.tx.finished || r.tx.advanced != nil {
		t.Fatalf("finished=%v advanced=%v", r.tx.finished, r.tx.advanced)
	}
}

func TestHandleDiscoveriesBeyondMaxDepthAreIgnored(t *testing.T) {
	r := newRig(t)
	r.engine.Settings = fakeSettings{max: 1}
	r.facet.result = domain.Result{
		Photos:      []catalog.Photo{{ID: 1, OwnerNSID: "100@N01"}},
		Discoveries: []domain.Discovery{{NSID: "200@N01", Via: "favorite"}},
	}
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if len(r.tx.interests) != 0 || len(r.tx.enqueued) != 0 || !r.tx.finished {
		t.Fatalf("tx = %+v", r.tx)
	}
	if len(r.tx.log) == 0 || r.tx.log[0] != "photos" {
		t.Fatalf("max-depth page did not store its photos: %v", r.tx.log)
	}
}

func TestHandleSkipsStalePages(t *testing.T) {
	for name, current := range map[string]domain.Item{
		"finished":         {State: domain.StateDone, Cursor: domain.Cursor{Page: 2}},
		"already moved":    {State: domain.StateRunning, Cursor: domain.Cursor{Page: 3}},
		"reclaimed":        {State: domain.StatePending, Cursor: domain.Cursor{Page: 2}},
		"deferred away":    {State: domain.StatePending, Cursor: domain.Cursor{Page: 3}},
		"failed meanwhile": {State: domain.StateFailed, Cursor: domain.Cursor{Page: 2}},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.tx.current = current
			r.facet.result = domain.Result{People: []catalog.Person{{NSID: "x"}}, HasMore: true}
			if err := r.engine.Handle(t.Context(), 7); err != nil {
				t.Fatal(err)
			}
			if len(r.tx.log) != 0 || r.tx.finished || r.tx.advanced != nil {
				t.Fatalf("stale page wrote: %+v", r.tx)
			}
		})
	}
}

func TestHandleSkipsStaleProbeWithSamePageNumber(t *testing.T) {
	r := newRig(t)
	r.tx.current = r.store.item
	r.tx.current.Cursor.Method = "flickr.favorites.getList"
	r.facet.result = domain.Result{People: []catalog.Person{{NSID: "x"}}}
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if len(r.tx.log) != 0 || r.tx.finished || r.tx.advanced != nil {
		t.Fatalf("stale probe wrote: %+v", r.tx)
	}
}

func TestHandleSkipsFinishedItems(t *testing.T) {
	for _, state := range []string{domain.StateDone, domain.StateFailed} {
		r := newRig(t)
		r.store.item.State = state
		if err := r.engine.Handle(t.Context(), 7); err != nil {
			t.Fatal(err)
		}
		if len(r.flickr.requests) != 0 || len(r.store.calls) != 0 {
			t.Fatalf("state %s ran: %v", state, r.store.calls)
		}
	}
}

func TestHandleMissingAndUnrunnableItems(t *testing.T) {
	r := newRig(t)
	r.store.loadErr = ErrItemNotFound
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatalf("missing item: %v", err)
	}
	r = newRig(t)
	r.store.started = false
	if err := r.engine.Handle(t.Context(), 7); err != nil || len(r.flickr.requests) != 0 {
		t.Fatalf("unrunnable item: %v", err)
	}
	r = newRig(t)
	r.store.item.Kind = "nope"
	if err := r.engine.Handle(t.Context(), 7); err != nil || r.store.calls[0] != "fail" {
		t.Fatalf("unknown facet: %v %v", err, r.store.calls)
	}
	r = newRig(t)
	r.store.loadErr = errors.New("db down")
	if err := r.engine.Handle(t.Context(), 7); err == nil {
		t.Fatal("load error must surface")
	}
	r = newRig(t)
	r.store.startErr = errors.New("db down")
	if err := r.engine.Handle(t.Context(), 7); err == nil {
		t.Fatal("start error must surface")
	}
}

func TestHandleErrorOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		call    string
		consume bool
		wait    time.Duration
		blocked bool
	}{
		{name: "quota waits without an attempt", err: flickr.ErrQuotaExceeded, call: "defer", wait: time.Minute},
		{name: "not found is done", err: flickr.ErrNotFound, call: "finish"},
		{name: "revoked token stops", err: flickr.ErrTokenRevoked, call: "stop"},
		{name: "invalid key stops", err: flickr.ErrInvalidKey, call: "stop"},
		{name: "ip blocked is reported and requeued", err: flickr.ErrIPBlocked, call: "defer", wait: 5 * time.Minute, blocked: true},
		{name: "transient backs off", err: flickr.ErrTransient, call: "defer", consume: true, wait: 60 * time.Second},
		{name: "wrapped transient backs off", err: fmt.Errorf("wrap: %w", flickr.ErrServiceUnavailable), call: "defer", consume: true, wait: 60 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.flickr.err = tc.err
			if err := r.engine.Handle(t.Context(), 7); err != nil {
				t.Fatal(err)
			}
			if got := r.store.calls[len(r.store.calls)-1]; got != tc.call {
				t.Fatalf("last call = %s, want %s", got, tc.call)
			}
			if tc.call == "defer" {
				d := r.store.deferral
				if d.ConsumeAttempt != tc.consume || !d.NotBefore.Equal(epoch.Add(tc.wait)) {
					t.Fatalf("deferral = %+v", d)
				}
			}
			if (len(r.health.reported) == 1) != tc.blocked {
				t.Fatalf("health reports = %v", r.health.reported)
			}
			if len(r.permit.done) != 1 || !errors.Is(r.permit.done[0], tc.err) {
				t.Fatalf("permit done = %v", r.permit.done)
			}
			if len(r.tx.log) != 0 {
				t.Fatalf("failed call must not commit: %v", r.tx.log)
			}
		})
	}
}

func TestHandleStopCarriesReason(t *testing.T) {
	r := newRig(t)
	r.flickr.err = flickr.ErrInvalidKey
	_ = r.engine.Handle(t.Context(), 7)
	if r.store.stop != (StopRequest{ItemID: 7, UserID: 3, KeyID: 9, Reason: StopInvalidKey}) {
		t.Fatalf("stop = %+v", r.store.stop)
	}
	r = newRig(t)
	r.flickr.err = flickr.ErrTokenRevoked
	_ = r.engine.Handle(t.Context(), 7)
	if r.store.stop.Reason != StopTokenRevoked {
		t.Fatalf("stop = %+v", r.store.stop)
	}
}

func TestHandleTransientFailsAfterMaxAttempts(t *testing.T) {
	r := newRig(t)
	r.engine.Timing = Timing{MaxAttempts: 2}
	r.flickr.err = flickr.ErrTransient
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	if r.store.calls[len(r.store.calls)-1] != "fail" {
		t.Fatalf("calls = %v", r.store.calls)
	}
}

func TestHandleBlockedWithoutHealthPort(t *testing.T) {
	r := newRig(t)
	r.engine.Health = nil
	r.flickr.err = flickr.ErrIPBlocked
	if err := r.engine.Handle(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	r = newRig(t)
	r.health.err = errors.New("health down")
	r.flickr.err = flickr.ErrIPBlocked
	if err := r.engine.Handle(t.Context(), 7); err != nil || r.store.calls[len(r.store.calls)-1] != "defer" {
		t.Fatalf("health failure must not block requeue: %v", err)
	}
}

func TestHandleFacetErrorUsesOutcomeMapping(t *testing.T) {
	r := newRig(t)
	r.facet.err = flickr.ErrNotFound
	if err := r.engine.Handle(t.Context(), 7); err != nil || r.store.calls[len(r.store.calls)-1] != "finish" {
		t.Fatalf("facet not found: %v %v", err, r.store.calls)
	}
	r = newRig(t)
	r.facet.err = errors.New("bad payload")
	if err := r.engine.Handle(t.Context(), 7); err != nil || !r.store.deferral.ConsumeAttempt {
		t.Fatalf("facet error: %v %+v", err, r.store.deferral)
	}
}

func TestHandleWaitsWithoutKeyOrPermit(t *testing.T) {
	r := newRig(t)
	r.engine.Keys = fakeKeys{err: ErrNoKey}
	if err := r.engine.Handle(t.Context(), 7); err != nil || r.store.deferral.ConsumeAttempt || len(r.flickr.requests) != 0 {
		t.Fatalf("no key: %v %+v", err, r.store.deferral)
	}
	r = newRig(t)
	r.engine.Governor = fakeGovernor{wait: 40 * time.Second}
	if err := r.engine.Handle(t.Context(), 7); err != nil || !r.store.deferral.NotBefore.Equal(epoch.Add(40*time.Second)) || r.store.deferral.ConsumeAttempt {
		t.Fatalf("no permit: %v %+v", err, r.store.deferral)
	}
	r = newRig(t)
	r.engine.Governor = fakeGovernor{err: errors.New("redis down")}
	if err := r.engine.Handle(t.Context(), 7); err != nil || r.store.deferral.ConsumeAttempt || len(r.flickr.requests) != 0 {
		t.Fatalf("governor error: %v %+v", err, r.store.deferral)
	}
	r = newRig(t)
	r.engine.Keys = fakeKeys{err: errors.New("db down")}
	if err := r.engine.Handle(t.Context(), 7); err == nil {
		t.Fatal("key lookup failure must surface")
	}
}

func TestHandleCommitFailuresSurface(t *testing.T) {
	for _, op := range []string{"people", "photos", "observe", "interest", "enqueue", "advance", "finish"} {
		t.Run(op, func(t *testing.T) {
			r := newRig(t)
			r.tx.failOn = op
			r.facet.result = domain.Result{
				People: []catalog.Person{{NSID: "a"}}, Photos: []catalog.Photo{{ID: 1}},
				Observations: []domain.Observation{{Kind: "k"}},
				Discoveries:  []domain.Discovery{{NSID: "200@N01", Via: "contact"}},
				Work:         []domain.WorkSpec{{Subject: "s", Method: "m"}},
				HasMore:      op == "advance",
			}
			if err := r.engine.Handle(t.Context(), 7); err == nil {
				t.Fatal("commit failure must surface so the job runs again")
			}
		})
	}
	r := newRig(t)
	r.tx.lockErr = errors.New("lock")
	if err := r.engine.Handle(t.Context(), 7); err == nil {
		t.Fatal("lock failure must surface")
	}
	r = newRig(t)
	r.engine.Settings = fakeSettings{err: errors.New("settings")}
	r.facet.result = domain.Result{Discoveries: []domain.Discovery{{NSID: "x", Via: "v"}}}
	if err := r.engine.Handle(t.Context(), 7); err == nil {
		t.Fatal("settings failure must surface")
	}
}

func TestHandleRejectsBadFacetOutput(t *testing.T) {
	r := newRig(t)
	r.facet.result = domain.Result{Work: []domain.WorkSpec{{Kind: "ghost", Subject: "s", Method: "m"}}}
	if err := r.engine.Handle(t.Context(), 7); err == nil {
		t.Fatal("unknown facet work must be rejected")
	}
	r = newRig(t)
	r.facet.result = domain.Result{Work: []domain.WorkSpec{{Subject: "", Method: "m"}}}
	if err := r.engine.Handle(t.Context(), 7); err == nil {
		t.Fatal("invalid spec must be rejected")
	}
	r = newRig(t)
	r.facet.result = domain.Result{Discoveries: []domain.Discovery{{NSID: "", Via: "v"}}}
	if err := r.engine.Handle(t.Context(), 7); err == nil {
		t.Fatal("invalid discovery must be rejected")
	}
}

func TestHandleWithoutUserSkipsDiscoveries(t *testing.T) {
	r := newRig(t)
	r.store.item.UserID = 0
	r.facet.result = domain.Result{Discoveries: []domain.Discovery{{NSID: "200@N01", Via: "contact"}}}
	if err := r.engine.Handle(t.Context(), 7); err != nil || len(r.tx.interests) != 0 {
		t.Fatalf("err=%v interests=%v", err, r.tx.interests)
	}
}

func TestHandleValidatesDependencies(t *testing.T) {
	if err := (&Engine{}).Handle(t.Context(), 1); !errors.Is(err, ErrEngineIncomplete) {
		t.Fatalf("err = %v", err)
	}
	r := newRig(t)
	//nolint:staticcheck // a nil context is the case under test
	if err := r.engine.Handle(nil, 1); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.engine.Handle(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseJobArgs(t *testing.T) {
	id, err := ParseJobArgs([]byte(`{"work_item_id":42,"kind":"photos"}`))
	if err != nil || id != 42 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	for _, bad := range []string{``, `{}`, `{"work_item_id":0}`, `nope`} {
		if _, err := ParseJobArgs([]byte(bad)); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestDefaultsAndDelays(t *testing.T) {
	e := &Engine{}
	if e.logger() == nil || e.now().IsZero() {
		t.Fatal("defaults missing")
	}
	timing := Timing{}.withDefaults()
	if timing != DefaultTiming {
		t.Fatalf("timing = %+v", timing)
	}
	if timing.retryDelay(0) != 30*time.Second || timing.retryDelay(2) != 2*time.Minute {
		t.Fatal("backoff must double per attempt")
	}
}
