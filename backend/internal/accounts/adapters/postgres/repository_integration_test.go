package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jooservices/xflickr/internal/accounts/adapters/flickr"
	"github.com/jooservices/xflickr/internal/accounts/adapters/postgres"
	accountsettings "github.com/jooservices/xflickr/internal/accounts/adapters/settings"
	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
)

const (
	validFlickrKey   = "0123456789abcdef0123456789abcdef"
	invalidFlickrKey = "ffffffffffffffffffffffffffffffff"
	originalSecret   = "abcdef0123456789"
	rotatedSecret    = "fedcba9876543210"
)

func TestAddValidAndInvalidKeysThroughFakeFlickr(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadDatabase(t))

	var userID int64
	email := fmt.Sprintf("jooxf97-%d@example.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, locale) VALUES ($1, $2, 'user', 'en') RETURNING id`, email, "integration-hash").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM outbox_events WHERE topic = $1 AND payload->>'user_id' = $2`, domain.DeletedTopic, strconv.FormatInt(userID, 10)); err != nil {
			t.Errorf("cleanup outbox: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})

	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ring := testKeyring(t)
	fake := flickr.NewFakeFlickr(validFlickrKey)
	t.Cleanup(fake.Close)
	keys := app.Keys{
		Repository: repository,
		Probe:      flickr.NewProbe(fake.URL, fake.Client()),
		Sealer:     ring,
		Settings:   accountsettings.Reader{Settings: settingsapp.New(settingsRepository)},
	}

	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE user_id = $1`, userID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Create(ctx, userID, app.CreateInput{Label: "Rejected", Key: invalidFlickrKey, Secret: originalSecret}); !errors.Is(err, app.ErrKeyInvalid) {
		t.Fatalf("invalid key error = %v", err)
	}
	var afterInvalid int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE user_id = $1`, userID).Scan(&afterInvalid); err != nil {
		t.Fatal(err)
	}
	if afterInvalid != before {
		t.Fatalf("invalid key stored rows: before %d after %d", before, afterInvalid)
	}

	over := 9
	if _, err := keys.Create(ctx, userID, app.CreateInput{Label: "Too fast", Key: validFlickrKey, Secret: originalSecret, PerSecondLimit: &over}); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("over cap error = %v", err)
	}

	created, err := keys.Create(ctx, userID, app.CreateInput{Label: "Personal", Key: validFlickrKey, Secret: originalSecret})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != domain.StatusValid || created.PerSecondLimit != 4 || created.Burst != 5 || created.PerHourLimit != domain.FlickrHourlyCeiling {
		t.Fatalf("created = %+v", created)
	}
	if created.MaskedPublicKey() == validFlickrKey || !strings.HasPrefix(created.MaskedPublicKey(), validFlickrKey[:domain.PublicKeyVisiblePrefix]) {
		t.Fatalf("masked key = %q", created.MaskedPublicKey())
	}

	var storedPublic string
	var sealed crypto.Sealed
	if err := pool.QueryRow(ctx, `SELECT key_public, secret_sealed FROM api_keys WHERE id = $1 AND user_id = $2`, created.ID, userID).Scan(&storedPublic, &sealed); err != nil {
		t.Fatal(err)
	}
	if storedPublic != validFlickrKey {
		t.Fatalf("stored public key = %q", storedPublic)
	}
	opened, err := ring.Open(sealed, domain.SecretAAD(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if string(opened) != originalSecret {
		t.Fatal("opened secret did not match the value entered by the user")
	}
	if _, err := ring.Open(sealed, domain.SecretAAD(created.ID+1)); !errors.Is(err, crypto.ErrAuthentication) {
		t.Fatalf("wrong associated data error = %v", err)
	}
	encoded, err := json.Marshal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(originalSecret)) {
		t.Fatal("sealed column contains the plaintext secret")
	}

	if _, err := pool.Exec(ctx, `INSERT INTO flickr_accounts (user_id, key_id, nsid, username, token_sealed, secret_sealed, status) VALUES ($1, $2, 'nsid-keys-test', 'tester', $3, $3, $4)`, userID, created.ID, sealed, string(domain.AccountOK)); err != nil {
		t.Fatal(err)
	}
	renamed, err := keys.Update(ctx, userID, created.ID, app.UpdateInput{Label: strPtr("Archive"), PerHourLimit: intPtr(120)})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Label != "Archive" || renamed.PerHourLimit != 120 || renamed.PublicKey != validFlickrKey {
		t.Fatalf("renamed = %+v", renamed)
	}
	assertAccountStatus(t, ctx, pool, created.ID, string(domain.AccountOK))

	if _, err := keys.Update(ctx, userID, created.ID, app.UpdateInput{Secret: strPtr(rotatedSecret)}); err != nil {
		t.Fatal(err)
	}
	assertAccountStatus(t, ctx, pool, created.ID, string(domain.AccountReconnectRequired))
	if err := pool.QueryRow(ctx, `SELECT secret_sealed FROM api_keys WHERE id = $1`, created.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	opened, err = ring.Open(sealed, domain.SecretAAD(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if string(opened) != rotatedSecret {
		t.Fatal("rotated secret did not round-trip")
	}

	if _, err := keys.Get(ctx, userID+1, created.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("other user get error = %v", err)
	}
	if err := keys.Delete(ctx, userID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Get(ctx, userID, created.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("deleted get error = %v", err)
	}
	var accounts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM flickr_accounts WHERE key_id = $1`, created.ID).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if accounts != 0 {
		t.Fatalf("flickr accounts remaining = %d", accounts)
	}
	var payload string
	if err := pool.QueryRow(ctx, `SELECT payload::text FROM outbox_events WHERE topic = $1 AND key = $2`, domain.DeletedTopic, fmt.Sprintf("%d", created.ID)).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, originalSecret) || strings.Contains(payload, rotatedSecret) || strings.Contains(payload, validFlickrKey) {
		t.Fatalf("delete event leaked key material: %s", payload)
	}
	if !strings.Contains(payload, fmt.Sprintf(`"id": %d`, created.ID)) && !strings.Contains(payload, fmt.Sprintf(`"id":%d`, created.ID)) {
		t.Fatalf("delete event payload = %s", payload)
	}
}

func loadDatabase(t *testing.T) config.DBConfig {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	cfg, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func testKeyring(t *testing.T) *crypto.Keyring {
	t.Helper()
	key := bytes.Repeat([]byte{7}, 32)
	ring, err := crypto.NewKeyring(map[string][]byte{"test": key}, "test")
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func assertAccountStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, keyID int64, want string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM flickr_accounts WHERE key_id = $1`, keyID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != want {
		t.Fatalf("connected account status = %s, want %s", status, want)
	}
}

func strPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }
