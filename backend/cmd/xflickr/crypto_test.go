package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jooservices/xflickr/internal/platform/crypto"
)

func TestExecuteWithCommandsDispatchesCryptoRotation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	code := executeWithCommands(context.Background(), []string{"crypto", "rotate", "--target", "public.settings,id,secret", "--batch-size=7"}, "", nil, &stdout, &stderr, nil,
		func(_ context.Context, targets []crypto.Target, batchSize int, output io.Writer) error {
			called = true
			if len(targets) != 1 || targets[0] != (crypto.Target{Table: "public.settings", IDColumn: "id", SealedColumn: "secret"}) {
				t.Errorf("rotation targets = %#v", targets)
			}
			if batchSize != 7 || output != &stdout {
				t.Errorf("rotation batch/output = (%d, %T), want (7, stdout)", batchSize, output)
			}
			return nil
		})
	if code != 0 || !called || stderr.Len() != 0 {
		t.Fatalf("executeWithCommands() = (%d, called=%t, stderr=%q)", code, called, stderr.String())
	}
}

func TestRunCryptoRotateValidatesArgumentsAndReportsRunnerErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"encrypt"},
	} {
		var stderr bytes.Buffer
		if code := runCrypto(context.Background(), args, &bytes.Buffer{}, &stderr, nil); code != 2 {
			t.Errorf("runCrypto(%v) code = %d, want 2", args, code)
		}
	}
	for _, args := range [][]string{
		{},
		{"--unknown"},
		{"--target", "not-enough-fields"},
		{"--target", "table,id,sealed", "positional"},
		{"--target", "table,id,sealed", "--batch-size=0"},
	} {
		var stderr bytes.Buffer
		if code := runCrypto(context.Background(), append([]string{"rotate"}, args...), &bytes.Buffer{}, &stderr, nil); code != 2 {
			t.Errorf("runCrypto(rotate %v) code = %d, want 2; stderr=%q", args, code, stderr.String())
		}
	}
	var stderr bytes.Buffer
	wantErr := errors.New("rotation failed")
	code := runCrypto(context.Background(), []string{"rotate", "--target", "table,id,sealed"}, &bytes.Buffer{}, &stderr,
		func(context.Context, []crypto.Target, int, io.Writer) error { return wantErr })
	if code != 1 || !strings.Contains(stderr.String(), wantErr.Error()) {
		t.Fatalf("runCrypto() = (%d, %q), want code 1 and runner error", code, stderr.String())
	}
}

func TestTargetFlagStringAndParse(t *testing.T) {
	var targets targetFlag
	if err := targets.Set(" settings , id , sealed "); err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != (crypto.Target{Table: "settings", IDColumn: "id", SealedColumn: "sealed"}) {
		t.Fatalf("targetFlag.Set() = %#v", targets)
	}
	if !strings.Contains(targets.String(), "settings") {
		t.Errorf("targetFlag.String() = %q, want parsed target", targets.String())
	}
}

func TestRunConfiguredRotationWithInjectedDependencies(t *testing.T) {
	keyring, err := crypto.NewKeyring(map[string][]byte{"active": bytes.Repeat([]byte{1}, 32)}, "active")
	if err != nil {
		t.Fatal(err)
	}
	pool := &rotationStoreStub{}
	targets := []crypto.Target{{Table: "settings", IDColumn: "id", SealedColumn: "secret"}}
	var stdout bytes.Buffer
	called := false
	err = runConfiguredRotationWith(context.Background(), targets, 17, &stdout,
		func() (*crypto.Keyring, error) { return keyring, nil },
		func(context.Context) (rotationPool, crypto.RotationDB, error) { return pool, pool, nil },
		func(_ context.Context, database crypto.RotationDB, gotRing *crypto.Keyring, gotTargets []crypto.Target, batchSize int) (int64, error) {
			called = true
			if database != pool || gotRing != keyring || batchSize != 17 || len(gotTargets) != 1 || gotTargets[0] != targets[0] {
				t.Errorf("rotation received database=%T keyring=%p targets=%#v batch=%d", database, gotRing, gotTargets, batchSize)
			}
			return 4, nil
		},
	)
	if err != nil || !called || !pool.closed || stdout.String() != "rotated 4 sealed value(s)\n" {
		t.Fatalf("runConfiguredRotationWith() = (err=%v, called=%t, closed=%t, stdout=%q)", err, called, pool.closed, stdout.String())
	}
}

