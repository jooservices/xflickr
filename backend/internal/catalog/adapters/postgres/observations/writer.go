// Package observations records catalog upserts together with their provenance.
package observations

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"

	basepostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	"github.com/jooservices/xflickr/internal/catalog/adapters/postgres/gen"
	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/catalog/domain"
)

type upserter interface {
	UpsertPeople(context.Context, []domain.Person) error
	UpsertPhotos(context.Context, []domain.Photo) error
}

type execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type observer struct{ db execer }

func (o observer) Observe(ctx context.Context, fact app.Observation) error {
	if ctx == nil || fact.UserID <= 0 || fact.FlickrAccountID <= 0 || fact.RecordID == "" || (fact.RecordKind != "person" && fact.RecordKind != "photo") {
		return errors.New("catalog: invalid observation")
	}
	_, err := o.db.Exec(ctx, `INSERT INTO observations (user_id, flickr_account_id, record_kind, record_id) VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, fact.UserID, fact.FlickrAccountID, fact.RecordKind, fact.RecordID)
	return err
}

// Writer performs user-scoped catalog upserts through the supplied database
// handle. Passing a transaction keeps catalog rows and observations atomic.
type Writer struct {
	userID    int64
	accountID int64
	catalog   upserter
	observer  app.Observer
}

func New(database gen.DBTX, userID, accountID int64) (*Writer, error) {
	if database == nil {
		return nil, errors.New("catalog: observation database is required")
	}
	if userID <= 0 || accountID <= 0 {
		return nil, errors.New("catalog: observation user and account are required")
	}
	repository, err := basepostgres.New(database)
	if err != nil {
		return nil, err
	}
	return newWriter(userID, accountID, repository, observer{db: database}), nil
}

func newWriter(userID, accountID int64, repository upserter, observer app.Observer) *Writer {
	return &Writer{userID: userID, accountID: accountID, catalog: repository, observer: observer}
}

func (w *Writer) UpsertPeople(ctx context.Context, people []domain.Person) error {
	if err := w.ready(ctx); err != nil {
		return err
	}
	if err := w.catalog.UpsertPeople(ctx, people); err != nil {
		return err
	}
	for _, person := range people {
		if err := w.observer.Observe(ctx, app.Observation{UserID: w.userID, FlickrAccountID: w.accountID, RecordKind: "person", RecordID: person.NSID}); err != nil {
			return fmt.Errorf("catalog: observe person: %w", err)
		}
	}
	return nil
}

func (w *Writer) UpsertPhotos(ctx context.Context, photos []domain.Photo) error {
	if err := w.ready(ctx); err != nil {
		return err
	}
	if err := w.catalog.UpsertPhotos(ctx, photos); err != nil {
		return err
	}
	for _, photo := range photos {
		if err := w.observer.Observe(ctx, app.Observation{UserID: w.userID, FlickrAccountID: w.accountID, RecordKind: "photo", RecordID: strconv.FormatInt(photo.ID, 10)}); err != nil {
			return fmt.Errorf("catalog: observe photo: %w", err)
		}
	}
	return nil
}

func (w *Writer) ready(ctx context.Context) error {
	if ctx == nil || w == nil || w.userID <= 0 || w.accountID <= 0 || w.catalog == nil || w.observer == nil {
		return errors.New("catalog: observed writer is incomplete")
	}
	return nil
}
