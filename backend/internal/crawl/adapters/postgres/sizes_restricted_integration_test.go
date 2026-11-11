package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accountspostgres "github.com/jooservices/xflickr/internal/accounts/adapters/postgres"
	catalogpostgres "github.com/jooservices/xflickr/internal/catalog/adapters/postgres"
	catalogapp "github.com/jooservices/xflickr/internal/catalog/app"
	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	"github.com/jooservices/xflickr/internal/crawl/app"
	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	quotadomain "github.com/jooservices/xflickr/internal/quota/domain"
	"github.com/jooservices/xflickr/pkg/flickr"
)

type restrictedSizesClient struct {
	accountSizes map[int64][]flickr.PhotoSize
	calls        []struct {
		AccountID int64
		Signed    bool
	}
}

func (f *restrictedSizesClient) GetSizes(context.Context, int64, string) ([]flickr.PhotoSize, error) {
	return nil, &flickr.CallError{Kind: flickr.ErrNotFound, Code: 1}
}

func (f *restrictedSizesClient) GetSizesClass(ctx context.Context, keyID int64, photoID string, _ quotadomain.Class) ([]flickr.PhotoSize, error) {
	return f.GetSizes(ctx, keyID, photoID)
}

func (f *restrictedSizesClient) GetSizesForAccount(_ context.Context, _, accountID int64, _ string, _ quotadomain.Class) ([]flickr.PhotoSize, error) {
	f.calls = append(f.calls, struct {
		AccountID int64
		Signed    bool
	}{AccountID: accountID, Signed: true})
	sizes := f.accountSizes[accountID]
	if len(sizes) == 0 {
		return nil, &flickr.CallError{Kind: flickr.ErrNotFound, Code: 1}
	}
	return sizes, nil
}

type sizesAuthBridge struct {
	accounts accountspostgres.SizesAuthCandidates
}

func (a sizesAuthBridge) SizesAuthCandidates(ctx context.Context, ownerNSID string, requestingUserID int64) ([]crawldomain.SizesAuthCandidate, error) {
	rows, err := a.accounts.List(ctx, ownerNSID, requestingUserID)
	if err != nil {
		return nil, err
	}
	out := make([]crawldomain.SizesAuthCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, crawldomain.SizesAuthCandidate{
			AccountID: row.AccountID, UserID: row.UserID, KeyID: row.KeyID, NSID: row.NSID, Owner: row.NSID == ownerNSID,
		})
	}
	return out, nil
}

type sizesKeyUser struct{ pool *pgxpool.Pool }

func (u sizesKeyUser) UserIDForKey(ctx context.Context, keyID int64) (int64, error) {
	var userID int64
	if err := u.pool.QueryRow(ctx, `SELECT user_id FROM api_keys WHERE id=$1`, keyID).Scan(&userID); err != nil {
		return 0, err
	}
	return userID, nil
}

