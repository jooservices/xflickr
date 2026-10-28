package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	mediadomain "github.com/jooservices/xflickr/internal/media/domain"
	"github.com/jooservices/xflickr/internal/platform/queue"
	platformsse "github.com/jooservices/xflickr/internal/platform/sse"
)

type mediaPoolStub struct {
	begin func(context.Context) (pgx.Tx, error)
	query func(context.Context, string, ...any) (pgx.Rows, error)
	row   func(context.Context, string, ...any) pgx.Row
}

func (p mediaPoolStub) Begin(ctx context.Context) (pgx.Tx, error) {
	if p.begin == nil {
		return nil, errors.New("unexpected begin")
	}
	return p.begin(ctx)
}

func (p mediaPoolStub) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if p.query == nil {
		return nil, errors.New("unexpected query")
	}
	return p.query(ctx, sql, args...)
}

func (p mediaPoolStub) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if p.row == nil {
		return mediaRow{err: errors.New("unexpected query row")}
	}
	return p.row(ctx, sql, args...)
}

type mediaRow struct {
	values []any
	err    error
}

func (r mediaRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan got %d destinations for %d values", len(dest), len(r.values))
	}
	for i, value := range r.values {
		ptr := reflect.ValueOf(dest[i])
		if ptr.Kind() != reflect.Pointer || ptr.IsNil() {
			return fmt.Errorf("destination %d is not a writable pointer", i)
		}
		v := reflect.ValueOf(value)
		if !v.IsValid() {
			ptr.Elem().SetZero()
			continue
		}
		if v.Type().AssignableTo(ptr.Elem().Type()) {
			ptr.Elem().Set(v)
		} else if v.Type().ConvertibleTo(ptr.Elem().Type()) {
			ptr.Elem().Set(v.Convert(ptr.Elem().Type()))
		} else {
			return fmt.Errorf("cannot scan %T into %T", value, dest[i])
		}
	}
	return nil
}

type mediaRowsStub struct {
	pgx.Rows
	values  [][]any
	index   int
	err     error
	scanErr error
}

func (r *mediaRowsStub) Next() bool {
	r.index++
	return r.index < len(r.values)
}

func (r *mediaRowsStub) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return (mediaRow{values: r.values[r.index]}).Scan(dest...)
}

func (*mediaRowsStub) Close()       {}
func (r *mediaRowsStub) Err() error { return r.err }

type mediaTxStub struct {
	pgx.Tx
	row         func(context.Context, string, ...any) pgx.Row
	exec        func(context.Context, string, ...any) (pgconn.CommandTag, error)
	copyFrom    func(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error)
	commitErr   error
	rollbackErr error
	commits     int
	rollbacks   int
}

func (tx *mediaTxStub) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.row == nil {
		return mediaRow{err: errors.New("unexpected transaction query row")}
	}
	return tx.row(ctx, sql, args...)
}

func (tx *mediaTxStub) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.exec == nil {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return tx.exec(ctx, sql, args...)
}

func (tx *mediaTxStub) CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, source pgx.CopyFromSource) (int64, error) {
	if tx.copyFrom == nil {
		return 0, nil
	}
	return tx.copyFrom(ctx, table, columns, source)
}

func (tx *mediaTxStub) Commit(context.Context) error {
	tx.commits++
	return tx.commitErr
}

func (tx *mediaTxStub) Rollback(context.Context) error {
	tx.rollbacks++
	return tx.rollbackErr
}

type mediaQueueStub struct {
	jobs    []queue.Job
	options []queue.Options
	err     error
}

func (q *mediaQueueStub) Enqueue(_ context.Context, _ pgx.Tx, job queue.Job, options queue.Options) error {
	q.jobs = append(q.jobs, job)
	q.options = append(q.options, options)
	return q.err
}

func (*mediaQueueStub) Register(string, queue.Handler) error { return nil }

type mediaEventsStub struct {
	events []platformsse.Event
	err    error
}

