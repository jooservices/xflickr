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

	mediaapp "github.com/jooservices/xflickr/internal/media/app"
	"github.com/jooservices/xflickr/internal/platform/queue"
)

func TestPackedPartsValidationAndRead(t *testing.T) {
	ctx := context.Background()
	expires := time.Date(2026, 10, 28, 13, 0, 0, 0, time.UTC)
	pool := mediaPoolStub{query: func(context.Context, string, ...any) (pgx.Rows, error) {
		return mediaTestRows([]any{1, "zip/one", int64(10), expires}), nil
	}}
	parts, err := (Downloads{Pool: pool}).PackedParts(ctx, 4)
	if err != nil || len(parts) != 1 || parts[0] != (mediaapp.ZIPPart{Number: 1, BlobKey: "zip/one", Bytes: 10, ExpiresAt: expires}) {
		t.Fatalf("PackedParts = %+v, %v", parts, err)
	}
	for _, tc := range []struct {
		name string
		repo Downloads
		ctx  context.Context
		id   int64
	}{
		{name: "pool", repo: Downloads{}, ctx: ctx, id: 1},
		{name: "context", repo: Downloads{Pool: pool}, id: 1},
		{name: "id", repo: Downloads{Pool: pool}, ctx: ctx},
	} {
		if _, err := tc.repo.PackedParts(tc.ctx, tc.id); err == nil {
			t.Errorf("%s: PackedParts accepted invalid input", tc.name)
		}
	}
	for _, tc := range []struct {
		name string
		rows *mediaRowsStub
		err  error
		want string
	}{
		{name: "query", err: errors.New("query"), want: "load recorded ZIP parts"},
		{name: "scan", rows: &mediaRowsStub{index: -1, values: [][]any{{1}}, scanErr: errors.New("scan")}, want: "scan recorded ZIP part"},
		{name: "read", rows: &mediaRowsStub{index: -1, err: errors.New("read")}, want: "read recorded ZIP parts"},
	} {
		testPool := mediaPoolStub{query: func(context.Context, string, ...any) (pgx.Rows, error) {
			if tc.err != nil {
				return nil, tc.err
			}
			return tc.rows, nil
		}}
		if _, err := (Downloads{Pool: testPool}).PackedParts(ctx, 1); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error=%v want=%q", tc.name, err, tc.want)
		}
	}
}

func TestPartsForDownloadOwnerFilterAndErrors(t *testing.T) {
	ctx := context.Background()
	expires := time.Now().UTC()
	valid := mediaPoolStub{query: func(context.Context, string, ...any) (pgx.Rows, error) {
		return mediaTestRows([]any{int64(8), 2, "key", int64(20), expires}), nil
	}}
	parts, err := (Downloads{Pool: valid}).PartsForDownload(ctx, 7, 8)
	if err != nil || len(parts) != 1 || parts[0].DownloadID != 8 || parts[0].Number != 2 || parts[0].BlobKey != "key" {
		t.Fatalf("PartsForDownload = %+v, %v", parts, err)
	}
	for _, tc := range []struct {
		name string
		repo Downloads
		ctx  context.Context
		user int64
		id   int64
	}{
		{name: "pool", repo: Downloads{}, ctx: ctx, user: 7, id: 8},
		{name: "context", repo: Downloads{Pool: valid}, user: 7, id: 8},
		{name: "user", repo: Downloads{Pool: valid}, ctx: ctx, id: 8},
		{name: "download", repo: Downloads{Pool: valid}, ctx: ctx, user: 7},
	} {
		if _, err := tc.repo.PartsForDownload(tc.ctx, tc.user, tc.id); err == nil {
			t.Errorf("%s: PartsForDownload accepted invalid input", tc.name)
		}
	}
	for _, tc := range []struct {
		name string
		rows *mediaRowsStub
		err  error
		want error
		text string
	}{
		{name: "query", err: errors.New("query"), text: "load download parts"},
		{name: "scan", rows: &mediaRowsStub{index: -1, values: [][]any{{int64(8)}}, scanErr: errors.New("scan")}, text: "scan download part"},
		{name: "read", rows: &mediaRowsStub{index: -1, err: errors.New("read")}, text: "read download parts"},
		{name: "empty", rows: mediaTestRows(), want: mediaapp.ErrDownloadNotFound},
	} {
		pool := mediaPoolStub{query: func(context.Context, string, ...any) (pgx.Rows, error) {
			if tc.err != nil {
				return nil, tc.err
			}
			return tc.rows, nil
		}}
		_, err := (Downloads{Pool: pool}).PartsForDownload(ctx, 7, 8)
		if tc.want != nil {
			if !errors.Is(err, tc.want) {
				t.Errorf("%s: error=%v want=%v", tc.name, err, tc.want)
			}
		} else if err == nil || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s: error=%v want=%q", tc.name, err, tc.text)
		}
	}
}

