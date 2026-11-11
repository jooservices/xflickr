package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/catalog/app"
)

type contactTestDB struct {
	readsDB
	resultRows    []*readsRows
	resultRowsErr error
	rowResults    []readsRow
	queries       []string
}

func (db *contactTestDB) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	db.queries = append(db.queries, query)
	if db.resultRowsErr != nil {
		return nil, db.resultRowsErr
	}
	if len(db.resultRows) == 0 {
		return &readsRows{}, nil
	}
	rows := db.resultRows[0]
	db.resultRows = db.resultRows[1:]
	return rows, nil
}

func (db *contactTestDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	db.queries = append(db.queries, query)
	if len(db.rowResults) == 0 {
		return readsRow{err: pgx.ErrNoRows}
	}
	row := db.rowResults[0]
	db.rowResults = db.rowResults[1:]
	return row
}

func contactTestRepository(t *testing.T, db *contactTestDB) *Repository {
	t.Helper()
	repository, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func contactTestRows(values ...[]any) *readsRows { return &readsRows{rows: values} }
func contactTestRow(values ...any) readsRow      { return readsRow{values: values} }
func contactTestTime() time.Time                 { return time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC) }

func contactInterestTestRow(nsid, name string, photos int64) []any {
	updated := contactTestTime()
	return []any{nsid, 1, true, int64(4), int64(2), int64(1), &updated, "user", name, "https://images.example.invalid/avatar.jpg", photos, "checking", []byte(`[{"id":"10","alt":"Photo","thumbnail_url":null}]`), &updated}
}

func contactDetailTestRow(nsid string) []any {
	updated := contactTestTime()
	avatar, location := "https://images.example.invalid/avatar.jpg", "Hanoi"
	return []any{
		nsid, "user", "User Name", &avatar, &location, 2, true, false, "running", int64(4), int64(10), 50,
		[]byte(`[{"name":"photos","completed":4,"total":10,"status":"partial","updated_at":null}]`), int64(10), int64(2), int64(1), int64(1), int64(2), &updated, &updated,
		[]byte(`[{"nsid":"root@N00","label":"Root"}]`),
	}
}

func contactPhotoTestRow(id int64) []any {
	updated := contactTestTime()
	title, thumb := "Photo", "https://images.example.invalid/photo.jpg"
	width, height := 640, 480
	return []any{id, title, &thumb, &width, &height, &updated, true, updated}
}

func TestContactCursorHelpersValidateAndRoundTrip(t *testing.T) {
	cursor, err := encodeContactCursor(contactCursor{Kind: "photos:one@N00", ID: "42", At: contactTestTime()})
	if err != nil || cursor == nil {
		t.Fatalf("encode cursor=%v err=%v", cursor, err)
	}
	decoded, err := decodeContactCursor(*cursor, "photos:one@N00")
	if err != nil || decoded.ID != "42" || !decoded.At.Equal(contactTestTime()) {
		t.Fatalf("decoded cursor=%+v err=%v", decoded, err)
	}
	for _, raw := range []string{"not-base64", ""} {
		if raw == "" {
			if got, err := decodeContactCursor(raw, "photos:one@N00"); err != nil || got != nil {
				t.Fatalf("empty cursor=%+v err=%v", got, err)
			}
			continue
		}
		if _, err := decodeContactCursor(raw, "photos:one@N00"); !errors.Is(err, app.ErrInvalidCursor) {
			t.Fatalf("malformed cursor error=%v", err)
		}
	}
	if _, err := decodeContactCursor(*cursor, "favorites:one@N00"); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("wrong cursor kind error=%v", err)
	}
	if _, err := decodeContactCursor("eyJraW5kIjoicGhvdG9zOm9uZUBOMDAifQ", "photos:one@N00"); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("missing cursor id error=%v", err)
	}
	if _, err := contactPageLimit(1); err != nil {
		t.Fatal(err)
	}
	if _, err := contactPageLimit(app.MaxPageSize + 1); !errors.Is(err, app.ErrInvalidPage) {
		t.Fatalf("page limit error=%v", err)
	}
}

