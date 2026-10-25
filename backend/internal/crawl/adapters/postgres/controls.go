package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/crawl/app"
)

type controlRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}
type controlDB interface {
	Query(context.Context, string, ...any) (controlRows, error)
	QueryRow(context.Context, string, ...any) rowScanner
	Exec(context.Context, string, ...any) error
}
type (
	rowScanner  interface{ Scan(...any) error }
	controlPool struct{ pool *pgxpool.Pool }
)

func (p controlPool) Query(ctx context.Context, q string, args ...any) (controlRows, error) {
	return p.pool.Query(ctx, q, args...)
}

func (p controlPool) QueryRow(ctx context.Context, q string, args ...any) rowScanner {
	return p.pool.QueryRow(ctx, q, args...)
}

func (p controlPool) Exec(ctx context.Context, q string, args ...any) error {
	_, err := p.pool.Exec(ctx, q, args...)
	return err
}

var errControlsDatabaseRequired = errors.New("crawl: control database is required")

// Controls persists crawl status and per-contact controls.
type Controls struct{ pool controlDB }

func NewControls(pool *pgxpool.Pool) (*Controls, error) {
	if pool == nil {
		return nil, errControlsDatabaseRequired
	}
	return &Controls{pool: controlPool{pool}}, nil
}

func (r *Controls) Status(ctx context.Context, userID int64) (app.CrawlStatus, error) {
	if r == nil || r.pool == nil || ctx == nil || userID <= 0 {
		return app.CrawlStatus{}, errors.New("crawl: control database, context, and user are required")
	}
	result := app.CrawlStatus{Depths: []app.DepthCount{}, Facets: []app.FacetProgress{}, Lanes: []app.LaneStatus{}}
	var err error
	if result.Depths, err = r.depthStatus(ctx, userID); err != nil {
		return result, err
	}
	if result.Facets, err = r.facetStatus(ctx, userID); err != nil {
		return result, err
	}
	if result.Lanes, err = r.laneStatus(ctx, userID); err != nil {
		return result, err
	}
	return result, nil
}

func (r *Controls) depthStatus(ctx context.Context, userID int64) ([]app.DepthCount, error) {
	result := make([]app.DepthCount, 0)
	depthRows, err := r.pool.Query(ctx, `SELECT depth,count(*) FROM crawl_interests WHERE user_id=$1 GROUP BY depth ORDER BY depth`, userID)
	if err != nil {
		return nil, fmt.Errorf("crawl: query depth status: %w", err)
	}
	for depthRows.Next() {
		var item app.DepthCount
		if err := depthRows.Scan(&item.Depth, &item.Contacts); err != nil {
			depthRows.Close()
			return nil, fmt.Errorf("crawl: scan depth status: %w", err)
		}
		result = append(result, item)
	}
	if err := depthRows.Err(); err != nil {
		depthRows.Close()
		return nil, fmt.Errorf("crawl: read depth status: %w", err)
	}
	depthRows.Close()
	return result, nil
}

func (r *Controls) facetStatus(ctx context.Context, userID int64) ([]app.FacetProgress, error) {
	result := make([]app.FacetProgress, 0)
	facetRows, err := r.pool.Query(ctx, `SELECT kind,count(*),count(*) FILTER(WHERE state='done'),count(*) FILTER(WHERE state IN ('pending','ready','running','waiting')),count(*) FILTER(WHERE state='failed') FROM work_items WHERE user_id=$1 GROUP BY kind ORDER BY kind`, userID)
	if err != nil {
		return nil, fmt.Errorf("crawl: query facet status: %w", err)
	}
	for facetRows.Next() {
		var item app.FacetProgress
		if err := facetRows.Scan(&item.Facet, &item.Total, &item.Done, &item.Active, &item.Failed); err != nil {
			facetRows.Close()
			return nil, fmt.Errorf("crawl: scan facet status: %w", err)
		}
		result = append(result, item)
	}
	if err := facetRows.Err(); err != nil {
		facetRows.Close()
		return nil, fmt.Errorf("crawl: read facet status: %w", err)
	}
	facetRows.Close()
	return result, nil
}

