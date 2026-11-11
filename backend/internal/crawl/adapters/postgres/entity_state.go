package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/crawl/app"
)

type entityStateDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// EntityStates stores terminal resource outcomes independently of work items.
type EntityStates struct{ DB entityStateDB }

// Suppressed checks global disappearance, photo tombstones and the requesting
// caller's facet restriction. Expiry never reopens a photo restriction.
func (s EntityStates) Suppressed(ctx context.Context, a app.EntityAccess, now time.Time) (bool, error) {
	photoID, _ := strconv.ParseInt(a.ID, 10, 64)
	var blocked bool
	err := s.DB.QueryRow(ctx, `SELECT
 EXISTS (SELECT 1 FROM entity_states WHERE entity_type=$1 AND entity_id=$2)
 OR ($1='photo' AND EXISTS (SELECT 1 FROM photos p WHERE p.id=$8 AND
     (p.tombstoned_at IS NOT NULL OR EXISTS (SELECT 1 FROM entity_states e WHERE e.entity_type='person' AND e.entity_id=p.owner_nsid))))
 OR EXISTS (SELECT 1 FROM entity_facet_restrictions WHERE entity_type=$1 AND entity_id=$2
 AND facet=$3 AND user_id=$4 AND key_id=$5
 AND (($9=0 AND account_id IS NULL) OR account_id=$9)
 AND CASE WHEN entity_type='photo' THEN photo_updated_at>=$6 ELSE recheck_at>$7 END)`,
		a.Kind, a.ID, a.Facet, a.UserID, a.KeyID, a.Version, now, photoID, a.AccountID).Scan(&blocked)
	return blocked, err
}

// RecordFailure preserves the first timestamp/reason. A new eligible probe may
// advance its restriction deadline or photo version without rewriting history.
func (s EntityStates) RecordFailure(ctx context.Context, a app.EntityAccess, state, reason string, now, recheck time.Time) error {
	if state == "gone" {
		_, err := s.DB.Exec(ctx, `INSERT INTO entity_states (entity_type,entity_id,first_seen_at,reason)
   VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, a.Kind, a.ID, now, reason)
		return err
	}
	var err error
	if a.AccountID > 0 {
		_, err = s.DB.Exec(ctx, `INSERT INTO entity_facet_restrictions
 (entity_type,entity_id,facet,user_id,key_id,account_id,first_seen_at,reason,recheck_at,photo_updated_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
 ON CONFLICT (entity_type,entity_id,facet,account_id) WHERE account_id IS NOT NULL DO UPDATE
 SET recheck_at=EXCLUDED.recheck_at,photo_updated_at=EXCLUDED.photo_updated_at
 WHERE (entity_facet_restrictions.entity_type<>'photo' AND entity_facet_restrictions.recheck_at<=$7)
 OR (entity_facet_restrictions.entity_type='photo' AND entity_facet_restrictions.photo_updated_at<$10)`,
			a.Kind, a.ID, a.Facet, a.UserID, a.KeyID, a.AccountID, now, reason, recheck, a.Version)
	} else {
		_, err = s.DB.Exec(ctx, `INSERT INTO entity_facet_restrictions
 (entity_type,entity_id,facet,user_id,key_id,first_seen_at,reason,recheck_at,photo_updated_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
 ON CONFLICT (entity_type,entity_id,facet,user_id,key_id) WHERE account_id IS NULL DO UPDATE
 SET recheck_at=EXCLUDED.recheck_at,photo_updated_at=EXCLUDED.photo_updated_at
 WHERE (entity_facet_restrictions.entity_type<>'photo' AND entity_facet_restrictions.recheck_at<=$6)
 OR (entity_facet_restrictions.entity_type='photo' AND entity_facet_restrictions.photo_updated_at<$9)`,
			a.Kind, a.ID, a.Facet, a.UserID, a.KeyID, now, reason, recheck, a.Version)
	}
	return err
}

// PhotoFacetAllowed checks the current version before a scheduler queues work.
func (s EntityStates) PhotoFacetAllowed(ctx context.Context, photoID, keyID int64, facet string) (bool, error) {
	var id string
	var version time.Time
	var userID int64
	err := s.DB.QueryRow(ctx, `SELECT p.id::text,p.last_update,k.user_id
	FROM photos AS p JOIN api_keys AS k ON k.id=$2 WHERE p.id=$1`, photoID, keyID).Scan(&id, &version, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	blocked, err := s.Suppressed(ctx, app.EntityAccess{EntityRef: app.EntityRef{Kind: "photo", ID: id}, UserID: userID, KeyID: keyID, Facet: facet, Version: version}, time.Now())
	return !blocked, err
}
