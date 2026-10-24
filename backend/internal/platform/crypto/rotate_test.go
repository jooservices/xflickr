package crypto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
)

func TestRotateRejectsInvalidArguments(t *testing.T) {
	keyring := testKeyring(t, "active", 1)
	tests := []struct {
		name      string
		database  RotationDB
		keyring   *Keyring
		targets   []Target
		batchSize int
	}{
		{name: "missing database", keyring: keyring, targets: []Target{{Table: "settings", IDColumn: "id", SealedColumn: "secret"}}, batchSize: 1},
		{name: "missing keyring", database: inertDatabase{}, targets: []Target{{Table: "settings", IDColumn: "id", SealedColumn: "secret"}}, batchSize: 1},
		{name: "no targets", database: inertDatabase{}, keyring: keyring, batchSize: 1},
		{name: "batch too small", database: inertDatabase{}, keyring: keyring, targets: []Target{{Table: "settings", IDColumn: "id", SealedColumn: "secret"}}, batchSize: 0},
		{name: "batch too large", database: inertDatabase{}, keyring: keyring, targets: []Target{{Table: "settings", IDColumn: "id", SealedColumn: "secret"}}, batchSize: MaxRotationBatchSize + 1},
		{name: "unsafe table", database: inertDatabase{}, keyring: keyring, targets: []Target{{Table: "settings; drop table users", IDColumn: "id", SealedColumn: "secret"}}, batchSize: 1},
		{name: "same columns", database: inertDatabase{}, keyring: keyring, targets: []Target{{Table: "settings", IDColumn: "secret", SealedColumn: "secret"}}, batchSize: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rotated, err := Rotate(context.Background(), test.database, test.keyring, test.targets, test.batchSize)
			if err == nil || rotated != 0 {
				t.Fatalf("Rotate() = (%d, %v), want an error before work", rotated, err)
			}
		})
	}
}

func TestRotationIdentifierValidation(t *testing.T) {
	for _, table := range []string{"records", "public.records", "_records1"} {
		if !validTableName(table) {
			t.Errorf("validTableName(%q) = false", table)
		}
	}
	for _, table := range []string{"", "public.records.extra", "records;drop", "9records", "public..records"} {
		if validTableName(table) {
			t.Errorf("validTableName(%q) = true, want false", table)
		}
	}
	if got := quoteTable("public.records"); got != `"public"."records"` {
		t.Errorf("quoteTable() = %q", got)
	}
	if got := quoteIdentifier("select"); got != `"select"` {
		t.Errorf("quoteIdentifier() = %q", got)
	}
}

func TestRotateRewrapsOldValuesInBatchesAndIsIdempotent(t *testing.T) {
	oldKey := bytes.Repeat([]byte{3}, 32)
	oldRing, err := NewKeyring(map[string][]byte{"old": oldKey}, "old")
	if err != nil {
		t.Fatal(err)
	}
	activeRing, err := NewKeyring(map[string][]byte{
		"old":    oldKey,
		"active": bytes.Repeat([]byte{9}, 32),
	}, "active")
	if err != nil {
		t.Fatal(err)
	}
	table := "settings"
	database := &fakeRotationDB{rows: map[string]fakeRotationRow{}}
	for _, id := range []string{"a", "b"} {
		sealed, err := oldRing.Seal([]byte("secret-"+id), RowAAD(table, id))
		if err != nil {
			t.Fatal(err)
		}
		database.rows[id] = fakeRotationRow{idText: id, sealed: sealed}
	}
	targets := []Target{{Table: table, IDColumn: "id", SealedColumn: "secret"}}
	rotated, err := Rotate(context.Background(), database, activeRing, targets, 1)
	if err != nil || rotated != 2 {
		t.Fatalf("Rotate() = (%d, %v), want two rows", rotated, err)
	}
	for id, row := range database.rows {
		if row.sealed.KeyID != "active" {
			t.Errorf("row %q key ID = %q, want active", id, row.sealed.KeyID)
		}
		opened, err := activeRing.Open(row.sealed, RowAAD(table, id))
		if err != nil || string(opened) != "secret-"+id {
			t.Errorf("Open(%q) = (%q, %v)", id, opened, err)
		}
	}
	rotated, err = Rotate(context.Background(), database, activeRing, targets, 1)
	if err != nil || rotated != 0 {
		t.Fatalf("second Rotate() = (%d, %v), want no rows", rotated, err)
	}
}

