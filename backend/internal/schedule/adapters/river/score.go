package river

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/schedule/domain"
)

const staticScoreExpr = `COALESCE(p.weight,1)*$1
	+ CASE WHEN work_items_active_direct_action(w2.direct_action,w2.user_id,w2.subject_key) THEN $2 ELSE 0 END
	+ CASE WHEN w2.starred THEN $3 ELSE 0 END
	+ ($4::int - w2.depth)*$5
	- $6*EXTRACT(EPOCH FROM w2.created_at)/60.0`

// RescorePendingBatch rewrites a bounded batch of pending scores with one
// set-based UPDATE. The stored value is the time-invariant static sort key.
func (s Store) RescorePendingBatch(ctx context.Context, batchSize int, weights domain.ScoreWeights, maxDepth int) (int, error) {
	if s.backend() == nil || ctx == nil || batchSize <= 0 || maxDepth < 0 {
		return 0, errors.New("schedule: invalid score rescore dependencies")
	}
	if _, err := domain.Score(domain.ScoreInput{MaxDepth: maxDepth}, weights); err != nil {
		return 0, err
	}
	return s.rescoreSet(ctx, `WHERE w2.state='pending' AND w2.score IS DISTINCT FROM (`+staticScoreExpr+`) ORDER BY w2.id LIMIT $7`, []any{
		weights.PlanWeight, weights.DirectAction, weights.Starred, maxDepth, weights.Depth, weights.AgeMinutes, batchSize,
	})
}

// RescoreExpiredSelectionsBatch removes direct-action bonuses after the
// associated user's selection expires, without scanning unrelated pending work.
func (s Store) RescoreExpiredSelectionsBatch(ctx context.Context, batchSize int, weights domain.ScoreWeights, maxDepth int) (int, error) {
	if s.backend() == nil || ctx == nil || batchSize <= 0 || maxDepth < 0 {
		return 0, errors.New("schedule: invalid expired selection rescore dependencies")
	}
	if _, err := domain.Score(domain.ScoreInput{MaxDepth: maxDepth}, weights); err != nil {
		return 0, err
	}
	return s.rescoreSet(ctx, `WHERE w2.state='pending'
		AND w2.direct_action
		AND ci.selected_until IS NOT NULL
		AND ci.selected_until <= now()
		AND w2.score IS DISTINCT FROM (`+staticScoreExpr+`)
		ORDER BY ci.selected_until, w2.id LIMIT $7`, []any{
		weights.PlanWeight, weights.DirectAction, weights.Starred, maxDepth, weights.Depth, weights.AgeMinutes, batchSize,
	})
}

// RescorePerson immediately refreshes the stored scores of one user's pending
// backlog for a person, so star/select changes affect dispatch order at once.
func (s Store) RescorePerson(ctx context.Context, userID int64, person string, weights domain.ScoreWeights, maxDepth int) (int, error) {
	if s.backend() == nil || ctx == nil || userID <= 0 || person == "" || maxDepth < 0 {
		return 0, errors.New("schedule: invalid person rescore dependencies")
	}
	if _, err := domain.Score(domain.ScoreInput{MaxDepth: maxDepth}, weights); err != nil {
		return 0, err
	}
	return s.rescoreSet(ctx, `WHERE w2.state='pending' AND w2.user_id=$7 AND w2.subject_key=$8`, []any{
		weights.PlanWeight, weights.DirectAction, weights.Starred, maxDepth, weights.Depth, weights.AgeMinutes, userID, person,
	})
}

func (s Store) rescoreSet(ctx context.Context, selection string, args []any) (int, error) {
	tag, err := s.backend().Exec(ctx, `
UPDATE work_items AS w SET score = batch.score
FROM (
	SELECT w2.id, `+staticScoreExpr+` AS score
	FROM work_items w2
	LEFT JOIN users u ON u.id=w2.user_id
	LEFT JOIN plans p ON p.id=u.plan_id
	LEFT JOIN crawl_interests ci ON ci.user_id=w2.user_id AND ci.person_nsid=w2.subject_key
	`+selection+`
) AS batch
WHERE w.id = batch.id`, args...)
	if err != nil {
		return 0, fmt.Errorf("schedule: set-based score update: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

const weightsFingerprintKey = "score_weights_fingerprint"

// ComputeWeightsFingerprint returns the live fingerprint of score weights, max
// depth, and plan weights used to detect a needed full pending rescore.
func (s Store) ComputeWeightsFingerprint(ctx context.Context, weights domain.ScoreWeights, maxDepth int) (string, error) {
	if s.backend() == nil || ctx == nil {
		return "", errors.New("schedule: fingerprint dependencies are required")
	}
	var planDigest string
	if err := s.backend().QueryRow(ctx, `SELECT coalesce(md5(string_agg(id::text || ':' || weight::text, ',' ORDER BY id)), '') FROM plans`).Scan(&planDigest); err != nil {
		return "", fmt.Errorf("schedule: hash plan weights: %w", err)
	}
	return domain.WeightsFingerprint(weights, maxDepth) + "|plans:" + planDigest, nil
}

// LoadWeightsFingerprint reads the last completed rescore fingerprint.
func (s Store) LoadWeightsFingerprint(ctx context.Context) (string, error) {
	if s.backend() == nil || ctx == nil {
		return "", errors.New("schedule: fingerprint dependencies are required")
	}
	var value string
	err := s.backend().QueryRow(ctx, `SELECT value FROM schedule_meta WHERE key=$1`, weightsFingerprintKey).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("schedule: load weights fingerprint: %w", err)
	}
	return value, nil
}

// StoreWeightsFingerprint records that a full pending rescore finished for fp.
func (s Store) StoreWeightsFingerprint(ctx context.Context, fp string) error {
	if s.backend() == nil || ctx == nil {
		return errors.New("schedule: fingerprint dependencies are required")
	}
	_, err := s.backend().Exec(ctx, `
INSERT INTO schedule_meta (key, value) VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, weightsFingerprintKey, fp)
	if err != nil {
		return fmt.Errorf("schedule: store weights fingerprint: %w", err)
	}
	return nil
}

// PersonRescorer rescores a person's backlog using the stored pump settings.
type PersonRescorer struct{ Pool *pgxpool.Pool }

func (r PersonRescorer) RescorePerson(ctx context.Context, userID int64, person string) error {
	if r.Pool == nil || ctx == nil {
		return errors.New("schedule: person rescorer dependencies are required")
	}
	settings, err := LoadSettings(ctx, r.Pool)
	if err != nil {
		return err
	}
	_, err = Store{Pool: r.Pool}.RescorePerson(ctx, userID, person, settings.ScoreWeights, settings.MaxDepth)
	return err
}
