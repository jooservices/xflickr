package main

import (
	"errors"
	"net/http"
	"testing"
)

func TestRunUsesEmbeddedDatasetAndConfiguredAddress(t *testing.T) {
	t.Setenv("FAKEFLICKR_SEED", "")
	t.Setenv("FAKEFLICKR_ADDR", "127.0.0.1:0")
	t.Setenv("FAKEFLICKR_QUOTA_PER_HOUR", "0")
	want := errors.New("stop after setup")
	called := false
	original := listenAndServe
	listenAndServe = func(address string, handler http.Handler) error {
		called = true
		if address != "127.0.0.1:0" || handler == nil {
			t.Errorf("address=%q handler=%v", address, handler)
		}
		return want
	}
	t.Cleanup(func() { listenAndServe = original })
	if err := run(); !errors.Is(err, want) || !called {
		t.Fatalf("run err=%v called=%v", err, called)
	}
}

func TestRunReturnsDatasetError(t *testing.T) {
	t.Setenv("FAKEFLICKR_SEED", "no-such-seed.yaml")
	if err := run(); err == nil {
		t.Fatal("expected dataset error")
	}
}

func TestMainReportsStartupError(t *testing.T) {
	t.Setenv("FAKEFLICKR_SEED", "")
	want := errors.New("server stopped")
	originalListen, originalExit := listenAndServe, exitWithError
	listenAndServe = func(string, http.Handler) error { return want }
	var got error
	exitWithError = func(err error) { got = err }
	t.Cleanup(func() { listenAndServe, exitWithError = originalListen, originalExit })
	main()
	if !errors.Is(got, want) {
		t.Fatalf("exit error=%v", got)
	}
}
