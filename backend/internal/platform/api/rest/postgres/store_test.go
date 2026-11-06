package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/platform/api/rest"
)

func TestImplementsStore(_ *testing.T) { var _ rest.IdempotencyStore = Store{} }

type fakeRow struct{ scan func(...any) error }

func (r fakeRow) Scan(dest ...any) error { return r.scan(dest...) }

type fakeDB struct{ rows []fakeRow }

func (d *fakeDB) QueryRow(context.Context, string, ...any) pgx.Row {
	r := d.rows[0]
	d.rows = d.rows[1:]
	return r
}
func errorRow(err error) fakeRow { return fakeRow{func(...any) error { return err }} }
func TestStorePaths(t *testing.T) {
	ctx := context.Background()
	r := rest.IdempotencyRecord{Principal: "user", Key: "key", Fingerprint: "fp", ExpiresAt: time.Now().UTC().Truncate(time.Microsecond)}
	success := fakeRow{func(dest ...any) error { *dest[0].(*string) = "key"; return nil }}
	failure := errors.New("db failure")
	for _, tt := range []struct {
		rows    []fakeRow
		claimed bool
		fails   bool
	}{{[]fakeRow{success}, true, false}, {[]fakeRow{errorRow(failure)}, false, true}, {[]fakeRow{errorRow(pgx.ErrNoRows), errorRow(failure)}, false, true}, {[]fakeRow{errorRow(pgx.ErrNoRows), {func(dest ...any) error {
		*dest[0].(*string) = "fp"
		*dest[1].(*[]byte) = []byte(`{"status":202,"body":"b2s="}`)
		*dest[2].(*time.Time) = r.ExpiresAt
		return nil
	}}}, false, false}, {[]fakeRow{errorRow(pgx.ErrNoRows), {func(dest ...any) error { *dest[1].(*[]byte) = []byte("invalid"); return nil }}}, false, true}, {[]fakeRow{errorRow(pgx.ErrNoRows), {func(...any) error { return nil }}}, false, false}} {
		s := Store{DB: &fakeDB{rows: tt.rows}}
		_, claimed, err := s.Claim(ctx, r)
		if claimed != tt.claimed || (err != nil) != tt.fails {
			t.Fatal(claimed, err)
		}
	}
	r.Response = &rest.StoredResponse{Status: 201}
	for _, row := range []fakeRow{success, errorRow(failure)} {
		s := Store{DB: &fakeDB{rows: []fakeRow{row}}}
		err := s.Complete(ctx, r)
		if errors.Is(err, failure) != (err != nil) {
			t.Fatal(err)
		}
	}
}
