package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/roles"
)

func TestCheckpointRepairCLI(t *testing.T) {
	for _, args := range [][]string{{"--batch-size", "0"}, {"--batch-size", "501"}, {"--unknown"}, {"--batch-size", "10", "extra"}} {
		var out, stderr bytes.Buffer
		if code := runCheckpointRepair(t.Context(), args, &out, &stderr, nil); code != 2 {
			t.Fatalf("%v code=%d stderr=%s", args, code, &stderr)
		}
	}
	args := []string{"--batch-size", "3"}
	for _, fail := range []bool{false, true} {
		var out, stderr bytes.Buffer
		code := runCheckpointRepair(t.Context(), args, &out, &stderr, func(_ context.Context, options roles.CheckpointRepairOptions) (roles.CheckpointRepairReport, error) {
			if options.BatchSize != 3 {
				t.Fatalf("options=%+v", options)
			}
			if fail {
				return roles.CheckpointRepairReport{PhotosUpdated: 1}, errors.New("failed")
			}
			return roles.CheckpointRepairReport{PhotosUpdated: 2, FavoritesUpdated: 1}, nil
		})
		if (!fail && code != 0) || (fail && code != 1) || out.Len() == 0 {
			t.Fatalf("code=%d out=%s err=%s", code, &out, &stderr)
		}
	}
	if code := runCheckpointRepair(t.Context(), args, brokenWriter{}, io.Discard, func(context.Context, roles.CheckpointRepairOptions) (roles.CheckpointRepairReport, error) {
		return roles.CheckpointRepairReport{}, nil
	}); code != 1 {
		t.Fatal(code)
	}
}
