package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/crawl/app"
)

type stateRow struct {
	err     error
	blocked bool
	accountID int64
	missing bool
}

func (r stateRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 1 {
		*dest[0].(*bool) = r.blocked
	} else {
		*dest[0].(*string) = "42"
		*dest[1].(*time.Time) = time.Unix(100, 0)
		if len(dest) >= 3 {
			*dest[2].(*int64) = 7
		}
		if len(dest) == 4 {
			*dest[3].(*int64) = r.accountID
		}
	}
	return nil
}

type stateDBFake struct {
	row   stateRow
	err   error
	execs int
	accessAccount int64
	accountBlocked bool
}

func (d *stateDBFake) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	if len(args) == 9 {
		d.accessAccount = args[8].(int64)
		if d.row.accountID > 0 {
			return stateRow{blocked: d.accessAccount == 0 || d.accountBlocked}
		}
	}
	return d.row
}
func (d *stateDBFake) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	d.execs++
	return pgconn.CommandTag{}, d.err
}

func TestEntityStateStorageErrorsAndEligibility(t *testing.T) {
	db := &stateDBFake{}
	s := EntityStates{DB: db}
	ctx := t.Context()
	a := app.EntityAccess{EntityRef: app.EntityRef{Kind: "person", ID: "p"}}
	for _, state := range []string{"gone", "restricted"} {
		if err := s.RecordFailure(ctx, a, state, "not_found", time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
		db.err = errors.New("write failed")
		if err := s.RecordFailure(ctx, a, state, "not_found", time.Now(), time.Now()); !errors.Is(err, db.err) {
			t.Fatal("lost write error")
		}
		db.err = nil
	}
	if ok, err := s.PhotoFacetAllowed(ctx, 42, 1, "sizes"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	db.row.blocked = true
	if ok, err := s.PhotoFacetAllowed(ctx, 42, 1, "sizes"); err != nil || ok {
		t.Fatal(ok, err)
	}
	db.row.err = pgx.ErrNoRows
	if ok, err := s.PhotoFacetAllowed(ctx, 42, 1, "sizes"); err != nil || ok {
		t.Fatal(ok, err)
	}
	db.row.err = errors.New("read failed")
	if _, err := s.PhotoFacetAllowed(ctx, 42, 1, "sizes"); !errors.Is(err, db.row.err) {
		t.Fatal("lost read error")
	}
	if _, err := s.Suppressed(ctx, a, time.Now()); !errors.Is(err, db.row.err) {
		t.Fatal("lost suppressed read error")
	}
}

func TestPhotoSizesEligibilitySeparatesAnonymousAndOwnerRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name string
		restricted bool
	}{ {"transient OAuth failure can be scheduled again", false}, {"terminal owner restriction closes scheduling", true} } {
		t.Run(tc.name, func(t *testing.T) {
			db := &stateDBFake{row: stateRow{accountID: 91}, accountBlocked: tc.restricted}
			allowed, err := (EntityStates{DB: db}).PhotoFacetAllowed(t.Context(), 42, 1, "sizes")
			if err != nil || allowed == tc.restricted {
				t.Fatalf("owner eligibility = %v, %v; want %v", allowed, err, !tc.restricted)
			}
			if db.accessAccount != 91 {
				t.Fatalf("restriction scope = %d; want owner account 91", db.accessAccount)
			}
		})
	}
}
