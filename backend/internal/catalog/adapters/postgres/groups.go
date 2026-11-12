package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
)

// Group stores Flickr group metadata.
type Group struct {
	NSID, Name, Description string
	MembersCount, PoolCount int64
	SeenAt                  time.Time
	Missing                 catalog.GroupMetadataMissing
}

// PersonGroup records a person's Flickr group membership.
type PersonGroup struct {
	PersonNSID, GroupNSID string
	SeenAt                time.Time
}

// GroupPoolPhoto records a photo in a Flickr group's pool.
type GroupPoolPhoto struct {
	GroupNSID string
	PhotoID   int64
	SeenAt    time.Time
}

// UpsertGroupsBatch inserts or refreshes group metadata.
// Inputs larger than UpsertPeopleBatchMax are written in successive chunks that
// share the caller's connection or transaction.
func (r *Repository) UpsertGroupsBatch(ctx context.Context, groups []Group) error {
	return forEachChunk(groups, UpsertPeopleBatchMax, func(chunk []Group) error {
		return r.upsertGroupsChunk(ctx, chunk)
	})
}

func (r *Repository) upsertGroupsChunk(ctx context.Context, groups []Group) error {
	if err := validateBatchSize(len(groups)); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, g := range groups {
		if g.NSID == "" || g.Name == "" || g.MembersCount < 0 || g.PoolCount < 0 {
			return errors.New("catalog: invalid group")
		}
		batch.Queue(`INSERT INTO groups (nsid,name,description,members_count,pool_count,seen_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (nsid) DO UPDATE SET name=EXCLUDED.name,description=CASE WHEN $7 THEN groups.description ELSE EXCLUDED.description END,members_count=CASE WHEN $8 THEN groups.members_count ELSE EXCLUDED.members_count END,pool_count=CASE WHEN $9 THEN groups.pool_count ELSE EXCLUDED.pool_count END,seen_at=EXCLUDED.seen_at WHERE groups.name IS DISTINCT FROM EXCLUDED.name OR groups.seen_at IS DISTINCT FROM EXCLUDED.seen_at OR (NOT $7 AND groups.description IS DISTINCT FROM EXCLUDED.description) OR (NOT $8 AND groups.members_count IS DISTINCT FROM EXCLUDED.members_count) OR (NOT $9 AND groups.pool_count IS DISTINCT FROM EXCLUDED.pool_count)`, g.NSID, g.Name, g.Description, g.MembersCount, g.PoolCount, seenAt(g.SeenAt), g.Missing.Description, g.Missing.MembersCount, g.Missing.PoolCount)
	}
	return executeBatch(ctx, r.db, batch, "groups")
}

// UpsertPersonGroupsBatch inserts or refreshes group membership rows.
// Inputs larger than UpsertPeopleBatchMax are written in successive chunks that
// share the caller's connection or transaction.
func (r *Repository) UpsertPersonGroupsBatch(ctx context.Context, memberships []PersonGroup) error {
	return forEachChunk(memberships, UpsertPeopleBatchMax, func(chunk []PersonGroup) error {
		return r.upsertPersonGroupsChunk(ctx, chunk)
	})
}

func (r *Repository) upsertPersonGroupsChunk(ctx context.Context, memberships []PersonGroup) error {
	if err := validateBatchSize(len(memberships)); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, m := range memberships {
		if m.PersonNSID == "" || m.GroupNSID == "" {
			return errors.New("catalog: invalid person group")
		}
		batch.Queue(`INSERT INTO person_groups (person_nsid,group_nsid,seen_at) VALUES ($1,$2,$3) ON CONFLICT (person_nsid,group_nsid) DO UPDATE SET seen_at=EXCLUDED.seen_at WHERE person_groups.seen_at IS DISTINCT FROM EXCLUDED.seen_at`, m.PersonNSID, m.GroupNSID, seenAt(m.SeenAt))
	}
	return executeBatch(ctx, r.db, batch, "person groups")
}

// UpsertGroupPoolPhotosBatch inserts or refreshes group pool photo rows.
func (r *Repository) UpsertGroupPoolPhotosBatch(ctx context.Context, photos []GroupPoolPhoto) error {
	if err := validateBatchSize(len(photos)); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, p := range photos {
		if p.GroupNSID == "" || p.PhotoID <= 0 {
			return errors.New("catalog: invalid group pool photo")
		}
		batch.Queue(`INSERT INTO group_pool_photos (group_nsid,photo_id,seen_at) VALUES ($1,$2,$3) ON CONFLICT (group_nsid,photo_id) DO UPDATE SET seen_at=EXCLUDED.seen_at WHERE group_pool_photos.seen_at IS DISTINCT FROM EXCLUDED.seen_at`, p.GroupNSID, p.PhotoID, seenAt(p.SeenAt))
	}
	return executeBatch(ctx, r.db, batch, "group pool photos")
}