func TestListContactInterestsUsesScopedSummariesAndStableSortCursors(t *testing.T) {
	ctx := context.Background()
	updated := contactTestTime()
	db := &contactTestDB{
		resultRows: []*readsRows{contactTestRows(contactInterestTestRow("one@N00", "One", 10), contactInterestTestRow("two@N00", "Two", 20))},
		rowResults: []readsRow{contactTestRow(int64(2), int64(1), int64(1), int64(0))},
	}
	page, err := contactTestRepository(t, db).ListContactInterests(ctx, 7, "needs_attention", "user", "name", "", 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil || page.Counts.All != 2 || page.Items[0].SizesPercent != 25 || page.Items[0].Status != "checking" || len(page.Items[0].RecentPhotos) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if !strings.Contains(db.queries[0], "ci.user_id=$1") || strings.Contains(db.queries[0], "count(*) FROM photos") || strings.Contains(db.queries[0], "LIMIT $") == false {
		t.Fatalf("contact list query was not scoped and bounded: %s", db.queries[0])
	}
	if !updated.Equal(*page.Items[0].UpdatedAt) {
		t.Fatalf("updated=%v want %v", page.Items[0].UpdatedAt, updated)
	}

	for _, sortBy := range []string{"name", "photos", "updated"} {
		value := "One"
		if sortBy == "photos" {
			value = "10"
		}
		cursor, encodeErr := encodeContactCursor(contactCursor{Kind: "interests:" + sortBy, ID: "one@N00", Value: value, At: updated})
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		db = &contactTestDB{resultRows: []*readsRows{contactTestRows()}, rowResults: []readsRow{contactTestRow(int64(1), int64(0), int64(0), int64(0))}}
		if _, err := contactTestRepository(t, db).ListContactInterests(ctx, 7, "all", "", sortBy, *cursor, 10); err != nil {
			t.Fatalf("sort %s cursor: %v", sortBy, err)
		}
		if len(db.queries) < 1 || !strings.Contains(db.queries[0], "ci.user_id=$1") {
			t.Fatalf("sort %s query missing actor scope", sortBy)
		}
	}

	db = &contactTestDB{resultRowsErr: errors.New("list failed")}
	if _, err := contactTestRepository(t, db).ListContactInterests(ctx, 7, "all", "", "updated", "", 10); err == nil {
		t.Fatal("list query failure was swallowed")
	}
	db = &contactTestDB{resultRows: []*readsRows{contactTestRows()}, rowResults: []readsRow{{err: errors.New("count failed")}}}
	if _, err := contactTestRepository(t, db).ListContactInterests(ctx, 7, "all", "", "updated", "", 10); err == nil {
		t.Fatal("count query failure was swallowed")
	}
	db = &contactTestDB{resultRows: []*readsRows{contactTestRows()}, rowResults: []readsRow{contactTestRow(int64(0), int64(0), int64(0), int64(0))}}
	if _, err := contactTestRepository(t, db).ListContactInterests(ctx, 7, "all", "", "updated", "bad cursor", 10); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("bad list cursor error=%v", err)
	}
	db = &contactTestDB{resultRows: []*readsRows{{scanErr: errors.New("scan failed")}}}
	if _, err := contactTestRepository(t, db).ListContactInterests(ctx, 7, "all", "", "updated", "", 10); err == nil {
		t.Fatal("list scan failure was swallowed")
	}
	db = &contactTestDB{resultRows: []*readsRows{{err: errors.New("rows failed")}}}
	if _, err := contactTestRepository(t, db).ListContactInterests(ctx, 7, "all", "", "updated", "", 10); err == nil {
		t.Fatal("list rows failure was swallowed")
	}
}

