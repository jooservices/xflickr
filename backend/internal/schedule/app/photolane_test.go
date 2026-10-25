package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
)

type fakeSelector struct {
	refs  []catalogapp.PhotoRef
	err   error
	calls int
}

func (s *fakeSelector) Select(_ context.Context, limit, skip int) ([]catalogapp.PhotoRef, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if skip >= len(s.refs) {
		return nil, nil
	}
	return s.refs[skip:min(len(s.refs), skip+limit)], nil
}

type fakeChooser struct {
	noKey map[string]bool
	err   error
}

func (c fakeChooser) Choose(_ context.Context, nsid string, class quotadomain.Class) (quotaapp.KeyID, error) {
	if class != quotadomain.Photo {
		return 0, errors.New("wrong class")
	}
	if c.err != nil {
		return 0, c.err
	}
	if c.noKey[nsid] {
		return 0, quotaapp.ErrNoKey
	}
	return 42, nil
}

type fakeEnqueuer struct {
	queued map[string]bool
	jobs   []PhotoJob
	err    error
}

func (e *fakeEnqueuer) EnqueuePhoto(_ context.Context, job PhotoJob) (bool, error) {
	if e.err != nil {
		return false, e.err
	}
	if e.queued[job.DedupeKey] {
		return false, nil
	}
	e.jobs = append(e.jobs, job)
	return true, nil
}

func refs(ids ...int64) []catalogapp.PhotoRef {
	out := make([]catalogapp.PhotoRef, len(ids))
	for i, id := range ids {
		out[i] = catalogapp.PhotoRef{ID: id, OwnerNSID: "owner"}
	}
	return out
}

func TestPhotoLaneNextEnqueuesInOrderWithKeyAndDedupeKey(t *testing.T) {
	enqueuer := &fakeEnqueuer{}
	lane := PhotoLane{Selector: &fakeSelector{refs: refs(5, 3, 9)}, Keys: fakeChooser{}, Queue: enqueuer}
	ids, err := lane.Next(t.Context(), 2)
	if err != nil || !reflect.DeepEqual(ids, []int64{5, 3}) {
		t.Fatalf("Next() = %v, %v, want [5 3]", ids, err)
	}
	job := enqueuer.jobs[0]
	if job.KeyID != 42 || job.Class != quotadomain.Photo || job.Facet != "sizes" || job.DedupeKey != "photo:5:sizes" {
		t.Fatalf("job = %+v", job)
	}
}

func TestPhotoLaneNextSkipsQueuedAndKeylessPhotosAndPages(t *testing.T) {
	selector := &fakeSelector{refs: []catalogapp.PhotoRef{{ID: 1, OwnerNSID: "owner"}, {ID: 2, OwnerNSID: "nokey"}, {ID: 3, OwnerNSID: "owner"}, {ID: 4, OwnerNSID: "owner"}}}
	enqueuer := &fakeEnqueuer{queued: map[string]bool{"photo:1:sizes": true}}
	lane := PhotoLane{Selector: selector, Keys: fakeChooser{noKey: map[string]bool{"nokey": true}}, Queue: enqueuer}
	ids, err := lane.Next(t.Context(), 2)
	if err != nil || !reflect.DeepEqual(ids, []int64{3, 4}) {
		t.Fatalf("Next() = %v, %v, want [3 4]", ids, err)
	}
	if selector.calls < 2 {
		t.Fatalf("selector calls = %d, want paging past skipped photos", selector.calls)
	}
}

func TestPhotoLaneNextStopsWhenBacklogIsDrainedOrPagesRunOut(t *testing.T) {
	lane := PhotoLane{Selector: &fakeSelector{}, Keys: fakeChooser{}, Queue: &fakeEnqueuer{}}
	if ids, err := lane.Next(t.Context(), 3); err != nil || len(ids) != 0 {
		t.Fatalf("Next() on empty catalog = %v, %v", ids, err)
	}
	all := make([]int64, 100)
	queued := map[string]bool{}
	for i := range all {
		all[i] = int64(i + 1)
		queued[PhotoDedupeKey(all[i], PhotoFacet)] = true
	}
	selector := &fakeSelector{refs: refs(all...)}
	lane = PhotoLane{Selector: selector, Keys: fakeChooser{}, Queue: &fakeEnqueuer{queued: queued}}
	if ids, err := lane.Next(t.Context(), 2); err != nil || len(ids) != 0 || selector.calls != maxPhotoPages {
		t.Fatalf("Next() = %v, %v after %d selects, want none after %d", ids, err, selector.calls, maxPhotoPages)
	}
}

func TestPhotoLaneNextValidatesAndPropagatesErrors(t *testing.T) {
	if _, err := (PhotoLane{}).Next(t.Context(), 1); err == nil {
		t.Fatal("Next() without dependencies succeeded")
	}
	good := PhotoLane{Selector: &fakeSelector{refs: refs(1)}, Keys: fakeChooser{}, Queue: &fakeEnqueuer{}}
	if ids, err := good.Next(t.Context(), 0); err != nil || ids != nil {
		t.Fatalf("Next(0) = %v, %v", ids, err)
	}
	boom := errors.New("boom")
	cases := map[string]PhotoLane{
		"select":  {Selector: &fakeSelector{err: boom}, Keys: fakeChooser{}, Queue: &fakeEnqueuer{}},
		"choose":  {Selector: &fakeSelector{refs: refs(1)}, Keys: fakeChooser{err: boom}, Queue: &fakeEnqueuer{}},
		"enqueue": {Selector: &fakeSelector{refs: refs(1)}, Keys: fakeChooser{}, Queue: &fakeEnqueuer{err: boom}},
	}
	for name, lane := range cases {
		if _, err := lane.Next(t.Context(), 1); !errors.Is(err, boom) {
			t.Fatalf("%s: Next() error = %v, want boom", name, err)
		}
	}
}