func (e *mediaEventsStub) Publish(_ context.Context, event platformsse.Event) error {
	e.events = append(e.events, event)
	return e.err
}

func mediaTestRows(values ...[]any) *mediaRowsStub {
	return &mediaRowsStub{index: -1, values: values}
}

func TestPackItemsLoadsSettledRowsAndRejectsBrokenRows(t *testing.T) {
	ctx := context.Background()
	t.Run("success with failed item fallback", func(t *testing.T) {
		pool := mediaPoolStub{
			row: func(context.Context, string, ...any) pgx.Row { return mediaRow{values: []any{48}} },
			query: func(context.Context, string, ...any) (pgx.Rows, error) {
				return mediaTestRows(
					[]any{int64(7), "photo-7-original", "blob-7", int64(120), "abcd", "", "ready"},
					[]any{int64(8), "photo-8-original", "", int64(0), "", "", "failed"},
				), nil
			},
		}
		items, ttl, err := (Downloads{Pool: pool}).PackItems(ctx, 4)
		if err != nil || ttl != 48*time.Hour || len(items) != 2 || items[0].BlobKey != "blob-7" || items[1].Failure != "download failed" {
			t.Fatalf("PackItems = %#v, %v, %v", items, ttl, err)
		}
	})
	for _, tc := range []struct {
		name     string
		rowErr   error
		queryErr error
		rows     *mediaRowsStub
		want     string
	}{
		{name: "TTL error", rowErr: errors.New("ttl"), want: "load download ZIP TTL"},
		{name: "item query error", queryErr: errors.New("items"), want: "load download items for pack"},
		{name: "scan error", rows: &mediaRowsStub{index: -1, values: [][]any{{int64(1)}}, scanErr: errors.New("scan")}, want: "scan download pack item"},
		{name: "unsettled", rows: mediaTestRows([]any{int64(9), "name", "blob", int64(1), "", "", "fetching"}), want: "not settled"},
		{name: "ready without blob", rows: mediaTestRows([]any{int64(9), "name", "", int64(1), "", "", "ready"}), want: "has no asset"},
		{name: "rows error", rows: &mediaRowsStub{index: -1, err: errors.New("iterate")}, want: "read download pack items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := mediaPoolStub{
				row: func(context.Context, string, ...any) pgx.Row {
					if tc.rowErr != nil {
						return mediaRow{err: tc.rowErr}
					}
					return mediaRow{values: []any{24}}
				},
				query: func(context.Context, string, ...any) (pgx.Rows, error) { return tc.rows, tc.queryErr },
			}
			_, _, err := (Downloads{Pool: pool}).PackItems(ctx, 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PackItems error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPackedOriginalsCompletionAndFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("incomplete skips blob query", func(t *testing.T) {
		queryCalls := 0
		pool := mediaPoolStub{
			row:   func(context.Context, string, ...any) pgx.Row { return mediaRow{values: []any{false}} },
			query: func(context.Context, string, ...any) (pgx.Rows, error) { queryCalls++; return mediaTestRows(), nil },
		}
		keys, complete, err := (Downloads{Pool: pool}).PackedOriginals(ctx, 1)
		if err != nil || complete || len(keys) != 0 || queryCalls != 0 {
			t.Fatalf("got %v, %v, %v, queries=%d", keys, complete, err, queryCalls)
		}
	})
	t.Run("complete returns originals", func(t *testing.T) {
		pool := mediaPoolStub{
			row: func(context.Context, string, ...any) pgx.Row { return mediaRow{values: []any{true}} },
			query: func(context.Context, string, ...any) (pgx.Rows, error) {
				return mediaTestRows([]any{"one"}, []any{"two"}), nil
			},
		}
		keys, complete, err := (Downloads{Pool: pool}).PackedOriginals(ctx, 1)
		if err != nil || !complete || !reflect.DeepEqual(keys, []string{"one", "two"}) {
			t.Fatalf("got %v, %v, %v", keys, complete, err)
		}
	})
	for _, tc := range []struct {
		name             string
		rowErr, queryErr error
		rows             *mediaRowsStub
		want             string
	}{
		{name: "complete check", rowErr: errors.New("check"), want: "check existing ZIP parts"},
		{name: "original query", queryErr: errors.New("query"), want: "load originals for ZIP cleanup"},
		{name: "scan", rows: &mediaRowsStub{index: -1, values: [][]any{{true}}}, want: "scan original for ZIP cleanup"},
		{name: "iteration", rows: &mediaRowsStub{index: -1, err: errors.New("iterate")}, want: "read originals for ZIP cleanup"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := mediaPoolStub{
				row: func(context.Context, string, ...any) pgx.Row {
					if tc.rowErr != nil {
						return mediaRow{err: tc.rowErr}
					}
					return mediaRow{values: []any{true}}
				},
				query: func(context.Context, string, ...any) (pgx.Rows, error) { return tc.rows, tc.queryErr },
			}
			_, _, err := (Downloads{Pool: pool}).PackedOriginals(ctx, 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want=%q", err, tc.want)
			}
		})
	}
}

func TestSettleItemTransactionAndProgress(t *testing.T) {
	ctx := context.Background()
	assetID := int64(90)
	for _, tc := range []struct {
		name                                                                     string
		rowsAffected                                                             int64
		unsettled                                                                int
		beginErr, lockErr, updateErr, countErr, commitErr, eventErr, progressErr error
		queueErr                                                                 error
		wantErr                                                                  string
		wantJobs, wantEvents                                                     int
		wantTx                                                                   bool
	}{
		{name: "last item enqueues and publishes", rowsAffected: 1, unsettled: 0, wantJobs: 1, wantEvents: 1, wantTx: true},
		{name: "remaining items do not enqueue", rowsAffected: 1, unsettled: 2, wantEvents: 1, wantTx: true},
		{name: "repeat settlement commits without progress", rowsAffected: 0, wantTx: true},
		{name: "begin failure", beginErr: errors.New("begin"), wantErr: "begin item settlement"},
		{name: "lock failure", lockErr: errors.New("lock"), wantErr: "lock download for item settlement", wantTx: true},
		{name: "update failure", rowsAffected: 1, updateErr: errors.New("update"), wantErr: "settle download item", wantTx: true},
		{name: "count failure", rowsAffected: 1, countErr: errors.New("count"), wantErr: "check unsettled download items", wantTx: true},
		{name: "enqueue failure", rowsAffected: 1, unsettled: 0, queueErr: errors.New("queue"), wantErr: "enqueue pack job", wantJobs: 1, wantTx: true},
		{name: "commit failure", rowsAffected: 1, unsettled: 2, commitErr: errors.New("commit"), wantErr: "commit item settlement", wantTx: true},
		{name: "progress read failure is best effort", rowsAffected: 1, unsettled: 2, progressErr: errors.New("progress"), wantEvents: 0, wantTx: true},
		{name: "event publish failure is best effort", rowsAffected: 1, unsettled: 2, eventErr: errors.New("publish"), wantEvents: 1, wantTx: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobs, events := &mediaQueueStub{err: tc.queueErr}, &mediaEventsStub{err: tc.eventErr}
			tx := &mediaTxStub{
				row: func(_ context.Context, sql string, _ ...any) pgx.Row {
					if strings.Contains(sql, "SELECT id FROM downloads") {
						if tc.lockErr != nil {
							return mediaRow{err: tc.lockErr}
						}
						return mediaRow{values: []any{int64(1)}}
					}
					if strings.Contains(sql, "SELECT count(*)") {
						if tc.countErr != nil {
							return mediaRow{err: tc.countErr}
						}
						return mediaRow{values: []any{tc.unsettled}}
					}
					return mediaRow{err: fmt.Errorf("unexpected SQL: %s", sql)}
				},
				exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
					if tc.updateErr != nil {
						return pgconn.CommandTag{}, tc.updateErr
					}
					return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", tc.rowsAffected)), nil
				},
				commitErr: tc.commitErr,
			}
			pool := mediaPoolStub{
				begin: func(context.Context) (pgx.Tx, error) {
					if tc.beginErr != nil {
						return nil, tc.beginErr
					}
					return tx, nil
				},
				row: func(context.Context, string, ...any) pgx.Row {
					if tc.progressErr != nil {
						return mediaRow{err: tc.progressErr}
					}
					return mediaRow{values: []any{int64(11), int64(3), int64(3)}}
				},
			}
			err := (Downloads{Pool: pool, Jobs: jobs, Events: events}).SettleItem(ctx, 1, 2, "ready", &assetID, "")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error=%v want %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(jobs.jobs) != tc.wantJobs || len(events.events) != tc.wantEvents {
				t.Fatalf("jobs/events = %d/%d, want %d/%d", len(jobs.jobs), len(events.events), tc.wantJobs, tc.wantEvents)
			}
			if tc.wantTx && tx.rollbacks == 0 {
				t.Fatal("transaction cleanup did not roll back")
			}
		})
	}
	t.Run("invalid settlement arguments", func(t *testing.T) {
		for _, repo := range []Downloads{{Pool: mediaPoolStub{}, Jobs: nil}, {Pool: nil, Jobs: &mediaQueueStub{}}, {Pool: mediaPoolStub{}, Jobs: &mediaQueueStub{}}} {
			if err := repo.SettleItem(ctx, 1, 1, "failed", nil, ""); err == nil {
				t.Fatal("accepted invalid settlement")
			}
		}
		if err := (Downloads{Pool: mediaPoolStub{}, Jobs: &mediaQueueStub{}}).SettleItem(nil, 1, 1, "failed", nil, "failure"); err == nil {
			t.Fatal("accepted nil context")
		}
		if err := (Downloads{Pool: mediaPoolStub{}, Jobs: &mediaQueueStub{}}).SettleItem(ctx, 1, 1, "ready", nil, ""); err == nil {
			t.Fatal("accepted missing asset")
		}
	})
}