func TestRunConfiguredRotationWithReturnsDependencyErrors(t *testing.T) {
	loadErr := errors.New("keyring unavailable")
	if err := runConfiguredRotationWith(context.Background(), nil, 1, &bytes.Buffer{},
		func() (*crypto.Keyring, error) { return nil, loadErr },
		func(context.Context) (rotationPool, crypto.RotationDB, error) {
			t.Fatal("opened store after keyring failure")
			return nil, nil, nil
		},
		func(context.Context, crypto.RotationDB, *crypto.Keyring, []crypto.Target, int) (int64, error) {
			t.Fatal("rotated after keyring failure")
			return 0, nil
		},
	); !errors.Is(err, loadErr) {
		t.Fatalf("keyring loader error = %v", err)
	}

	keyring, err := crypto.NewKeyring(map[string][]byte{"active": bytes.Repeat([]byte{1}, 32)}, "active")
	if err != nil {
		t.Fatal(err)
	}
	storeErr := errors.New("database unavailable")
	if err := runConfiguredRotationWith(context.Background(), nil, 1, &bytes.Buffer{},
		func() (*crypto.Keyring, error) { return keyring, nil },
		func(context.Context) (rotationPool, crypto.RotationDB, error) { return nil, nil, storeErr },
		func(context.Context, crypto.RotationDB, *crypto.Keyring, []crypto.Target, int) (int64, error) {
			t.Fatal("rotated after store failure")
			return 0, nil
		},
	); !errors.Is(err, storeErr) {
		t.Fatalf("store opener error = %v", err)
	}

	pool := &rotationStoreStub{}
	rotateErr := errors.New("rotation failed")
	if err := runConfiguredRotationWith(context.Background(), nil, 1, &bytes.Buffer{},
		func() (*crypto.Keyring, error) { return keyring, nil },
		func(context.Context) (rotationPool, crypto.RotationDB, error) { return pool, pool, nil },
		func(context.Context, crypto.RotationDB, *crypto.Keyring, []crypto.Target, int) (int64, error) {
			return 0, rotateErr
		},
	); !errors.Is(err, rotateErr) || !pool.closed {
		t.Fatalf("rotation error = %v, store closed=%t", err, pool.closed)
	}

	if err := runConfiguredRotationWith(context.Background(), nil, 1, errorWriter{},
		func() (*crypto.Keyring, error) { return keyring, nil },
		func(context.Context) (rotationPool, crypto.RotationDB, error) {
			return &rotationStoreStub{}, &rotationStoreStub{}, nil
		},
		func(context.Context, crypto.RotationDB, *crypto.Keyring, []crypto.Target, int) (int64, error) {
			return 0, nil
		},
	); err == nil {
		t.Fatal("writer error was ignored")
	}
}

func TestRunConfiguredRotationRequiresMasterKeys(t *testing.T) {
	t.Setenv(crypto.MasterKeysEnv, "")
	t.Setenv(crypto.ActiveKeyEnv, "")
	err := runConfiguredRotation(context.Background(), nil, 1, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), crypto.MasterKeysEnv) {
		t.Fatalf("runConfiguredRotation() error = %v, want missing master keys", err)
	}
}

type rotationStoreStub struct {
	closed bool
}

func (pool *rotationStoreStub) Close() { pool.closed = true }

func (*rotationStoreStub) Begin(context.Context) (crypto.RotationTx, error) {
	return nil, errors.New("unexpected transaction")
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failure") }
