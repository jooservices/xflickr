package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/crawl/app"
)

func TestNewControls(t *testing.T) {
	if _, err := NewControls(nil); !errors.Is(err, errControlsDatabaseRequired) {
		t.Fatalf("nil pool error=%v", err)
	}
	if repo, err := NewControls(&pgxpool.Pool{}); err != nil || repo == nil || repo.pool == nil {
		t.Fatalf("repo=%v err=%v", repo, err)
	}
}

type controlFakeDB struct {
	queryResults []controlRows
	queryErrs    []error
	queryIndex   int
	execErr      error
	execs        int
	execArgs     []any
	exists       bool
	rowErr       error
}

func (d *controlFakeDB) Query(context.Context, string, ...any) (controlRows, error) {
	i := d.queryIndex
	d.queryIndex++
	if i < len(d.queryErrs) && d.queryErrs[i] != nil {
		return nil, d.queryErrs[i]
	}
	if i < len(d.queryResults) {
		return d.queryResults[i], nil
	}
	return &controlFakeRows{}, nil
}

func (d *controlFakeDB) QueryRow(context.Context, string, ...any) rowScanner {
	return controlFakeRow{exists: d.exists, err: d.rowErr}
}

func (d *controlFakeDB) Exec(_ context.Context, _ string, args ...any) error {
	d.execs++
	d.execArgs = args
	return d.execErr
}

type controlFakeRow struct {
	exists bool
	err    error
}

func (r controlFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.exists
	return nil
}

type controlFakeRows struct {
	items             [][]any
	index             int
	scanErr, errorErr error
	closed            bool
}

func (r *controlFakeRows) Next() bool { return r.index < len(r.items) }
func (r *controlFakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	values := r.items[r.index]
	r.index++
	for i, v := range values {
		p := reflect.ValueOf(dest[i])
		p.Elem().Set(reflect.ValueOf(v))
	}
	return nil
}
func (r *controlFakeRows) Err() error { return r.errorErr }
func (r *controlFakeRows) Close()     { r.closed = true }

func TestControlsStatus(t *testing.T) {
	db := &controlFakeDB{queryResults: []controlRows{
		&controlFakeRows{items: [][]any{{1, int64(3)}}},
		&controlFakeRows{items: [][]any{{"favorites", int64(5), int64(2), int64(2), int64(1)}}},
		&controlFakeRows{items: [][]any{{"person", int64(2), int64(1), int64(3), int64(1)}}},
	}}
	repo := &Controls{pool: db}
	got, err := repo.Status(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Depths) != 1 || got.Depths[0].Depth != 1 || got.Facets[0].Done != 2 || got.Lanes[0].Paused != 1 {
		t.Fatalf("status=%+v", got)
	}
	for _, tc := range []struct {
		name string
		db   controlDB
		ctx  context.Context
		user int64
	}{
		{"nil repo", nil, context.Background(), 1}, {"nil context", &controlFakeDB{}, nil, 1}, {"bad user", &controlFakeDB{}, context.Background(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&Controls{pool: tc.db}).Status(tc.ctx, tc.user)
			if err == nil {
				t.Fatal("invalid dependencies accepted")
			}
		})
	}
	for stage := 0; stage < 3; stage++ {
		t.Run("query error", func(t *testing.T) {
			fake := &controlFakeDB{queryErrs: make([]error, 3)}
			fake.queryErrs[stage] = errors.New("query")
			if _, err := (&Controls{pool: fake}).Status(context.Background(), 1); err == nil {
				t.Fatal("query error swallowed")
			}
		})
	}
	for stage := 0; stage < 3; stage++ {
		t.Run("scan error", func(t *testing.T) {
			rows := &controlFakeRows{items: [][]any{{1, int64(1)}}, scanErr: errors.New("scan")}
			fake := &controlFakeDB{queryResults: []controlRows{&controlFakeRows{items: [][]any{{1, int64(1)}}}, &controlFakeRows{items: [][]any{{"f", int64(1), int64(1), int64(0), int64(0)}}}, &controlFakeRows{items: [][]any{{"person", int64(1), int64(0), int64(0), int64(0)}}}}}
			fake.queryResults[stage] = rows
			if _, err := (&Controls{pool: fake}).Status(context.Background(), 1); err == nil {
				t.Fatal("scan error swallowed")
			}
		})
	}
	for stage := 0; stage < 3; stage++ {
		t.Run("iteration error", func(t *testing.T) {
			fake := &controlFakeDB{queryResults: []controlRows{
				&controlFakeRows{items: [][]any{{1, int64(1)}}},
				&controlFakeRows{items: [][]any{{"f", int64(1), int64(1), int64(0), int64(0)}}},
				&controlFakeRows{items: [][]any{{"person", int64(1), int64(0), int64(0), int64(0)}}},
			}}
			fake.queryResults[stage] = &controlFakeRows{errorErr: errors.New("iteration")}
			if _, err := (&Controls{pool: fake}).Status(context.Background(), 1); err == nil {
				t.Fatal("iteration error swallowed")
			}
		})
	}
}

func TestContactMutations(t *testing.T) {
	ctx := context.Background()
	when := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	for _, run := range []struct {
		name string
		call func(*Controls) error
	}{{"star", func(r *Controls) error { return r.SetStarred(ctx, 2, "p", true) }}, {"select", func(r *Controls) error { return r.Select(ctx, 2, "p", when) }}, {"pause", func(r *Controls) error { return r.SetPaused(ctx, 2, "p", true) }}, {"resume", func(r *Controls) error { return r.SetPaused(ctx, 2, "p", false) }}} {
		t.Run(run.name, func(t *testing.T) {
			db := &controlFakeDB{exists: true}
			repo := &Controls{pool: db}
			if err := run.call(repo); err != nil || db.execs != 1 {
				t.Fatalf("execs=%d err=%v", db.execs, err)
			}
		})
		t.Run(run.name+" missing", func(t *testing.T) {
			db := &controlFakeDB{}
			if err := run.call(&Controls{pool: db}); !errors.Is(err, app.ErrPersonNotFound) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if err := (&Controls{pool: &controlFakeDB{execErr: errors.New("write")}}).SetStarred(ctx, 2, "p", true); err == nil {
		t.Fatal("write error swallowed")
	}
	if err := (&Controls{pool: &controlFakeDB{exists: true, rowErr: errors.New("read")}}).SetPaused(ctx, 2, "p", true); err == nil {
		t.Fatal("read error swallowed")
	}
	for _, repo := range []*Controls{nil, {}, {pool: &controlFakeDB{}}} {
		if err := repo.SetStarred(nil, 0, "", true); err == nil {
			t.Fatal("invalid mutation accepted")
		}
		if err := repo.SetPaused(nil, 0, "", true); err == nil {
			t.Fatal("invalid pause accepted")
		}
	}
}
