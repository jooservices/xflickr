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

func TestNewAndGuards(t *testing.T) {
	if _, err := New(nil); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("New(nil) error = %v", err)
	}
	if _, err := (*Repository)(nil).List(t.Context(), 1); err == nil {
		t.Fatal("nil repository listed keys")
	}
	database := newMemoryDB()
	repository, err := New(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(nil, 1, 1); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("nil context error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := repository.Delete(canceled, 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled delete error = %v", err)
	}
	if _, err := repository.Create(t.Context(), domain.APIKey{}, nil); err == nil || !strings.Contains(err.Error(), "sealer is required") {
		t.Fatalf("nil sealer error = %v", err)
	}
	if _, err := repository.Update(t.Context(), 1, 1, nil); err == nil || !strings.Contains(err.Error(), "update is required") {
		t.Fatalf("nil update error = %v", err)
	}
	if database.begins != 0 {
		t.Fatalf("rejected calls began %d transactions", database.begins)
	}
}

func TestQueriesDoNotReadSecrets(t *testing.T) {
	for _, query := range []string{listSQL, getSQL, lockSQL, deleteSQL} {
		if strings.Contains(query, "secret") {
			t.Fatalf("read query selects a secret: %s", query)
		}
	}
	if !strings.Contains(markSQL, string(domain.AccountReconnectRequired)) {
		t.Fatalf("mark SQL = %s", markSQL)
	}
}

