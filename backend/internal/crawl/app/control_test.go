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

func TestControlsOperations(t *testing.T) {
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	repo := &controlsStub{status: CrawlStatus{Depths: []DepthCount{{Depth: 1, Contacts: 2}}}}
	svc := Controls{Repository: repo, Now: func() time.Time { return now }}
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
	}{{"repository", Controls{}, ctx, 1, "", ErrControlRepositoryRequired}, {"nil context", Controls{Repository: repo}, nil, 1, "", ErrContextRequired}, {"canceled", Controls{Repository: repo}, canceledContext(), 1, "", context.Canceled}, {"user", Controls{Repository: repo}, ctx, 0, "", nil}, {"person", Controls{Repository: repo}, ctx, 1, " ", ErrInvalidPerson}} {
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

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
