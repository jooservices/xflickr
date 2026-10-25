package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestRunRequeue(t *testing.T) {
	var got []int64
	ok := func(_ context.Context, ids []int64, _ io.Writer) error { got = ids; return nil }
	var out, errb bytes.Buffer
	if code := runRequeue(t.Context(), []string{"--id", "1,2", "--id", "3"}, &out, &errb, ok); code != 0 || !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("code=%d ids=%v err=%s", code, got, errb.String())
	}
	got = nil
	if code := runRequeue(t.Context(), nil, &out, &errb, ok); code != 0 || len(got) != 0 {
		t.Fatalf("all: code=%d ids=%v", code, got)
	}
	for _, args := range [][]string{{"--id", "x"}, {"--id", "0"}, {"extra"}, {"--bogus"}} {
		if code := runRequeue(t.Context(), args, &out, &errb, ok); code != 2 {
			t.Fatalf("args %v code=%d, want 2", args, code)
		}
	}
	fail := func(context.Context, []int64, io.Writer) error { return errors.New("boom") }
	if code := runRequeue(t.Context(), nil, &out, &errb, fail); code != 1 {
		t.Fatalf("failing runner code=%d", code)
	}
}

func TestRunConfiguredRequeueWith(t *testing.T) {
	var out bytes.Buffer
	if err := runConfiguredRequeueWith(t.Context(), []int64{4}, &out, func(_ context.Context, ids []int64) (int, error) { return len(ids), nil }); err != nil || out.String() != "requeued 1 failed work item(s)\n" {
		t.Fatalf("out=%q err=%v", out.String(), err)
	}
	if err := runConfiguredRequeueWith(t.Context(), nil, &out, func(context.Context, []int64) (int, error) { return 0, errors.New("boom") }); err == nil {
		t.Fatal("expected error")
	}
}