func TestMarkPartFetchedValidationAndErrors(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		repo Downloads
		ctx  context.Context
		user int64
		id   int64
		part int
	}{
		{repo: Downloads{}, ctx: ctx, user: 1, id: 2, part: 1},
		{repo: Downloads{Pool: mediaPoolStub{}}, user: 1, id: 2, part: 1},
		{repo: Downloads{Pool: mediaPoolStub{}}, ctx: ctx, id: 2, part: 1},
		{repo: Downloads{Pool: mediaPoolStub{}}, ctx: ctx, user: 1, part: 1},
		{repo: Downloads{Pool: mediaPoolStub{}}, ctx: ctx, user: 1, id: 2},
	} {
		if err := tc.repo.MarkPartFetched(tc.ctx, tc.user, tc.id, tc.part); err == nil {
			t.Error("MarkPartFetched accepted invalid input")
		}
	}
	for _, tc := range []struct {
		name string
		row  error
		want error
	}{
		{name: "not found", row: pgx.ErrNoRows, want: mediaapp.ErrDownloadNotFound},
		{name: "database", row: errors.New("database"), want: errors.New("mark download part fetched")},
	} {
		pool := mediaPoolStub{row: func(context.Context, string, ...any) pgx.Row { return mediaRow{err: tc.row} }}
		err := (Downloads{Pool: pool}).MarkPartFetched(ctx, 1, 2, 3)
		if tc.name == "not found" {
			if !errors.Is(err, tc.want) {
				t.Errorf("%s error=%v", tc.name, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "mark download part fetched") {
			t.Errorf("%s error=%v", tc.name, err)
		}
	}
	pool := mediaPoolStub{row: func(context.Context, string, ...any) pgx.Row { return mediaRow{values: []any{int64(5)}} }}
	if err := (Downloads{Pool: pool}).MarkPartFetched(ctx, 1, 2, 3); err != nil {
		t.Fatalf("MarkPartFetched success = %v", err)
	}
}

func retryTransaction(status string, expired bool, items *mediaRowsStub, beginErr, selectErr, itemQueryErr, statusExecErr, commitErr error, jobs queue.JobQueue) Downloads {
	tx := &mediaTxStub{
		row: func(context.Context, string, ...any) pgx.Row {
			if selectErr != nil {
				return mediaRow{err: selectErr}
			}
			return mediaRow{values: []any{status, expired}}
		},
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			if itemQueryErr != nil {
				return nil, itemQueryErr
			}
			return items, nil
		},
		exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			if statusExecErr != nil {
				return pgconn.CommandTag{}, statusExecErr
			}
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
		commitErr: commitErr,
	}
	return Downloads{
		Pool: mediaPoolStub{begin: func(context.Context) (pgx.Tx, error) {
			if beginErr != nil {
				return nil, beginErr
			}
			return tx, nil
		}},
		Jobs: jobs,
	}
}