func TestGetContactDecodesScopedIdentityAndFacetSummaries(t *testing.T) {
	ctx := context.Background()
	db := &contactTestDB{rowResults: []readsRow{contactTestRow(contactDetailTestRow("one@N00")...)}}
	value, found, err := contactTestRepository(t, db).GetContact(ctx, 7, "one@N00")
	if err != nil || !found || value.NSID != "one@N00" || value.PhotoRecordsTotal == nil || *value.PhotoRecordsTotal != 10 || len(value.Facets) != 1 || len(value.FoundVia) != 1 {
		t.Fatalf("contact=%+v found=%v err=%v", value, found, err)
	}
	for _, test := range []struct {
		name string
		row  readsRow
	}{
		{"missing", readsRow{err: pgx.ErrNoRows}},
		{"query error", readsRow{err: errors.New("query failed")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := &contactTestDB{rowResults: []readsRow{test.row}}
			_, found, err := contactTestRepository(t, db).GetContact(ctx, 7, "one@N00")
			if test.name == "missing" && (err != nil || found) {
				t.Fatalf("missing found=%v err=%v", found, err)
			}
			if test.name == "query error" && err == nil {
				t.Fatal("query error was swallowed")
			}
		})
	}
	badFacets := contactDetailTestRow("one@N00")
	badFacets[12] = []byte("not-json")
	db = &contactTestDB{rowResults: []readsRow{contactTestRow(badFacets...)}}
	if _, _, err := contactTestRepository(t, db).GetContact(ctx, 7, "one@N00"); err == nil {
		t.Fatal("invalid facets JSON accepted")
	}
	badPath := contactDetailTestRow("one@N00")
	badPath[20] = []byte("not-json")
	db = &contactTestDB{rowResults: []readsRow{contactTestRow(badPath...)}}
	if _, _, err := contactTestRepository(t, db).GetContact(ctx, 7, "one@N00"); err == nil {
		t.Fatal("invalid discovery JSON accepted")
	}
	if _, _, err := contactTestRepository(t, &contactTestDB{}).GetContact(nil, 7, "one@N00"); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestContactPhotoAndFavoritePagesValidateCursorsAndRetainSummaries(t *testing.T) {
	ctx := context.Background()
	updated := contactTestTime()
	cursor, err := encodeContactCursor(contactCursor{Kind: "photos:one@N00", ID: "7", At: updated})
	if err != nil {
		t.Fatal(err)
	}
	db := &contactTestDB{resultRows: []*readsRows{contactTestRows(contactPhotoTestRow(7))}, rowResults: []readsRow{contactTestRow(int64(4), &updated)}}
	page, err := contactTestRepository(t, db).ListContactPhotos(ctx, 7, "one@N00", "", 10)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "7" || page.Total != 4 {
		t.Fatalf("photo page=%+v err=%v", page, err)
	}
	if !strings.Contains(db.queries[0], "observations") || !strings.Contains(db.queries[0], "LIMIT") {
		t.Fatalf("photo visibility or page limit missing: %s", db.queries[0])
	}
	db = &contactTestDB{resultRows: []*readsRows{contactTestRows(contactPhotoTestRow(7), contactPhotoTestRow(8))}, rowResults: []readsRow{contactTestRow(int64(4), &updated)}}
	page, err = contactTestRepository(t, db).ListContactPhotos(ctx, 7, "one@N00", *cursor, 1)
	if err != nil || page.NextCursor == nil || len(page.Items) != 1 {
		t.Fatalf("photo cursor page=%+v err=%v", page, err)
	}
	photoNext, err := decodeContactCursor(*page.NextCursor, "photos:one@N00")
	if err != nil || photoNext.ID != "7" {
		t.Fatalf("photo next cursor=%+v err=%v, want last returned photo 7", photoNext, err)
	}
	badCursor, _ := encodeContactCursor(contactCursor{Kind: "photos:one@N00", ID: "invalid", At: updated})
	if _, err := contactTestRepository(t, &contactTestDB{}).ListContactPhotos(ctx, 7, "one@N00", *badCursor, 10); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("invalid photo cursor=%v", err)
	}
	wrongCursor, _ := encodeContactCursor(contactCursor{Kind: "favorites:one@N00", ID: "7", At: updated})
	if _, err := contactTestRepository(t, &contactTestDB{}).ListContactPhotos(ctx, 7, "one@N00", *wrongCursor, 10); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("wrong photo cursor=%v", err)
	}

	favoriteCursor, _ := encodeContactCursor(contactCursor{Kind: "favorites:one@N00", ID: "7", At: updated})
	favoriteRow := []any{int64(7), "Favorite", (*string)(nil), (*int)(nil), (*int)(nil), &updated, true, "owner@N00", stringPointerForTest("Owner"), (*string)(nil), updated}
	favoriteExtra := []any{int64(8), "Favorite 2", (*string)(nil), (*int)(nil), (*int)(nil), &updated, true, "owner@N00", stringPointerForTest("Owner"), (*string)(nil), updated.Add(-time.Second)}
	db = &contactTestDB{resultRows: []*readsRows{contactTestRows(favoriteRow, favoriteExtra)}, rowResults: []readsRow{contactTestRow(int64(2), &updated)}}
	favorites, err := contactTestRepository(t, db).ListContactFavorites(ctx, 7, "one@N00", *favoriteCursor, 1)
	if err != nil || len(favorites.Items) != 1 || favorites.Items[0].OwnerName == nil || *favorites.Items[0].OwnerName != "Owner" {
		t.Fatalf("favorites=%+v err=%v", favorites, err)
	}
	favoriteNext, err := decodeContactCursor(*favorites.NextCursor, "favorites:one@N00")
	if err != nil || favoriteNext.ID != "7" {
		t.Fatalf("favorite next cursor=%+v err=%v, want last returned favorite 7", favoriteNext, err)
	}
	if !strings.Contains(db.queries[0], "fave_date") || !strings.Contains(db.queries[0], "observations") {
		t.Fatalf("favorite query missing ordering or visibility: %s", db.queries[0])
	}
	if _, err := contactTestRepository(t, &contactTestDB{}).ListContactFavorites(ctx, 7, "one@N00", *cursor, 10); !errors.Is(err, app.ErrInvalidCursor) {
		t.Fatalf("wrong favorites cursor=%v", err)
	}
	if _, err := contactTestRepository(t, &contactTestDB{}).ListContactPhotos(ctx, 7, "one@N00", "", 0); !errors.Is(err, app.ErrInvalidPage) {
		t.Fatalf("invalid photo limit=%v", err)
	}
	if _, err := contactTestRepository(t, &contactTestDB{}).ListContactFavorites(ctx, 7, "one@N00", "", app.MaxPageSize+1); !errors.Is(err, app.ErrInvalidPage) {
		t.Fatalf("invalid favorite limit=%v", err)
	}
	if _, err := contactTestRepository(t, &contactTestDB{resultRowsErr: errors.New("query failed")}).ListContactPhotos(ctx, 7, "one@N00", "", 10); err == nil {
		t.Fatal("photo query error swallowed")
	}
	if _, err := contactTestRepository(t, &contactTestDB{resultRowsErr: errors.New("query failed")}).ListContactFavorites(ctx, 7, "one@N00", "", 10); err == nil {
		t.Fatal("favorite query error swallowed")
	}
	db = &contactTestDB{resultRows: []*readsRows{contactTestRows()}, rowResults: []readsRow{{err: pgx.ErrNoRows}}}
	if _, err := contactTestRepository(t, db).ListContactPhotos(ctx, 7, "one@N00", "", 10); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("missing photo summary=%v", err)
	}
	db = &contactTestDB{resultRows: []*readsRows{contactTestRows()}, rowResults: []readsRow{{err: errors.New("summary failed")}}}
	if _, err := contactTestRepository(t, db).ListContactFavorites(ctx, 7, "one@N00", "", 10); err == nil {
		t.Fatal("favorite summary error swallowed")
	}
	db = &contactTestDB{resultRows: []*readsRows{{scanErr: errors.New("scan failed")}}}
	if _, err := contactTestRepository(t, db).ListContactPhotos(ctx, 7, "one@N00", "", 10); err == nil {
		t.Fatal("photo scan error swallowed")
	}
	db = &contactTestDB{resultRows: []*readsRows{{err: errors.New("rows failed")}}}
	if _, err := contactTestRepository(t, db).ListContactFavorites(ctx, 7, "one@N00", "", 10); err == nil {
		t.Fatal("favorite rows error swallowed")
	}
}

