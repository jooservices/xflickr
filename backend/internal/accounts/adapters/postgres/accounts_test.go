package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jooservices/xflickr/internal/accounts/app"
	"github.com/jooservices/xflickr/internal/accounts/domain"
	"github.com/jooservices/xflickr/internal/platform/crypto"
)

// scriptedDB answers account queries from a table keyed by SQL fragment.
type scriptedDB struct {
	rows    map[string]memoryRow
	execErr error
	events  []recordedEvent
	exec    []string
	commits int
}

func (d *scriptedDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return &scriptedTx{db: d}, nil
}

type scriptedTx struct {
	pgx.Tx
	db *scriptedDB
}

func (tx *scriptedTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	for fragment, row := range tx.db.rows {
		if strings.Contains(sql, fragment) {
			return row
		}
	}
	return memoryRow{err: errors.New("unexpected query row: " + sql)}
}

func (tx *scriptedTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.db.execErr != nil {
		return pgconn.CommandTag{}, tx.db.execErr
	}
	tx.db.exec = append(tx.db.exec, sql)
	if strings.Contains(sql, "INSERT INTO outbox_events") {
		tx.db.events = append(tx.db.events, recordedEvent{Topic: args[0].(string), Key: args[1].(string), Payload: args[2].([]byte)})
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *scriptedTx) Commit(context.Context) error {
	tx.db.commits++
	return nil
}

func (tx *scriptedTx) Rollback(context.Context) error { return nil }

func scanInto(values ...any) memoryRow {
	return memoryRow{scan: func(dest ...any) error {
		if len(dest) != len(values) {
			return errors.New("scan destination count")
		}
		for index, value := range values {
			switch target := dest[index].(type) {
			case *int64:
				*target = value.(int64)
			case *string:
				*target = value.(string)
			case *time.Time:
				*target = value.(time.Time)
			case *int:
				*target = value.(int)
			case *crypto.Sealed:
				*target = value.(crypto.Sealed)
			default:
				return errors.New("scan destination type")
			}
		}
		return nil
	}}
}

var accountCreatedAt = time.Unix(1_700_000_000, 0).UTC()

func newAccountDB(keyStatus string) *scriptedDB {
	return &scriptedDB{rows: map[string]memoryRow{
		"FOR SHARE":                      scanInto(keyStatus),
		"SELECT id FROM flickr_accounts": {err: pgx.ErrNoRows},
		"nextval":                        scanInto(int64(21)),
		"INSERT INTO flickr_accounts":    scanInto(accountCreatedAt),
		"SET key_id":                     scanInto(accountCreatedAt),
	}}
}

func sampleAccount() domain.Account {
	return domain.Account{UserID: 7, KeyID: 4, NSID: "21207597@N07", Username: "jamal"}
}

func sealPair(id int64) (crypto.Sealed, crypto.Sealed, error) {
	if string(domain.TokenAAD(id)) == "" {
		return crypto.Sealed{}, crypto.Sealed{}, errors.New("missing id")
	}
	return mustSeal("token"), mustSeal("secret"), nil
}

func newScriptedRepository(t *testing.T, database *scriptedDB) *Repository {
	t.Helper()
	repository, err := New(database)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func decodeEvent(t *testing.T, event recordedEvent, into any) {
	t.Helper()
	if err := json.Unmarshal(event.Payload, into); err != nil {
		t.Fatal(err)
	}
}

func TestConnectInsertsAccountAndPublishesDepthZeroPersonInterest(t *testing.T) {
	database := newAccountDB("valid")
	repository := newScriptedRepository(t, database)
	account, reconnected, err := repository.Connect(t.Context(), sampleAccount(), sealPair)
	if err != nil || reconnected {
		t.Fatalf("connect = %+v %v %v", account, reconnected, err)
	}
	if account.ID != 21 || account.Status != domain.AccountOK || !account.CreatedAt.Equal(accountCreatedAt) {
		t.Fatalf("account = %+v", account)
	}
	if len(database.events) != 1 || database.events[0].Topic != domain.ConnectedTopic || database.events[0].Key != "21" {
		t.Fatalf("events = %+v", database.events)
	}
	var event domain.Connected
	decodeEvent(t, database.events[0], &event)
	want := domain.Connected{AccountID: 21, UserID: 7, KeyID: 4, NSID: "21207597@N07", Username: "jamal", Interest: "person", Depth: 0}
	if event != want {
		t.Fatalf("event = %+v, want %+v", event, want)
	}
	if strings.Contains(string(database.events[0].Payload), "token") {
		t.Fatalf("event carries token material: %s", database.events[0].Payload)
	}
	if database.commits != 1 {
		t.Fatalf("commits = %d", database.commits)
	}
}

func TestConnectRefreshesAnExistingAccount(t *testing.T) {
	database := newAccountDB("valid")
	database.rows["SELECT id FROM flickr_accounts"] = scanInto(int64(5))
	repository := newScriptedRepository(t, database)
	account, reconnected, err := repository.Connect(t.Context(), sampleAccount(), sealPair)
	if err != nil || !reconnected || account.ID != 5 {
		t.Fatalf("connect = %+v %v %v", account, reconnected, err)
	}
	var event domain.Connected
	decodeEvent(t, database.events[0], &event)
	if !event.Reconnected || event.AccountID != 5 {
		t.Fatalf("event = %+v", event)
	}
}

func TestConnectRefusesAnUnusableKey(t *testing.T) {
	for name, row := range map[string]memoryRow{
		"deleted": {err: pgx.ErrNoRows},
		"invalid": scanInto("invalid"),
		"revoked": scanInto("revoked"),
	} {
		database := newAccountDB("valid")
		database.rows["FOR SHARE"] = row
		repository := newScriptedRepository(t, database)
		if _, _, err := repository.Connect(t.Context(), sampleAccount(), sealPair); !errors.Is(err, app.ErrKeyNotUsable) {
			t.Errorf("%s: error = %v", name, err)
		}
		if len(database.events) != 0 {
			t.Errorf("%s: published %d events", name, len(database.events))
		}
	}
}

func TestConnectReportsStorageFailures(t *testing.T) {
	boom := errors.New("boom")
	cases := map[string]func(*scriptedDB){
		"lock":    func(d *scriptedDB) { d.rows["FOR SHARE"] = memoryRow{err: boom} },
		"find":    func(d *scriptedDB) { d.rows["SELECT id FROM flickr_accounts"] = memoryRow{err: boom} },
		"nextval": func(d *scriptedDB) { d.rows["nextval"] = memoryRow{err: boom} },
		"insert":  func(d *scriptedDB) { d.rows["INSERT INTO flickr_accounts"] = memoryRow{err: boom} },
		"outbox":  func(d *scriptedDB) { d.execErr = boom },
		"refresh": func(d *scriptedDB) {
			d.rows["SELECT id FROM flickr_accounts"] = scanInto(int64(5))
			d.rows["SET key_id"] = memoryRow{err: boom}
		},
	}
	for name, mutate := range cases {
		database := newAccountDB("valid")
		mutate(database)
		repository := newScriptedRepository(t, database)
		if _, _, err := repository.Connect(t.Context(), sampleAccount(), sealPair); err == nil {
			t.Errorf("%s: failure was ignored", name)
		}
		if database.commits != 0 {
			t.Errorf("%s: committed %d times", name, database.commits)
		}
	}
	repository := newScriptedRepository(t, newAccountDB("valid"))
	if _, _, err := repository.Connect(t.Context(), sampleAccount(), func(int64) (crypto.Sealed, crypto.Sealed, error) {
		return crypto.Sealed{}, crypto.Sealed{}, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("seal failure = %v", err)
	}
	if _, _, err := repository.Connect(t.Context(), sampleAccount(), nil); err == nil {
		t.Fatal("nil sealer was accepted")
	}
	if _, _, err := (*Repository)(nil).Connect(t.Context(), sampleAccount(), sealPair); err == nil {
		t.Fatal("nil repository was accepted")
	}
}

func TestMarkRevokedFlipsStatusAndPublishesOnce(t *testing.T) {
	database := &scriptedDB{rows: map[string]memoryRow{
		"UPDATE flickr_accounts": scanInto(int64(7), int64(4), "21207597@N07"),
	}}
	repository := newScriptedRepository(t, database)
	changed, err := repository.MarkRevoked(t.Context(), 21)
	if err != nil || !changed {
		t.Fatalf("revoke = %v %v", changed, err)
	}
	if len(database.events) != 1 || database.events[0].Topic != domain.RevokedTopic || database.events[0].Key != "21" {
		t.Fatalf("events = %+v", database.events)
	}
	var event domain.Revoked
	decodeEvent(t, database.events[0], &event)
	if event != (domain.Revoked{AccountID: 21, UserID: 7, KeyID: 4, NSID: "21207597@N07"}) {
		t.Fatalf("event = %+v", event)
	}

	database = &scriptedDB{rows: map[string]memoryRow{"UPDATE flickr_accounts": {err: pgx.ErrNoRows}}}
	repository = newScriptedRepository(t, database)
	if changed, err := repository.MarkRevoked(t.Context(), 21); err != nil || changed || len(database.events) != 0 {
		t.Fatalf("already revoked = %v %v events=%d", changed, err, len(database.events))
	}

	database = &scriptedDB{rows: map[string]memoryRow{"UPDATE flickr_accounts": {err: errors.New("boom")}}}
	if _, err := newScriptedRepository(t, database).MarkRevoked(t.Context(), 21); err == nil {
		t.Fatal("update failure was ignored")
	}
	database = &scriptedDB{rows: map[string]memoryRow{"UPDATE flickr_accounts": scanInto(int64(7), int64(4), "n")}, execErr: errors.New("boom")}
	if changed, err := newScriptedRepository(t, database).MarkRevoked(t.Context(), 21); err == nil || changed {
		t.Fatalf("publish failure = %v %v", changed, err)
	}
	if _, err := (*Repository)(nil).MarkRevoked(t.Context(), 21); err == nil {
		t.Fatal("nil repository was accepted")
	}
}

func TestRevokeSQLNeverTouchesARevokedAccount(t *testing.T) {
	if !strings.Contains(revokeSQL, "status <> 'revoked'") || !strings.Contains(revokeSQL, "SET status = 'revoked'") {
		t.Fatalf("revokeSQL = %s", revokeSQL)
	}
}

func TestKeyCredentialsReturnsTheSealedSecretOnlyForTheOwner(t *testing.T) {
	sealed := mustSeal("secret")
	database := &scriptedDB{rows: map[string]memoryRow{
		"FROM api_keys": scanInto(int64(4), int64(7), "Personal", "public", 100, 2, 3, "valid", accountCreatedAt, sealed),
	}}
	repository := newScriptedRepository(t, database)
	key, got, err := repository.KeyCredentials(t.Context(), 7, 4)
	if err != nil || key.ID != 4 || key.PublicKey != "public" || key.Status != domain.StatusValid || string(got.Nonce) != string(sealed.Nonce) {
		t.Fatalf("credentials = %+v %+v %v", key, got, err)
	}
	database.rows["FROM api_keys"] = memoryRow{err: pgx.ErrNoRows}
	if _, _, err := repository.KeyCredentials(t.Context(), 8, 4); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("other user = %v", err)
	}
	database.rows["FROM api_keys"] = memoryRow{err: errors.New("boom")}
	if _, _, err := repository.KeyCredentials(t.Context(), 7, 4); err == nil || errors.Is(err, app.ErrNotFound) {
		t.Fatalf("query failure = %v", err)
	}
	if _, _, err := (*Repository)(nil).KeyCredentials(t.Context(), 7, 4); err == nil {
		t.Fatal("nil repository was accepted")
	}
}
