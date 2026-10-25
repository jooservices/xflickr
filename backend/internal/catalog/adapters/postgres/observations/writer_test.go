package observations

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/catalog/app"
	"github.com/jooservices/xflickr/internal/catalog/domain"
)

type execStub struct {
	err   error
	calls int
}

type (
	dbtxStub struct{}
	rowStub  struct{}
)

func (rowStub) Scan(...any) error { return nil }
func (dbtxStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (dbtxStub) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (dbtxStub) QueryRow(context.Context, string, ...any) pgx.Row        { return rowStub{} }
func (dbtxStub) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults  { return nil }

func (s *execStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.calls++
	return pgconn.NewCommandTag("INSERT 0 1"), s.err
}

type catalogStub struct {
	people []domain.Person
	photos []domain.Photo
	err    error
}

func (s *catalogStub) UpsertPeople(_ context.Context, values []domain.Person) error {
	s.people = values
	return s.err
}

func (s *catalogStub) UpsertPhotos(_ context.Context, values []domain.Photo) error {
	s.photos = values
	return s.err
}

type observerStub struct {
	facts []app.Observation
	err   error
}

func (s *observerStub) Observe(_ context.Context, fact app.Observation) error {
	s.facts = append(s.facts, fact)
	return s.err
}

func TestWriterObservedUpserts(t *testing.T) {
	ctx := context.Background()
	catalog := &catalogStub{}
	observer := &observerStub{}
	writer := newWriter(3, 4, catalog, observer)
	people := []domain.Person{{NSID: "person@flickr", Username: "person"}}
	if err := writer.UpsertPeople(ctx, people); err != nil {
		t.Fatal(err)
	}
	photos := []domain.Photo{{ID: 52}}
	if err := writer.UpsertPhotos(ctx, photos); err != nil {
		t.Fatal(err)
	}
	if len(catalog.people) != 1 || len(catalog.photos) != 1 || len(observer.facts) != 2 {
		t.Fatalf("writes = people:%d photos:%d observations:%d", len(catalog.people), len(catalog.photos), len(observer.facts))
	}
	if observer.facts[0].RecordKind != "person" || observer.facts[0].RecordID != "person@flickr" || observer.facts[1].RecordKind != "photo" || observer.facts[1].RecordID != "52" {
		t.Fatalf("observations = %+v", observer.facts)
	}
}

func TestWriterErrors(t *testing.T) {
	ctx := context.Background()
	if err := (*Writer)(nil).UpsertPeople(ctx, nil); err == nil {
		t.Fatal("expected incomplete writer error")
	}
	writer := newWriter(0, 0, &catalogStub{}, &observerStub{})
	if err := writer.UpsertPhotos(ctx, nil); err == nil {
		t.Fatal("expected invalid scope error")
	}
	wantErr := errors.New("catalog failed")
	if err := newWriter(1, 2, &catalogStub{err: wantErr}, &observerStub{}).UpsertPeople(ctx, []domain.Person{{NSID: "p"}}); !errors.Is(err, wantErr) {
		t.Fatalf("catalog error = %v", err)
	}
	wantErr = errors.New("observer failed")
	if err := newWriter(1, 2, &catalogStub{}, &observerStub{err: wantErr}).UpsertPhotos(ctx, []domain.Photo{{ID: 4}}); !errors.Is(err, wantErr) {
		t.Fatalf("observer error = %v", err)
	}
	if err := newWriter(1, 2, &catalogStub{}, &observerStub{}).UpsertPhotos(nil, nil); err == nil {
		t.Fatal("expected missing context error")
	}
}

func TestObserver(t *testing.T) {
	if _, err := New(nil, 1, 2); err == nil {
		t.Fatal("expected missing database error")
	}
	if _, err := New(nil, 0, 0); err == nil {
		t.Fatal("expected invalid scope error")
	}
	if _, err := New(dbtxStub{}, 1, 2); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	stub := &execStub{}
	observer := observer{db: stub}
	if err := observer.Observe(context.Background(), app.Observation{UserID: 1, FlickrAccountID: 2, RecordKind: "photo", RecordID: "8"}); err != nil || stub.calls != 1 {
		t.Fatalf("Observe() = %v, calls %d", err, stub.calls)
	}
	for _, fact := range []app.Observation{{}, {UserID: 1, FlickrAccountID: 2, RecordKind: "invalid", RecordID: "1"}} {
		if err := observer.Observe(context.Background(), fact); err == nil {
			t.Fatal("expected invalid observation")
		}
	}
	wantErr := errors.New("insert failed")
	stub.err = wantErr
	if err := observer.Observe(context.Background(), app.Observation{UserID: 1, FlickrAccountID: 2, RecordKind: "person", RecordID: "p"}); !errors.Is(err, wantErr) {
		t.Fatalf("Observe() error = %v", err)
	}
}
