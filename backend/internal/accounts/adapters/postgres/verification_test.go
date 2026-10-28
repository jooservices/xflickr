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

func TestLoadVerificationReadsSealedCredentials(t *testing.T) {
	keySecret := crypto.Sealed{Ciphertext: []byte("sealed-key")}
	token := crypto.Sealed{Ciphertext: []byte("sealed-token")}
	tokenSecret := crypto.Sealed{Ciphertext: []byte("sealed-token-secret")}
	database := &verificationDB{rows: map[string]memoryRow{
		"FROM flickr_accounts a": scanInto(
			int64(42), int64(7), int64(5), "user-1", "jamal", string(domain.AccountOK), accountCreatedAt,
			int64(5), int64(7), "Personal", "public-key", 3600, 10, 20, string(domain.StatusValid), accountCreatedAt,
			keySecret, token, tokenSecret,
		),
	}}
	repository := verificationRepository(t, database)
	got, err := repository.LoadVerification(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if got.Account.ID != 42 || got.Account.UserID != 7 || got.Account.KeyID != 5 || got.Account.NSID != "user-1" || got.Account.Status != domain.AccountOK {
		t.Fatalf("account = %+v", got.Account)
	}
	if got.Key.ID != 5 || got.Key.PublicKey != "public-key" || got.Key.Status != domain.StatusValid || string(got.KeySecret.Ciphertext) != "sealed-key" || string(got.Token.Ciphertext) != "sealed-token" || string(got.TokenSecret.Ciphertext) != "sealed-token-secret" {
		t.Fatalf("verification credentials = %+v", got)
	}

	database.rows["FROM flickr_accounts a"] = memoryRow{err: pgx.ErrNoRows}
	if _, err := repository.LoadVerification(t.Context(), 42); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("missing account error = %v", err)
	}
}

func TestRecordVerificationPublishesReconnectAndRevokedEventsIdempotently(t *testing.T) {
	t.Run("invalid key requests reconnect", func(t *testing.T) {
		expected := verificationSnapshot(domain.AccountOK, domain.StatusValid)
		database := verificationStatusDB(domain.AccountOK, domain.StatusValid)
		repository := verificationRepository(t, database)
		if err := repository.RecordVerification(t.Context(), expected, domain.StatusInvalid, domain.AccountReconnectRequired); err != nil {
			t.Fatal(err)
		}
		if database.commits != 1 || len(database.events) != 1 || database.events[0].Topic != domain.NotificationRequestedTopic {
			t.Fatalf("commits = %d events = %+v", database.commits, database.events)
		}
		var notification domain.NotificationRequested
		if err := json.Unmarshal(database.events[0].Payload, &notification); err != nil {
			t.Fatal(err)
		}
		if notification != (domain.NotificationRequested{AccountID: 42, UserID: 7, KeyID: 5, Kind: domain.NotificationReconnect}) {
			t.Fatalf("notification = %+v", notification)
		}
	})

	t.Run("revoked token emits revocation and reconnect", func(t *testing.T) {
		expected := verificationSnapshot(domain.AccountOK, domain.StatusValid)
		database := verificationStatusDB(domain.AccountOK, domain.StatusValid)
		repository := verificationRepository(t, database)
		if err := repository.RecordVerification(t.Context(), expected, domain.StatusValid, domain.AccountRevoked); err != nil {
			t.Fatal(err)
		}
		if len(database.events) != 2 || database.events[0].Topic != domain.RevokedTopic || database.events[1].Topic != domain.NotificationRequestedTopic {
			t.Fatalf("events = %+v", database.events)
		}
	})

	t.Run("repeated invalid result does not notify twice", func(t *testing.T) {
		expected := verificationSnapshot(domain.AccountReconnectRequired, domain.StatusInvalid)
		database := verificationStatusDB(domain.AccountReconnectRequired, domain.StatusInvalid)
		repository := verificationRepository(t, database)
		if err := repository.RecordVerification(t.Context(), expected, domain.StatusInvalid, domain.AccountReconnectRequired); err != nil {
			t.Fatal(err)
		}
		if len(database.events) != 0 || len(database.exec) != 0 {
			t.Fatalf("repeat emitted events or writes: %+v, %+v", database.events, database.exec)
		}
	})
}

