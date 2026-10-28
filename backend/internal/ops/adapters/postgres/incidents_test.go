package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jooservices/xflickr/internal/ops/app"
	"github.com/jooservices/xflickr/internal/ops/domain"
	"github.com/jooservices/xflickr/internal/platform/db"
)

func TestNewIncidentRepository(t *testing.T) {
	if _, err := NewIncidentRepository(nil); err == nil {
		t.Fatal("NewIncidentRepository(nil) succeeded")
	}
	if repository, err := NewIncidentRepository(&incidentTestDB{}); err != nil || repository == nil {
		t.Fatalf("NewIncidentRepository() = %v, %v", repository, err)
	}
}

func TestIncidentRepositoryOpenAndNotification(t *testing.T) {
	critical := testIncident(domain.IncidentOpen)
	tx := &incidentTestTx{rows: []pgx.Row{incidentCreatedRow(critical, true)}}
	repository := testRepository(t, tx)
	opened, err := repository.Open(t.Context(), critical)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if opened.ID != critical.ID || opened.Count != 1 || tx.commits != 1 || tx.execCalls != 1 {
		t.Fatalf("Open() = %+v, commits=%d execs=%d", opened, tx.commits, tx.execCalls)
	}
	if !strings.Contains(tx.execQueries[0], "INSERT INTO outbox_events") ||
		tx.execArgs[0][0] != domain.NotificationRequestedTopic || tx.execArgs[0][1] != fmt.Sprint(critical.ID) {
		t.Fatalf("critical outbox call = %q %v", tx.execQueries[0], tx.execArgs[0])
	}

	duplicate := critical
	duplicate.Count = 2
	duplicate.ID = critical.ID
	duplicate.Severity = domain.SeverityCritical
	tx = &incidentTestTx{rows: []pgx.Row{incidentCreatedRow(duplicate, false)}}
	repository = testRepository(t, tx)
	if _, err := repository.Open(t.Context(), duplicate); err != nil {
		t.Fatalf("Open() duplicate error = %v", err)
	}
	if tx.execCalls != 0 {
		t.Fatalf("duplicate open emitted %d events", tx.execCalls)
	}

	warning := critical
	warning.Severity = domain.SeverityWarning
	tx = &incidentTestTx{rows: []pgx.Row{incidentCreatedRow(warning, true)}}
	repository = testRepository(t, tx)
	if _, err := repository.Open(t.Context(), warning); err != nil {
		t.Fatalf("Open() warning error = %v", err)
	}
	if tx.execCalls != 0 {
		t.Fatalf("warning open emitted %d events", tx.execCalls)
	}
}

func TestIncidentRepositoryOpenErrors(t *testing.T) {
	incident := testIncident(domain.IncidentOpen)
	if _, err := (*IncidentRepository)(nil).Open(t.Context(), incident); err == nil {
		t.Fatal("nil repository Open() succeeded")
	}
	repository := testRepository(t, &incidentTestTx{})
	if _, err := repository.Open(nil, incident); err == nil {
		t.Fatal("Open(nil context) succeeded")
	}
	if _, err := repository.Open(t.Context(), domain.Incident{}); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("Open(invalid incident) error = %v", err)
	}

	beginErr := errors.New("begin failed")
	repository = &IncidentRepository{unit: newTestUnit(&incidentTestDB{beginErr: beginErr})}
	if _, err := repository.Open(t.Context(), incident); !errors.Is(err, beginErr) {
		t.Fatalf("Open(begin failure) error = %v", err)
	}

	scanErr := errors.New("scan failed")
	tx := &incidentTestTx{rows: []pgx.Row{incidentErrorRow(scanErr)}}
	if _, err := testRepository(t, tx).Open(t.Context(), incident); !errors.Is(err, scanErr) {
		t.Fatalf("Open(scan failure) error = %v", err)
	}

	tx = &incidentTestTx{rows: []pgx.Row{incidentCreatedRow(incident, true)}, execErrAt: 1, execErr: errors.New("outbox failed")}
	if _, err := testRepository(t, tx).Open(t.Context(), incident); !errors.Is(err, tx.execErr) || tx.rollbacks != 1 {
		t.Fatalf("Open(outbox failure) error = %v, rollbacks=%d", err, tx.rollbacks)
	}

	tx = &incidentTestTx{rows: []pgx.Row{incidentCreatedRow(incident, false)}, commitErr: errors.New("commit failed")}
	if _, err := testRepository(t, tx).Open(t.Context(), incident); !errors.Is(err, tx.commitErr) {
		t.Fatalf("Open(commit failure) error = %v", err)
	}
}

