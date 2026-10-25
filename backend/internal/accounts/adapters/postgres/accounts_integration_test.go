package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	redisv9 "github.com/redis/go-redis/v9"

	"github.com/jooservices/xflickr/internal/accounts/adapters/flickr"
	"github.com/jooservices/xflickr/internal/accounts/adapters/postgres"
	accountsredis "github.com/jooservices/xflickr/internal/accounts/adapters/redis"
	accountsettings "github.com/jooservices/xflickr/internal/accounts/adapters/settings"
	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db/dbtest"
	settingspostgres "github.com/jooservices/xflickr/internal/settings/adapters/postgres"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

const (
	connectNSID     = "21207597@N07"
	connectCallback = "http://127.0.0.1:8080/flickr-accounts/callback"
)

func TestConnectFlowAgainstFakeFlickrOAuth(t *testing.T) {
	if os.Getenv("XFLICKR_INTEGRATION_TEST") != "1" {
		t.Skip("set XFLICKR_INTEGRATION_TEST=1 to use the shared worktree database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := dbtest.Open(t, loadDatabase(t))
	userID, otherID := insertUser(t, ctx, pool, "a"), insertUser(t, ctx, pool, "b")
	redisClient, prefix := openRedis(t, ctx)

	repository, err := postgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	settingsRepository, err := settingspostgres.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	ring := testKeyring(t)
	echo := flickr.NewFakeFlickr(validFlickrKey)
	t.Cleanup(echo.Close)
	keys := app.Keys{
		Repository: repository,
		Probe:      flickr.NewProbe(echo.URL, echo.Client()),
		Sealer:     ring,
		Settings:   accountsettings.Reader{Settings: settingsapp.New(settingsRepository)},
	}
	key, err := keys.Create(ctx, userID, app.CreateInput{Label: "Personal", Key: validFlickrKey, Secret: originalSecret})
	if err != nil {
		t.Fatal(err)
	}

	oauth := flickr.NewFakeOAuth(validFlickrKey, connectNSID, "jamal")
	t.Cleanup(oauth.Close)
	connect := app.Connect{
		Repository:  repository,
		Authorizer:  flickr.NewAuthorizer(oauth.Client(), oauth.Endpoints()),
		States:      accountsredis.States{Client: redisClient, Prefix: prefix},
		Cipher:      ring,
		CallbackURL: connectCallback,
	}

	// Start: the request token is stored for 10 minutes and the callback carries the state.
	authorizeURL, err := connect.Start(ctx, userID, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	authorize, err := url.Parse(authorizeURL)
	if err != nil || authorize.Path != flickr.FakeAuthorizePath {
		t.Fatalf("authorize url = %q %v", authorizeURL, err)
	}
	requestToken := authorize.Query().Get("oauth_token")
	callback, err := url.Parse(oauth.Callback(requestToken))
	if err != nil {
		t.Fatal(err)
	}
	state := callback.Query().Get("state")
	if state == "" || callback.Path != "/flickr-accounts/callback" {
		t.Fatalf("callback = %q", callback)
	}
	var redisKeys []string
	for _, name := range scanKeys(t, ctx, redisClient, prefix+"accounts:oauth:*") {
		redisKeys = append(redisKeys, name)
		if ttl := redisClient.TTL(ctx, name).Val(); ttl <= 9*time.Minute || ttl > app.StateTTL {
			t.Fatalf("state ttl = %s", ttl)
		}
	}
	if len(redisKeys) == 0 {
		t.Fatal("no pending authorization in Redis")
	}
	if _, err := connect.Start(ctx, otherID, key.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("other user's key = %v", err)
	}

	// Callback guards: wrong user, wrong token and a forged state all fail, and the state is single use.
	verifier := oauth.Approve(requestToken)
	if _, err := connect.Complete(ctx, userID, "forged-state", requestToken, verifier); !errors.Is(err, app.ErrInvalidState) {
		t.Fatalf("forged state = %v", err)
	}
	if _, err := connect.Complete(ctx, otherID, state, requestToken, verifier); !errors.Is(err, app.ErrInvalidState) {
		t.Fatalf("other user's callback = %v", err)
	}
	if _, err := connect.Complete(ctx, userID, state, requestToken, verifier); !errors.Is(err, app.ErrInvalidState) {
		t.Fatalf("consumed state = %v", err)
	}

	// A fresh flow completes: tokens are sealed and the connected event is a depth 0 person interest.
	authorizeURL, err = connect.Start(ctx, userID, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	requestToken = queryParam(t, authorizeURL, "oauth_token")
	state = queryParam(t, oauth.Callback(requestToken), "state")
	account, err := connect.Complete(ctx, userID, state, requestToken, oauth.Approve(requestToken))
	if err != nil {
		t.Fatal(err)
	}
	if account.UserID != userID || account.KeyID != key.ID || account.NSID != connectNSID || account.Username != "jamal" || account.Status != domain.AccountOK {
		t.Fatalf("account = %+v", account)
	}
	listed, err := connect.List(ctx, userID)
	if err != nil || len(listed) != 1 || listed[0].ID != account.ID || listed[0].Status != domain.AccountOK {
		t.Fatalf("listed accounts = %+v, %v", listed, err)
	}
	otherAccounts, err := connect.List(ctx, otherID)
	if err != nil || len(otherAccounts) != 0 {
		t.Fatalf("other user's accounts = %+v, %v", otherAccounts, err)
	}
	var token, secret crypto.Sealed
	if err := pool.QueryRow(ctx, `SELECT token_sealed, secret_sealed FROM flickr_accounts WHERE id = $1`, account.ID).Scan(&token, &secret); err != nil {
		t.Fatal(err)
	}
	if opened, err := ring.Open(token, domain.TokenAAD(account.ID)); err != nil || !strings.HasPrefix(string(opened), "access-") {
		t.Fatalf("token = %q %v", opened, err)
	}
	if opened, err := ring.Open(secret, domain.TokenSecretAAD(account.ID)); err != nil || !strings.HasPrefix(string(opened), "access-secret-") {
		t.Fatalf("secret = %q %v", opened, err)
	}
	if _, err := ring.Open(token, domain.TokenSecretAAD(account.ID)); !errors.Is(err, crypto.ErrAuthentication) {
		t.Fatalf("token opened with the secret's associated data: %v", err)
	}
	assertConnectedEvent(t, ctx, pool, account.ID, false)

	// Reconnecting the same Flickr account keeps one row (unique user_id, nsid).
	if _, err := pool.Exec(ctx, `UPDATE flickr_accounts SET status = 'reconnect_required' WHERE id = $1`, account.ID); err != nil {
		t.Fatal(err)
	}
	authorizeURL, err = connect.Start(ctx, userID, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	requestToken = queryParam(t, authorizeURL, "oauth_token")
	state = queryParam(t, oauth.Callback(requestToken), "state")
	again, err := connect.Complete(ctx, userID, state, requestToken, oauth.Approve(requestToken))
	if err != nil || again.ID != account.ID || again.Status != domain.AccountOK {
		t.Fatalf("reconnect = %+v %v", again, err)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM flickr_accounts WHERE user_id = $1 AND nsid = $2`, userID, connectNSID).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("rows = %d %v", rows, err)
	}
	assertConnectedEvent(t, ctx, pool, account.ID, true)

	// A secret change marks the account reconnect_required; a revoked token flips it to revoked once.
	if _, err := keys.Update(ctx, userID, key.ID, app.UpdateInput{Secret: strPtr(rotatedSecret)}); err != nil {
		t.Fatal(err)
	}
	assertFlickrAccountStatus(t, ctx, pool, account.ID, domain.AccountReconnectRequired)
	tokenErr := &flickrclient.CallError{Kind: flickrclient.ErrTokenRevoked, Code: 98, Message: "Login failed / Invalid auth token"}
	if revoked, err := connect.ReportCallError(ctx, account.ID, tokenErr); err != nil || !revoked {
		t.Fatalf("revoke = %v %v", revoked, err)
	}
	if revoked, err := connect.ReportCallError(ctx, account.ID, tokenErr); err != nil || revoked {
		t.Fatalf("second revoke = %v %v", revoked, err)
	}
	if revoked, err := connect.ReportCallError(ctx, account.ID, flickrclient.ErrQuotaExceeded); err != nil || revoked {
		t.Fatalf("quota error revoked the account: %v %v", revoked, err)
	}
	assertFlickrAccountStatus(t, ctx, pool, account.ID, domain.AccountRevoked)
	var revokedEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic = $1 AND payload->>'account_id' = $2`, domain.RevokedTopic, strconv.FormatInt(account.ID, 10)).Scan(&revokedEvents); err != nil || revokedEvents != 1 {
		t.Fatalf("account.revoked events = %d %v", revokedEvents, err)
	}

	// A key secret change leaves a revoked account revoked.
	if _, err := keys.Update(ctx, userID, key.ID, app.UpdateInput{Secret: strPtr(originalSecret)}); err != nil {
		t.Fatal(err)
	}
	assertFlickrAccountStatus(t, ctx, pool, account.ID, domain.AccountRevoked)

	// Authorizing a revoked account again makes it ok, and an invalid key cannot start the flow.
	authorizeURL, err = connect.Start(ctx, userID, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	requestToken = queryParam(t, authorizeURL, "oauth_token")
	state = queryParam(t, oauth.Callback(requestToken), "state")
	if _, err := connect.Complete(ctx, userID, state, requestToken, ""); !errors.Is(err, app.ErrAuthorizationDenied) {
		t.Fatalf("declined = %v", err)
	}
	// An expired pending authorization (its Redis entry is gone) is an invalid state, not an account.
	authorizeURL, err = connect.Start(ctx, userID, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	requestToken = queryParam(t, authorizeURL, "oauth_token")
	state = queryParam(t, oauth.Callback(requestToken), "state")
	for _, name := range scanKeys(t, ctx, redisClient, prefix+"accounts:oauth:*") {
		redisClient.Del(ctx, name)
	}
	if _, err := connect.Complete(ctx, userID, state, requestToken, oauth.Approve(requestToken)); !errors.Is(err, app.ErrInvalidState) {
		t.Fatalf("expired state = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE api_keys SET status = 'invalid' WHERE id = $1`, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := connect.Start(ctx, userID, key.ID); !errors.Is(err, app.ErrKeyNotUsable) {
		t.Fatalf("invalid key = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE api_keys SET status = 'valid' WHERE id = $1`, key.ID); err != nil {
		t.Fatal(err)
	}

	// Deleting the key removes its accounts: the token belongs to the key.
	if err := keys.Delete(ctx, userID, key.ID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM flickr_accounts WHERE id = $1`, account.ID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("accounts after key delete = %d %v", rows, err)
	}
}

func assertConnectedEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountID int64, wantReconnected bool) {
	t.Helper()
	var depth int
	var interest, nsid string
	var reconnected bool
	err := pool.QueryRow(ctx, `
SELECT (payload->>'depth')::int, payload->>'interest', payload->>'nsid', (payload->>'reconnected')::boolean
FROM outbox_events
WHERE topic = $1 AND payload->>'account_id' = $2 AND (payload->>'reconnected')::boolean = $3`,
		domain.ConnectedTopic, strconv.FormatInt(accountID, 10), wantReconnected).Scan(&depth, &interest, &nsid, &reconnected)
	if err != nil {
		t.Fatalf("connected event (reconnected=%v): %v", wantReconnected, err)
	}
	if depth != 0 || interest != "person" || nsid != connectNSID {
		t.Fatalf("event depth=%d interest=%q nsid=%q", depth, interest, nsid)
	}
	var payload string
	if err := pool.QueryRow(ctx, `SELECT payload::text FROM outbox_events WHERE topic = $1 AND payload->>'account_id' = $2 LIMIT 1`, domain.ConnectedTopic, strconv.FormatInt(accountID, 10)).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "access-") || strings.Contains(payload, originalSecret) {
		t.Fatalf("event leaked token material: %s", payload)
	}
}

func assertFlickrAccountStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id int64, want domain.AccountStatus) {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM flickr_accounts WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(want) {
		t.Fatalf("account status = %s, want %s", status, want)
	}
}

func insertUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tag string) int64 {
	t.Helper()
	var userID int64
	email := fmt.Sprintf("jooxf98-%s-%d@example.invalid", tag, time.Now().UnixNano())
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role, locale) VALUES ($1, $2, 'user', 'en') RETURNING id`, email, "integration-hash").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		id := strconv.FormatInt(userID, 10)
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM outbox_events WHERE payload->>'user_id' = $1 AND topic IN ($2, $3, $4)`, id, domain.ConnectedTopic, domain.RevokedTopic, domain.DeletedTopic); err != nil {
			t.Errorf("cleanup outbox: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})
	return userID
}

// openRedis connects to the shared Redis with the worktree prefix from the generated environment.
func openRedis(t *testing.T, ctx context.Context) (*redisv9.Client, string) {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../../../"))
	values, err := godotenv.Read(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal("read shared worktree environment")
	}
	port, _ := strconv.Atoi(values["REDIS_PORT"])
	if port == 0 {
		port = 16382
	}
	host := values["REDIS_HOST"]
	if host == "" {
		host = "127.0.0.1"
	}
	db, _ := strconv.Atoi(values["REDIS_DB"])
	prefix := values["REDIS_PREFIX"]
	if prefix == "" {
		t.Fatal("REDIS_PREFIX is required so the test stays inside the worktree namespace")
	}
	client := redisv9.NewClient(&redisv9.Options{Addr: net.JoinHostPort(host, strconv.Itoa(port)), Password: values["REDIS_PASSWORD"], DB: db})
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, name := range scanKeys(t, cleanupCtx, client, prefix+"accounts:oauth:*") {
			client.Del(cleanupCtx, name)
		}
		_ = client.Close()
	})
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping shared Redis: %v", err)
	}
	return client, prefix
}

func scanKeys(t *testing.T, ctx context.Context, client *redisv9.Client, pattern string) []string {
	t.Helper()
	var found []string
	iterator := client.Scan(ctx, 0, pattern, 100).Iterator()
	for iterator.Next(ctx) {
		found = append(found, iterator.Val())
	}
	if err := iterator.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

func queryParam(t *testing.T, raw, name string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Query().Get(name) == "" {
		t.Fatalf("%s missing from %q: %v", name, raw, err)
	}
	return parsed.Query().Get(name)
}
