package roles

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogdomain "github.com/jooservices/xflickr/internal/catalog/domain"
	crawlpostgres "github.com/jooservices/xflickr/internal/crawl/adapters/postgres"
	crawlapp "github.com/jooservices/xflickr/internal/crawl/app"
)

func TestObservedCatalogRejectsMissingAttribution(t *testing.T) {
	writer := &observedCatalog{}
	for _, scope := range [][2]int64{{0, 1}, {1, 0}} {
		if _, err := writer.ForObserver(context.Background(), scope[0], scope[1]); err == nil {
			t.Fatal("missing provenance accepted")
		}
	}
	var _ crawlapp.ScopedCatalogWriter = writer
}

func TestResolveObservationAccount(t *testing.T) {
	for _, tc := range []struct {
		user    int64
		row     pgx.Row
		wantErr bool
	}{
		{0, nil, true},
		{1, credentialRow{err: errors.New("no account")}, true},
		{1, credentialRow{values: []any{int64(4), int64(9)}}, false},
	} {
		pool := &credentialPool{rows: []pgx.Row{tc.row}}
		got, err := (&crawlFlickr{pool: pool}).Resolve(t.Context(), crawlapp.Request{UserID: tc.user, KeyID: 99})
		if (err != nil) != tc.wantErr {
			t.Fatalf("resolve=%+v %v", got, err)
		}
		if !tc.wantErr && (got.UserID != 1 || got.KeyID != 4 || got.AccountID != 9) {
			t.Fatalf("scope=%+v", got)
		}
	}
}

type observationConn struct{ crawlpostgres.Conn }

type splitPeopleWriter interface {
	EnsurePeople(context.Context, []catalogdomain.Person) error
	RefreshPeople(context.Context, []catalogdomain.Person) error
}

func (observationConn) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (observationConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestObservedCatalogBindsTransactionalWriter(t *testing.T) {
	conn := observationConn{}
	repo, err := catalogpostgres.New(conn)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := (&observedCatalog{Repository: repo, conn: conn}).ForObserver(t.Context(), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	peopleWriter, ok := scoped.(splitPeopleWriter)
	if !ok {
		t.Fatal("scoped catalog does not support split people writes")
	}
	if err = peopleWriter.EnsurePeople(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err = peopleWriter.RefreshPeople(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err = scoped.UpsertPhotos(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err = peopleWriter.EnsurePeople(t.Context(), []catalogdomain.Person{{NSID: "invalid"}}); err == nil {
		t.Fatal("invalid person accepted")
	}
	if err = scoped.UpsertPhotos(t.Context(), []catalogdomain.Photo{{ID: 0}}); err == nil {
		t.Fatal("invalid photo accepted")
	}
}
