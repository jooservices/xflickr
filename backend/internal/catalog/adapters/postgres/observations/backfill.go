package observations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// BackfillOptions requires an operator's explicit legacy attribution.
type BackfillOptions struct {
	UserID    int64
	AccountID int64
	BatchSize int
}

// BackfillReport is safe to print, including when attribution is refused.
type BackfillReport struct {
	Users    int64     `json:"users"`
	Accounts int64     `json:"accounts"`
	People   int64     `json:"people"`
	Photos   int64     `json:"photos"`
	Inserted int64     `json:"inserted"`
	Batches  int       `json:"batches"`
	RunAt    time.Time `json:"run_at"`
}

// BackfillDB starts each bounded batch in its own transaction.
type BackfillDB interface {
	Begin(context.Context) (pgx.Tx, error)
}

// Backfill attributes the legacy catalog only under the lead-approved
// single-user/account attestation. Restarting scans keysets again and skips
// existing observations; committed batches and first-seen times are preserved.
func Backfill(ctx context.Context, database BackfillDB, options BackfillOptions) (BackfillReport, error) {
	report := BackfillReport{RunAt: time.Now().UTC()}
	if ctx == nil || database == nil || options.UserID <= 0 || options.AccountID <= 0 || options.BatchSize < 1 || options.BatchSize > 10000 {
		return report, errors.New("catalog: backfill requires database, positive user/account and batch size 1..10000")
	}
	for _, kind := range []string{"person", "photo"} {
		after := ""
		for {
			next, n, err := backfillBatch(ctx, database, options, kind, after, &report)
			if err != nil {
				return report, err
			}
			report.Inserted += n
			report.Batches++
			if next == "" {
				break
			}
			after = next
		}
	}
	return report, nil
}

func backfillBatch(ctx context.Context, database BackfillDB, options BackfillOptions, kind, after string, report *BackfillReport) (string, int64, error) {
	tx, err := database.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // safe after commit
	if err = validateLegacyObserver(ctx, tx, options, report); err != nil {
		return "", 0, err
	}
	table, id := "people", "nsid"
	if kind == "photo" {
		table, id = "photos", "id"
	}
	// Identifiers come only from the fixed person/photo mapping above. Photos
	// use the numeric primary key rather than a text cast for index pagination.
	boundary, order := id+` > $5`, id
	if kind == "photo" {
		boundary = `(NULLIF($5,'') IS NULL OR id > NULLIF($5,'')::bigint)`
	}
	query := `WITH page AS MATERIALIZED (
 SELECT ` + id + ` AS key, COALESCE(last_seen_at,$4) AS seen FROM ` + table + `
 WHERE ` + boundary + ` ORDER BY ` + order + ` LIMIT $6
 ), inserted AS (
 INSERT INTO observations(user_id,flickr_account_id,record_kind,record_id,first_seen_at)
 SELECT $1,$2,$3,key::text,seen FROM page ON CONFLICT DO NOTHING RETURNING 1
 ) SELECT COALESCE((SELECT key::text FROM page ORDER BY key DESC LIMIT 1),''), (SELECT count(*) FROM inserted)`
	var next string
	var inserted int64
	if err = tx.QueryRow(ctx, query, options.UserID, options.AccountID, kind, report.RunAt, after, options.BatchSize).Scan(&next, &inserted); err != nil {
		return "", 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	return next, inserted, nil
}

func validateLegacyObserver(ctx context.Context, tx pgx.Tx, options BackfillOptions, report *BackfillReport) error {
	// Prevent registration/account changes between validating attribution and commit.
	if _, err := tx.Exec(ctx, `LOCK TABLE users, flickr_accounts IN SHARE MODE`); err != nil {
		return err
	}
	var matches bool
	err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM flickr_accounts),
 EXISTS(SELECT 1 FROM flickr_accounts a JOIN users u ON u.id=a.user_id WHERE u.id=$1 AND a.id=$2)`, options.UserID, options.AccountID).Scan(&report.Users, &report.Accounts, &matches)
	if err != nil {
		return err
	}
	if report.Batches == 0 || report.Users != 1 || report.Accounts != 1 || !matches {
		if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM people), (SELECT count(*) FROM photos)`).Scan(&report.People, &report.Photos); err != nil {
			return err
		}
	}
	if report.Users != 1 || report.Accounts != 1 || !matches {
		return fmt.Errorf("catalog: attribution refused: users=%d accounts=%d people=%d photos=%d; resolve unattributed catalog records before retrying", report.Users, report.Accounts, report.People, report.Photos)
	}
	return nil
}