func TestRetryFailedDownloadAtomicQueueAndValidation(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		repo Downloads
		ctx  context.Context
		user int64
		id   int64
	}{
		{repo: Downloads{}, ctx: ctx, user: 1, id: 2},
		{repo: Downloads{Pool: mediaPoolStub{}, Jobs: &mediaQueueStub{}}, user: 1, id: 2},
		{repo: Downloads{Pool: mediaPoolStub{}, Jobs: &mediaQueueStub{}}, ctx: ctx, id: 2},
		{repo: Downloads{Pool: mediaPoolStub{}, Jobs: &mediaQueueStub{}}, ctx: ctx, user: 1},
		{repo: Downloads{Pool: mediaPoolStub{}, Jobs: &mediaQueueStub{}}, ctx: ctx, user: 1, id: 2},
	} {
		if _, err := tc.repo.RetryFailedDownload(tc.ctx, tc.user, tc.id); err == nil {
			t.Error("RetryFailedDownload accepted invalid input")
		}
	}
	for _, tc := range []struct {
		name      string
		status    string
		expired   bool
		items     *mediaRowsStub
		beginErr  error
		selectErr error
		queryErr  error
		execErr   error
		commitErr error
		queueErr  error
		wantCount int
		want      string
		wantNot   error
	}{
		{name: "begin", beginErr: errors.New("begin"), want: "begin failed-download retry"},
		{name: "missing owner", selectErr: pgx.ErrNoRows, wantNot: mediaapp.ErrDownloadNotFound},
		{name: "lock", selectErr: errors.New("lock"), want: "lock download for retry"},
		{name: "expired", status: "ready", expired: true, wantNot: mediaapp.ErrDownloadNotFound},
		{name: "status expired", status: "expired", wantNot: mediaapp.ErrDownloadNotFound},
		{name: "status invalid", status: "creating", wantNot: mediaapp.ErrDownloadNotFound},
		{name: "failed items query", status: "ready", queryErr: errors.New("items"), want: "reset failed download items"},
		{name: "scan failed item", status: "ready", items: &mediaRowsStub{index: -1, values: [][]any{{int64(10)}}, scanErr: errors.New("scan")}, want: "scan failed download item"},
		{name: "read failed items", status: "ready", items: &mediaRowsStub{index: -1, err: errors.New("iterate")}, want: "read failed download items"},
		{name: "empty retries", status: "ready", items: mediaTestRows(), wantCount: 0},
		{name: "reset status", status: "ready", items: mediaTestRows([]any{int64(10)}), execErr: errors.New("status"), want: "reset download status for retry"},
		{name: "queue rollback", status: "ready", items: mediaTestRows([]any{int64(10)}), queueErr: errors.New("queue"), want: "enqueue failed download item"},
		{name: "commit rollback", status: "ready", items: mediaTestRows([]any{int64(10)}), commitErr: errors.New("commit"), want: "commit failed-download retry"},
		{name: "success", status: "ready_with_errors", items: mediaTestRows([]any{int64(10)}, []any{int64(11)}), wantCount: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobs := &mediaQueueStub{err: tc.queueErr}
			repo := retryTransaction(tc.status, tc.expired, tc.items, tc.beginErr, tc.selectErr, tc.queryErr, tc.execErr, tc.commitErr, jobs)
			count, err := repo.RetryFailedDownload(ctx, 7, 8)
			if tc.wantNot != nil {
				if !errors.Is(err, tc.wantNot) {
					t.Fatalf("error=%v want=%v", err, tc.wantNot)
				}
				return
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("error=%v want=%q", err, tc.want)
			}
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.name == "success" {
				if count != tc.wantCount || len(jobs.jobs) != 2 || jobs.jobs[0].Kind != "media.fetch" || jobs.options[0].Queue != "media" || jobs.options[0].MaxAttempts != mediaapp.FetchMaxAttempts {
					t.Fatalf("count=%d jobs=%+v options=%+v", count, jobs.jobs, jobs.options)
				}
				var args map[string]int64
				if err := json.Unmarshal(jobs.jobs[0].Args, &args); err != nil || args["download_id"] != 8 || args["photo_id"] != 10 {
					t.Fatalf("job args=%s err=%v", jobs.jobs[0].Args, err)
				}
			}
			if tc.name == "empty retries" && count != 0 {
				t.Fatalf("empty retry count=%d", count)
			}
		})
	}
}