func TestIncidentRepositoryAck(t *testing.T) {
	open := testIncident(domain.IncidentOpen)
	acked := testIncident(domain.IncidentAcked)
	userID := int64(72)
	acked.AckedBy = &userID
	tx := &incidentTestTx{rows: []pgx.Row{incidentRow(open), incidentRow(acked)}}
	updated, err := testRepository(t, tx).Ack(t.Context(), open.ID, userID)
	if err != nil || updated.State != domain.IncidentAcked || tx.commits != 1 {
		t.Fatalf("Ack() = %+v, %v", updated, err)
	}

	repository := testRepository(t, &incidentTestTx{})
	if _, err := repository.Ack(nil, 1, 2); err == nil {
		t.Fatal("Ack(nil context) succeeded")
	}
	if _, err := repository.Ack(t.Context(), 0, 2); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("Ack(invalid ids) error = %v", err)
	}
	if _, err := repository.Ack(t.Context(), 1, 0); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("Ack(invalid user) error = %v", err)
	}
	repository = testRepository(t, &incidentTestTx{rows: []pgx.Row{incidentErrorRow(pgx.ErrNoRows)}})
	if _, err := repository.Ack(t.Context(), 1, 2); !errors.Is(err, app.ErrIncidentNotFound) {
		t.Fatalf("Ack(missing) error = %v", err)
	}

	lockErr := errors.New("lock failed")
	tx = &incidentTestTx{rows: []pgx.Row{incidentErrorRow(lockErr)}}
	if _, err := testRepository(t, tx).Ack(t.Context(), open.ID, userID); !errors.Is(err, lockErr) {
		t.Fatalf("Ack(lock error) = %v", err)
	}

	closed := testIncident(domain.IncidentClosed)
	tx = &incidentTestTx{rows: []pgx.Row{incidentRow(closed)}}
	if _, err := testRepository(t, tx).Ack(t.Context(), closed.ID, userID); !errors.Is(err, domain.ErrInvalidIncidentTransition) {
		t.Fatalf("Ack(closed) error = %v", err)
	}

	tx = &incidentTestTx{rows: []pgx.Row{incidentRow(open), incidentErrorRow(errors.New("update failed"))}}
	if _, err := testRepository(t, tx).Ack(t.Context(), open.ID, userID); err == nil {
		t.Fatal("Ack(update failure) succeeded")
	}
}

func TestIncidentRepositoryClose(t *testing.T) {
	open := testIncident(domain.IncidentOpen)
	closed := testIncident(domain.IncidentClosed)
	at := time.Date(2026, 10, 28, 22, 0, 0, 0, time.UTC)
	closed.ClosedAt = &at
	tx := &incidentTestTx{rows: []pgx.Row{incidentRow(open), incidentRow(closed)}}
	updated, err := testRepository(t, tx).Close(t.Context(), open.ID, at)
	if err != nil || updated.State != domain.IncidentClosed {
		t.Fatalf("Close() = %+v, %v", updated, err)
	}

	repository := testRepository(t, &incidentTestTx{})
	if _, err := repository.Close(nil, 1, at); err == nil {
		t.Fatal("Close(nil context) succeeded")
	}
	if _, err := repository.Close(t.Context(), 0, at); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("Close(invalid id) error = %v", err)
	}
	if _, err := repository.Close(t.Context(), 1, time.Time{}); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("Close(zero time) error = %v", err)
	}
	repository = testRepository(t, &incidentTestTx{rows: []pgx.Row{incidentErrorRow(pgx.ErrNoRows)}})
	if _, err := repository.Close(t.Context(), 1, at); !errors.Is(err, app.ErrIncidentNotFound) {
		t.Fatalf("Close(missing) error = %v", err)
	}

	lockErr := errors.New("lock failed")
	tx = &incidentTestTx{rows: []pgx.Row{incidentErrorRow(lockErr)}}
	if _, err := testRepository(t, tx).Close(t.Context(), open.ID, at); !errors.Is(err, lockErr) {
		t.Fatalf("Close(lock error) = %v", err)
	}
	tx = &incidentTestTx{rows: []pgx.Row{incidentRow(closed)}}
	if _, err := testRepository(t, tx).Close(t.Context(), closed.ID, at); !errors.Is(err, domain.ErrInvalidIncidentTransition) {
		t.Fatalf("Close(already closed) = %v", err)
	}
	tx = &incidentTestTx{rows: []pgx.Row{incidentRow(open), incidentErrorRow(errors.New("update failed"))}}
	if _, err := testRepository(t, tx).Close(t.Context(), open.ID, at); err == nil {
		t.Fatal("Close(update failure) succeeded")
	}
}