func TestContactCollectionReadsRequireAccessAndReturnSectionData(t *testing.T) {
	ctx := context.Background()
	updated := contactTestTime()
	contactExists := contactTestRow(true)
	db := &contactTestDB{
		rowResults: []readsRow{contactExists, contactExists, contactExists},
		resultRows: []*readsRows{
			contactTestRows([]any{"album", "Trip", "user", int64(3), []byte(`[]`), &updated}),
			contactTestRows([]any{"gallery", "Best", int64(5), &updated}),
			contactTestRows([]any{"group", "Street", int64(20), true, &updated}),
		},
	}
	repository := contactTestRepository(t, db)
	albums, err := repository.ListContactAlbums(ctx, 7, "one@N00")
	if err != nil || albums.Total != 1 || albums.Items[0].StoredCount != 3 {
		t.Fatalf("albums=%+v err=%v", albums, err)
	}
	galleries, err := repository.ListContactGalleries(ctx, 7, "one@N00")
	if err != nil || galleries.Items[0].PhotoCount != 5 {
		t.Fatalf("galleries=%+v err=%v", galleries, err)
	}
	groups, err := repository.ListContactGroups(ctx, 7, "one@N00")
	if err != nil || groups.Items[0].NSID != "group" || !groups.Items[0].PoolCrawlEnabled {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}

	db = &contactTestDB{rowResults: []readsRow{contactTestRow(contactDetailTestRow("one@N00")...), contactTestRow(&updated, int64(2), &updated)}}
	about, found, err := contactTestRepository(t, db).GetContactAbout(ctx, 7, "one@N00")
	if err != nil || !found || about.TombstoneCount != 2 || len(about.Facets) != 1 {
		t.Fatalf("about=%+v found=%v err=%v", about, found, err)
	}

	for _, read := range []func(*Repository) error{
		func(r *Repository) error { _, err := r.ListContactAlbums(ctx, 7, "one@N00"); return err },
		func(r *Repository) error { _, err := r.ListContactGalleries(ctx, 7, "one@N00"); return err },
		func(r *Repository) error { _, err := r.ListContactGroups(ctx, 7, "one@N00"); return err },
	} {
		db = &contactTestDB{rowResults: []readsRow{contactTestRow(false)}}
		if err := read(contactTestRepository(t, db)); !errors.Is(err, app.ErrNotFound) {
			t.Fatalf("unowned contact error=%v", err)
		}
	}
	db = &contactTestDB{rowResults: []readsRow{{err: pgx.ErrNoRows}}}
	if _, _, err := contactTestRepository(t, db).GetContactAbout(ctx, 7, "one@N00"); err != nil {
		t.Fatalf("missing detail about error=%v", err)
	}
	if _, err := contactTestRepository(t, &contactTestDB{rowResults: []readsRow{{err: errors.New("access check failed")}}}).ListContactAlbums(ctx, 7, "one@N00"); err == nil {
		t.Fatal("access check error was swallowed")
	}
}

