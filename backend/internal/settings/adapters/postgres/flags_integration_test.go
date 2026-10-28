package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

func TestFeatureFlagPostgresLifecycle(t *testing.T) {
	pool := integrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	prefix := fmt.Sprintf("integration.flag.%d", time.Now().UnixNano())
	emailPrefix := fmt.Sprintf("joo87-%d-", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM feature_flags WHERE name LIKE $1`, prefix+"%"); err != nil {
			t.Errorf("delete feature flags: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE email LIKE $1`, emailPrefix+"%"); err != nil {
			t.Errorf("delete feature flag users: %v", err)
		}
	})

	repository, err := postgres.NewFlagRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	var adminID, memberID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','admin') RETURNING id`, emailPrefix+"admin@example.invalid").Scan(&adminID); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','user') RETURNING id`, emailPrefix+"member@example.invalid").Scan(&memberID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	flags := []domain.FeatureFlag{
		{Name: prefix + ".all", Enabled: true, Audience: domain.AudienceAll},
		{Name: prefix + ".admins", Enabled: true, Audience: domain.AudienceAdmins},
		{Name: prefix + ".users", Enabled: true, Audience: domain.AudienceUserIDs, UserIDs: []int64{memberID}},
	}
	if err := repository.UpsertFlags(ctx, flags); err != nil {
		t.Fatalf("UpsertFlags() error = %v", err)
	}
	loaded, err := repository.ListFlags(ctx)
	if err != nil {
		t.Fatalf("ListFlags() error = %v", err)
	}
	byName := make(map[string]domain.FeatureFlag, len(loaded))
	for _, flag := range loaded {
		if len(flag.Name) >= len(prefix) && flag.Name[:len(prefix)] == prefix {
			byName[flag.Name] = flag
		}
	}
	if len(byName) != 3 || byName[prefix+".users"].UserIDs[0] != memberID || !byName[prefix+".all"].Enabled {
		t.Fatalf("stored feature flags = %+v", byName)
	}
	if admin, err := repository.UserIsAdmin(ctx, adminID); err != nil || !admin {
		t.Fatalf("admin role = %v, %v", admin, err)
	}
	if admin, err := repository.UserIsAdmin(ctx, memberID); err != nil || admin {
		t.Fatalf("member role = %v, %v", admin, err)
	}
	if admin, err := repository.UserIsAdmin(ctx, 0); err != nil || admin {
		t.Fatalf("system identity role = %v, %v", admin, err)
	}

	flags[0].Enabled = false
	if err := repository.UpsertFlags(ctx, flags[:1]); err != nil {
		t.Fatalf("upsert changed flag: %v", err)
	}
	loaded, err = repository.ListFlags(ctx)
	if err != nil {
		t.Fatalf("ListFlags() after upsert: %v", err)
	}
	for _, flag := range loaded {
		if flag.Name == prefix+".all" && flag.Enabled {
			t.Fatal("upsert did not change enabled state")
		}
	}

	if err := repository.UpsertFlags(ctx, []domain.FeatureFlag{{Name: "INVALID", Audience: domain.AudienceAll}}); !errors.Is(err, domain.ErrInvalidFeatureFlag) {
		t.Fatalf("invalid flag error = %v", err)
	}
}
