package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jooservices/xflickr/internal/catalog/app"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

type readsDB struct {
	queryErr error
	rowErr   error
	rows     *readsRows
	row      *readsRow
	query    string
	args     []any
}

func (*readsDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (db *readsDB) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	db.query, db.args = query, append([]any(nil), args...)
	return db.rows, db.queryErr
}

func (db *readsDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	db.query, db.args = query, append([]any(nil), args...)
	if db.row == nil {
		return readsRow{err: db.rowErr}
	}
	return *db.row
}
func (*readsDB) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

type readsRow struct {
	values []any
	err    error
}

func (row readsRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	return assignReadValues(dest, row.values)
}

type readsRows struct {
	pgx.Rows
	rows    [][]any
	index   int
	scanErr error
	err     error
}

func (rows *readsRows) Next() bool {
	if rows.index >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}
func (*readsRows) Close()          {}
func (rows *readsRows) Err() error { return rows.err }
func (rows *readsRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	return assignReadValues(dest, rows.rows[rows.index-1])
}

func assignReadValues(dest []any, values []any) error {
	if len(dest) != len(values) {
		return errors.New("unexpected scan value count")
	}
	for i, target := range dest {
		out := reflect.ValueOf(target)
		if out.Kind() != reflect.Pointer {
			return errors.New("scan destination is not a pointer")
		}
		if values[i] == nil {
			out.Elem().SetZero()
			continue
		}
		value := reflect.ValueOf(values[i])
		if !value.Type().AssignableTo(out.Elem().Type()) {
			return errors.New("unexpected scan value type")
		}
		out.Elem().Set(value)
	}
	return nil
}

func readTestPhotoRow(id int64, extra ...any) []any {
	values := []any{id, "owner", "title", "description", []string{"tag"}, pgtype.Timestamptz{}, pgtype.Timestamptz{}, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), "public", "confirmed", []byte(`[ {"label":"Large","width":1200,"height":800} ]`), "ready"}
	return append(values, extra...)
}