func TestRotateReturnsBatchFailures(t *testing.T) {
	oldRing := testKeyring(t, "old", 3)
	activeRing, err := NewKeyring(map[string][]byte{
		"old":    bytes.Repeat([]byte{3}, 32),
		"active": bytes.Repeat([]byte{9}, 32),
	}, "active")
	if err != nil {
		t.Fatal(err)
	}
	good, err := oldRing.Seal([]byte("secret"), RowAAD("settings", "id-1"))
	if err != nil {
		t.Fatal(err)
	}
	beginErr := errors.New("begin failure")
	queryErr := errors.New("query failure")
	scanErr := errors.New("scan failure")
	rowsErr := errors.New("iteration failure")
	updateErr := errors.New("update failure")
	commitErr := errors.New("commit failure")
	tests := []struct {
		name string
		make func() *fakeRotationDB
		want error
	}{
		{name: "begin", make: func() *fakeRotationDB { return &fakeRotationDB{beginErr: beginErr} }, want: beginErr},
		{name: "query", make: func() *fakeRotationDB { return &fakeRotationDB{queryErr: queryErr} }, want: queryErr},
		{name: "scan", make: func() *fakeRotationDB {
			return &fakeRotationDB{rows: map[string]fakeRotationRow{"id-1": {idText: "id-1", sealed: good}}, scanErr: scanErr}
		}, want: scanErr},
		{name: "iteration", make: func() *fakeRotationDB {
			return &fakeRotationDB{rows: map[string]fakeRotationRow{"id-1": {idText: "id-1", sealed: good}}, rowsErr: rowsErr}
		}, want: rowsErr},
		{name: "rewrap", make: func() *fakeRotationDB {
			return &fakeRotationDB{rows: map[string]fakeRotationRow{"id-1": {idText: "id-1", sealed: good}}}
		}, want: ErrAuthentication},
		{name: "update", make: func() *fakeRotationDB {
			return &fakeRotationDB{rows: map[string]fakeRotationRow{"id-1": {idText: "id-1", sealed: good}}, execErr: updateErr}
		}, want: updateErr},
		{name: "commit", make: func() *fakeRotationDB {
			return &fakeRotationDB{rows: map[string]fakeRotationRow{"id-1": {idText: "id-1", sealed: good}}, commitErr: commitErr}
		}, want: commitErr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := test.make()
			if test.name == "rewrap" {
				goodWrongAAD, err := oldRing.Seal([]byte("secret"), RowAAD("settings", "different"))
				if err != nil {
					t.Fatal(err)
				}
				database.rows = map[string]fakeRotationRow{"id-1": {idText: "id-1", sealed: goodWrongAAD}}
			}
			rotated, err := Rotate(context.Background(), database, activeRing, []Target{{Table: "settings", IDColumn: "id", SealedColumn: "secret"}}, 1)
			if rotated != 0 || !errors.Is(err, test.want) {
				t.Fatalf("Rotate() = (%d, %v), want error %v", rotated, err, test.want)
			}
		})
	}
}

type fakeRotationDB struct {
	rows      map[string]fakeRotationRow
	beginErr  error
	queryErr  error
	scanErr   error
	rowsErr   error
	execErr   error
	commitErr error
}

func (database *fakeRotationDB) Begin(context.Context) (RotationTx, error) {
	if database.beginErr != nil {
		return nil, database.beginErr
	}
	return &fakeRotationTx{database: database, pending: make(map[string]Sealed)}, nil
}

type fakeRotationTx struct {
	database *fakeRotationDB
	pending  map[string]Sealed
}

func (tx *fakeRotationTx) Query(_ context.Context, _ string, args ...any) (RotationRows, error) {
	if tx.database.queryErr != nil {
		return nil, tx.database.queryErr
	}
	activeID, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("active key ID has type %T", args[0])
	}
	batchSize, ok := args[1].(int)
	if !ok {
		return nil, fmt.Errorf("batch size has type %T", args[1])
	}
	ids := make([]string, 0, len(tx.database.rows))
	for id, row := range tx.database.rows {
		if row.sealed.KeyID != activeID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	rows := make([]fakeRotationRow, 0, batchSize)
	for _, id := range ids {
		if len(rows) >= batchSize {
			break
		}
		rows = append(rows, tx.database.rows[id])
	}
	return &fakeRotationRows{rows: rows, scanErr: tx.database.scanErr, rowsErr: tx.database.rowsErr}, nil
}

func (tx *fakeRotationTx) Exec(_ context.Context, _ string, args ...any) error {
	if tx.database.execErr != nil {
		return tx.database.execErr
	}
	encoded, ok := args[0].([]byte)
	if !ok {
		return fmt.Errorf("sealed value has type %T", args[0])
	}
	id, ok := args[1].(string)
	if !ok {
		return fmt.Errorf("row ID has type %T", args[1])
	}
	var sealed Sealed
	if err := sealed.Scan(encoded); err != nil {
		return err
	}
	row := tx.database.rows[id]
	row.sealed = sealed
	tx.pending[id] = row.sealed
	return nil
}

func (tx *fakeRotationTx) Commit(context.Context) error {
	if tx.database.commitErr != nil {
		return tx.database.commitErr
	}
	for id, sealed := range tx.pending {
		row := tx.database.rows[id]
		row.sealed = sealed
		tx.database.rows[id] = row
	}
	return nil
}

func (*fakeRotationTx) Rollback(context.Context) error { return nil }

type fakeRotationRows struct {
	rows    []fakeRotationRow
	index   int
	scanErr error
	rowsErr error
}

func (rows *fakeRotationRows) Next() bool {
	if rows.index >= len(rows.rows) {
		return false
	}
	rows.index++
	return true
}

func (rows *fakeRotationRows) Scan(destinations ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	row := rows.rows[rows.index-1]
	*destinations[0].(*any) = row.idText
	*destinations[1].(*string) = row.idText
	*destinations[2].(*Sealed) = row.sealed.clone()
	return nil
}

func (rows *fakeRotationRows) Err() error { return rows.rowsErr }

func (*fakeRotationRows) Close() {}

type fakeRotationRow struct {
	idText string
	sealed Sealed
}

type inertDatabase struct{}

func (inertDatabase) Begin(context.Context) (RotationTx, error) {
	return nil, nil
}
