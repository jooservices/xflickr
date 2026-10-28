package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
)

type repositoryPool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Begin(context.Context) (pgx.Tx, error)
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type Repository struct{ pool repositoryPool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (repo *Repository) UsersExist(ctx context.Context) (bool, error) {
	var exists bool
	err := repo.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists)
	return exists, err
}

func (repo *Repository) Register(ctx context.Context, user domain.User, registrationOpen bool) (domain.User, error) {
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(93101)`); err != nil {
		return domain.User{}, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return domain.User{}, err
	}
	if count > 0 {
		if !registrationOpen {
			return domain.User{}, app.ErrRegistrationClosed
		}
		user.Role = domain.RoleUser
	} else {
		user.Role = domain.RoleAdmin
	}
	err = tx.QueryRow(ctx, `INSERT INTO users(email,password_hash,role,locale) VALUES($1,$2,$3,$4) RETURNING id,email,role,locale,created_at`, user.Email, user.PasswordHash, string(user.Role), user.Locale).Scan(&user.ID, &user.Email, &user.Role, &user.Locale, &user.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.User{}, app.ErrEmailExists
		}
		return domain.User{}, fmt.Errorf("identity: create user: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func (repo *Repository) FindByEmail(ctx context.Context, email string) (domain.User, error) {
	var user domain.User
	var disabledAt *string
	err := repo.pool.QueryRow(ctx, `SELECT id,email,password_hash,role,locale,created_at,disabled_at::text FROM users WHERE email=$1`, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Locale, &user.CreatedAt, &disabledAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, app.ErrUserNotFound
		}
		return domain.User{}, err
	}
	user.Disabled = disabledAt != nil
	return user, nil
}

func (repo *Repository) FindByID(ctx context.Context, id int64) (domain.User, error) {
	var user domain.User
	var disabledAt *string
	err := repo.pool.QueryRow(ctx, `SELECT id,email,password_hash,role,locale,created_at,disabled_at::text FROM users WHERE id=$1`, id).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role, &user.Locale, &user.CreatedAt, &disabledAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, app.ErrUserNotFound
		}
		return domain.User{}, err
	}
	user.Disabled = disabledAt != nil
	return user, nil
}

func (repo *Repository) UpdateLocale(ctx context.Context, id int64, locale string) (domain.User, error) {
	var user domain.User
	err := repo.pool.QueryRow(ctx, `UPDATE users SET locale=$2 WHERE id=$1 AND disabled_at IS NULL RETURNING id,email,role,locale,created_at`, id, locale).Scan(&user.ID, &user.Email, &user.Role, &user.Locale, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, app.ErrUserNotFound
		}
		return domain.User{}, fmt.Errorf("identity: update user locale: %w", err)
	}
	return user, nil
}

func (repo *Repository) UpdatePassword(ctx context.Context, id int64, passwordHash string) error {
	tag, err := repo.pool.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1 AND disabled_at IS NULL`, id, passwordHash)
	if err != nil {
		return fmt.Errorf("identity: update user password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return app.ErrUserNotFound
	}
	return nil
}