func TestRecordVerificationRejectsIllegalTransitionsAndRollsBackFailures(t *testing.T) {
	expected := verificationSnapshot(domain.AccountOK, domain.StatusRevoked)
	database := verificationStatusDB(domain.AccountOK, domain.StatusRevoked)
	repository := verificationRepository(t, database)
	if err := repository.RecordVerification(t.Context(), expected, domain.StatusInvalid, domain.AccountReconnectRequired); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("revoked key transition error = %v", err)
	}
	if database.commits != 0 {
		t.Fatalf("illegal transition committed %d times", database.commits)
	}

	database = verificationStatusDB(domain.AccountOK, domain.StatusValid)
	database.execErr = errors.New("write failed")
	repository = verificationRepository(t, database)
	if err := repository.RecordVerification(t.Context(), verificationSnapshot(domain.AccountOK, domain.StatusValid), domain.StatusInvalid, domain.AccountReconnectRequired); err == nil {
		t.Fatal("write failure was ignored")
	}
	if database.commits != 0 {
		t.Fatalf("failed write committed %d times", database.commits)
	}
}

func TestRecordVerificationRejectsStaleCredentialSnapshot(t *testing.T) {
	expected := verificationSnapshot(domain.AccountOK, domain.StatusValid)
	database := verificationStatusDB(domain.AccountOK, domain.StatusValid)
	database.rows["FOR UPDATE OF a, k"] = scanInto(
		int64(7), int64(5), "user-1", string(domain.AccountOK), string(domain.StatusValid), "public-key",
		crypto.Sealed{Ciphertext: []byte("new-token")}, expected.TokenSecret, expected.KeySecret,
	)
	repository := verificationRepository(t, database)
	if err := repository.RecordVerification(t.Context(), expected, domain.StatusValid, domain.AccountRevoked); !errors.Is(err, app.ErrVerificationChanged) {
		t.Fatalf("stale verification error = %v", err)
	}
	if database.commits != 0 || len(database.events) != 0 {
		t.Fatalf("stale check committed or emitted events: commits=%d events=%+v", database.commits, database.events)
	}
}

func TestVerificationAccountIDsListsOnlyIDs(t *testing.T) {
	database := &verificationDB{ids: []int64{2, 9}}
	repository := verificationRepository(t, database)
	ids, err := repository.VerificationAccountIDs(t.Context())
	if err != nil || len(ids) != 2 || ids[0] != 2 || ids[1] != 9 {
		t.Fatalf("account ids = %v, %v", ids, err)
	}
	if !strings.Contains(database.query, "SELECT id FROM flickr_accounts ORDER BY id") {
		t.Fatalf("account ID query = %s", database.query)
	}
	if strings.Contains(database.query, "token") || strings.Contains(database.query, "secret") {
		t.Fatalf("account ID query selected credentials: %s", database.query)
	}

	database.queryErr = errors.New("query failed")
	if _, err := repository.VerificationAccountIDs(t.Context()); err == nil {
		t.Fatal("account ID query error was ignored")
	}
}

func TestListAccountsReadsMetadataWithoutSecrets(t *testing.T) {
	database := &verificationDB{accountRows: [][]any{{int64(42), int64(7), int64(5), "user-1", "jamal", string(domain.AccountOK), accountCreatedAt}}}
	repository := verificationRepository(t, database)
	accounts, err := repository.ListAccounts(t.Context(), 7)
	if err != nil || len(accounts) != 1 || accounts[0].ID != 42 || accounts[0].Status != domain.AccountOK {
		t.Fatalf("accounts = %+v, %v", accounts, err)
	}
	if strings.Contains(database.query, "secret") || strings.Contains(database.query, "token") {
		t.Fatalf("account list query selected credentials: %s", database.query)
	}

	database.accountRows = [][]any{{int64(42), int64(7), int64(5), "user-1", "jamal", string(domain.AccountOK), accountCreatedAt}}
	database.queryScanErr = errors.New("scan failed")
	if _, err := repository.ListAccounts(t.Context(), 7); err == nil {
		t.Fatal("account row scan error was ignored")
	}

	database.queryScanErr = nil
	database.queryRowsErr = errors.New("rows failed")
	if _, err := repository.ListAccounts(t.Context(), 7); err == nil {
		t.Fatal("account rows error was ignored")
	}

	database.queryRowsErr = nil
	database.accountRows = nil
	empty, err := repository.ListAccounts(t.Context(), 7)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty account list = %#v, %v", empty, err)
	}
}

