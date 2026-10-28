package roles

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	accountsdomain "github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

func TestCrawlFlickrCredentialsAndCall(t *testing.T) {
	ring, err := crypto.NewKeyring(map[string][]byte{"test": make([]byte, 32)}, "test")
	if err != nil {
		t.Fatal(err)
	}
	keyID, accountID := int64(4), int64(9)
	sealedKey, err := ring.Seal([]byte("api-secret"), accountsdomain.SecretAAD(keyID))
	if err != nil {
		t.Fatal(err)
	}
	sealedToken, err := ring.Seal([]byte("oauth-token"), accountsdomain.TokenAAD(accountID))
	if err != nil {
		t.Fatal(err)
	}
	sealedTokenSecret, err := ring.Seal([]byte("oauth-secret"), accountsdomain.TokenSecretAAD(accountID))
	if err != nil {
		t.Fatal(err)
	}
	keyRow := func(value crypto.Sealed) pgx.Row { return credentialRow{values: []any{"public-key", value}} }
	accountRow := credentialRow{values: []any{accountID, sealedToken, sealedTokenSecret}}
	request := app.Request{KeyID: keyID, UserID: 0}
	withoutUser, err := (&crawlFlickr{pool: &credentialPool{rows: []pgx.Row{keyRow(sealedKey)}}, ring: ring}).credentials(context.Background(), request)
	if err != nil || withoutUser.APIKey != "public-key" || withoutUser.APISecret != "api-secret" || withoutUser.Token != "" {
		t.Fatalf("key-only credentials=%+v err=%v", withoutUser, err)
	}
	request.UserID = 5
	withUser, err := (&crawlFlickr{pool: &credentialPool{rows: []pgx.Row{keyRow(sealedKey), accountRow}}, ring: ring}).credentials(context.Background(), request)
	if err != nil || withUser.Token != "oauth-token" || withUser.TokenSecret != "oauth-secret" {
		t.Fatalf("account credentials=%+v err=%v", withUser, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("method") != "flickr.test.echo" {
			t.Errorf("form=%v err=%v", r.Form, err)
		}
		_, _ = w.Write([]byte(`{"stat":"ok","echo":{"value":"yes"}}`))
	}))
	defer server.Close()
	client := flickrclient.NewClient(server.URL, server.Client())
	client.SetNonceGenerator(func() (string, error) { return "nonce", nil })
	client.SetClock(func() time.Time { return time.Unix(1700000000, 0) })
	flickr := &crawlFlickr{pool: &credentialPool{rows: []pgx.Row{keyRow(sealedKey), accountRow}}, client: client, ring: ring}
	payload, err := flickr.Call(context.Background(), app.Request{KeyID: keyID, UserID: 5, Method: "flickr.test.echo", Params: map[string]string{"value": "yes"}})
	if err != nil || !json.Valid(payload) || !reflect.DeepEqual(json.RawMessage(payload), json.RawMessage(`{"stat":"ok","echo":{"value":"yes"}}`)) {
		t.Fatalf("Call() payload=%s err=%v", payload, err)
	}
}

