package http

import (
	"context"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type serviceStub struct {
	status    app.CrawlStatus
	err       error
	calls     int
	user      int64
	nsid      string
	starred   bool
	operation string
}

func (s *serviceStub) Status(_ context.Context, u int64) (app.CrawlStatus, error) {
	s.calls++
	s.user = u
	return s.status, s.err
}

func (s *serviceStub) Star(_ context.Context, u int64, n string, v bool) error {
	s.record("star", u, n, v)
	return s.err
}

func (s *serviceStub) Select(_ context.Context, u int64, n string) error {
	s.record("select", u, n, false)
	return s.err
}

func (s *serviceStub) Pause(_ context.Context, u int64, n string) error {
	s.record("pause", u, n, false)
	return s.err
}

func (s *serviceStub) Resume(_ context.Context, u int64, n string) error {
	s.record("resume", u, n, false)
	return s.err
}

func (s *serviceStub) record(op string, u int64, n string, b bool) {
	s.calls++
	s.operation = op
	s.user = u
	s.nsid = n
	s.starred = b
}
func testActor(context.Context) (int64, bool) { return 9, true }

func TestGetMeCrawl(t *testing.T) {
	if _, err := (CrawlHandler{}).GetMeCrawl(context.Background(), gen.GetMeCrawlRequestObject{}); err != nil {
		t.Fatal(err)
	}
	h := CrawlHandler{ActorID: testActor}
	if _, err := h.GetMeCrawl(context.Background(), gen.GetMeCrawlRequestObject{}); err == nil {
		t.Fatal("missing service accepted")
	}
	s := &serviceStub{status: app.CrawlStatus{Depths: []app.DepthCount{{Depth: 2, Contacts: 4}}, Facets: []app.FacetProgress{{Facet: "favorites", Total: 3, Done: 1, Active: 1, Failed: 1}}, Lanes: []app.LaneStatus{{Lane: "person", Pending: 2, Running: 1, Waiting: 3, Paused: 1}}}}
	h.Controls = s
	r, err := h.GetMeCrawl(context.Background(), gen.GetMeCrawlRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	got := r.(gen.GetMeCrawl200JSONResponse)
	if s.user != 9 || len(got.Depths) != 1 || got.Depths[0].Contacts != 4 || got.Facets[0].Done != 1 || got.Lanes[0].Paused != 1 {
		t.Fatalf("status=%+v", got)
	}
	s.err = errors.New("store error")
	if _, err := h.GetMeCrawl(context.Background(), gen.GetMeCrawlRequestObject{}); err == nil {
		t.Fatal("service error swallowed")
	}
}

func TestContactControls(t *testing.T) {
	ctx := context.Background()
	h := CrawlHandler{ActorID: testActor, Controls: &serviceStub{}}
	s := h.Controls.(*serviceStub)
	if _, err := h.StarMyContact(ctx, gen.StarMyContactRequestObject{Nsid: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.StarMyContact(ctx, gen.StarMyContactRequestObject{Nsid: " a ", Body: &gen.StarContactRequest{Starred: true}}); err != nil || s.nsid != "a" || !s.starred || s.operation != "star" {
		t.Fatalf("star result err=%v state=%+v", err, s)
	}
	if _, err := h.SelectMyContact(ctx, gen.SelectMyContactRequestObject{Nsid: "a"}); err != nil || s.operation != "select" {
		t.Fatalf("select err=%v state=%+v", err, s)
	}
	if _, err := h.PauseCrawlContact(ctx, gen.PauseCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: "a"}}); err != nil || s.operation != "pause" {
		t.Fatalf("pause err=%v state=%+v", err, s)
	}
	if _, err := h.ResumeCrawlContact(ctx, gen.ResumeCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: "a"}}); err != nil || s.operation != "resume" {
		t.Fatalf("resume err=%v state=%+v", err, s)
	}
	if response, err := h.PauseCrawlContact(ctx, gen.PauseCrawlContactRequestObject{}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.PauseCrawlContact400JSONResponse); !ok {
		t.Fatal("missing pause body was not rejected")
	}
	if response, err := h.ResumeCrawlContact(ctx, gen.ResumeCrawlContactRequestObject{}); err != nil {
		t.Fatal(err)
	} else if _, ok := response.(gen.ResumeCrawlContact400JSONResponse); !ok {
		t.Fatal("missing resume body was not rejected")
	}
	s.err = app.ErrPersonNotFound
	if _, err := h.SelectMyContact(ctx, gen.SelectMyContactRequestObject{Nsid: "a"}); err != nil {
		t.Fatal(err)
	}
	s.err = errors.New("store")
	if _, err := h.SelectMyContact(ctx, gen.SelectMyContactRequestObject{Nsid: "a"}); err == nil {
		t.Fatal("service error swallowed")
	}
	for _, nsid := range []string{"", " \t"} {
		if response, err := h.SelectMyContact(ctx, gen.SelectMyContactRequestObject{Nsid: nsid}); err != nil {
			t.Fatal(err)
		} else if _, ok := response.(gen.SelectMyContact400JSONResponse); !ok {
			t.Fatalf("select nsid %q not rejected: %T", nsid, response)
		}
		if response, err := h.StarMyContact(ctx, gen.StarMyContactRequestObject{Nsid: nsid, Body: &gen.StarContactRequest{}}); err != nil {
			t.Fatal(err)
		} else if _, ok := response.(gen.StarMyContact400JSONResponse); !ok {
			t.Fatalf("star nsid %q not rejected: %T", nsid, response)
		}
		if response, err := h.PauseCrawlContact(ctx, gen.PauseCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: nsid}}); err != nil {
			t.Fatal(err)
		} else if _, ok := response.(gen.PauseCrawlContact400JSONResponse); !ok {
			t.Fatalf("pause nsid %q not rejected: %T", nsid, response)
		}
		if response, err := h.ResumeCrawlContact(ctx, gen.ResumeCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: nsid}}); err != nil {
			t.Fatal(err)
		} else if _, ok := response.(gen.ResumeCrawlContact400JSONResponse); !ok {
			t.Fatalf("resume nsid %q not rejected: %T", nsid, response)
		}
	}
	if _, err := (CrawlHandler{ActorID: testActor}).PauseCrawlContact(ctx, gen.PauseCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: "a"}}); err == nil {
		t.Fatal("missing service accepted")
	}
	if _, err := (CrawlHandler{ActorID: testActor}).StarMyContact(ctx, gen.StarMyContactRequestObject{Nsid: "a", Body: &gen.StarContactRequest{}}); err == nil {
		t.Fatal("missing service accepted for star")
	}
}