func TestVisibleReadsValidatePolicyAndPaginatePeopleAndPhotos(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 72
	predicate := sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
	db := &readsDB{}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)

	db.rows = &readsRows{rows: [][]any{{"owner-a", "A", "", "icon", int64(2)}, {"owner-b", "B", "", "icon", int64(1)}}}
	people, err := repository.ListPeople(ctx, userID, predicate, nil, 1)
	if err != nil || len(people.Items) != 1 || people.NextCursor == nil || people.NextCursor.NSID != "owner-a" {
		t.Fatalf("people=%+v err=%v", people, err)
	}
	if !strings.Contains(db.query, predicate.Expression) || db.args[0] != userID || db.args[1] != 2 {
		t.Fatalf("people query scope=%q args=%v", db.query, db.args)
	}
	db.rows = &readsRows{rows: [][]any{{"owner-z", "Z", "", "icon", int64(0)}}}
	if _, err := repository.ListPeople(ctx, userID, predicate, &app.PersonCursor{NSID: "owner-a"}, 1); err != nil {
		t.Fatalf("cursor people: %v", err)
	}
	if _, err := repository.ListPeople(ctx, userID, predicate, &app.PersonCursor{}, 1); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("invalid people cursor=%v", err)
	}
	if _, err := repository.ListPeople(ctx, userID, sharingapp.SQLPredicate{Expression: "true", Args: []any{userID}}, nil, 1); err == nil {
		t.Fatal("foreign visibility predicate accepted")
	}
	if _, err := repository.ListPeople(ctx, userID, predicate, nil, 0); err == nil {
		t.Fatal("zero limit accepted")
	}
	db.queryErr = errors.New("query failed")
	if _, err := repository.ListPeople(ctx, userID, predicate, nil, 1); !errors.Is(err, db.queryErr) {
		t.Fatalf("people query error=%v", err)
	}
	db.queryErr = nil
	db.rows = &readsRows{rows: [][]any{{"owner", "O", "", "", int64(1)}}, scanErr: errors.New("scan failed")}
	if _, err := repository.ListPeople(ctx, userID, predicate, nil, 1); err == nil {
		t.Fatal("people scan failure was ignored")
	}
	db.rows = &readsRows{err: errors.New("rows failed")}
	if _, err := repository.ListPeople(ctx, userID, predicate, nil, 1); err == nil {
		t.Fatal("people rows failure was ignored")
	}

	db.row = &readsRow{values: []any{"owner", "Owner", "Name", "icon", int64(1)}}
	person, ok, err := repository.GetPerson(ctx, userID, "owner", predicate)
	if err != nil || !ok || person.NSID != "owner" || person.PhotosCount != 1 {
		t.Fatalf("person=%+v ok=%v err=%v", person, ok, err)
	}
	db.row = &readsRow{err: pgx.ErrNoRows}
	if _, ok, err := repository.GetPerson(ctx, userID, "missing", predicate); err != nil || ok {
		t.Fatalf("missing person ok=%v err=%v", ok, err)
	}
	if _, _, err := repository.GetPerson(ctx, userID, " ", predicate); err == nil {
		t.Fatal("empty person id accepted")
	}
	db.row = &readsRow{err: errors.New("person query failed")}
	if _, _, err := repository.GetPerson(ctx, userID, "owner", predicate); err == nil {
		t.Fatal("person query failure ignored")
	}

	db.row = &readsRow{values: []any{"owner", "Owner", "Name", "icon", int64(1)}}
	db.rows = &readsRows{rows: [][]any{readTestPhotoRow(42)}}
	photos, err := repository.ListPersonPhotos(ctx, userID, "owner", predicate, nil, 10)
	if err != nil || len(photos.Items) != 1 || photos.Items[0].Labels[0] != "tag" || photos.Items[0].Sizes[0].Label != "Large" {
		t.Fatalf("person photos=%+v err=%v", photos, err)
	}
	db.rows = &readsRows{rows: [][]any{readTestPhotoRow(42), readTestPhotoRow(43)}}
	photos, err = repository.ListPersonPhotos(ctx, userID, "owner", predicate, &app.PhotoCursor{LastUpdate: now, ID: 42}, 1)
	if err != nil || len(photos.Items) != 1 || photos.NextCursor == nil {
		t.Fatalf("person photo keyset=%+v err=%v", photos, err)
	}
	db.row = &readsRow{err: pgx.ErrNoRows}
	if _, err := repository.ListPersonPhotos(ctx, userID, "hidden", predicate, nil, 10); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("hidden person's photos error=%v", err)
	}
	db.row = &readsRow{values: []any{"owner", "Owner", "Name", "icon", int64(1)}}
	db.rows = &readsRows{rows: [][]any{readTestPhotoRow(1)}}
	if _, err := repository.ListPersonPhotos(ctx, userID, "owner", predicate, &app.PhotoCursor{}, 1); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("invalid photo cursor=%v", err)
	}
	invalidSizes := readTestPhotoRow(1)
	invalidSizes[10] = []byte("not-json")
	db.rows = &readsRows{rows: [][]any{invalidSizes}}
	if _, err := repository.ListPersonPhotos(ctx, userID, "owner", predicate, nil, 1); err == nil {
		t.Fatal("invalid sizes payload accepted")
	}
}

