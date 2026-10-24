package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jooservices/xflickr/internal/settings/domain"
)

func TestNewRequiresDatabase(t *testing.T) {
	if _, err := New(nil); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("New(nil) error = %v", err)
	}
	if _, err := (*Repository)(nil).Load(t.Context(), 1); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("nil repository Load() error = %v", err)
	}
	if err := (&Repository{}).SetUserOverride(t.Context(), 1, domain.MaxDepth, json.RawMessage("2")); err == nil || !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("empty repository SetUserOverride() error = %v", err)
	}
}

func TestRepositoryRejectsMissingContextAndEmptyWrites(t *testing.T) {
	database := newMemoryDB(domain.Defaults())
	repository, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := repository.Load(nil, 1); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("Load() error = %v", err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := repository.Load(canceled, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v", err)
	}
	if err := repository.SetUserOverride(t.Context(), 1, " ", json.RawMessage("1")); err == nil || !strings.Contains(err.Error(), "key is required") {
		t.Fatalf("blank key error = %v", err)
	}
	if err := repository.SetUserOverride(t.Context(), 1, domain.MaxDepth, json.RawMessage(" \n")); err == nil || !strings.Contains(err.Error(), "value for max_depth") {
		t.Fatalf("blank value error = %v", err)
	}
	if err := repository.SetApplicationSetting(t.Context(), domain.Definition{Value: json.RawMessage("1")}); err == nil || !strings.Contains(err.Error(), "application setting key is required") {
		t.Fatalf("blank application key error = %v", err)
	}
	if err := repository.SetApplicationSetting(t.Context(), domain.Definition{Key: domain.Burst, Value: json.RawMessage(" ")}); err == nil || !strings.Contains(err.Error(), "value for burst") {
		t.Fatalf("blank application value error = %v", err)
	}
	if err := repository.SetApplicationSetting(nil, domain.Defaults()[0]); !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("nil context error = %v", err)
	}
	if database.begins != 0 {
		t.Fatalf("rejected writes began %d transactions", database.begins)
	}
}

func TestLoadReadsApplicationAndUserSettings(t *testing.T) {
	database := newMemoryDB(domain.Defaults())
	database.users[7] = map[domain.Key]json.RawMessage{domain.MaxDepth: json.RawMessage("4")}
	repository, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	loaded, err := repository.Load(t.Context(), 7)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Settings) != len(domain.Defaults()) || string(loaded.Overrides[domain.MaxDepth]) != "4" {
		t.Fatalf("loaded settings/override = %d/%s", len(loaded.Settings), loaded.Overrides[domain.MaxDepth])
	}
	depth := loaded.Settings[domain.MaxDepth]
	if string(depth.Value) != "2" || !depth.UserOverridable || depth.Min == nil || *depth.Min != 1 || depth.Max == nil || *depth.Max != 4 {
		t.Fatalf("max_depth = %+v", depth)
	}
	if depth.Value[0] = '7'; string(database.settings[domain.MaxDepth].Value) != "2" {
		t.Fatal("Load() returned a shared JSON buffer")
	}
	empty, err := repository.Load(t.Context(), 8)
	if err != nil {
		t.Fatalf("Load(other) error = %v", err)
	}
	if len(empty.Overrides) != 0 || empty.Overrides == nil {
		t.Fatalf("overrides for a user without rows = %#v", empty.Overrides)
	}
	if database.begins != 2 || database.options[0].IsoLevel != pgx.Serializable {
		t.Fatalf("begins/isolation = %d/%v", database.begins, database.options)
	}
	if len(database.txs) != 2 || database.txs[0].commits != 1 || database.txs[0].rollbacks != 0 {
		t.Fatalf("transactions = %+v", database.txs)
	}
	joined := strings.Join(database.txs[1].statements, "\n")
	if !strings.Contains(joined, "FROM app_settings") || !strings.Contains(joined, "FROM user_settings") {
		t.Fatalf("queries = %s", joined)
	}
}

