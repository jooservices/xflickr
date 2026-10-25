package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Group stores Flickr group metadata.
type Group struct {
	NSID, Name, Description string
	MembersCount, PoolCount int64
	SeenAt                  time.Time
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
func (r *Repository) UpsertGroupsBatch(ctx context.Context, groups []Group) error {
	if err := validateBatchSize(len(groups)); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	for _, g := range groups {
		if g.NSID == "" || g.Name == "" || g.MembersCount < 0 || g.PoolCount < 0 {
			return errors.New("catalog: invalid group")
		}
		batch.Queue(`INSERT INTO groups (nsid,name,description,members_count,pool_count,seen_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (nsid) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,members_count=EXCLUDED.members_count,pool_count=EXCLUDED.pool_count,seen_at=EXCLUDED.seen_at WHERE (groups.name,groups.description,groups.members_count,groups.pool_count,groups.seen_at) IS DISTINCT FROM (EXCLUDED.name,EXCLUDED.description,EXCLUDED.members_count,EXCLUDED.pool_count,EXCLUDED.seen_at)`, g.NSID, g.Name, g.Description, g.MembersCount, g.PoolCount, seenAt(g.SeenAt))
	}
	return executeBatch(ctx, r.db, batch, "groups")
}

// UpsertPersonGroupsBatch inserts or refreshes group membership rows.
func (r *Repository) UpsertPersonGroupsBatch(ctx context.Context, memberships []PersonGroup) error {
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