func TestContactGroupPreferenceAndSizePriorityRemainActorScoped(t *testing.T) {
	ctx := context.Background()
	now := contactTestTime()
	db := &contactTestDB{rowResults: []readsRow{contactTestRow(true, now)}}
	value, found, err := contactTestRepository(t, db).SetContactGroupPoolCrawl(ctx, 7, "one@N00", "group", true)
	if err != nil || !found || !value.Enabled || !value.ChangedAt.Equal(now) {
		t.Fatalf("pool=%+v found=%v err=%v", value, found, err)
	}
	if !strings.Contains(db.queries[0], "INSERT INTO work_items") || !strings.Contains(db.queries[0], "group_pool") {
		t.Fatalf("enabling group pool did not enqueue crawl work: %s", db.queries[0])
	}
	db = &contactTestDB{rowResults: []readsRow{{err: pgx.ErrNoRows}}}
	if _, found, err := contactTestRepository(t, db).SetContactGroupPoolCrawl(ctx, 7, "one@N00", "group", true); err != nil || found {
		t.Fatalf("missing group preference found=%v err=%v", found, err)
	}
	db = &contactTestDB{rowResults: []readsRow{contactTestRow(true), contactTestRow(int64(500)), contactTestRow(int64(3))}}
	count, err := contactTestRepository(t, db).PrioritiseContactSizes(ctx, 7, "one@N00")
	if err != nil || count != 503 {
		t.Fatalf("prioritised=%d err=%v", count, err)
	}
	if len(db.queries) != 3 || !strings.Contains(db.queries[1], "UPDATE work_items") || !strings.Contains(db.queries[1], "wi.user_id=$1") || !strings.Contains(db.queries[1], "LIMIT") || !strings.Contains(db.queries[1], "direct_action=FALSE") {
		t.Fatalf("size priority query is not actor scoped: %s", db.queries[1])
	}
	db = &contactTestDB{rowResults: []readsRow{contactTestRow(false)}}
	if _, err := contactTestRepository(t, db).PrioritiseContactSizes(ctx, 7, "one@N00"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("unowned size priority error=%v", err)
	}
	db = &contactTestDB{rowResults: []readsRow{{err: errors.New("priority query failed")}}}
	if _, err := contactTestRepository(t, db).PrioritiseContactSizes(ctx, 7, "one@N00"); err == nil {
		t.Fatal("priority existence query error swallowed")
	}
	db = &contactTestDB{rowResults: []readsRow{contactTestRow(true), {err: errors.New("priority write failed")}}}
	if _, err := contactTestRepository(t, db).PrioritiseContactSizes(ctx, 7, "one@N00"); err == nil {
		t.Fatal("priority write error swallowed")
	}
	if _, found, err := contactTestRepository(t, &contactTestDB{}).SetContactGroupPoolCrawl(ctx, 7, "one@N00", "group", true); err != nil || found {
		t.Fatalf("missing group preference found=%v err=%v", found, err)
	}
	if _, _, err := contactTestRepository(t, &contactTestDB{}).GetContact(ctx, 0, "one@N00"); !errors.Is(err, app.ErrReadUnavailable) {
		t.Fatalf("invalid actor error=%v", err)
	}
}

func stringPointerForTest(value string) *string { return &value }
