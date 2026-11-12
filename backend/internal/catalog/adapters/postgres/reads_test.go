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
	queryErr       error
	pageErr        error
	rowErr         error
	rows           *readsRows
	ownerRows      *readsRows
	row            *readsRow
	query          string
	args           []any
	queries        []string
	queryArgs      [][]any
	queryResponses []*readsRows
	queryErrors    []error
	queryCount     int
}

func (*readsDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (db *readsDB) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	db.query, db.args = query, append([]any(nil), args...)
	db.queries = append(db.queries, query)
	db.queryArgs = append(db.queryArgs, append([]any(nil), args...))
	queryIndex := db.queryCount
	db.queryCount++
	if queryIndex < len(db.queryErrors) && db.queryErrors[queryIndex] != nil {
		return nil, db.queryErrors[queryIndex]
	}
	if queryIndex < len(db.queryResponses) {
		return db.queryResponses[queryIndex], nil
	}
	if strings.Contains(query, "first_contacts AS") && strings.Contains(query, "selected_contact AS") {
		if db.ownerRows == nil {
			return &readsRows{}, db.queryErr
		}
		return db.ownerRows, db.queryErr
	}
	if strings.Contains(query, "WITH contact_owners AS MATERIALIZED") {
		if db.rows == nil {
			return &readsRows{}, db.pageErr
		}
		return db.rows, db.pageErr
	}
	if db.rows == nil {
		return &readsRows{}, db.queryErr
	}
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

func TestListContactsPaginatesAndValidates(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 88
	db := &readsDB{}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	db.rows = &readsRows{rows: [][]any{
		{"c-a", "A", "Alpha", "icon", "acct"},
		{"c-b", "B", "Beta", "icon", "acct"},
	}}
	page, err := repository.ListContacts(ctx, userID, nil, 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil || page.NextCursor.NSID != "c-a" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if item := page.Items[0]; item.NSID != "c-a" || item.Username != "A" || item.Realname != "Alpha" || item.AccountNSID != "acct" {
		t.Fatalf("item=%+v", item)
	}
	limitAt, detailsAt := strings.Index(db.query, "LIMIT $3"), strings.Index(db.query, "JOIN people")
	if !strings.Contains(db.query, "flickr_accounts") || !strings.Contains(db.query, "WITH contact_page AS MATERIALIZED") || !strings.Contains(db.query, "SELECT DISTINCT c.contact_nsid") || !strings.Contains(db.query, "c.contact_nsid > $2") || limitAt < 0 || detailsAt < 0 || limitAt > detailsAt || strings.Contains(strings.ToUpper(db.query), "GROUP BY") || strings.Contains(strings.ToUpper(db.query), "MIN(") || db.args[0] != userID || db.args[1] != "" || db.args[2] != 2 {
		t.Fatalf("query=%q args=%v", db.query, db.args)
	}
	if strings.Contains(db.query, "photos") || strings.Contains(db.query, "observations") {
		t.Fatalf("contacts query must not calculate photo counts: %q", db.query)
	}
	db.rows = &readsRows{rows: [][]any{{"c-z", "Z", "", "", "acct"}}}
	if _, err := repository.ListContacts(ctx, userID, &app.ContactCursor{NSID: "c-a"}, 1); err != nil {
		t.Fatalf("cursor list: %v", err)
	}
	if db.args[1] != "c-a" || db.args[2] != 2 {
		t.Fatalf("cursor query args=%v", db.args)
	}
	if _, err := repository.ListContacts(ctx, userID, &app.ContactCursor{}, 1); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("invalid cursor=%v", err)
	}
	if _, err := repository.ListContacts(ctx, 0, nil, 1); err == nil {
		t.Fatal("invalid user accepted")
	}
	if _, err := repository.ListContacts(ctx, userID, nil, 0); err == nil {
		t.Fatal("zero limit accepted")
	}
	db.queryErr = errors.New("query failed")
	if _, err := repository.ListContacts(ctx, userID, nil, 1); !errors.Is(err, db.queryErr) {
		t.Fatalf("query error=%v", err)
	}
	db.queryErr = nil
	db.rows = &readsRows{rows: [][]any{{"c", "C", "", "", int64(1), "acct"}}, scanErr: errors.New("scan failed")}
	if _, err := repository.ListContacts(ctx, userID, nil, 1); err == nil {
		t.Fatal("scan failure ignored")
	}
	db.rows = &readsRows{err: errors.New("rows failed")}
	if _, err := repository.ListContacts(ctx, userID, nil, 1); err == nil {
		t.Fatal("rows failure ignored")
	}
}

func TestContactQueryDoesNotCountPhotos(t *testing.T) {
	db := &readsDB{rows: &readsRows{}}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ListContacts(t.Context(), 88, nil, 10); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(db.query, "photos") || strings.Contains(db.query, "observations") {
		t.Fatalf("contacts query must not look up photos or observations: %s", db.query)
	}
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
	if !strings.Contains(db.query, "SELECT count(*) FROM photos") || strings.Contains(db.query, "LIMIT 1000") || strings.Contains(db.query, "observations o\n    WHERE o.record_kind='photo'") {
		t.Fatalf("people query does not match develop photo-count behavior: %q", db.query)
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
	if !strings.Contains(db.query, "SELECT count(*) FROM photos") || strings.Contains(db.query, "LIMIT 1000") || strings.Contains(db.query, "observations o\n    WHERE o.record_kind='photo'") {
		t.Fatalf("person query does not match develop photo-count behavior: %q", db.query)
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
	db.queryResponses = []*readsRows{
		{rows: [][]any{
			{int64(900), int64(10), pgtype.Timestamptz{Time: now, Valid: true}, pgtype.Timestamptz{}},
			{int64(899), int64(11), pgtype.Timestamptz{Time: now.Add(-time.Second), Valid: true}, pgtype.Timestamptz{}},
			{int64(898), int64(12), pgtype.Timestamptz{Time: now.Add(-2 * time.Second), Valid: true}, pgtype.Timestamptz{}},
		}},
		{rows: [][]any{readTestPhotoRow(10), readTestPhotoRow(11)}},
	}
	favorites, err := repository.ListVisibleFavorites(ctx, userID, predicate, nil, 1)
	if err != nil || len(favorites.Items) != 1 || favorites.NextCursor == nil || favorites.NextCursor.FlickrAccountID != 900 || favorites.Items[0].Photo.ID != 10 {
		t.Fatalf("favorites=%+v err=%v", favorites, err)
	}
	if len(db.queries) != 2 {
		t.Fatalf("favorites query count=%d, want bounded scan and photo load", len(db.queries))
	}
	scanQuery := db.queries[0]
	lateralIndex := strings.Index(scanQuery, "CROSS JOIN LATERAL")
	innerLimitIndex := strings.Index(scanQuery, "LIMIT $2")
	pageCTEIndex := strings.Index(scanQuery, "scan_page AS MATERIALIZED")
	mergeOrderIndex := strings.Index(scanQuery, "ORDER BY fave_date DESC,photo_id DESC,flickr_account_id DESC")
	globalLimitIndex := -1
	if mergeOrderIndex >= 0 {
		if offset := strings.Index(scanQuery[mergeOrderIndex:], "LIMIT $2"); offset >= 0 {
			globalLimitIndex = mergeOrderIndex + offset
		}
	}
	if lateralIndex < 0 || innerLimitIndex < lateralIndex || pageCTEIndex < innerLimitIndex ||
		mergeOrderIndex < pageCTEIndex || globalLimitIndex < mergeOrderIndex ||
		!strings.Contains(scanQuery, "accounts.id AS flickr_account_id") ||
		strings.Contains(scanQuery, "removed_at IS NULL") || strings.Contains(scanQuery, "JOIN photos") {
		t.Fatalf("favorite scan must bound raw rows before filtering photos: %s", scanQuery)
	}
	photoQuery := db.queries[1]
	if !strings.Contains(photoQuery, "photos.id=ANY($2)") || !strings.Contains(photoQuery, "photos.tombstoned_at IS NULL") || !strings.Contains(photoQuery, predicate.Expression) {
		t.Fatalf("favorite photo load must enforce visibility and tombstones: %s", photoQuery)
	}
	db.queryResponses = []*readsRows{
		{rows: [][]any{{int64(898), int64(12), pgtype.Timestamptz{Time: now.Add(-2 * time.Second), Valid: true}, pgtype.Timestamptz{}}}},
		{rows: [][]any{readTestPhotoRow(12)}},
	}
	db.queryCount = 0
	db.queries, db.queryArgs = nil, nil
	if _, err := repository.ListVisibleFavorites(ctx, userID, predicate, &app.FavoriteCursor{FaveDate: now, PhotoID: 11, FlickrAccountID: 900}, 1); err != nil {
		t.Fatalf("favorite cursor=%v", err)
	}
	if !strings.Contains(db.queries[0], "favorites.fave_date,favorites.photo_id)<=($2,$3)") ||
		!strings.Contains(db.queries[0], "flickr_account_id<$4") ||
		!strings.Contains(db.queries[0], "cursor_favorites AS MATERIALIZED") ||
		db.queryArgs[0][4] != 4 || db.queryArgs[0][5] != 3 {
		t.Fatalf("favorite cursor must constrain a bounded account scan: query=%s args=%v", db.queries[0], db.queryArgs[0])
	}
	if _, err := repository.ListVisibleFavorites(ctx, userID, predicate, &app.FavoriteCursor{PhotoID: 1}, 1); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("invalid favorite cursor=%v", err)
	}
	db.queryResponses = []*readsRows{{}}
	db.queryCount = 0
	emptyFavorites, err := repository.ListVisibleFavorites(ctx, userID, predicate, nil, 1)
	if err != nil || len(emptyFavorites.Items) != 0 || emptyFavorites.NextCursor != nil {
		t.Fatalf("empty favorites=%+v err=%v", emptyFavorites, err)
	}
	db.queryResponses = []*readsRows{{rows: [][]any{
		{int64(900), int64(10), pgtype.Timestamptz{Time: now, Valid: true}, pgtype.Timestamptz{Valid: true, Time: now}},
		{int64(899), int64(11), pgtype.Timestamptz{Time: now.Add(-time.Second), Valid: true}, pgtype.Timestamptz{Valid: true, Time: now}},
		{int64(898), int64(12), pgtype.Timestamptz{Time: now.Add(-2 * time.Second), Valid: true}, pgtype.Timestamptz{Valid: true, Time: now}},
	}}}
	db.queryCount = 0
	db.queries, db.queryArgs = nil, nil
	removedFavorites, err := repository.ListVisibleFavorites(ctx, userID, predicate, nil, 1)
	if err != nil || len(removedFavorites.Items) != 0 || removedFavorites.NextCursor == nil ||
		removedFavorites.NextCursor.PhotoID != 11 || removedFavorites.NextCursor.FlickrAccountID != 899 || len(db.queries) != 1 {
		t.Fatalf("removed favorite batch=%+v queries=%d err=%v", removedFavorites, len(db.queries), err)
	}

	boom := errors.New("favorite photo query failed")
	db.queryResponses, db.queryErrors, db.queryCount = nil, nil, 0
	db.queryErr = boom
	if _, err := repository.ListVisibleFavorites(ctx, userID, predicate, nil, 1); !errors.Is(err, boom) {
		t.Fatalf("favorite query error=%v", err)
	}
	db.queryErr = nil
	db.queryResponses = []*readsRows{
		{rows: [][]any{{int64(900), int64(10), pgtype.Timestamptz{Time: now, Valid: true}, pgtype.Timestamptz{}}}},
		{rows: nil},
	}
	db.queryErrors = []error{nil, boom}
	db.queryCount = 0
	if _, err := repository.ListVisibleFavorites(ctx, userID, predicate, nil, 1); !errors.Is(err, boom) {
		t.Fatalf("favorite photo query error=%v", err)
	}
	db.queryErrors = nil
	db.queryResponses = []*readsRows{{rows: [][]any{{int64(900), int64(10), pgtype.Timestamptz{Time: now, Valid: true}, pgtype.Timestamptz{}}}, scanErr: boom}}
	db.queryCount = 0
	if _, err := repository.ListVisibleFavorites(ctx, userID, predicate, nil, 1); !errors.Is(err, boom) {
		t.Fatalf("favorite scan error=%v", err)
	}
	db.queryResponses = []*readsRows{{err: boom}}
	db.queryCount = 0
	if _, err := repository.ListVisibleFavorites(ctx, userID, predicate, nil, 1); !errors.Is(err, boom) {
		t.Fatalf("favorite rows error=%v", err)
	}
	db.queryResponses = []*readsRows{
		{rows: [][]any{{int64(900), int64(10), pgtype.Timestamptz{Time: now, Valid: true}, pgtype.Timestamptz{}}}},
		{rows: [][]any{readTestPhotoRow(10)}, scanErr: boom},
	}
	db.queryCount = 0
	if _, err := repository.ListVisibleFavorites(ctx, userID, predicate, nil, 1); !errors.Is(err, boom) {
		t.Fatalf("favorite photo scan error=%v", err)
	}
	db.queryResponses = []*readsRows{
		{rows: [][]any{{int64(900), int64(10), pgtype.Timestamptz{Time: now, Valid: true}, pgtype.Timestamptz{}}}},
		{err: boom},
	}
	db.queryCount = 0
	if _, err := repository.ListVisibleFavorites(ctx, userID, predicate, nil, 1); !errors.Is(err, boom) {
		t.Fatalf("favorite photo rows error=%v", err)
	}
	db.queryResponses, db.queryErrors, db.queryCount = nil, nil, 0

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

func TestContactAlbumVisibilityBoundsPhotoCandidates(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 75
	predicate := sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
	db := &readsDB{rows: &readsRows{}}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 10); err != nil {
		t.Fatal(err)
	}
	photoScan := strings.Index(db.query, "FROM album_photos")
	if photoScan < 0 {
		t.Fatalf("contact album query has no bounded membership probe: %s", db.query)
	}
	photoJoin := strings.Index(db.query[photoScan:], "JOIN photos")
	photoLimit := strings.Index(db.query[photoScan:], "LIMIT")
	if photoLimit < 0 || (photoJoin >= 0 && photoLimit > photoJoin) {
		t.Fatalf("album photo candidates are not limited before visibility checks: %s", db.query)
	}
}

func TestContactAlbumProbeCursorBoundsEmptyPages(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 76
	predicate := sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
	db := &readsDB{
		rows: &readsRows{rows: [][]any{
			{"hidden-1", "contact", "Hidden", pgtype.Timestamptz{}, false, false, nil, nil},
			{"hidden-2", "contact", "Hidden", pgtype.Timestamptz{}, false, false, nil, nil},
		}},
	}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1)
	if err != nil || len(page.Items) != 0 || page.NextCursor == nil || page.NextCursor.ID != "hidden-2" {
		t.Fatalf("bounded empty page=%+v err=%v", page, err)
	}

	db.rows = &readsRows{rows: [][]any{{
		"long-album", "contact", "Long album",
		pgtype.Timestamptz{},
		false, true,
		pgtype.Int4{Int32: 127, Valid: true},
		pgtype.Int8{Int64: 128, Valid: true},
	}}}
	page, err = repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1)
	if err != nil || page.NextCursor == nil || page.NextCursor.ID != "long-album" ||
		page.NextCursor.ProbePosition != 127 || page.NextCursor.ProbePhotoID != 128 {
		t.Fatalf("photo probe continuation=%+v err=%v", page.NextCursor, err)
	}

	db.rows = &readsRows{rows: [][]any{{"long-album", "contact", "Long album", pgtype.Timestamptz{}, false, true, nil, nil}}}
	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1); err == nil {
		t.Fatal("probe continuation without a photo key accepted")
	}
}