func TestLoadReportsQueryScanAndIterationErrors(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*memoryDB)
		want    string
	}{
		{name: "application query", prepare: func(database *memoryDB) { database.queryErr = "app" }, want: "query application settings"},
		{name: "application scan", prepare: func(database *memoryDB) { database.scanErr = "app" }, want: "read application setting"},
		{name: "application rows", prepare: func(database *memoryDB) { database.rowsErr = "app" }, want: "iterate application settings"},
		{name: "user query", prepare: func(database *memoryDB) { database.queryErr = "user" }, want: "query user settings"},
		{name: "user scan", prepare: func(database *memoryDB) { database.scanErr = "user" }, want: "read user setting"},
		{name: "user rows", prepare: func(database *memoryDB) { database.rowsErr = "user" }, want: "iterate user settings"},
		{name: "begin", prepare: func(database *memoryDB) { database.beginErr = errors.New("database unavailable") }, want: "begin unit of work"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newMemoryDB(domain.Defaults())
			database.users[1] = map[domain.Key]json.RawMessage{domain.Burst: json.RawMessage("6")}
			test.prepare(database)
			repository, err := New(database)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if _, err := repository.Load(t.Context(), 1); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestUserOverridePersistsAndPublishesChange(t *testing.T) {
	database := newMemoryDB(domain.Defaults())
	repository, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := repository.SetUserOverride(t.Context(), 7, domain.MaxDepth, json.RawMessage("4")); err != nil {
		t.Fatalf("SetUserOverride() error = %v", err)
	}
	loaded, err := repository.Load(t.Context(), 7)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if string(loaded.Overrides[domain.MaxDepth]) != "4" {
		t.Fatalf("stored override = %s", loaded.Overrides[domain.MaxDepth])
	}
	if len(database.events) != 1 {
		t.Fatalf("events = %d, want 1", len(database.events))
	}
	event := database.events[0]
	if event.topic != domain.ChangedTopic || event.key != "user:7:max_depth" {
		t.Fatalf("event topic/key = %s/%s", event.topic, event.key)
	}
	var change domain.Change
	if err := json.Unmarshal(event.payload, &change); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if change.Key != domain.MaxDepth || change.Scope != domain.ScopeUser || change.UserID == nil || *change.UserID != 7 || string(change.Value) != "4" {
		t.Fatalf("change = %+v", change)
	}
	write := database.txs[0]
	if write.commits != 1 || len(write.statements) != 2 || !strings.Contains(write.statements[0], "INSERT INTO user_settings") || !strings.Contains(write.statements[1], "INSERT INTO outbox_events") {
		t.Fatalf("write statements = %+v", write)
	}
}

func TestApplicationSettingPersistsAndPublishesChange(t *testing.T) {
	database := newMemoryDB(domain.Defaults())
	repository, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	setting := database.settings[domain.QuotaReserve]
	setting.Value = json.RawMessage("250")
	minimum, maximum := 0.0, 1000.0
	setting.Min = &minimum
	setting.Max = &maximum
	if err := repository.SetApplicationSetting(t.Context(), setting); err != nil {
		t.Fatalf("SetApplicationSetting() error = %v", err)
	}
	loaded, err := repository.Load(t.Context(), 1)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	stored := loaded.Settings[domain.QuotaReserve]
	if string(stored.Value) != "250" || stored.Min == nil || *stored.Min != 0 || stored.Max == nil || *stored.Max != 1000 || !stored.UserOverridable {
		t.Fatalf("stored quota = %+v", stored)
	}
	if len(database.events) != 1 || database.events[0].topic != domain.ChangedTopic || database.events[0].key != "application:quota_reserve" {
		t.Fatalf("event = %+v", database.events)
	}
	var change domain.Change
	if err := json.Unmarshal(database.events[0].payload, &change); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if change.Scope != domain.ScopeApplication || change.UserID != nil || change.UserOverridable == nil || !*change.UserOverridable || change.Min == nil || *change.Min != 0 || change.Max == nil || *change.Max != 1000 || string(change.Value) != "250" {
		t.Fatalf("change = %+v", change)
	}
}

func TestWriteRollsBackWhenSaveOrPublishFails(t *testing.T) {
	tests := []struct {
		name     string
		failExec int
		commit   bool
		want     string
	}{
		{name: "user save", failExec: 1, want: "save user setting"},
		{name: "user publish", failExec: 2, want: "publish user setting change"},
		{name: "user commit", commit: true, want: "commit failed"},
		{name: "application save", failExec: 1, want: "save application setting"},
		{name: "application publish", failExec: 2, want: "publish application setting change"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newMemoryDB(domain.Defaults())
			database.failExec = test.failExec
			database.execErr = errors.New("write failed")
			if test.commit {
				database.failExec = 0
				database.commitErr = errors.New("commit failed")
			}
			repository, err := New(database)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			var writeErr error
			if strings.HasPrefix(test.name, "application") {
				writeErr = repository.SetApplicationSetting(t.Context(), database.settings[domain.Burst])
			} else {
				writeErr = repository.SetUserOverride(t.Context(), 4, domain.MaxDepth, json.RawMessage("3"))
			}
			if writeErr == nil || !strings.Contains(writeErr.Error(), test.want) {
				t.Fatalf("write error = %v, want %q", writeErr, test.want)
			}
			database.failExec = 0
			database.execErr = nil
			database.commitErr = nil
			loaded, err := repository.Load(t.Context(), 4)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if len(loaded.Overrides) != 0 || string(loaded.Settings[domain.Burst].Value) != "5" || len(database.events) != 0 {
				t.Fatalf("state changed after rollback: overrides=%v burst=%s events=%d", loaded.Overrides, loaded.Settings[domain.Burst].Value, len(database.events))
			}
		})
	}
}

type memoryDB struct {
	settings  map[domain.Key]domain.Definition
	users     map[int64]map[domain.Key]json.RawMessage
	events    []recordedEvent
	beginErr  error
	queryErr  string
	scanErr   string
	rowsErr   string
	failExec  int
	execErr   error
	commitErr error
	begins    int
	options   []pgx.TxOptions
	txs       []*memoryTx
}

type recordedEvent struct {
	topic   string
	key     string
	payload []byte
}

func newMemoryDB(settings []domain.Definition) *memoryDB {
	database := &memoryDB{
		settings: make(map[domain.Key]domain.Definition, len(settings)),
		users:    make(map[int64]map[domain.Key]json.RawMessage),
	}
	for _, setting := range settings {
		stored := setting
		stored.Value = append(json.RawMessage(nil), setting.Value...)
		stored.Min = cloneFloat(setting.Min)
		stored.Max = cloneFloat(setting.Max)
		database.settings[setting.Key] = stored
	}
	return database
}

func (database *memoryDB) BeginTx(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	database.begins++
	database.options = append(database.options, options)
	if database.beginErr != nil {
		return nil, database.beginErr
	}
	tx := &memoryTx{database: database, commitErr: database.commitErr}
	database.txs = append(database.txs, tx)
	return tx, nil
}

type memoryTx struct {
	pgx.Tx
	database   *memoryDB
	statements []string
	userWrites []userWrite
	appWrites  []domain.Definition
	events     []recordedEvent
	execs      int
	commits    int
	rollbacks  int
	commitErr  error
}

type userWrite struct {
	userID int64
	key    domain.Key
	value  json.RawMessage
}

func (tx *memoryTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.statements = append(tx.statements, sql)
	switch {
	case strings.Contains(sql, "FROM app_settings"):
		if tx.database.queryErr == "app" {
			return nil, errors.New("app query failed")
		}
		return tx.applicationRows(), nil
	case strings.Contains(sql, "FROM user_settings"):
		if tx.database.queryErr == "user" {
			return nil, errors.New("user query failed")
		}
		if len(args) != 1 {
			return nil, errors.New("user query requires the user id")
		}
		userID, ok := args[0].(int64)
		if !ok {
			return nil, errors.New("user id has the wrong type")
		}
		return tx.userRows(userID), nil
	default:
		return nil, errors.New("unexpected settings query")
	}
}

func (tx *memoryTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.statements = append(tx.statements, sql)
	tx.execs++
	if tx.database.failExec == tx.execs {
		return pgconn.CommandTag{}, tx.database.execErr
	}
	if err := tx.stage(sql, args); err != nil {
		return pgconn.CommandTag{}, err
	}
	return pgconn.CommandTag{}, nil
}

func (tx *memoryTx) Commit(context.Context) error {
	tx.commits++
	if tx.commitErr != nil {
		return tx.commitErr
	}
	for _, write := range tx.userWrites {
		if tx.database.users[write.userID] == nil {
			tx.database.users[write.userID] = map[domain.Key]json.RawMessage{}
		}
		tx.database.users[write.userID][write.key] = append(json.RawMessage(nil), write.value...)
	}
	for _, write := range tx.appWrites {
		tx.database.settings[write.Key] = write
	}
	tx.database.events = append(tx.database.events, tx.events...)
	return nil
}

func (tx *memoryTx) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

func (tx *memoryTx) stage(sql string, args []any) error {
	switch {
	case strings.Contains(sql, "INSERT INTO user_settings"):
		userID, key, value, err := userArgs(args)
		if err != nil {
			return err
		}
		tx.userWrites = append(tx.userWrites, userWrite{userID: userID, key: domain.Key(key), value: json.RawMessage(value)})
	case strings.Contains(sql, "INSERT INTO app_settings"):
		setting, err := applicationArgs(args)
		if err != nil {
			return err
		}
		tx.appWrites = append(tx.appWrites, setting)
	case strings.Contains(sql, "INSERT INTO outbox_events"):
		event, err := eventArgs(args)
		if err != nil {
			return err
		}
		tx.events = append(tx.events, event)
	default:
		return errors.New("unexpected settings exec")
	}
	return nil
}

func (tx *memoryTx) applicationRows() pgx.Rows {
	scans := make([]func(...any) error, 0, len(tx.database.settings))
	for _, setting := range tx.database.settings {
		stored := setting
		scans = append(scans, func(dest ...any) error {
			if len(dest) != 5 {
				return errors.New("application scan destination count")
			}
			*dest[0].(*string) = string(stored.Key)
			*dest[1].(*[]byte) = stored.Value
			*dest[2].(*bool) = stored.UserOverridable
			*dest[3].(**float64) = cloneFloat(stored.Min)
			*dest[4].(**float64) = cloneFloat(stored.Max)
			return nil
		})
	}
	return &memoryRows{scans: scans, scanErr: errFor("app", tx.database.scanErr), rowsErr: errFor("app", tx.database.rowsErr)}
}

func (tx *memoryTx) userRows(userID int64) pgx.Rows {
	scans := make([]func(...any) error, 0)
	for key, value := range tx.database.users[userID] {
		storedKey, storedValue := key, value
		scans = append(scans, func(dest ...any) error {
			if len(dest) != 2 {
				return errors.New("user scan destination count")
			}
			*dest[0].(*string) = string(storedKey)
			*dest[1].(*[]byte) = storedValue
			return nil
		})
	}
	return &memoryRows{scans: scans, scanErr: errFor("user", tx.database.scanErr), rowsErr: errFor("user", tx.database.rowsErr)}
}

func errFor(target, got string) error {
	if got == target {
		return errors.New(got + " rows failed")
	}
	return nil
}

type memoryRows struct {
	pgx.Rows
	scans   []func(...any) error
	index   int
	scanErr error
	rowsErr error
}

func (rows *memoryRows) Next() bool {
	if rows.index >= len(rows.scans) {
		return false
	}
	rows.index++
	return true
}

func (rows *memoryRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	return rows.scans[rows.index-1](dest...)
}

func (rows *memoryRows) Err() error { return rows.rowsErr }

func (rows *memoryRows) Close() {}

func userArgs(args []any) (int64, string, string, error) {
	if len(args) != 3 {
		return 0, "", "", errors.New("user setting argument count")
	}
	userID, ok := args[0].(int64)
	if !ok {
		return 0, "", "", errors.New("user id type")
	}
	key, ok := args[1].(string)
	if !ok {
		return 0, "", "", errors.New("user key type")
	}
	value, ok := args[2].(string)
	if !ok {
		return 0, "", "", errors.New("user value type")
	}
	return userID, key, value, nil
}

func applicationArgs(args []any) (domain.Definition, error) {
	if len(args) != 5 {
		return domain.Definition{}, errors.New("application setting argument count")
	}
	key, ok := args[0].(string)
	if !ok {
		return domain.Definition{}, errors.New("application key type")
	}
	value, ok := args[1].(string)
	if !ok {
		return domain.Definition{}, errors.New("application value type")
	}
	overridable, ok := args[2].(bool)
	if !ok {
		return domain.Definition{}, errors.New("application overridable type")
	}
	minimum, err := floatArg(args[3])
	if err != nil {
		return domain.Definition{}, err
	}
	maximum, err := floatArg(args[4])
	if err != nil {
		return domain.Definition{}, err
	}
	return domain.Definition{
		Key:             domain.Key(key),
		Value:           json.RawMessage(value),
		UserOverridable: overridable,
		Min:             minimum,
		Max:             maximum,
	}, nil
}

func eventArgs(args []any) (recordedEvent, error) {
	if len(args) != 3 {
		return recordedEvent{}, errors.New("event argument count")
	}
	topic, ok := args[0].(string)
	if !ok {
		return recordedEvent{}, errors.New("event topic type")
	}
	key, ok := args[1].(string)
	if !ok {
		return recordedEvent{}, errors.New("event key type")
	}
	payload, ok := args[2].([]byte)
	if !ok {
		return recordedEvent{}, errors.New("event payload type")
	}
	return recordedEvent{topic: topic, key: key, payload: append([]byte(nil), payload...)}, nil
}

func floatArg(value any) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	pointer, ok := value.(*float64)
	if !ok {
		return nil, errors.New("cap type")
	}
	return cloneFloat(pointer), nil
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
