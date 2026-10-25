package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestKeysOwned(t *testing.T) {
	pool := &fakePool{row: valRow{values: []any{int64(3), int64(9), "main"}}}
	got, ok, err := (Keys{Pool: pool}).Owned(context.Background(), 9, 3)
	if err != nil || !ok || got.ID != 3 || got.UserID != 9 || got.Label != "main" {
		t.Fatalf("Owned = %+v %v %v", got, ok, err)
	}
	pool.row = valRow{err: pgx.ErrNoRows}
	if _, ok, err := (Keys{Pool: pool}).Owned(context.Background(), 9, 3); err != nil || ok {
		t.Fatalf("missing key = %v %v", ok, err)
	}
	pool.row = valRow{err: errors.New("down")}
	if _, _, err := (Keys{Pool: pool}).Owned(context.Background(), 9, 3); err == nil {
		t.Fatal("query error ignored")
	}
	for _, bad := range [][2]int64{{0, 1}, {1, 0}} {
		if _, _, err := (Keys{Pool: pool}).Owned(context.Background(), bad[0], bad[1]); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	if _, _, err := (Keys{}).Owned(context.Background(), 1, 1); err == nil {
		t.Fatal("nil pool accepted")
	}
}

func TestKeysActive(t *testing.T) {
	pool := &fakePool{rows: &fakeRows{data: [][]any{{int64(1), int64(5), "a"}, {int64(2), int64(6), "b"}}}}
	keys, err := (Keys{Pool: pool}).Active(context.Background())
	if err != nil || len(keys) != 2 || keys[1].ID != 2 || keys[1].UserID != 6 || keys[1].Label != "b" {
		t.Fatalf("Active = %+v %v", keys, err)
	}
	pool.qErr = errors.New("down")
	if _, err := (Keys{Pool: pool}).Active(context.Background()); err == nil {
		t.Fatal("query error ignored")
	}
	pool = &fakePool{rows: &fakeRows{data: [][]any{{"bad", int64(1), "a"}}}}
	if _, err := (Keys{Pool: pool}).Active(context.Background()); err == nil {
		t.Fatal("scan error ignored")
	}
	if _, err := (Keys{}).Active(context.Background()); err == nil {
		t.Fatal("nil pool accepted")
	}
}

func TestKeysPrevious(t *testing.T) {
	pool := &fakePool{row: valRow{values: []any{at, int64(42)}}}
	got, ok, err := (Keys{Pool: pool}).Previous(context.Background(), 4, at.Add(-3*time.Hour), at.Add(time.Hour))
	if err != nil || !ok || got.KeyID != 4 || got.Used != 42 || !got.WindowStart.Equal(at) {
		t.Fatalf("Previous = %+v %v %v", got, ok, err)
	}
	pool.row = valRow{err: pgx.ErrNoRows}
	if _, ok, err := (Keys{Pool: pool}).Previous(context.Background(), 4, at, at); err != nil || ok {
		t.Fatalf("no row = %v %v", ok, err)
	}
	pool.row = valRow{err: errors.New("down")}
	if _, _, err := (Keys{Pool: pool}).Previous(context.Background(), 4, at, at); err == nil {
		t.Fatal("query error ignored")
	}
	if _, _, err := (Keys{Pool: pool}).Previous(context.Background(), 0, at, at); err == nil {
		t.Fatal("zero key accepted")
	}
	if _, _, err := (Keys{}).Previous(context.Background(), 1, at, at); err == nil {
		t.Fatal("nil pool accepted")
	}
}