func TestOwnRestrictedPhotoGetsConfirmedSizes(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("run make test-integration to use the configured worktree database")
	}
	ctx := t.Context()
	pool := dbtest.Open(t, sizesRestrictedDatabase(t))
	repository, err := catalogpostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UnixNano()
	owner := fmt.Sprintf("restricted-owner-%d@N00", base)
	email := fmt.Sprintf("restricted-%d@integration.invalid", base)
	photoID := base
	var userID, keyID, accountID, ownerKeyID, ownerAccountID int64
	var invalidKeyID, invalidAccountID, foreignUserID, foreignKeyID, foreignAccountID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','user') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','user') RETURNING id`, email+"-foreign").Scan(&foreignUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO people(nsid, username) VALUES($1,$1)`, owner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status) VALUES($1,'foreign','foreign-public','{}',3600,4,5,'valid') RETURNING id`, foreignUserID).Scan(&foreignKeyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'foreign-owner','{}','{}','ok') RETURNING id`, foreignUserID, foreignKeyID, owner).Scan(&foreignAccountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status) VALUES($1,'invalid','invalid-public','{}',3600,4,5,'invalid') RETURNING id`, userID).Scan(&invalidKeyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,'invalid@N00','invalid-owner','{}','{}','ok') RETURNING id`, userID, invalidKeyID).Scan(&invalidAccountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status) VALUES($1,'restricted','public','{}',3600,4,5,'valid') RETURNING id`, userID).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,'viewer@N00','viewer','{}','{}','ok') RETURNING id`, userID, keyID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO api_keys(user_id,label,key_public,secret_sealed,per_hour_limit,per_second_limit,burst,status) VALUES($1,'owner','owner-public','{}',3600,4,5,'valid') RETURNING id`, userID).Scan(&ownerKeyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO flickr_accounts(user_id,key_id,nsid,username,token_sealed,secret_sealed,status) VALUES($1,$2,$3,'owner','{}','{}','ok') RETURNING id`, userID, ownerKeyID, owner).Scan(&ownerAccountID); err != nil {
		t.Fatal(err)
	}
	checked := time.Now().UTC().Truncate(time.Microsecond)
	lastUpdate := checked.Add(-time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO photos(id,owner_nsid,server,last_update,visibility,sizes,sizes_status,raw,content_hash)
		VALUES($1,$2,$3,$4,'private','[]'::jsonb,'provisional','{}',decode(repeat('ab',32),'hex'))`,
		photoID, owner, catalog.RestrictedPlaceholderServer, lastUpdate); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM entity_facet_restrictions WHERE entity_id=$1`, fmt.Sprint(photoID))
		_, _ = pool.Exec(context.Background(), `DELETE FROM photos WHERE id=$1`, photoID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE id=$1`, accountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE id=$1`, ownerAccountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, keyID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, ownerKeyID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE id=$1`, invalidAccountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, invalidKeyID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM flickr_accounts WHERE id=$1`, foreignAccountID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM api_keys WHERE id=$1`, foreignKeyID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, foreignUserID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM people WHERE nsid=$1`, owner)
	})

	client := &restrictedSizesClient{accountSizes: map[int64][]flickr.PhotoSize{
		accountID: {{Label: "Original", Width: 1200, Height: 800, Source: "https://example.test/restricted"}},
	}}
	var events []catalogapp.SizesResolved
	resolved := catalogpostgres.ResolvedSizes{Repository: repository, Pool: pool, Publish: func(_ context.Context, _ pgx.Tx, event catalogapp.SizesResolved) error {
		events = append(events, event)
		return nil
	}}
	handler := app.Sizes{
		States:   EntityStates{DB: pool},
		Store:    resolved,
		Accounts: resolved,
		Flickr:   client,
		Auth:     sizesAuthBridge{accounts: accountspostgres.SizesAuthCandidates{DB: pool}},
		Users:    sizesKeyUser{pool: pool},
		Now:      func() time.Time { return checked },
	}
	if err := handler.Handle(ctx, app.SizesJob{PhotoID: photoID, KeyID: keyID, Class: string(quotadomain.Interactive)}); err != nil {
		t.Fatal(err)
	}
	var globalStatus string
	var globalSizes []byte
	if err := pool.QueryRow(ctx, `SELECT sizes_status, sizes FROM photos WHERE id=$1`, photoID).Scan(&globalStatus, &globalSizes); err != nil {
		t.Fatal(err)
	}
	if globalStatus != string(catalog.SizesProvisional) || string(globalSizes) != `[]` {
		t.Fatalf("global photo sizes leaked account result: status=%s sizes=%s", globalStatus, globalSizes)
	}
	var status string
	var sizes []byte
	if err := pool.QueryRow(ctx, `SELECT sizes_status, sizes FROM photo_account_sizes WHERE photo_id=$1 AND account_id=$2`, photoID, accountID).Scan(&status, &sizes); err != nil {
		t.Fatal(err)
	}
	if status != string(catalog.SizesConfirmed) {
		t.Fatalf("account sizes status = %s", status)
	}
	var parsed []flickr.PhotoSize
	if err := json.Unmarshal(sizes, &parsed); err != nil || len(parsed) != 1 || parsed[0].Label != "Original" {
		t.Fatalf("sizes = %s err=%v", sizes, err)
	}
	if len(client.calls) != 2 || !client.calls[0].Signed || client.calls[0].AccountID != ownerAccountID || !client.calls[1].Signed || client.calls[1].AccountID != accountID {
		t.Fatalf("authenticated calls = %+v", client.calls)
	}
	var ownerStatus string
	if err := pool.QueryRow(ctx, `SELECT sizes_status FROM photo_account_sizes WHERE photo_id=$1 AND account_id=$2`, photoID, ownerAccountID).Scan(&ownerStatus); err != nil {
		t.Fatal(err)
	}
	if ownerStatus != string(catalog.SizesUnavailable) {
		t.Fatalf("owner account sizes status = %s", ownerStatus)
	}
	if len(events) != 1 || events[0].UserID != userID || events[0].Status != string(catalog.SizesConfirmed) {
		t.Fatalf("sizes continuation events = %+v, want one confirmed event after fallback", events)
	}
}

func sizesRestrictedDatabase(t *testing.T) config.DBConfig {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate sizes restricted integration test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	dbConfig, err := config.LoadDatabaseWithDotEnv(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("load test database settings: %v", err)
	}
	if err := dbtest.ValidateDatabaseName(dbConfig.Name); err != nil {
		t.Fatal(err)
	}
	return dbConfig
}
