package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type laneRows struct {
	pgx.Rows
	ids     []int64 // a zero id scans as a NULL photo
	more    bool
	pos     int
	scanErr error
	err     error
}

func (r *laneRows) Next() bool { r.pos++; return r.pos <= len(r.ids) }
func (r *laneRows) Close()     {}
func (r *laneRows) Err() error { return r.err }
func (r *laneRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	*dest[0].(*bool) = r.more
	if id := r.ids[r.pos-1]; id != 0 {
		owner := "owner"
		*dest[1].(**int64) = &id
		*dest[2].(**string) = &owner
	}
	return nil
}

// lanePool answers each probe with the next scripted result.
type lanePool struct {
	results  []*laneRows
	queryErr error
	args     [][]any
}

func (p *lanePool) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	p.args = append(p.args, args)
	if p.queryErr != nil {
		return nil, p.queryErr
	}
	rows := p.results[min(len(p.args), len(p.results))-1]
	rows.pos = 0
	return rows, nil
}

func TestPhotoLaneSelectValidatesInput(t *testing.T) {
	if _, err := (PhotoLane{}).Select(t.Context(), 1, 0); err == nil {
		t.Fatal("Select() without a pool succeeded")
	}
	lane := PhotoLane{Pool: &lanePool{}}
	if _, err := lane.Select(t.Context(), 0, 0); err == nil {
		t.Fatal("Select() with zero limit succeeded")
	}
	if _, err := lane.Select(t.Context(), 1, -1); err == nil {
		t.Fatal("Select() with negative skip succeeded")
	}
}

func TestPhotoLaneSelectSkipsAndStopsWhenFilled(t *testing.T) {
	pool := &lanePool{results: []*laneRows{{ids: []int64{7, 8, 9}, more: true}}}
	refs, err := (PhotoLane{Pool: pool}).Select(t.Context(), 2, 1)
	if err != nil || len(refs) != 2 || refs[0].ID != 8 || refs[1].ID != 9 || refs[1].OwnerNSID != "owner" {
		t.Fatalf("Select() = %v, %v, want photos 8 and 9", refs, err)
	}
	if len(pool.args) != 1 || pool.args[0][0] != firstPersonChunk || pool.args[0][1] != 3 {
		t.Fatalf("probe args = %v, want one probe of %d persons for 3 photos", pool.args, firstPersonChunk)
	}
}

func TestPhotoLaneSelectWidensWhileTooFewPhotosAndMorePersons(t *testing.T) {
	pool := &lanePool{results: []*laneRows{{ids: []int64{0}, more: true}, {ids: []int64{5}, more: true}, {ids: []int64{5, 6}, more: false}}}
	refs, err := (PhotoLane{Pool: pool}).Select(t.Context(), 2, 0)
	if err != nil || len(refs) != 2 {
		t.Fatalf("Select() = %v, %v, want two photos", refs, err)
	}
	if len(pool.args) != 3 || pool.args[1][0] != 2*firstPersonChunk || pool.args[2][0] != 4*firstPersonChunk {
		t.Fatalf("probe args = %v, want the person chunk doubled each time", pool.args)
	}
}

func TestPhotoLaneSelectStopsWhenNoMorePersons(t *testing.T) {
	pool := &lanePool{results: []*laneRows{{ids: []int64{0}, more: false}}}
	refs, err := (PhotoLane{Pool: pool}).Select(t.Context(), 2, 0)
	if err != nil || len(refs) != 0 || len(pool.args) != 1 {
		t.Fatalf("Select() = %v, %v after %d probes, want none after one", refs, err, len(pool.args))
	}
	pool = &lanePool{results: []*laneRows{{ids: []int64{4}, more: false}}}
	if refs, err = (PhotoLane{Pool: pool}).Select(t.Context(), 1, 5); err != nil || len(refs) != 0 {
		t.Fatalf("Select() past the end = %v, %v", refs, err)
	}
}

func TestPhotoLaneSelectReportsErrors(t *testing.T) {
	boom := errors.New("boom")
	for name, p := range map[string]*lanePool{
		"query": {queryErr: boom},
		"scan":  {results: []*laneRows{{ids: []int64{1}, scanErr: boom}}},
		"rows":  {results: []*laneRows{{err: boom}}},
	} {
		if _, err := (PhotoLane{Pool: p}).Select(t.Context(), 1, 0); !errors.Is(err, boom) {
			t.Fatalf("%s: Select() error = %v, want boom", name, err)
		}
	}
}