func TestIncidentRepositoryAutoClose(t *testing.T) {
	tx := &incidentTestTx{rowsAffected: 3}
	count, err := testRepository(t, tx).AutoClose(t.Context(), "worker.down", "node-1", time.Now())
	if err != nil || count != 3 || tx.commits != 1 {
		t.Fatalf("AutoClose() = %d, %v", count, err)
	}

	repository := testRepository(t, &incidentTestTx{})
	if _, err := repository.AutoClose(nil, "kind", "scope", time.Now()); err == nil {
		t.Fatal("AutoClose(nil context) succeeded")
	}
	if _, err := repository.AutoClose(t.Context(), "", "scope", time.Now()); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("AutoClose(invalid identity) error = %v", err)
	}
	if _, err := repository.AutoClose(t.Context(), "kind", " ", time.Now()); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("AutoClose(trimmed identity) error = %v", err)
	}
	if _, err := repository.AutoClose(t.Context(), "kind", "scope", time.Time{}); !errors.Is(err, domain.ErrInvalidIncident) {
		t.Fatalf("AutoClose(zero time) error = %v", err)
	}
	tx = &incidentTestTx{execErrAt: 1, execErr: errors.New("update failed")}
	if _, err := testRepository(t, tx).AutoClose(t.Context(), "kind", "scope", time.Now()); !errors.Is(err, tx.execErr) {
		t.Fatalf("AutoClose(exec failure) error = %v", err)
	}
}

func TestIncidentRepositoryReady(t *testing.T) {
	var repository *IncidentRepository
	if err := repository.ready(t.Context()); err == nil {
		t.Fatal("nil repository was ready")
	}
	repository = &IncidentRepository{}
	if err := repository.ready(t.Context()); err == nil {
		t.Fatal("nil unit of work was ready")
	}
	repository = testRepository(t, &incidentTestTx{})
	if err := repository.ready(nil); err == nil {
		t.Fatal("nil context was ready")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := repository.ready(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ready(canceled) error = %v", err)
	}
}

func testIncident(state domain.State) domain.Incident {
	return domain.Incident{
		ID: 31, Kind: "crawl.stalled", Scope: "node-1", Severity: domain.SeverityCritical,
		Title: "Crawl stalled", Detail: json.RawMessage(`{"pending":4}`), State: state,
		FirstSeen: time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC),
		LastSeen:  time.Date(2026, 10, 28, 20, 1, 0, 0, time.UTC), Count: 1,
	}
}

func testRepository(t *testing.T, tx *incidentTestTx) *IncidentRepository {
	t.Helper()
	repository, err := NewIncidentRepository(&incidentTestDB{tx: tx})
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func newTestUnit(database *incidentTestDB) *db.UnitOfWork {
	return db.NewUnitOfWork(database)
}

type incidentTestDB struct {
	tx       *incidentTestTx
	beginErr error
}

func (database *incidentTestDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	if database.beginErr != nil {
		return nil, database.beginErr
	}
	return database.tx, nil
}

type incidentTestTx struct {
	pgx.Tx
	rows         []pgx.Row
	execCalls    int
	execErrAt    int
	execErr      error
	execQueries  []string
	execArgs     [][]any
	rowsAffected int64
	commitErr    error
	commits      int
	rollbacks    int
}

func (tx *incidentTestTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if len(tx.rows) == 0 {
		return incidentErrorRow(errors.New("no stub row"))
	}
	row := tx.rows[0]
	tx.rows = tx.rows[1:]
	return row
}

func (tx *incidentTestTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	tx.execCalls++
	tx.execQueries = append(tx.execQueries, query)
	tx.execArgs = append(tx.execArgs, append([]any(nil), args...))
	if tx.execCalls == tx.execErrAt {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", tx.rowsAffected)), nil
}

func (tx *incidentTestTx) Commit(context.Context) error {
	tx.commits++
	return tx.commitErr
}

func (tx *incidentTestTx) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

type incidentTestRow struct {
	values []any
	err    error
}

func incidentRow(incident domain.Incident) pgx.Row {
	values := []any{
		incident.ID, incident.Kind, incident.Scope, string(incident.Severity), incident.Title,
		[]byte(incident.Detail), incident.State, incident.FirstSeen, incident.LastSeen,
		incident.Count, incident.AckedBy, incident.ClosedAt,
	}
	return incidentTestRow{values: values}
}

func incidentCreatedRow(incident domain.Incident, created bool) pgx.Row {
	row := incidentRow(incident).(incidentTestRow)
	row.values = append(row.values, created)
	return row
}

func incidentErrorRow(err error) pgx.Row { return incidentTestRow{err: err} }

func (row incidentTestRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != len(row.values) {
		return fmt.Errorf("scan received %d destinations, want %d", len(destinations), len(row.values))
	}
	for index, destination := range destinations {
		target := reflect.ValueOf(destination)
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return errors.New("scan destination must be a non-nil pointer")
		}
		target = target.Elem()
		value := row.values[index]
		if value == nil {
			target.SetZero()
			continue
		}
		source := reflect.ValueOf(value)
		switch {
		case source.Type().AssignableTo(target.Type()):
			target.Set(source)
		case source.Type().ConvertibleTo(target.Type()):
			target.Set(source.Convert(target.Type()))
		default:
			return fmt.Errorf("cannot scan %T into %s", value, target.Type())
		}
	}
	return nil
}