func TestCompletePackTransactionAndEvent(t *testing.T) {
	ctx := context.Background()
	part := mediaapp.ZIPPart{Number: 1, BlobKey: "zip/part-1", Bytes: 4, ExpiresAt: time.Now().UTC()}
	for _, tc := range []struct {
		name                                              string
		beginErr, execErr, updateErr, commitErr, eventErr error
		noRows                                            bool
		want                                              string
		wantEvents                                        int
	}{
		{name: "success publishes", wantEvents: 1},
		{name: "begin error", beginErr: errors.New("begin"), want: "begin ZIP completion"},
		{name: "part insert error", execErr: errors.New("insert"), want: "save ZIP part"},
		{name: "missing download", noRows: true, want: "download not found"},
		{name: "update error", updateErr: errors.New("update"), want: "update packed download"},
		{name: "commit error", commitErr: errors.New("commit"), want: "commit ZIP completion"},
		{name: "event is best effort", eventErr: errors.New("publish"), wantEvents: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &mediaTxStub{
				exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
					if tc.execErr != nil {
						return pgconn.CommandTag{}, tc.execErr
					}
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				},
				row: func(context.Context, string, ...any) pgx.Row {
					if tc.noRows {
						return mediaRow{err: pgx.ErrNoRows}
					}
					if tc.updateErr != nil {
						return mediaRow{err: tc.updateErr}
					}
					return mediaRow{values: []any{int64(44)}}
				},
				commitErr: tc.commitErr,
			}
			pool := mediaPoolStub{begin: func(context.Context) (pgx.Tx, error) {
				if tc.beginErr != nil {
					return nil, tc.beginErr
				}
				return tx, nil
			}}
			events := &mediaEventsStub{err: tc.eventErr}
			err := (Downloads{Pool: pool, Events: events}).CompletePack(ctx, 7, "ready", []mediaapp.ZIPPart{part})
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error=%v want %q", err, tc.want)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(events.events) != tc.wantEvents {
				t.Fatalf("events=%d want=%d", len(events.events), tc.wantEvents)
			}
		})
	}
	t.Run("metadata and argument validation", func(t *testing.T) {
		valid := Downloads{Pool: mediaPoolStub{}}
		cases := []struct {
			repo   Downloads
			ctx    context.Context
			id     int64
			status string
			parts  []mediaapp.ZIPPart
		}{
			{repo: Downloads{}, ctx: context.Background(), id: 1, status: "ready", parts: []mediaapp.ZIPPart{part}},
			{repo: valid, ctx: nil, id: 1, status: "ready", parts: []mediaapp.ZIPPart{part}},
			{repo: valid, ctx: context.Background(), id: 0, status: "ready", parts: []mediaapp.ZIPPart{part}},
			{repo: valid, ctx: context.Background(), id: 1, status: "pending", parts: []mediaapp.ZIPPart{part}},
			{repo: valid, ctx: context.Background(), id: 1, status: "ready"},
			{repo: valid, ctx: context.Background(), id: 1, status: "ready", parts: []mediaapp.ZIPPart{{Number: 0, BlobKey: "key", ExpiresAt: part.ExpiresAt}}},
			{repo: valid, ctx: context.Background(), id: 1, status: "ready", parts: []mediaapp.ZIPPart{{Number: 1, ExpiresAt: part.ExpiresAt}}},
			{repo: valid, ctx: context.Background(), id: 1, status: "ready", parts: []mediaapp.ZIPPart{{Number: 1, BlobKey: "key", Bytes: -1, ExpiresAt: part.ExpiresAt}}},
			{repo: valid, ctx: context.Background(), id: 1, status: "ready", parts: []mediaapp.ZIPPart{{Number: 1, BlobKey: "key"}}},
		}
		for _, tc := range cases {
			if err := tc.repo.CompletePack(tc.ctx, tc.id, tc.status, tc.parts); err == nil {
				t.Fatalf("accepted invalid completion %#v", tc)
			}
		}
	})
}

