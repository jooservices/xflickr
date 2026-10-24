package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestRunMigrateRequiresExactlyOneSupportedAction(t *testing.T) {
	for _, args := range [][]string{nil, {"up", "down"}, {"fresh"}} {
		var stderr bytes.Buffer
		called := false
		code := runMigrate(context.Background(), args, &bytes.Buffer{}, &stderr, func(context.Context, string, io.Writer) error {
			called = true
			return nil
		})
		if code != 2 || called || stderr.Len() == 0 {
			t.Errorf("runMigrate(%v) = (%d, called=%t, stderr=%q), want rejected command", args, code, called, stderr.String())
		}
	}
}

func TestRunMigrateDelegatesActionAndReportsErrors(t *testing.T) {
	for _, action := range []string{"up", "down", "status"} {
		t.Run(action, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			called := false
			code := runMigrate(context.Background(), []string{action}, &stdout, &stderr, func(_ context.Context, got string, output io.Writer) error {
				called = true
				if got != action || output != &stdout {
					t.Fatalf("migration runner got (%q, %T), want (%q, stdout)", got, output, action)
				}
				return errors.New("migration failed")
			})
			if code != 1 || !called || !bytes.Contains(stderr.Bytes(), []byte("migration failed")) {
				t.Fatalf("runMigrate() = (%d, called=%t, stderr=%q)", code, called, stderr.String())
			}
		})
	}
}
