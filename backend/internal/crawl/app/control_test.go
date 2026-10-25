package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type controlsStub struct {
	status          CrawlStatus
	err             error
	action          string
	user            int64
	person          string
	until           time.Time
	starred, paused bool
}

func (s *controlsStub) Status(_ context.Context, u int64) (CrawlStatus, error) {
	s.user = u
	return s.status, s.err
}

func (s *controlsStub) SetStarred(_ context.Context, u int64, p string, v bool) error {
	s.user, s.person, s.starred, s.action = u, p, v, "star"
	return s.err
}

func (s *controlsStub) Select(_ context.Context, u int64, p string, until time.Time) error {
	s.user, s.person, s.until, s.action = u, p, until, "select"
	return s.err
}

func (s *controlsStub) SetPaused(_ context.Context, u int64, p string, v bool) error {
	s.user, s.person, s.paused, s.action = u, p, v, "pause"
	return s.err
}

type rescorerStub struct {
	user   int64
	person string
	calls  int
	err    error
}

func (s *rescorerStub) RescorePerson(_ context.Context, u int64, p string) error {
	s.user, s.person, s.calls = u, p, s.calls+1
	return s.err
}

func TestControlsRescoreBacklog(t *testing.T) {
	repo := &controlsStub{}
	rescore := &rescorerStub{}
	svc := Controls{Repository: repo, Rescorer: rescore}
	if err := svc.Star(context.Background(), 3, " p ", true); err != nil || rescore.calls != 1 || rescore.user != 3 || rescore.person != "p" {
		t.Fatalf("star rescore=%+v err=%v", rescore, err)
	}
	if err := svc.Select(context.Background(), 3, "p"); err != nil || rescore.calls != 2 {
		t.Fatalf("select rescore=%+v err=%v", rescore, err)
	}
	repo.err = ErrPersonNotFound
	if err := svc.Star(context.Background(), 3, "p", true); !errors.Is(err, ErrPersonNotFound) || rescore.calls != 2 {
		t.Fatalf("missing person rescored calls=%d err=%v", rescore.calls, err)
	}
	repo.err = nil
	rescore.err = errors.New("rescore failed")
	if err := svc.Select(context.Background(), 3, "p"); !errors.Is(err, rescore.err) {
		t.Fatalf("rescore error=%v", err)
	}
}

func TestControlsOperations(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	repo := &controlsStub{status: CrawlStatus{Depths: []DepthCount{{Depth: 1, Contacts: 2}}}}
	rescore := &rescorerStub{}
	svc := Controls{Repository: repo, Rescorer: rescore, Now: func() time.Time { return now }}
	got, err := svc.Status(context.Background(), 7)
	if err != nil || got.Depths[0].Contacts != 2 || repo.user != 7 {
		t.Fatalf("status=%+v err=%v", got, err)
	}
	if err := svc.Star(context.Background(), 7, " person ", true); err != nil || repo.person != "person" || !repo.starred {
		t.Fatalf("star repo=%+v err=%v", repo, err)
	}
	if err := svc.Select(context.Background(), 7, "person"); err != nil || !repo.until.Equal(now.Add(7*24*time.Hour)) {
		t.Fatalf("selection until=%v err=%v", repo.until, err)
	}
	if err := svc.Pause(context.Background(), 7, "person"); err != nil || repo.action != "pause" || !repo.paused {
		t.Fatalf("pause repo=%+v err=%v", repo, err)
	}
	if err := svc.Resume(context.Background(), 7, "person"); err != nil || repo.action != "pause" || repo.paused {
		t.Fatalf("resume repo=%+v err=%v", repo, err)
	}
	repo.err = errors.New("store failed")
	if _, err := svc.Status(context.Background(), 7); err == nil {
		t.Fatal("status error swallowed")
	}
	if err := svc.Star(context.Background(), 7, "person", false); err == nil {
		t.Fatal("star error swallowed")
	}
	if err := svc.Select(context.Background(), 7, "person"); err == nil {
		t.Fatal("select error swallowed")
	}
	if err := svc.Pause(context.Background(), 7, "person"); err == nil {
		t.Fatal("pause error swallowed")
	}
}

func TestControlsValidation(t *testing.T) {
	ctx := context.Background()
	repo := &controlsStub{}
	for _, tc := range []struct {
		name   string
		svc    Controls
		ctx    context.Context
		user   int64
		person string
		want   error
	}{{"repository", Controls{}, ctx, 1, "", ErrControlRepositoryRequired}, {"nil context", Controls{Repository: repo, Rescorer: rescore}, nil, 1, "", ErrContextRequired}, {"canceled", Controls{Repository: repo, Rescorer: rescore}, canceledContext(), 1, "", context.Canceled}, {"user", Controls{Repository: repo, Rescorer: rescore}, ctx, 0, "", nil}, {"person", Controls{Repository: repo, Rescorer: rescore}, ctx, 1, " ", ErrInvalidPerson}, {"rescorer", Controls{Repository: repo}, ctx, 1, "", ErrRescorerRequired}} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.svc.ready(tc.ctx, tc.user, tc.person)
			if tc.name == "user" {
				if err == nil {
					t.Fatal("invalid user accepted")
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want %v", err, tc.want)
			}
		})
	}
}

var rescore = &rescorerStub{}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