func TestVisibleFavoritesAlbumsAndAlbumPhotosKeysets(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 73
	predicate := sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
	db := &readsDB{}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	db.rows = &readsRows{rows: [][]any{readTestPhotoRow(10, pgtype.Timestamptz{Time: now, Valid: true}), readTestPhotoRow(11, pgtype.Timestamptz{Time: now.Add(-time.Second), Valid: true})}}
	favorites, err := repository.ListVisibleFavorites(ctx, userID, predicate, nil, 1)
	if err != nil || len(favorites.Items) != 1 || favorites.NextCursor == nil || favorites.Items[0].Photo.ID != 10 {
		t.Fatalf("favorites=%+v err=%v", favorites, err)
	}
	db.rows = &readsRows{rows: [][]any{readTestPhotoRow(12, pgtype.Timestamptz{Time: now.Add(-2 * time.Second), Valid: true})}}
	if _, err := repository.ListVisibleFavorites(ctx, userID, predicate, &app.FavoriteCursor{FaveDate: now, PhotoID: 11}, 1); err != nil {
		t.Fatalf("favorite cursor=%v", err)
	}
	if _, err := repository.ListVisibleFavorites(ctx, userID, predicate, &app.FavoriteCursor{PhotoID: 1}, 1); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("invalid favorite cursor=%v", err)
	}

	db.rows = &readsRows{rows: [][]any{{"album-a", "owner", "A", "", pgtype.Int8{}, int64(1), pgtype.Timestamptz{}, pgtype.Timestamptz{}}, {"album-b", "owner", "B", "", pgtype.Int8{Int64: 9, Valid: true}, int64(2), pgtype.Timestamptz{}, pgtype.Timestamptz{Time: now, Valid: true}}}}
	albums, err := repository.ListVisibleAlbums(ctx, userID, predicate, nil, 1)
	if err != nil || len(albums.Items) != 1 || albums.NextCursor == nil || albums.Items[0].PrimaryPhotoID != nil {
		t.Fatalf("albums=%+v err=%v", albums, err)
	}
	db.rows = &readsRows{rows: [][]any{{"album-c", "owner", "C", "", pgtype.Int8{}, int64(1), pgtype.Timestamptz{}, pgtype.Timestamptz{}}}}
	updatedAt := now
	if _, err := repository.ListVisibleAlbums(ctx, userID, predicate, &app.AlbumCursor{ID: "album-b", UpdatedAt: &updatedAt}, 1); err != nil {
		t.Fatalf("album cursor=%v", err)
	}
	if _, err := repository.ListVisibleAlbums(ctx, userID, predicate, &app.AlbumCursor{ID: "album-b"}, 1); err != nil {
		t.Fatalf("null updated album cursor=%v query=%s", err, db.query)
	}
	if _, err := repository.ListVisibleAlbums(ctx, userID, predicate, &app.AlbumCursor{}, 1); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("invalid album cursor=%v", err)
	}

	db.row = &readsRow{values: []any{true}}
	db.rows = &readsRows{rows: [][]any{readTestPhotoRow(21, 0), readTestPhotoRow(22, 1)}}
	albumPhotos, err := repository.ListVisibleAlbumPhotos(ctx, userID, "album", predicate, nil, 1)
	if err != nil || len(albumPhotos.Items) != 1 || albumPhotos.NextCursor == nil || albumPhotos.NextCursor.Position != 0 {
		t.Fatalf("album photos=%+v err=%v", albumPhotos, err)
	}
	db.rows = &readsRows{rows: [][]any{readTestPhotoRow(23, 2)}}
	if _, err := repository.ListVisibleAlbumPhotos(ctx, userID, "album", predicate, &app.AlbumPhotoCursor{Position: 1, PhotoID: 22}, 1); err != nil {
		t.Fatalf("album photo cursor=%v", err)
	}
	db.row = &readsRow{values: []any{false}}
	if _, err := repository.ListVisibleAlbumPhotos(ctx, userID, "hidden", predicate, nil, 1); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("hidden album=%v", err)
	}
	db.row = &readsRow{err: errors.New("album check failed")}
	if _, err := repository.ListVisibleAlbumPhotos(ctx, userID, "album", predicate, nil, 1); err == nil {
		t.Fatal("album check failure ignored")
	}
	db.row = &readsRow{values: []any{true}}
	if _, err := repository.ListVisibleAlbumPhotos(ctx, userID, "album", predicate, &app.AlbumPhotoCursor{Position: -1, PhotoID: 1}, 1); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("invalid album photo cursor=%v", err)
	}
}

func TestVisiblePhotoReadFailuresAreNotExposedAsRecords(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 74
	predicate := sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
	db := &readsDB{}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	db.row = &readsRow{values: readTestPhotoRow(42)}
	photo, ok, err := repository.GetPhoto(ctx, userID, 42, predicate)
	if err != nil || !ok || photo.ID != 42 || photo.AIStatus != "ready" {
		t.Fatalf("photo=%+v ok=%v err=%v", photo, ok, err)
	}
	db.row = &readsRow{err: pgx.ErrNoRows}
	if _, ok, err := repository.GetPhoto(ctx, userID, 42, predicate); err != nil || ok {
		t.Fatalf("missing photo ok=%v err=%v", ok, err)
	}
	db.row = &readsRow{err: errors.New("photo query failed")}
	if _, _, err := repository.GetPhoto(ctx, userID, 42, predicate); err == nil {
		t.Fatal("photo query failure ignored")
	}
	if _, _, err := repository.GetPhoto(ctx, userID, 0, predicate); err == nil {
		t.Fatal("invalid photo id accepted")
	}
	if err := validateRead(ctx, (*Repository)(nil), userID, predicate, 1); err == nil {
		t.Fatal("nil repository accepted")
	}
	if err := validateRead(nil, repository, userID, predicate, 1); err == nil {
		t.Fatal("nil context accepted")
	}
	if err := validateRead(ctx, repository, userID, predicate, app.MaxPageSize+1); err == nil {
		t.Fatal("oversized page accepted")
	}
	if err := validateRead(ctx, repository, userID, sharingapp.SQLPredicate{Expression: predicate.Expression, Args: []any{int64(2)}}, 1); err == nil {
		t.Fatal("wrong actor predicate accepted")
	}
	if err := validateRead(ctx, repository, 0, predicate, 1); err == nil {
		t.Fatal("zero user accepted")
	}
}