func TestCreateDownloadTransactionAndQueueSelection(t *testing.T) {
	ctx := context.Background()
	createdAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	base := mediadomain.Download{UserID: 7, Kind: mediadomain.DownloadTray, Subject: "tray", CreatedAt: createdAt, ExpiresAt: createdAt.Add(time.Hour), Items: []mediadomain.DownloadItem{{PhotoID: 10, State: mediadomain.ItemPending}, {PhotoID: 11, State: mediadomain.ItemPending}}}
	candidates := []mediaapp.DownloadItemCandidate{{PhotoID: 10, OwnerNSID: "owner", SizesConfirmed: false}, {PhotoID: 11, OwnerNSID: "owner", SizesConfirmed: true}}
	for _, tc := range []struct {
		name                                                                string
		beginErr, insertErr, keyErr, copyErr, queueErr, commitErr, eventErr error
		noKey                                                               bool
		want                                                                string
		wantJobs, wantEvents                                                int
	}{
		{name: "success routes jobs and publishes", wantJobs: 2, wantEvents: 1},
		{name: "begin failure", beginErr: errors.New("begin"), want: "begin download"},
		{name: "insert failure", insertErr: errors.New("insert"), want: "insert download"},
		{name: "missing Flickr key", noKey: true, want: "no valid Flickr key"},
		{name: "key lookup error", keyErr: errors.New("key"), want: "find user Flickr key"},
		{name: "copy error", copyErr: errors.New("copy"), want: "insert download items"},
		{name: "queue error", queueErr: errors.New("queue"), want: "enqueue photo.sizes", wantJobs: 1},
		{name: "commit error", commitErr: errors.New("commit"), want: "commit download", wantJobs: 2},
		{name: "event is best effort", eventErr: errors.New("publish"), wantJobs: 2, wantEvents: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &mediaTxStub{
				row: func(_ context.Context, sql string, _ ...any) pgx.Row {
					switch {
					case strings.Contains(sql, "INSERT INTO downloads"):
						if tc.insertErr != nil {
							return mediaRow{err: tc.insertErr}
						}
						return mediaRow{values: []any{int64(101)}}
					case strings.Contains(sql, "SELECT fa.key_id"):
						if tc.noKey {
							return mediaRow{err: pgx.ErrNoRows}
						}
						if tc.keyErr != nil {
							return mediaRow{err: tc.keyErr}
						}
						return mediaRow{values: []any{int64(22)}}
					default:
						return mediaRow{err: fmt.Errorf("unexpected SQL: %s", sql)}
					}
				},
				copyFrom: func(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
					if tc.copyErr != nil {
						return 0, tc.copyErr
					}
					return 2, nil
				},
				commitErr: tc.commitErr,
			}
			pool := mediaPoolStub{begin: func(context.Context) (pgx.Tx, error) {
				if tc.beginErr != nil {
					return nil, tc.beginErr
				}
				return tx, nil
			}}
			jobs, events := &mediaQueueStub{err: tc.queueErr}, &mediaEventsStub{err: tc.eventErr}
			got, err := (Downloads{Pool: pool, Jobs: jobs, Events: events}).Create(ctx, base, candidates)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error=%v want %q", err, tc.want)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" && got.ID != 101 {
				t.Fatalf("created ID=%d", got.ID)
			}
			if len(jobs.jobs) != tc.wantJobs || len(events.events) != tc.wantEvents {
				t.Fatalf("jobs/events=%d/%d want=%d/%d", len(jobs.jobs), len(events.events), tc.wantJobs, tc.wantEvents)
			}
			if tc.name == "success routes jobs and publishes" {
				if jobs.jobs[0].Kind != "photo.sizes" || jobs.jobs[1].Kind != "media.fetch" || jobs.options[0].Queue != "sync" || jobs.options[1].Queue != "media" {
					t.Fatalf("jobs/options=%#v %#v", jobs.jobs, jobs.options)
				}
			}
		})
	}
	t.Run("confirmed-size download does not look up a Flickr key", func(t *testing.T) {
		item := base
		item.Items = item.Items[:1]
		item.Items[0].PhotoID = 11
		tx := &mediaTxStub{row: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "INSERT INTO downloads") {
				return mediaRow{values: []any{int64(8)}}
			}
			return mediaRow{err: fmt.Errorf("unexpected SQL: %s", sql)}
		}}
		pool := mediaPoolStub{begin: func(context.Context) (pgx.Tx, error) { return tx, nil }}
		jobs := &mediaQueueStub{}
		if _, err := (Downloads{Pool: pool, Jobs: jobs}).Create(ctx, item, candidates[1:]); err != nil {
			t.Fatal(err)
		}
		if len(jobs.jobs) != 1 || jobs.jobs[0].Kind != "media.fetch" {
			t.Fatalf("jobs=%#v", jobs.jobs)
		}
	})
	t.Run("repository validation and empty-item helpers", func(t *testing.T) {
		if _, err := (Downloads{Pool: mediaPoolStub{}}).Create(ctx, base, candidates); err == nil {
			t.Fatal("accepted missing job queue")
		}
		if _, err := (Downloads{Pool: mediaPoolStub{}, Jobs: &mediaQueueStub{}}).Create(nil, base, candidates); err == nil {
			t.Fatal("accepted nil context")
		}
		tx := &mediaTxStub{}
		if err := insertDownloadItems(ctx, tx, mediadomain.Download{}); err != nil {
			t.Fatal(err)
		}
		if _, err := userKeyForPendingSizes(ctx, tx, 1, nil, nil); err != nil {
			t.Fatal(err)
		}
		if err := enqueueDownloadJobs(ctx, tx, &mediaQueueStub{}, mediadomain.Download{}, nil, 0); err != nil {
			t.Fatal(err)
		}
		mapped := candidateMap([]mediaapp.DownloadItemCandidate{{PhotoID: 1}, {PhotoID: 1, OwnerNSID: "last"}})
		if mapped[1].OwnerNSID != "last" {
			t.Fatalf("candidateMap=%#v", mapped)
		}
	})
}
