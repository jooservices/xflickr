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
	if _, err := h.SelectMyContact(ctx, gen.SelectMyContactRequestObject{Nsid: " "}); !errors.Is(err, app.ErrInvalidPerson) {
		t.Fatalf("blank nsid error=%v", err)
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
