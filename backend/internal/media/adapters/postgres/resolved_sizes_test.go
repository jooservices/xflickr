package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestResolveSizesTransactions(t *testing.T) {
	boom := errors.New("database failure")
	for _, tc := range []struct {
		name, status, fail string
		ids                [][]any
		affected           int
		unsettled          int
		wantJobs           int
	}{
		{name: "confirmed", status: "confirmed", ids: [][]any{{int64(1)}, {int64(2)}}, affected: 1, wantJobs: 2},
		{name: "unavailable final", status: "unavailable", ids: [][]any{{int64(1)}}, affected: 1, wantJobs: 1},
		{name: "unavailable pending", status: "unavailable", ids: [][]any{{int64(1)}}, affected: 1, unsettled: 1},
		{name: "duplicate", status: "confirmed"},
		{name: "already moved", status: "confirmed", ids: [][]any{{int64(1)}}},
		{name: "begin", status: "confirmed", fail: "begin"},
		{name: "query", status: "confirmed", fail: "query"},
		{name: "scan", status: "confirmed", fail: "scan", ids: [][]any{{int64(1)}}},
		{name: "rows", status: "confirmed", fail: "rows"},
		{name: "update", status: "confirmed", fail: "update", ids: [][]any{{int64(1)}}},
		{name: "enqueue", status: "confirmed", fail: "enqueue", ids: [][]any{{int64(1)}}, affected: 1},
		{name: "count", status: "unavailable", fail: "count", ids: [][]any{{int64(1)}}, affected: 1},
		{name: "pack", status: "unavailable", fail: "enqueue", ids: [][]any{{int64(1)}}, affected: 1},
		{name: "commit", status: "confirmed", fail: "commit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := &mediaRowsStub{values: tc.ids, index: -1}
			if tc.fail == "scan" {
				rows.scanErr = boom
			}
			if tc.fail == "rows" {
				rows.err = boom
			}
			tx := &mediaTxStub{
				query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
					if !strings.Contains(sql, "d.user_id=$2") || args[0] != int64(42) || args[1] != int64(7) {
						t.Fatalf("user-scoped wait query = %s, args=%v", sql, args)
					}
					if tc.fail == "query" {
						return nil, boom
					}
					return rows, nil
				},
				exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if !strings.Contains(sql, "state='waiting_sizes'") {
						t.Fatal(sql)
					}
					if tc.fail == "update" {
						return pgconn.CommandTag{}, boom
					}
					if tc.status == "unavailable" && (args[2] != "failed" || args[3] != "sizes_unavailable") {
						t.Fatal(args)
					}
					return pgconn.NewCommandTag("UPDATE " + string(rune('0'+tc.affected))), nil
				}, row: func(context.Context, string, ...any) pgx.Row {
					if tc.fail == "count" {
						return mediaRow{err: boom}
					}
					return mediaRow{values: []any{tc.unsettled}}
				},
			}
			if tc.fail == "commit" {
				tx.commitErr = boom
			}
			jobs := &mediaQueueStub{}
			if tc.fail == "enqueue" {
				jobs.err = boom
			}
			pool := mediaPoolStub{begin: func(context.Context) (pgx.Tx, error) {
				if tc.fail == "begin" {
					return nil, boom
				}
				return tx, nil
			}}
			err := (Downloads{Pool: pool, Jobs: jobs}).ResolveSizes(t.Context(), 42, 7, tc.status)
			if tc.fail != "" {
				if !errors.Is(err, boom) {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil || tx.commits != 1 || len(jobs.jobs) != tc.wantJobs {
				t.Fatalf("err=%v commits=%d jobs=%+v", err, tx.commits, jobs.jobs)
			}
			for i, j := range jobs.jobs {
				want := "media.fetch"
				if tc.status == "unavailable" {
					want = "media.pack"
				}
				if j.Kind != want || jobs.options[i].Queue != "media" {
					t.Fatal(jobs.jobs)
				}
			}
		})
	}
	for _, repo := range []Downloads{{}, {Pool: mediaPoolStub{}}, {Jobs: &mediaQueueStub{}}} {
		if repo.ResolveSizes(t.Context(), 0, 0, "") == nil {
			t.Fatal("missing dependencies accepted")
		}
	}
	if (Downloads{Pool: mediaPoolStub{}, Jobs: &mediaQueueStub{}}).ResolveSizes(nil, 0, 0, "") == nil {
		t.Fatal("nil context accepted")
	}
}