func TestDownloadExpiryRepositoryMethodsValidateAndHandleRows(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		repo Downloads
		ctx  context.Context
		lim  int
	}{
		{name: "expired pool", repo: Downloads{}, ctx: ctx, lim: 1},
		{name: "expired context", repo: Downloads{Pool: mediaPoolStub{}}, lim: 1},
		{name: "expired limit", repo: Downloads{Pool: mediaPoolStub{}}, ctx: ctx},
	} {
		if _, err := tc.repo.ExpiredDownloadParts(tc.ctx, tc.lim); err == nil {
			t.Errorf("%s accepted invalid input", tc.name)
		}
	}
	expires := time.Now().UTC()
	success := mediaPoolStub{query: func(context.Context, string, ...any) (pgx.Rows, error) {
		return mediaTestRows([]any{int64(7), 1, "part"}), nil
	}}
	parts, err := (Downloads{Pool: success}).ExpiredDownloadParts(ctx, 4)
	if err != nil || len(parts) != 1 || parts[0] != (mediaapp.ExpiredDownloadPart{DownloadID: 7, Number: 1, BlobKey: "part"}) {
		t.Fatalf("ExpiredDownloadParts = %+v, %v (expires fixture %s)", parts, err, expires)
	}
	for _, tc := range []struct {
		name string
		rows *mediaRowsStub
		err  error
		want string
	}{
		{name: "query", err: errors.New("query"), want: "list expired download parts"},
		{name: "scan", rows: &mediaRowsStub{index: -1, values: [][]any{{int64(1)}}, scanErr: errors.New("scan")}, want: "scan expired download part"},
		{name: "read", rows: &mediaRowsStub{index: -1, err: errors.New("read")}, want: "read expired download parts"},
	} {
		pool := mediaPoolStub{query: func(context.Context, string, ...any) (pgx.Rows, error) {
			if tc.err != nil {
				return nil, tc.err
			}
			return tc.rows, nil
		}}
		if _, err := (Downloads{Pool: pool}).ExpiredDownloadParts(ctx, 1); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s error=%v want=%q", tc.name, err, tc.want)
		}
	}

	for _, method := range []string{"delete", "mark", "partless"} {
		for _, invalid := range []struct {
			repo Downloads
			ctx  context.Context
			id   int64
			part int
			lim  int
		}{
			{repo: Downloads{}, ctx: ctx, id: 1, part: 1, lim: 1},
			{repo: Downloads{Pool: mediaPoolStub{}}, id: 1, part: 1, lim: 1},
		} {
			var err error
			switch method {
			case "delete":
				err = invalid.repo.DeleteDownloadPart(invalid.ctx, invalid.id, invalid.part)
			case "mark":
				err = invalid.repo.MarkDownloadExpired(invalid.ctx, invalid.id)
			case "partless":
				err = invalid.repo.MarkExpiredDownloads(invalid.ctx, invalid.lim)
			}
			if err == nil {
				t.Errorf("%s accepted invalid input", method)
			}
		}
	}

	for _, tc := range []struct {
		name string
		call func(Downloads) error
		want string
	}{
		{name: "delete", call: func(r Downloads) error { return r.DeleteDownloadPart(ctx, 1, 2) }, want: "delete download part metadata"},
		{name: "mark", call: func(r Downloads) error { return r.MarkDownloadExpired(ctx, 1) }, want: "mark download expired"},
		{name: "partless", call: func(r Downloads) error { return r.MarkExpiredDownloads(ctx, 1) }, want: "mark expired partless downloads"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := Downloads{Pool: mediaPoolStub{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("exec")
			}}}
			if err := tc.call(repo); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want=%q", err, tc.want)
			}
			repo = Downloads{Pool: mediaPoolStub{}}
			if err := tc.call(repo); err != nil {
				t.Fatalf("success error=%v", err)
			}
		})
	}
}

func TestRetryTransactionHelperProducesReadableQueueFailures(t *testing.T) {
	err := errors.New("fixture")
	repo := retryTransaction("ready", false, mediaTestRows([]any{int64(1)}), nil, nil, nil, nil, nil, &mediaQueueStub{err: err})
	_, got := repo.RetryFailedDownload(context.Background(), 1, 2)
	if got == nil || !strings.Contains(got.Error(), "enqueue failed download item") {
		t.Fatalf("retry error = %v", got)
	}
}
