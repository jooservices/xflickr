package postgres

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/jooservices/xflickr/internal/catalog/app"
)

//go:embed photolane.sql
var photoLaneSQL string

const (
	// firstPersonChunk is how many top-ranked persons the first probe reads.
	firstPersonChunk = 128
	// maxPersonChunk bounds how wide a probe may grow.
	maxPersonChunk = 1 << 20
)

type photoLanePool interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

var _ app.PhotoSelector = PhotoLane{}

// PhotoLane selects the photos the photo lane should fetch next.
type PhotoLane struct{ Pool photoLanePool }

// Select returns up to limit photos after skipping the first skip, ordered by
// user-selected contact, starred contact, depth and upload date. It probes the
// top-ranked persons and widens the probe only while it finds too few photos.
func (l PhotoLane) Select(ctx context.Context, limit, skip int) ([]app.PhotoRef, error) {
	if l.Pool == nil || ctx == nil {
		return nil, errors.New("catalog: photo lane database and context are required")
	}
	if limit < 1 || skip < 0 {
		return nil, errors.New("catalog: photo lane limit must be positive and skip cannot be negative")
	}
	want := limit + skip
	var refs []app.PhotoRef
	for chunk := firstPersonChunk; ; chunk *= 2 {
		found, more, err := l.probe(ctx, chunk, want)
		if err != nil {
			return nil, err
		}
		refs = found
		if len(refs) >= want || !more || chunk >= maxPersonChunk {
			break
		}
	}
	if len(refs) <= skip {
		return []app.PhotoRef{}, nil
	}
	return refs[skip:], nil
}

func (l PhotoLane) probe(ctx context.Context, persons, photos int) ([]app.PhotoRef, bool, error) {
	rows, err := l.Pool.Query(ctx, photoLaneSQL, persons, photos)
	if err != nil {
		return nil, false, fmt.Errorf("catalog: select photo lane: %w", err)
	}
	defer rows.Close()
	out := make([]app.PhotoRef, 0, photos)
	more := false
	for rows.Next() {
		var id *int64
		var owner *string
		if err := rows.Scan(&more, &id, &owner); err != nil {
			return nil, false, fmt.Errorf("catalog: scan photo lane: %w", err)
		}
		if id != nil && owner != nil {
			out = append(out, app.PhotoRef{ID: *id, OwnerNSID: *owner})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("catalog: read photo lane: %w", err)
	}
	return out, more, nil
}