func TestCrawlFlickrCredentialsPropagateQueryAndEnvelopeErrors(t *testing.T) {
	ring, err := crypto.NewKeyring(map[string][]byte{"test": make([]byte, 32)}, "test")
	if err != nil {
		t.Fatal(err)
	}
	keyID := int64(4)
	sealed, err := ring.Seal([]byte("secret"), accountsdomain.SecretAAD(keyID))
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("database unavailable")
	request := app.Request{KeyID: keyID, UserID: 6}
	if _, err := (&crawlFlickr{pool: &credentialPool{rows: []pgx.Row{credentialRow{err: boom}}}, ring: ring}).credentials(context.Background(), request); !errors.Is(err, boom) {
		t.Fatalf("key query error=%v", err)
	}
	if _, err := (&crawlFlickr{pool: &credentialPool{rows: []pgx.Row{credentialRow{values: []any{"public", crypto.Sealed{}}}}}, ring: ring}).credentials(context.Background(), request); err == nil {
		t.Fatal("invalid key envelope was accepted")
	}
	if _, err := (&crawlFlickr{pool: &credentialPool{rows: []pgx.Row{credentialRow{values: []any{"public", sealed}}, credentialRow{err: boom}}}, ring: ring}).credentials(context.Background(), request); !errors.Is(err, boom) {
		t.Fatalf("account query error=%v", err)
	}
	wrongAAD, err := ring.Seal([]byte("wrong"), []byte("wrong aad"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&crawlFlickr{pool: &credentialPool{rows: []pgx.Row{
		credentialRow{values: []any{"public", sealed}},
		credentialRow{values: []any{int64(1), wrongAAD, wrongAAD}},
	}}, ring: ring}).credentials(context.Background(), request); err == nil {
		t.Fatal("token with the wrong associated data was accepted")
	}
}

func TestCrawlInterestAndUsableKeyAdapters(t *testing.T) {
	db := crawlQueryPool{rows: &crawlIDRows{ids: []int64{2, 3}}}
	users, err := (personInterests{pool: &db}).InterestedUsers(context.Background(), "person")
	if err != nil || !reflect.DeepEqual(users, []int64{2, 3}) {
		t.Fatalf("InterestedUsers=%v err=%v", users, err)
	}
	db.rows = &crawlIDRows{ids: []int64{7, 8}}
	keys, err := (usableKeys{pool: &db}).KeysOf(context.Background(), 2)
	if err != nil || len(keys) != 2 || int64(keys[0].ID) != 7 || !keys[0].Valid || int64(keys[1].ID) != 8 {
		t.Fatalf("KeysOf=%+v err=%v", keys, err)
	}
	boom := errors.New("query failed")
	db.queryErr = boom
	if _, err := (personInterests{pool: &db}).InterestedUsers(context.Background(), "person"); !errors.Is(err, boom) {
		t.Fatalf("InterestedUsers query error=%v", err)
	}
	if _, err := (usableKeys{pool: &db}).KeysOf(context.Background(), 2); !errors.Is(err, boom) {
		t.Fatalf("KeysOf query error=%v", err)
	}
	db.queryErr = nil
	db.rows = &crawlIDRows{ids: []int64{1}, scanErr: boom}
	if _, err := (personInterests{pool: &db}).InterestedUsers(context.Background(), "person"); !errors.Is(err, boom) {
		t.Fatalf("InterestedUsers scan error=%v", err)
	}
	db.rows = &crawlIDRows{ids: []int64{1}, scanErr: boom}
	if _, err := (usableKeys{pool: &db}).KeysOf(context.Background(), 2); !errors.Is(err, boom) {
		t.Fatalf("KeysOf scan error=%v", err)
	}
	db.rows = &crawlIDRows{err: boom}
	if _, err := (personInterests{pool: &db}).InterestedUsers(context.Background(), "person"); !errors.Is(err, boom) {
		t.Fatalf("InterestedUsers rows error=%v", err)
	}
	db.rows = &crawlIDRows{err: boom}
	if _, err := (usableKeys{pool: &db}).KeysOf(context.Background(), 2); !errors.Is(err, boom) {
		t.Fatalf("KeysOf rows error=%v", err)
	}
}

type credentialPool struct {
	rows  []pgx.Row
	index int
}

func (pool *credentialPool) QueryRow(context.Context, string, ...any) pgx.Row {
	row := pool.rows[pool.index]
	pool.index++
	return row
}

type credentialRow struct {
	values []any
	err    error
}

func (row credentialRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != len(row.values) {
		return errors.New("unexpected scan value count")
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *crypto.Sealed:
			encoded, err := json.Marshal(row.values[i])
			if err != nil {
				return err
			}
			if err := target.Scan(encoded); err != nil {
				return err
			}
		default:
			out, value := reflect.ValueOf(dest[i]), reflect.ValueOf(row.values[i])
			if out.Kind() != reflect.Pointer || !value.Type().AssignableTo(out.Elem().Type()) {
				return errors.New("unexpected scan value type")
			}
			out.Elem().Set(value)
		}
	}
	return nil
}

type crawlQueryPool struct {
	rows     pgx.Rows
	queryErr error
}

func (pool crawlQueryPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if pool.queryErr != nil {
		return nil, pool.queryErr
	}
	return pool.rows, nil
}

type crawlIDRows struct {
	pgx.Rows
	ids     []int64
	index   int
	scanErr error
	err     error
}

func (rows *crawlIDRows) Next() bool {
	if rows.index >= len(rows.ids) {
		return false
	}
	rows.index++
	return true
}
func (*crawlIDRows) Close()          {}
func (rows *crawlIDRows) Err() error { return rows.err }
func (rows *crawlIDRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	if len(dest) != 1 {
		return errors.New("unexpected scan destination count")
	}
	*dest[0].(*int64) = rows.ids[rows.index-1]
	return nil
}
