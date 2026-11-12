// Package blobstoretest is the contract suite that every app.BlobStore
// implementation and every in-memory fake must pass.
package blobstoretest

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/media/app"
)

const (
	contractKey     = "contract/blob.bin"
	contractMissing = "contract/missing.bin"
	contractFirst   = "first"
	contractSecond  = "second-version"
)

// Run checks the behaviour that every BlobStore must share. newStore returns an
// empty store; each subtest gets its own.
func Run(t *testing.T, newStore func(t *testing.T) app.BlobStore) {
	t.Helper()
	cases := []struct {
		name  string
		check func(*testing.T, app.BlobStore)
	}{
		{"PutThenGetReturnsSameBytes", checkPutThenGet},
		{"PutThenStatReportsKeyAndSize", checkStatAfterPut},
		{"GetMissingKeyReturnsNotFound", checkGetMissing},
		{"StatMissingKeyReturnsNotFound", checkStatMissing},
		{"DeleteMissingKeyIsNoError", checkDeleteMissing},
		{"DeleteRemovesBlob", checkDeleteRemoves},
		{"OverwriteReplacesBytes", checkOverwrite},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.check(t, newStore(t))
		})
	}
}

func checkPutThenGet(t *testing.T, store app.BlobStore) {
	t.Helper()
	put(t, store, contractKey, contractFirst)
	if got := read(t, store, contractKey); got != contractFirst {
		t.Fatalf("Get(%q) = %q, want %q", contractKey, got, contractFirst)
	}
}

func checkStatAfterPut(t *testing.T, store app.BlobStore) {
	t.Helper()
	put(t, store, contractKey, contractFirst)
	info, err := store.Stat(t.Context(), contractKey)
	if err != nil {
		t.Fatalf("Stat(%q): %v", contractKey, err)
	}
	if info.Key != contractKey || info.Size != int64(len(contractFirst)) {
		t.Fatalf("Stat(%q) = %+v, want key and size %d", contractKey, info, len(contractFirst))
	}
}

func checkGetMissing(t *testing.T, store app.BlobStore) {
	t.Helper()
	_, err := store.Get(t.Context(), contractMissing)
	if !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("Get(%q) error = %v, want ErrNotFound", contractMissing, err)
	}
}

func checkStatMissing(t *testing.T, store app.BlobStore) {
	t.Helper()
	_, err := store.Stat(t.Context(), contractMissing)
	if !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("Stat(%q) error = %v, want ErrNotFound", contractMissing, err)
	}
}

func checkDeleteMissing(t *testing.T, store app.BlobStore) {
	t.Helper()
	if err := store.Delete(t.Context(), contractMissing); err != nil {
		t.Fatalf("Delete(%q) of a missing key: %v", contractMissing, err)
	}
}

func checkDeleteRemoves(t *testing.T, store app.BlobStore) {
	t.Helper()
	put(t, store, contractKey, contractFirst)
	if err := store.Delete(t.Context(), contractKey); err != nil {
		t.Fatalf("Delete(%q): %v", contractKey, err)
	}
	if _, err := store.Stat(t.Context(), contractKey); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("Stat(%q) after Delete error = %v, want ErrNotFound", contractKey, err)
	}
}

func checkOverwrite(t *testing.T, store app.BlobStore) {
	t.Helper()
	put(t, store, contractKey, contractFirst)
	put(t, store, contractKey, contractSecond)
	if got := read(t, store, contractKey); got != contractSecond {
		t.Fatalf("Get(%q) after overwrite = %q, want %q", contractKey, got, contractSecond)
	}
	info, err := store.Stat(t.Context(), contractKey)
	if err != nil || info.Size != int64(len(contractSecond)) {
		t.Fatalf("Stat(%q) after overwrite = %+v, err %v, want size %d", contractKey, info, err, len(contractSecond))
	}
}

func put(t *testing.T, store app.BlobStore, key, content string) {
	t.Helper()
	if err := store.Put(t.Context(), key, strings.NewReader(content)); err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
}

func read(t *testing.T, store app.BlobStore, key string) string {
	t.Helper()
	body, err := store.Get(t.Context(), key)
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	if body == nil {
		t.Fatalf("Get(%q) returned a nil body and no error", key)
	}
	data, readErr := io.ReadAll(body)
	if err := errors.Join(readErr, body.Close()); err != nil {
		t.Fatalf("read %q: %v", key, err)
	}
	return string(data)
}