func verificationRepository(t *testing.T, database *verificationDB) *Repository {
	t.Helper()
	repository, err := New(database)
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func verificationStatusDB(accountStatus domain.AccountStatus, keyStatus domain.Status) *verificationDB {
	expected := verificationSnapshot(accountStatus, keyStatus)
	return &verificationDB{rows: map[string]memoryRow{
		"FOR UPDATE OF a, k": scanInto(
			int64(7), int64(5), "user-1", string(accountStatus), string(keyStatus), "public-key",
			expected.Token, expected.TokenSecret, expected.KeySecret,
		),
	}}
}

func verificationSnapshot(accountStatus domain.AccountStatus, keyStatus domain.Status) app.VerificationCredentials {
	return app.VerificationCredentials{
		Account:     domain.Account{ID: 42, UserID: 7, KeyID: 5, NSID: "user-1", Status: accountStatus},
		Key:         domain.APIKey{ID: 5, UserID: 7, PublicKey: "public-key", Status: keyStatus},
		Token:       crypto.Sealed{Ciphertext: []byte("token")},
		TokenSecret: crypto.Sealed{Ciphertext: []byte("token-secret")},
		KeySecret:   crypto.Sealed{Ciphertext: []byte("key-secret")},
	}
}

type verificationDB struct {
	rows         map[string]memoryRow
	ids          []int64
	accountRows  [][]any
	queryErr     error
	queryScanErr error
	queryRowsErr error
	execErr      error
	events       []recordedEvent
	exec         []string
	query        string
	commits      int
}

func (d *verificationDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return &verificationTx{db: d}, nil
}

type verificationTx struct {
	pgx.Tx
	db *verificationDB
}

func (tx *verificationTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	for fragment, row := range tx.db.rows {
		if strings.Contains(sql, fragment) {
			return row
		}
	}
	return memoryRow{err: errors.New("unexpected query row: " + sql)}
}

func (tx *verificationTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	tx.db.query = sql
	if tx.db.queryErr != nil {
		return nil, tx.db.queryErr
	}
	return &verificationRows{ids: tx.db.ids, values: tx.db.accountRows, scanErr: tx.db.queryScanErr, rowsErr: tx.db.queryRowsErr, index: -1}, nil
}

func (tx *verificationTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.db.execErr != nil {
		return pgconn.CommandTag{}, tx.db.execErr
	}
	tx.db.exec = append(tx.db.exec, sql)
	if strings.Contains(sql, "INSERT INTO outbox_events") {
		tx.db.events = append(tx.db.events, recordedEvent{Topic: args[0].(string), Key: args[1].(string), Payload: args[2].([]byte)})
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *verificationTx) Commit(context.Context) error {
	tx.db.commits++
	return nil
}

func (tx *verificationTx) Rollback(context.Context) error { return nil }

type verificationRows struct {
	pgx.Rows
	ids     []int64
	values  [][]any
	scanErr error
	rowsErr error
	index   int
}

func (rows *verificationRows) Next() bool {
	rows.index++
	return rows.index < max(len(rows.ids), len(rows.values))
}

func (rows *verificationRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	if len(rows.values) != 0 {
		values := rows.values[rows.index]
		if len(dest) != len(values) {
			return errors.New("unexpected account row scan destination count")
		}
		for index, value := range values {
			switch target := dest[index].(type) {
			case *int64:
				*target = value.(int64)
			case *string:
				*target = value.(string)
			case *time.Time:
				*target = value.(time.Time)
			default:
				return errors.New("unexpected account row scan destination")
			}
		}
		return nil
	}
	if len(dest) != 1 {
		return errors.New("unexpected account ID scan destination count")
	}
	id, ok := dest[0].(*int64)
	if !ok {
		return errors.New("unexpected account ID scan destination")
	}
	*id = rows.ids[rows.index]
	return nil
}

func (rows *verificationRows) Err() error { return rows.rowsErr }
func (rows *verificationRows) Close()     {}
