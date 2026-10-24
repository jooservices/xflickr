package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
)

// adminChangeLockKey serialises role and disabled changes so two concurrent
// requests cannot each remove a different administrator and leave none.
const adminChangeLockKey = 93102

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

const userColumns = `id,email,password_hash,role,locale,created_at,disabled_at IS NOT NULL`

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Locale, &user.CreatedAt, &user.Disabled)
	return user, err
}

// ListUsers returns users newest first whose email contains search, ignoring case.
func (repo *Repository) ListUsers(ctx context.Context, search string, limit, offset int) ([]domain.User, int64, error) {
	pattern := "%" + likeEscaper.Replace(search) + "%"
	var total int64
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email::text ILIKE $1`, pattern).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("identity: count users: %w", err)
	}
	rows, err := repo.pool.Query(ctx, `SELECT `+userColumns+` FROM users WHERE email::text ILIKE $1 ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, pattern, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("identity: list users: %w", err)
	}
	defer rows.Close()
	users := []domain.User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("identity: scan user: %w", err)
		}
		users = append(users, user)
	}
	return users, total, rows.Err()
}

// UpdateUser applies decide to the user under a transaction-wide lock and stores the result.
func (repo *Repository) UpdateUser(ctx context.Context, id int64, decide func(domain.User, int) (domain.User, error)) (domain.User, domain.User, error) {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return domain.User{}, domain.User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, adminChangeLockKey); err != nil {
		return domain.User{}, domain.User{}, err
	}
	before, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.User{}, app.ErrUserNotFound
	}
	if err != nil {
		return domain.User{}, domain.User{}, fmt.Errorf("identity: load user: %w", err)
	}
	var admins int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role='admin' AND disabled_at IS NULL`).Scan(&admins); err != nil {
		return domain.User{}, domain.User{}, fmt.Errorf("identity: count administrators: %w", err)
	}
	next, err := decide(before, admins)
	if err != nil {
		return domain.User{}, domain.User{}, err
	}
	after, err := scanUser(tx.QueryRow(ctx, `UPDATE users SET role=$2, disabled_at = CASE WHEN $3 THEN COALESCE(disabled_at, now()) ELSE NULL END WHERE id=$1 RETURNING `+userColumns, id, string(next.Role), next.Disabled))
	if err != nil {
		return domain.User{}, domain.User{}, fmt.Errorf("identity: update user: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.User{}, domain.User{}, err
	}
	return before, after, nil
}