func TestRepositoryCreateListGetAndDuplicate(t *testing.T) {
	database := newMemoryDB()
	repository, err := New(database)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repository.Create(t.Context(), sampleKey(7, "Personal", "public-key-one"), sealWith("secret-one"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.Create(t.Context(), sampleKey(7, "Archive", "public-key-two"), sealWith("secret-two"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := repository.Create(t.Context(), sampleKey(8, "Other", "public-key-one"), sealWith("secret-other"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == 0 || second.ID == first.ID || first.CreatedAt.IsZero() || first.Status != domain.StatusValid {
		t.Fatalf("created = %+v %+v", first, second)
	}
	if string(database.keys[first.ID].sealed.Ciphertext) == "" || database.keys[first.ID].key.PublicKey != "public-key-one" {
		t.Fatalf("stored = %+v", database.keys[first.ID])
	}
	listed, err := repository.List(t.Context(), 7)
	if err != nil || len(listed) != 2 || listed[0].ID != first.ID || listed[1].ID != second.ID {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	empty, err := repository.List(t.Context(), 99)
	if err != nil || len(empty) != 0 || empty == nil {
		t.Fatalf("empty list = %#v, %v", empty, err)
	}
	got, err := repository.Get(t.Context(), 7, first.ID)
	if err != nil || got.Label != "Personal" || got.PublicKey != "public-key-one" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := repository.Get(t.Context(), 8, first.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("other user get error = %v", err)
	}
	if _, err := repository.Create(t.Context(), sampleKey(7, "Again", "public-key-one"), sealWith("secret-one")); !errors.Is(err, app.ErrKeyExists) {
		t.Fatalf("duplicate error = %v", err)
	}
	if other.UserID != 8 {
		t.Fatalf("other user key = %+v", other)
	}
	if database.options[0].IsoLevel != pgx.Serializable {
		t.Fatalf("isolation = %s", database.options[0].IsoLevel)
	}
}

func TestRepositoryUpdateMarksAccountsOnlyWhenSecretChanges(t *testing.T) {
	database := newMemoryDB()
	repository, err := New(database)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repository.Create(t.Context(), sampleKey(7, "Personal", "public-key-one"), sealWith("secret-one"))
	if err != nil {
		t.Fatal(err)
	}
	database.accounts[created.ID] = string(domain.AccountOK)
	updated, err := repository.Update(t.Context(), 7, created.ID, func(current domain.APIKey) (domain.APIKey, *crypto.Sealed, error) {
		current.Label = "Renamed"
		current.PerHourLimit = 50
		current.PublicKey = "should-not-stick"
		current.Status = domain.StatusRevoked
		return current, nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Label != "Renamed" || updated.PerHourLimit != 50 || updated.PublicKey != "public-key-one" || updated.Status != domain.StatusValid {
		t.Fatalf("label update = %+v", updated)
	}
	if database.accounts[created.ID] != string(domain.AccountOK) {
		t.Fatalf("status after label update = %s", database.accounts[created.ID])
	}
	replacement := mustSeal("secret-two")
	updated, err = repository.Update(t.Context(), 7, created.ID, func(current domain.APIKey) (domain.APIKey, *crypto.Sealed, error) {
		return current, &replacement, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if database.accounts[created.ID] != string(domain.AccountReconnectRequired) {
		t.Fatalf("status after secret update = %s", database.accounts[created.ID])
	}
	if database.keys[created.ID].sealed.KeyID != replacement.KeyID {
		t.Fatal("sealed secret was not replaced")
	}
	if updated.ID != created.ID {
		t.Fatalf("updated id = %d", updated.ID)
	}
	if _, err := repository.Update(t.Context(), 7, created.ID+99, func(current domain.APIKey) (domain.APIKey, *crypto.Sealed, error) {
		return current, nil, nil
	}); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("missing update error = %v", err)
	}
}

func TestRepositoryDeletePublishesEventWithoutSecrets(t *testing.T) {
	database := newMemoryDB()
	repository, err := New(database)
	if err != nil {
		t.Fatal(err)
	}
	created, err := repository.Create(t.Context(), sampleKey(4, "Personal", "public-key-one"), sealWith("secret-value"))
	if err != nil {
		t.Fatal(err)
	}
	database.accounts[created.ID] = string(domain.AccountOK)
	if err := repository.Delete(t.Context(), 4, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := database.keys[created.ID]; ok {
		t.Fatal("key remained after delete")
	}
	if _, ok := database.accounts[created.ID]; ok {
		t.Fatal("connected account remained after delete")
	}
	if len(database.events) != 1 || database.events[0].Topic != domain.DeletedTopic || database.events[0].Key != itoa(created.ID) {
		t.Fatalf("events = %+v", database.events)
	}
	var payload domain.Deleted
	if err := json.Unmarshal(database.events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != created.ID || payload.UserID != 4 {
		t.Fatalf("payload = %+v", payload)
	}
	encoded := string(database.events[0].Payload)
	if strings.Contains(encoded, "secret-value") || strings.Contains(encoded, "public-key-one") {
		t.Fatalf("event leaked key material: %s", encoded)
	}
	if err := repository.Delete(t.Context(), 4, created.ID); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("second delete error = %v", err)
	}
	if len(database.events) != 1 {
		t.Fatal("missing delete published an event")
	}
}

func TestRepositoryRollsBackFailedWrites(t *testing.T) {
	database := newMemoryDB()
	repository, err := New(database)
	if err != nil {
		t.Fatal(err)
	}
	database.queryErr = "next"
	if _, err := repository.Create(t.Context(), sampleKey(1, "Personal", "public"), sealWith("secret")); err == nil || !strings.Contains(err.Error(), "allocate") {
		t.Fatalf("allocate error = %v", err)
	}
	database.queryErr = ""
	database.sealErr = errors.New("seal failed")
	if _, err := repository.Create(t.Context(), sampleKey(1, "Personal", "public"), func(int64) (crypto.Sealed, error) {
		return crypto.Sealed{}, database.sealErr
	}); err == nil || !strings.Contains(err.Error(), "seal failed") || len(database.keys) != 0 {
		t.Fatalf("seal error = %v keys=%d", err, len(database.keys))
	}
	created, err := repository.Create(t.Context(), sampleKey(1, "Personal", "public"), sealWith("secret"))
	if err != nil {
		t.Fatal(err)
	}
	database.failExec = 2
	database.execErr = errors.New("mark failed")
	replacement := mustSeal("next-secret")
	if _, err := repository.Update(t.Context(), 1, created.ID, func(current domain.APIKey) (domain.APIKey, *crypto.Sealed, error) {
		current.Label = "Changed"
		return current, &replacement, nil
	}); err == nil || !strings.Contains(err.Error(), "mark connected") {
		t.Fatalf("mark error = %v", err)
	}
	if database.keys[created.ID].key.Label != "Personal" {
		t.Fatal("failed secret update was committed")
	}
	database.failExec = 1
	database.execErr = errors.New("delete failed")
	if err := repository.Delete(t.Context(), 1, created.ID); err == nil || !strings.Contains(err.Error(), "delete api key") {
		t.Fatalf("delete error = %v", err)
	}
	if _, ok := database.keys[created.ID]; !ok {
		t.Fatal("failed delete removed the key")
	}
	database.failExec = 0
	database.queryErr = "list"
	if _, err := repository.List(t.Context(), 1); err == nil || !strings.Contains(err.Error(), "list api keys") {
		t.Fatalf("list error = %v", err)
	}
	database.queryErr = ""
	database.rowsErr = errors.New("rows failed")
	if _, err := repository.List(t.Context(), 1); err == nil || !strings.Contains(err.Error(), "rows failed") {
		t.Fatalf("rows error = %v", err)
	}
	database.rowsErr = nil
	stored := database.keys[created.ID]
	stored.key.Status = "paused"
	database.keys[created.ID] = stored
	if _, err := repository.Get(t.Context(), 1, created.ID); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Fatalf("bad status error = %v", err)
	}
	database.beginErr = errors.New("begin failed")
	if _, err := repository.List(t.Context(), 1); err == nil || !strings.Contains(err.Error(), "begin failed") {
		t.Fatalf("begin error = %v", err)
	}
}

func sampleKey(userID int64, label, public string) domain.APIKey {
	return domain.APIKey{
		UserID: userID, Label: label, PublicKey: public,
		PerHourLimit: 3600, PerSecondLimit: 4, Burst: 5, Status: domain.StatusValid,
	}
}

func sealWith(secret string) func(int64) (crypto.Sealed, error) {
	return func(id int64) (crypto.Sealed, error) {
		if string(domain.SecretAAD(id)) == "" {
			return crypto.Sealed{}, errors.New("missing id")
		}
		return mustSeal(secret), nil
	}
}

func mustSeal(secret string) crypto.Sealed {
	sealed := crypto.Sealed{
		KeyID:      "test",
		Nonce:      []byte(secret + "nonce-value"),
		Ciphertext: append([]byte(secret), bytesOf(16)...),
		WrappedDEK: bytesOf(60),
	}
	sealed.Nonce = sealed.Nonce[:12]
	sealed.Ciphertext = sealed.Ciphertext[:16]
	return sealed
}

func bytesOf(n int) []byte {
	return make([]byte, n)
}

func itoa(id int64) string {
	return jsonNumber(id)
}

func jsonNumber(id int64) string {
	encoded, _ := json.Marshal(id)
	return strings.Trim(string(encoded), `"`)
}

type memoryDB struct {
	nextID    int64
	keys      map[int64]storedKey
	accounts  map[int64]string
	events    []recordedEvent
	options   []pgx.TxOptions
	begins    int
	beginErr  error
	commitErr error
	queryErr  string
	rowsErr   error
	failExec  int
	execErr   error
	sealErr   error
}

type storedKey struct {
	key    domain.APIKey
	sealed crypto.Sealed
}

type recordedEvent struct {
	Topic   string
	Key     string
	Payload []byte
}

func newMemoryDB() *memoryDB {
	return &memoryDB{keys: map[int64]storedKey{}, accounts: map[int64]string{}}
}

func (database *memoryDB) BeginTx(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	database.begins++
	database.options = append(database.options, options)
	if database.beginErr != nil {
		return nil, database.beginErr
	}
	return &memoryTx{
		database:  database,
		keys:      cloneKeys(database.keys),
		accounts:  cloneAccounts(database.accounts),
		nextID:    database.nextID,
		commitErr: database.commitErr,
	}, nil
}

type memoryTx struct {
	pgx.Tx
	database  *memoryDB
	keys      map[int64]storedKey
	accounts  map[int64]string
	events    []recordedEvent
	nextID    int64
	execs     int
	commitErr error
}

func (tx *memoryTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "WHERE user_id = $1") {
		if tx.database.queryErr == "list" {
			return nil, errors.New("list query failed")
		}
		userID, ok := args[0].(int64)
		if !ok || len(args) != 1 {
			return nil, errors.New("list query arguments")
		}
		return tx.listRows(userID), nil
	}
	return nil, errors.New("unexpected query")
}

func (tx *memoryTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "nextval"):
		if tx.database.queryErr == "next" {
			return memoryRow{err: errors.New("nextval failed")}
		}
		tx.nextID++
		id := tx.nextID
		return memoryRow{scan: func(dest ...any) error {
			if len(dest) != 1 {
				return errors.New("nextval scan")
			}
			*dest[0].(*int64) = id
			return nil
		}}
	case strings.Contains(sql, "INSERT INTO api_keys"):
		key, sealed, err := keyFromInsert(args)
		if err != nil {
			return memoryRow{err: err}
		}
		for _, existing := range tx.keys {
			if existing.key.UserID == key.UserID && existing.key.PublicKey == key.PublicKey {
				return memoryRow{err: &pgconn.PgError{Code: "23505"}}
			}
		}
		key.CreatedAt = time.Unix(1_700_000_000, 0).UTC()
		tx.keys[key.ID] = storedKey{key: key, sealed: sealed}
		return memoryRow{scan: func(dest ...any) error {
			if len(dest) != 1 {
				return errors.New("insert scan")
			}
			*dest[0].(*time.Time) = key.CreatedAt
			return nil
		}}
	case strings.Contains(sql, "FROM api_keys"):
		if len(args) != 2 {
			return memoryRow{err: errors.New("get query arguments")}
		}
		id, idOK := args[0].(int64)
		userID, userOK := args[1].(int64)
		if !idOK || !userOK {
			return memoryRow{err: errors.New("get query argument types")}
		}
		stored, ok := tx.keys[id]
		if !ok || stored.key.UserID != userID {
			return memoryRow{err: pgx.ErrNoRows}
		}
		return memoryRow{scan: scanStored(stored.key)}
	default:
		return memoryRow{err: errors.New("unexpected query row")}
	}
}

func (tx *memoryTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.execs++
	if tx.database.failExec == tx.execs {
		return pgconn.CommandTag{}, tx.database.execErr
	}
	switch {
	case strings.Contains(sql, "UPDATE api_keys"):
		if len(args) != 7 {
			return pgconn.CommandTag{}, errors.New("update arguments")
		}
		id := args[5].(int64)
		userID := args[6].(int64)
		stored, ok := tx.keys[id]
		if !ok || stored.key.UserID != userID {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		stored.key.Label = args[0].(string)
		stored.key.PerHourLimit = args[1].(int)
		stored.key.PerSecondLimit = args[2].(int)
		stored.key.Burst = args[3].(int)
		if args[4] != nil {
			stored.sealed = args[4].(crypto.Sealed)
		}
		tx.keys[id] = stored
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.Contains(sql, "UPDATE flickr_accounts"):
		id := args[0].(int64)
		if status, ok := tx.accounts[id]; ok && status == string(domain.AccountOK) {
			tx.accounts[id] = string(domain.AccountReconnectRequired)
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.Contains(sql, "DELETE FROM api_keys"):
		id := args[0].(int64)
		userID := args[1].(int64)
		stored, ok := tx.keys[id]
		if !ok || stored.key.UserID != userID {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
		delete(tx.keys, id)
		delete(tx.accounts, id)
		return pgconn.NewCommandTag("DELETE 1"), nil
	case strings.Contains(sql, "INSERT INTO outbox_events"):
		if len(args) != 3 {
			return pgconn.CommandTag{}, errors.New("outbox arguments")
		}
		payload, ok := args[2].([]byte)
		if !ok {
			return pgconn.CommandTag{}, errors.New("outbox payload type")
		}
		tx.events = append(tx.events, recordedEvent{Topic: args[0].(string), Key: args[1].(string), Payload: append([]byte(nil), payload...)})
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		return pgconn.CommandTag{}, errors.New("unexpected exec")
	}
}

func (tx *memoryTx) Commit(context.Context) error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
	tx.database.keys = tx.keys
	tx.database.accounts = tx.accounts
	tx.database.nextID = tx.nextID
	tx.database.events = append(tx.database.events, tx.events...)
	return nil
}

func (tx *memoryTx) Rollback(context.Context) error { return nil }

func (tx *memoryTx) listRows(userID int64) pgx.Rows {
	ids := make([]int64, 0)
	for id, stored := range tx.keys {
		if stored.key.UserID == userID {
			ids = append(ids, id)
		}
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	scans := make([]func(...any) error, 0, len(ids))
	for _, id := range ids {
		stored := tx.keys[id].key
		scans = append(scans, scanStored(stored))
	}
	return &memoryRows{scans: scans, err: tx.database.rowsErr}
}

func scanStored(key domain.APIKey) func(...any) error {
	return func(dest ...any) error {
		if len(dest) != 9 {
			return errors.New("key scan destination count")
		}
		*dest[0].(*int64) = key.ID
		*dest[1].(*int64) = key.UserID
		*dest[2].(*string) = key.Label
		*dest[3].(*string) = key.PublicKey
		*dest[4].(*int) = key.PerHourLimit
		*dest[5].(*int) = key.PerSecondLimit
		*dest[6].(*int) = key.Burst
		*dest[7].(*string) = string(key.Status)
		*dest[8].(*time.Time) = key.CreatedAt
		return nil
	}
}

func keyFromInsert(args []any) (domain.APIKey, crypto.Sealed, error) {
	if len(args) != 9 {
		return domain.APIKey{}, crypto.Sealed{}, errors.New("insert arguments")
	}
	key := domain.APIKey{
		ID:             args[0].(int64),
		UserID:         args[1].(int64),
		Label:          args[2].(string),
		PublicKey:      args[3].(string),
		PerHourLimit:   args[5].(int),
		PerSecondLimit: args[6].(int),
		Burst:          args[7].(int),
		Status:         domain.Status(args[8].(string)),
	}
	sealed, ok := args[4].(crypto.Sealed)
	if !ok {
		return domain.APIKey{}, crypto.Sealed{}, errors.New("sealed argument type")
	}
	return key, sealed, nil
}

func cloneKeys(source map[int64]storedKey) map[int64]storedKey {
	clone := make(map[int64]storedKey, len(source))
	for id, key := range source {
		clone[id] = key
	}
	return clone
}

func cloneAccounts(source map[int64]string) map[int64]string {
	clone := make(map[int64]string, len(source))
	for id, status := range source {
		clone[id] = status
	}
	return clone
}

type memoryRow struct {
	scan func(...any) error
	err  error
}

func (row memoryRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	return row.scan(dest...)
}

type memoryRows struct {
	pgx.Rows
	scans []func(...any) error
	index int
	err   error
}

func (rows *memoryRows) Next() bool { return rows.index < len(rows.scans) }

func (rows *memoryRows) Scan(dest ...any) error {
	if rows.index >= len(rows.scans) {
		return errors.New("scan without a row")
	}
	err := rows.scans[rows.index](dest...)
	rows.index++
	return err
}

func (rows *memoryRows) Err() error {
	if rows.index < len(rows.scans) {
		return nil
	}
	return rows.err
}

func (rows *memoryRows) Close() {}