func TestContactAlbumReadFailuresAndOwnerValidation(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 77
	predicate := sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
	db := &readsDB{}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.ListContactAlbums(ctx, userID, strings.Repeat("x", 256), predicate, nil, 1); !errors.Is(err, app.ErrInvalidPage) {
		t.Fatalf("oversized owner filter error=%v", err)
	}
	if _, err := repository.ListContactAlbums(ctx, userID, " owner ", predicate, nil, 1); !errors.Is(err, app.ErrInvalidPage) {
		t.Fatalf("trimmed owner filter error=%v", err)
	}
	if _, err := repository.ListContactAlbums(ctx, userID, "owner", predicate, &app.AlbumCursor{ID: "cursor"}, 1); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("cursor from another owner filter error=%v", err)
	}
	db.ownerRows = &readsRows{rows: [][]any{{"owner"}}, scanErr: errors.New("owner scan failed")}
	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1); err == nil {
		t.Fatal("owner scan failure ignored")
	}
	db.ownerRows = &readsRows{err: errors.New("owner rows failed")}
	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1); err == nil {
		t.Fatal("owner rows failure ignored")
	}
	db.ownerRows = nil
	db.pageErr = errors.New("page query failed")
	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1); err == nil {
		t.Fatal("page query failure ignored")
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

func TestContactAlbumReadsUseOwnerVisibilityAndStableKeysets(t *testing.T) {
	ctx := context.Background()
	const userID int64 = 75
	predicate := sharingapp.Filter(sharingdomain.User{ID: userID}).SQL
	db := &readsDB{}
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	db.rows = &readsRows{rows: [][]any{
		{"contact-album-1", "contact-1", "Album 1", pgtype.Timestamptz{Time: now, Valid: true}, true, false, nil, nil},
		{"contact-album-2", "contact-2", "Album 2", pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}, true, false, nil, nil},
	}}

	db.ownerRows = &readsRows{rows: [][]any{{"contact-1"}, {"contact-2"}}}
	page, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "contact-album-1" || page.NextCursor == nil {
		t.Fatalf("contact albums=%+v err=%v", page, err)
	}
	if len(page.ContactOwners) != 2 || page.ContactOwners[1] != "contact-2" {
		t.Fatalf("contact owner options=%v", page.ContactOwners)
	}
	if strings.Contains(db.query, "count(") ||
		!strings.Contains(db.query, "contacts.person_nsid=flickr_accounts.nsid") ||
		!strings.Contains(db.query, "a.owner_nsid=contact_owners.contact_nsid") ||
		!strings.Contains(db.query, "album_photos.album_id=candidates.id") ||
		!strings.Contains(db.query, "photos.tombstoned_at IS NULL") ||
		!strings.Contains(db.query, predicate.Expression) ||
		!strings.Contains(db.query, "flickr_accounts.user_id=$1") ||
		!strings.Contains(db.query, "ORDER BY albums.updated_at DESC NULLS LAST,albums.id DESC") ||
		!strings.Contains(db.query, "LIMIT $8") {
		t.Fatalf("contact album query must use contact and photo visibility evidence: %s", db.query)
	}
	if db.args[0] != userID || db.args[1] != "" || db.args[6] != 2 {
		t.Fatalf("contact album args=%v", db.args)
	}

	db.rows = &readsRows{rows: [][]any{{"contact-album-2", "contact-2", "Album 2", pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}, true, false, nil, nil}}}
	page, err = repository.ListContactAlbums(ctx, userID, "", predicate, page.NextCursor, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "contact-album-2" {
		t.Fatalf("contact album cursor page=%+v err=%v", page, err)
	}
	cursorTime, ok := db.args[3].(*time.Time)
	if !strings.Contains(db.query, "a.updated_at<$4::timestamptz") || db.args[2] != "contact-album-1" || !ok || !cursorTime.Equal(now) {
		t.Fatalf("contact album cursor query=%q args=%v", db.query, db.args)
	}

	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, &app.AlbumCursor{}, 1); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("invalid cursor error=%v", err)
	}
	if _, err := repository.ListContactAlbums(ctx, 0, "", predicate, nil, 1); err == nil {
		t.Fatal("zero user accepted")
	}
	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, app.MaxPageSize+1); err == nil {
		t.Fatal("oversized page accepted")
	}
	db.queryErr = errors.New("query failed")
	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1); !errors.Is(err, db.queryErr) {
		t.Fatalf("query error=%v", err)
	}
	db.queryErr = nil
	db.rows = &readsRows{rows: [][]any{{"contact-album-1", "contact-1", "Album 1", pgtype.Timestamptz{Time: now, Valid: true}, true, false, nil, nil}}, scanErr: errors.New("scan failed")}
	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1); err == nil {
		t.Fatal("scan error ignored")
	}
	db.rows = &readsRows{err: errors.New("rows failed")}
	if _, err := repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1); err == nil {
		t.Fatal("rows error ignored")
	}
	db.rows = &readsRows{rows: [][]any{{"contact-album-null", "contact-3", "Album without update time", pgtype.Timestamptz{}, true, false, nil, nil}}}
	page, err = repository.ListContactAlbums(ctx, userID, "", predicate, nil, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].UpdatedAt != nil {
		t.Fatalf("contact album with null timestamp=%+v err=%v", page, err)
	}
	_, err = repository.ListContactAlbums(ctx, userID, "", predicate, &app.AlbumCursor{ID: "contact-album-null"}, 1)
	if err != nil || !strings.Contains(db.query, "a.updated_at IS NULL AND a.id<$3") || db.args[2] != "contact-album-null" {
		t.Fatalf("null timestamp cursor query=%q args=%v err=%v", db.query, db.args, err)
	}
}
