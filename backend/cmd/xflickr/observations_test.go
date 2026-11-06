package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/roles"
)

func TestObservationBackfillCLI(t *testing.T) {
	for _, args := range [][]string{nil, {"--user-id", "1"}, {"--user-id", "1", "--account-id", "2", "--batch-size", "0"}, {"--unknown"}, {"--user-id", "1", "--account-id", "2", "extra"}} {
		var out, stderr bytes.Buffer
		if code := runObservationBackfill(t.Context(), args, &out, &stderr, nil); code != 2 {
			t.Fatalf("%v code=%d", args, code)
		}
	}
	args := []string{"--user-id", "1", "--account-id", "2", "--batch-size", "3"}
	for _, fail := range []bool{false, true} {
		var out, stderr bytes.Buffer
		code := runObservationBackfill(t.Context(), args, &out, &stderr, func(_ context.Context, options roles.ObservationBackfillOptions) (roles.ObservationBackfillReport, error) {
			if options.UserID != 1 || options.AccountID != 2 || options.BatchSize != 3 {
				t.Fatalf("options=%+v", options)
			}
			if fail {
				return roles.ObservationBackfillReport{Users: 2}, errors.New("refused")
			}
			return roles.ObservationBackfillReport{Inserted: 4}, nil
		})
		if (!fail && code != 0) || (fail && code != 1) || out.Len() == 0 {
			t.Fatalf("code=%d out=%s err=%s", code, &out, &stderr)
		}
	}
	if code := runObservationBackfill(t.Context(), args, brokenWriter{}, io.Discard, func(context.Context, roles.ObservationBackfillOptions) (roles.ObservationBackfillReport, error) {
		return roles.ObservationBackfillReport{}, nil
	}); code != 1 {
		t.Fatal(code)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