func TestContactControlAuthenticationAndNotFound(t *testing.T) {
	ctx := context.Background()
	unauth := CrawlHandler{}
	if _, err := unauth.StarMyContact(ctx, gen.StarMyContactRequestObject{Nsid: "a", Body: &gen.StarContactRequest{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := unauth.SelectMyContact(ctx, gen.SelectMyContactRequestObject{Nsid: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := unauth.PauseCrawlContact(ctx, gen.PauseCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: "a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := unauth.ResumeCrawlContact(ctx, gen.ResumeCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: "a"}}); err != nil {
		t.Fatal(err)
	}
	s := &serviceStub{err: app.ErrPersonNotFound}
	h := CrawlHandler{ActorID: testActor, Controls: s}
	if _, err := h.StarMyContact(ctx, gen.StarMyContactRequestObject{Nsid: "a", Body: &gen.StarContactRequest{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.PauseCrawlContact(ctx, gen.PauseCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: "a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ResumeCrawlContact(ctx, gen.ResumeCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: "a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SelectMyContact(ctx, gen.SelectMyContactRequestObject{Nsid: "a"}); err != nil {
		t.Fatal(err)
	}
	for _, call := range []func() error{
		func() error {
			_, e := (CrawlHandler{ActorID: testActor}).StarMyContact(ctx, gen.StarMyContactRequestObject{Nsid: "a", Body: &gen.StarContactRequest{}})
			return e
		},
		func() error {
			_, e := (CrawlHandler{ActorID: testActor}).SelectMyContact(ctx, gen.SelectMyContactRequestObject{Nsid: "a"})
			return e
		},
		func() error {
			_, e := (CrawlHandler{ActorID: testActor}).PauseCrawlContact(ctx, gen.PauseCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: "a"}})
			return e
		},
		func() error {
			_, e := (CrawlHandler{ActorID: testActor}).ResumeCrawlContact(ctx, gen.ResumeCrawlContactRequestObject{Body: &gen.CrawlContactRequest{Nsid: "a"}})
			return e
		},
	} {
		if err := call(); err == nil {
			t.Fatal("missing service accepted")
		}
	}
}

type depthStub struct {
	policy app.DepthPolicy
	err    error
	set    int
}

func (d *depthStub) Get(context.Context, int64) (app.DepthPolicy, error) { return d.policy, d.err }

func (d *depthStub) Set(_ context.Context, _ int64, depth int) (app.DepthPolicy, error) {
	d.set = depth
	return d.policy, d.err
}

func TestCrawlSettingsRequireAUserAndService(t *testing.T) {
	if r, err := (CrawlHandler{}).GetMeCrawlSettings(context.Background(), gen.GetMeCrawlSettingsRequestObject{}); err != nil || r == nil {
		t.Fatalf("get without actor = %v, %v", r, err)
	} else if _, ok := r.(gen.GetMeCrawlSettings401JSONResponse); !ok {
		t.Fatalf("get without actor = %T", r)
	}
	if r, _ := (CrawlHandler{}).UpdateMeCrawlSettings(context.Background(), gen.UpdateMeCrawlSettingsRequestObject{}); r == nil {
		t.Fatal("update without actor returned nil")
	} else if _, ok := r.(gen.UpdateMeCrawlSettings401JSONResponse); !ok {
		t.Fatalf("update without actor = %T", r)
	}
	handler := CrawlHandler{ActorID: testActor}
	if _, err := handler.GetMeCrawlSettings(context.Background(), gen.GetMeCrawlSettingsRequestObject{}); err == nil {
		t.Fatal("get accepted a missing depth service")
	}
	if _, err := handler.UpdateMeCrawlSettings(context.Background(), gen.UpdateMeCrawlSettingsRequestObject{}); err == nil {
		t.Fatal("update accepted a missing depth service")
	}
}

func TestCrawlSettingsReadAndWrite(t *testing.T) {
	stub := &depthStub{policy: app.DepthPolicy{Depth: 3, Default: 2, Max: 4}}
	handler := CrawlHandler{ActorID: testActor, Depth: stub}
	got, err := handler.GetMeCrawlSettings(context.Background(), gen.GetMeCrawlSettingsRequestObject{})
	body, ok := got.(gen.GetMeCrawlSettings200JSONResponse)
	if err != nil || !ok || body.Depth != 3 || body.DepthDefault != 2 || body.DepthMax != 4 {
		t.Fatalf("get = %#v, %v", got, err)
	}
	put, err := handler.UpdateMeCrawlSettings(context.Background(), gen.UpdateMeCrawlSettingsRequestObject{Body: &gen.UpdateCrawlSettingsRequest{Depth: 3}})
	if _, ok := put.(gen.UpdateMeCrawlSettings200JSONResponse); err != nil || !ok || stub.set != 3 {
		t.Fatalf("put = %#v, %v", put, err)
	}
	if r, _ := handler.UpdateMeCrawlSettings(context.Background(), gen.UpdateMeCrawlSettingsRequestObject{}); r == nil {
		t.Fatal("nil body returned nil")
	} else if _, ok := r.(gen.UpdateMeCrawlSettings400JSONResponse); !ok {
		t.Fatalf("nil body = %T", r)
	}
}

func TestCrawlSettingsMapErrors(t *testing.T) {
	stub := &depthStub{policy: app.DepthPolicy{Max: 4}}
	handler := CrawlHandler{ActorID: testActor, Depth: stub}
	request := gen.UpdateMeCrawlSettingsRequestObject{Body: &gen.UpdateCrawlSettingsRequest{Depth: 9}}
	stub.err = app.ErrDepthAboveCap
	r, _ := handler.UpdateMeCrawlSettings(context.Background(), request)
	capped, ok := r.(gen.UpdateMeCrawlSettings422JSONResponse)
	if !ok || capped.Code != "depth_above_cap" || (*capped.Details)["max_depth"] != 4 {
		t.Fatalf("cap = %#v", r)
	}
	stub.err = app.ErrDepthInvalid
	if r, _ = handler.UpdateMeCrawlSettings(context.Background(), request); r == nil {
		t.Fatal("invalid returned nil")
	} else if _, ok := r.(gen.UpdateMeCrawlSettings400JSONResponse); !ok {
		t.Fatalf("invalid = %T", r)
	}
	stub.err = errors.New("down")
	if _, err := handler.UpdateMeCrawlSettings(context.Background(), request); err == nil {
		t.Fatal("update swallowed a store failure")
	}
	if _, err := handler.GetMeCrawlSettings(context.Background(), gen.GetMeCrawlSettingsRequestObject{}); err == nil {
		t.Fatal("get swallowed a store failure")
	}
}
