// Package postgres persists application and user settings in PostgreSQL.
// A settings write and its settings.changed outbox event commit together.
package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jooservices/xflickr/internal/platform/db"
	"github.com/jooservices/xflickr/internal/platform/outbox"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

const (
	loadApplicationSettingsSQL = `
SELECT key, value, user_overridable, min, max
FROM app_settings
ORDER BY key`

	loadUserSettingsSQL = `
SELECT key, value
FROM user_settings
WHERE user_id = $1
ORDER BY key`

	upsertUserSettingSQL = `
INSERT INTO user_settings (user_id, key, value)
VALUES ($1, $2, $3::jsonb)
ON CONFLICT (user_id, key) DO UPDATE SET value = EXCLUDED.value`

	upsertApplicationSettingSQL = `
INSERT INTO app_settings (key, value, user_overridable, min, max)
VALUES ($1, $2::jsonb, $3, $4, $5)
ON CONFLICT (key) DO UPDATE SET
    value = EXCLUDED.value,
    user_overridable = EXCLUDED.user_overridable,
    min = EXCLUDED.min,
    max = EXCLUDED.max`
)

var (
	errDatabaseRequired = errors.New("settings: postgres database is required")
	errContextRequired  = errors.New("settings: context is required")
)

// Repository reads and writes settings through a serializable unit of work.
type Repository struct {
	unitOfWork *db.UnitOfWork
}

// New creates a repository backed by database. database is typically a pgx pool.
func New(database db.Beginner) (*Repository, error) {
	if database == nil {
		return nil, errDatabaseRequired
	}
	return &Repository{unitOfWork: db.NewUnitOfWork(database)}, nil
}

// Load returns the application settings and one user's overrides from one transaction.
func (repository *Repository) Load(ctx context.Context, userID int64) (domain.Snapshot, error) {
	if err := repository.ready(ctx); err != nil {
		return domain.Snapshot{}, err
	}
	var snapshot domain.Snapshot
	err := repository.unitOfWork.Do(ctx, func(tx db.Tx) error {
		loaded, err := readSnapshot(ctx, tx, userID)
		if err != nil {
			return err
		}
		snapshot = loaded
		return nil
	})
	if err != nil {
		return domain.Snapshot{}, err
	}
	return snapshot, nil
}

// SetUserOverride upserts one user value and publishes settings.changed.
func (repository *Repository) SetUserOverride(ctx context.Context, userID int64, key domain.Key, value json.RawMessage) error {
	if err := repository.ready(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(string(key)) == "" {
		return errors.New("settings: key is required")
	}
	if len(bytes.TrimSpace(value)) == 0 {
		return fmt.Errorf("settings: value for %s is required", key)
	}
	return repository.unitOfWork.Do(ctx, func(tx db.Tx) error {
		return setUserOverride(ctx, tx, userID, key, value)
	})
}

// SetApplicationSetting upserts one application default and publishes settings.changed.
func (repository *Repository) SetApplicationSetting(ctx context.Context, setting domain.Definition) error {
	if err := repository.ready(ctx); err != nil {
		return err
	}
	if strings.TrimSpace(string(setting.Key)) == "" {
		return errors.New("settings: application setting key is required")
	}
	if len(bytes.TrimSpace(setting.Value)) == 0 {
		return fmt.Errorf("settings: value for %s is required", setting.Key)
	}
	return repository.unitOfWork.Do(ctx, func(tx db.Tx) error {
		return setApplicationSetting(ctx, tx, setting)
	})
}

func (repository *Repository) ready(ctx context.Context) error {
	if repository == nil || repository.unitOfWork == nil {
		return errDatabaseRequired
	}
	if ctx == nil {
		return errContextRequired
	}
	return ctx.Err()
}

func readSnapshot(ctx context.Context, tx db.Tx, userID int64) (domain.Snapshot, error) {
	settings, err := loadApplicationSettings(ctx, tx)
	if err != nil {
		return domain.Snapshot{}, err
	}
	overrides, err := loadUserSettings(ctx, tx, userID)
	if err != nil {
		return domain.Snapshot{}, err
	}
	return domain.Snapshot{Settings: settings, Overrides: overrides}, nil
}

func loadApplicationSettings(ctx context.Context, tx db.Tx) (map[domain.Key]domain.Definition, error) {
	rows, err := tx.Query(ctx, loadApplicationSettingsSQL)
	if err != nil {
		return nil, fmt.Errorf("settings: query application settings: %w", err)
	}
	defer rows.Close()

	settings := make(map[domain.Key]domain.Definition)
	for rows.Next() {
		var key string
		var value []byte
		var overridable bool
		var minimum, maximum *float64
		if err := rows.Scan(&key, &value, &overridable, &minimum, &maximum); err != nil {
			return nil, fmt.Errorf("settings: read application setting: %w", err)
		}
		typed := domain.Key(key)
		settings[typed] = domain.Definition{
			Key:             typed,
			Value:           bytes.Clone(value),
			UserOverridable: overridable,
			Min:             minimum,
			Max:             maximum,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: iterate application settings: %w", err)
	}
	return settings, nil
}

func loadUserSettings(ctx context.Context, tx db.Tx, userID int64) (map[domain.Key]json.RawMessage, error) {
	rows, err := tx.Query(ctx, loadUserSettingsSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("settings: query user settings: %w", err)
	}
	defer rows.Close()

	overrides := make(map[domain.Key]json.RawMessage)
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("settings: read user setting: %w", err)
		}
		overrides[domain.Key(key)] = bytes.Clone(value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("settings: iterate user settings: %w", err)
	}
	return overrides, nil
}

func setUserOverride(ctx context.Context, tx db.Tx, userID int64, key domain.Key, value json.RawMessage) error {
	if _, err := tx.Exec(ctx, upsertUserSettingSQL, userID, string(key), string(value)); err != nil {
		return fmt.Errorf("settings: save user setting %s: %w", key, err)
	}
	userIDCopy := userID
	change := domain.Change{
		Key:    key,
		Scope:  domain.ScopeUser,
		UserID: &userIDCopy,
		Value:  value,
	}
	if err := outbox.PublishContext(ctx, tx, domain.ChangedTopic, userEventKey(userID, key), change); err != nil {
		return fmt.Errorf("settings: publish user setting change %s: %w", key, err)
	}
	return nil
}

func setApplicationSetting(ctx context.Context, tx db.Tx, setting domain.Definition) error {
	if _, err := tx.Exec(ctx, upsertApplicationSettingSQL, string(setting.Key), string(setting.Value), setting.UserOverridable, setting.Min, setting.Max); err != nil {
		return fmt.Errorf("settings: save application setting %s: %w", setting.Key, err)
	}
	overridable := setting.UserOverridable
	change := domain.Change{
		Key:             setting.Key,
		Scope:           domain.ScopeApplication,
		Value:           setting.Value,
		UserOverridable: &overridable,
		Min:             setting.Min,
		Max:             setting.Max,
	}
	if err := outbox.PublishContext(ctx, tx, domain.ChangedTopic, applicationEventKey(setting.Key), change); err != nil {
		return fmt.Errorf("settings: publish application setting change %s: %w", setting.Key, err)
	}
	return nil
}

func userEventKey(userID int64, key domain.Key) string {
	return fmt.Sprintf("user:%d:%s", userID, key)
}

func applicationEventKey(key domain.Key) string {
	return "application:" + string(key)
}
