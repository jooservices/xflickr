package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

const favoritesBatchMax = UpsertPeopleBatchMax

const favoriteInsertSQL = `INSERT INTO favorites (person_nsid, photo_id, fave_date, seen_at)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (person_nsid, photo_id) DO NOTHING`

const favoriteUpdateSQL = `UPDATE favorites
	SET fave_date = $3, seen_at = $4, removed_at = NULL
	WHERE person_nsid = $1 AND photo_id = $2
	  AND ((fave_date, seen_at) IS DISTINCT FROM ($3, $4)
	       OR removed_at IS NOT NULL)`

// Favorite is a person's Flickr favorite and its reconciliation timestamp.
type Favorite struct {
	PersonNSID string
	PhotoID    int64
	FaveDate   time.Time
	SeenAt     time.Time
}

// FavoriteCursor continues a descending favorite-date/photo-id keyset page.
type FavoriteCursor struct {
	FaveDate time.Time
	PhotoID  int64
}

// FavoritePage contains one page and a cursor when more rows may follow.
type FavoritePage struct {
	Items      []Favorite
	NextCursor *FavoriteCursor
}

// UpsertFavoritesBatch inserts or refreshes a batch of favorites.
func (r *Repository) UpsertFavoritesBatch(ctx context.Context, favorites []Favorite) error {
	if err := validateBatchSize(len(favorites)); err != nil {
		return err
	}
	prepared, err := prepareFavorites(favorites)
	if err != nil {
		return err
	}
	if len(prepared) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, favorite := range prepared {
		batch.Queue(favoriteInsertSQL, favorite.PersonNSID, favorite.PhotoID, favorite.FaveDate, favorite.SeenAt)
	}
	for _, favorite := range prepared {
		batch.Queue(favoriteUpdateSQL, favorite.PersonNSID, favorite.PhotoID, favorite.FaveDate, favorite.SeenAt)
	}
	return executeBatch(ctx, r.db, batch, "favorites")
}

func prepareFavorites(favorites []Favorite) ([]Favorite, error) {
	ordered := append([]Favorite(nil), favorites...)
	for i := range ordered {
		favorite := &ordered[i]
		if favorite.PersonNSID == "" || favorite.PhotoID <= 0 || favorite.FaveDate.IsZero() {
			return nil, errors.New("catalog: invalid favorite")
		}
		favorite.SeenAt = seenAt(favorite.SeenAt)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].PersonNSID != ordered[j].PersonNSID {
			return ordered[i].PersonNSID < ordered[j].PersonNSID
		}
		return ordered[i].PhotoID < ordered[j].PhotoID
	})
	unique := make([]Favorite, 0, len(ordered))
	for _, favorite := range ordered {
		if len(unique) > 0 && unique[len(unique)-1].PersonNSID == favorite.PersonNSID && unique[len(unique)-1].PhotoID == favorite.PhotoID {
			unique[len(unique)-1] = favorite
			continue
		}
		unique = append(unique, favorite)
	}
	return unique, nil
}

// ListFavorites returns at most limit favorites in descending keyset order.
func (r *Repository) ListFavorites(ctx context.Context, person string, cursor *FavoriteCursor, limit int) (FavoritePage, error) {
	if person == "" || limit < 1 || limit > 500 {
		return FavoritePage{}, errors.New("catalog: invalid favorites page request")
	}
	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = r.db.Query(ctx, `SELECT person_nsid, photo_id, fave_date, seen_at FROM favorites WHERE person_nsid=$1 AND removed_at IS NULL ORDER BY fave_date DESC, photo_id DESC LIMIT $2`, person, limit+1)
	} else {
		if cursor.FaveDate.IsZero() || cursor.PhotoID <= 0 {
			return FavoritePage{}, errors.New("catalog: invalid favorites cursor")
		}
		rows, err = r.db.Query(ctx, `SELECT person_nsid, photo_id, fave_date, seen_at FROM favorites WHERE person_nsid=$1 AND removed_at IS NULL AND (fave_date, photo_id)<($2,$3) ORDER BY fave_date DESC, photo_id DESC LIMIT $4`, person, cursor.FaveDate, cursor.PhotoID, limit+1)
	}
	if err != nil {
		return FavoritePage{}, fmt.Errorf("catalog: list favorites: %w", err)
	}
	defer rows.Close()
	items := make([]Favorite, 0, limit+1)
	for rows.Next() {
		var item Favorite
		if err := rows.Scan(&item.PersonNSID, &item.PhotoID, &item.FaveDate, &item.SeenAt); err != nil {
			return FavoritePage{}, fmt.Errorf("catalog: scan favorite: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return FavoritePage{}, fmt.Errorf("catalog: list favorites: %w", err)
	}
	page := FavoritePage{Items: items}
	if len(items) > limit {
		last := items[limit-1]
		page.Items = items[:limit]
		page.NextCursor = &FavoriteCursor{FaveDate: last.FaveDate, PhotoID: last.PhotoID}
	}
	return page, nil
}