func (r *Controls) laneStatus(ctx context.Context, userID int64) ([]app.LaneStatus, error) {
	result := make([]app.LaneStatus, 0)
	laneRows, err := r.pool.Query(ctx, `SELECT lane, count(*) FILTER(WHERE state='pending'), count(*) FILTER(WHERE state IN ('ready','running')), count(*) FILTER(WHERE state='waiting'), count(*) FILTER(WHERE paused) FROM (SELECT CASE WHEN kind ILIKE '%size%' THEN 'sizes' ELSE 'person' END lane,state,EXISTS(SELECT 1 FROM crawl_interests ci WHERE ci.user_id=work_items.user_id AND ci.person_nsid=work_items.subject_key AND ci.paused_at IS NOT NULL) paused FROM work_items WHERE user_id=$1 AND state IN ('pending','ready','running','waiting')) backlog GROUP BY lane ORDER BY lane`, userID)
	if err != nil {
		return nil, fmt.Errorf("crawl: query lane status: %w", err)
	}
	for laneRows.Next() {
		var item app.LaneStatus
		if err := laneRows.Scan(&item.Lane, &item.Pending, &item.Running, &item.Waiting, &item.Paused); err != nil {
			laneRows.Close()
			return nil, fmt.Errorf("crawl: scan lane status: %w", err)
		}
		result = append(result, item)
	}
	if err := laneRows.Err(); err != nil {
		laneRows.Close()
		return nil, fmt.Errorf("crawl: read lane status: %w", err)
	}
	laneRows.Close()
	return result, nil
}

func (r *Controls) SetStarred(ctx context.Context, userID int64, person string, starred bool) error {
	return r.update(ctx, userID, person, `WITH changed AS (UPDATE crawl_interests SET starred=$3 WHERE user_id=$1 AND person_nsid=$2 RETURNING user_id,person_nsid,starred,selected_until) UPDATE work_items w SET starred=changed.starred,score_updated_at=NULL,updated_at=now() FROM changed WHERE w.user_id=changed.user_id AND w.subject_key=changed.person_nsid AND w.state IN ('pending','ready','running','waiting')`, starred)
}

func (r *Controls) Select(ctx context.Context, userID int64, person string, until time.Time) error {
	return r.update(ctx, userID, person, `WITH changed AS (UPDATE crawl_interests SET selected_until=$3 WHERE user_id=$1 AND person_nsid=$2 RETURNING user_id,person_nsid,starred,selected_until) UPDATE work_items w SET starred=changed.starred,direct_action=TRUE,score_updated_at=NULL,updated_at=now() FROM changed WHERE w.user_id=changed.user_id AND w.subject_key=changed.person_nsid AND w.state IN ('pending','ready','running','waiting')`, until)
}

func (r *Controls) SetPaused(ctx context.Context, userID int64, person string, paused bool) error {
	var err error
	if r == nil || r.pool == nil || ctx == nil || userID <= 0 || person == "" {
		return errors.New("crawl: invalid contact control request")
	}
	if paused {
		err = r.pool.Exec(ctx, `UPDATE crawl_interests SET paused_at=now() WHERE user_id=$1 AND person_nsid=$2`, userID, person)
	} else {
		err = r.pool.Exec(ctx, `UPDATE crawl_interests SET paused_at=NULL WHERE user_id=$1 AND person_nsid=$2`, userID, person)
	}
	if err != nil {
		return fmt.Errorf("crawl: update contact pause: %w", err)
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2)`, userID, person).Scan(&exists); err != nil {
		return fmt.Errorf("crawl: verify contact pause: %w", err)
	}
	if !exists {
		return app.ErrPersonNotFound
	}
	return nil
}

func (r *Controls) update(ctx context.Context, userID int64, person, query string, value any) error {
	if r == nil || r.pool == nil || ctx == nil || userID <= 0 || person == "" {
		return errors.New("crawl: invalid contact control request")
	}
	if err := r.pool.Exec(ctx, query, userID, person, value); err != nil {
		return fmt.Errorf("crawl: update contact control: %w", err)
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM crawl_interests WHERE user_id=$1 AND person_nsid=$2)`, userID, person).Scan(&exists); err != nil {
		return fmt.Errorf("crawl: verify contact control: %w", err)
	}
	if !exists {
		return app.ErrPersonNotFound
	}
	return nil
}
