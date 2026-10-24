package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/crypto"
	"github.com/jooservices/xflickr/internal/platform/db"
)

type (
	rotationRunner func(context.Context, []crypto.Target, int, io.Writer) error
	rotationPool   interface {
		Close()
	}
)

type (
	rotationStoreOpener func(context.Context) (rotationPool, crypto.RotationDB, error)
	rotationExecutor    func(context.Context, crypto.RotationDB, *crypto.Keyring, []crypto.Target, int) (int64, error)
)

type targetFlag []crypto.Target

func (targets *targetFlag) String() string {
	return fmt.Sprint([]crypto.Target(*targets))
}

func (targets *targetFlag) Set(value string) error {
	parts := strings.Split(value, ",")
	if len(parts) != 3 {
		return fmt.Errorf("expected table,id-column,sealed-column")
	}
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	*targets = append(*targets, crypto.Target{Table: parts[0], IDColumn: parts[1], SealedColumn: parts[2]})
	return nil
}

func runCrypto(ctx context.Context, args []string, stdout, stderr io.Writer, run rotationRunner) int {
	if len(args) == 0 || args[0] != "rotate" {
		fmt.Fprintln(stderr, "xflickr crypto: expected action rotate")
		return 2
	}
	return runCryptoRotate(ctx, args[1:], stdout, stderr, run)
}

func runCryptoRotate(ctx context.Context, args []string, stdout, stderr io.Writer, run rotationRunner) int {
	flags := flag.NewFlagSet("xflickr crypto rotate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var targets targetFlag
	flags.Var(&targets, "target", "repeatable table,id-column,sealed-jsonb-column target")
	batchSize := flags.Int("batch-size", crypto.DefaultRotationBatchSize, "rows per committed batch")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "xflickr crypto rotate: %v\n", err)
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "xflickr crypto rotate: unexpected arguments %q\n", flags.Args())
		return 2
	}
	if len(targets) == 0 {
		fmt.Fprintln(stderr, "xflickr crypto rotate: provide at least one --target table,id-column,sealed-column")
		return 2
	}
	if *batchSize < 1 || *batchSize > crypto.MaxRotationBatchSize {
		fmt.Fprintf(stderr, "xflickr crypto rotate: batch size must be between 1 and %d\n", crypto.MaxRotationBatchSize)
		return 2
	}
	if run == nil {
		fmt.Fprintln(stderr, "xflickr crypto rotate: rotation runner is unavailable")
		return 1
	}
	if err := run(ctx, targets, *batchSize, stdout); err != nil {
		fmt.Fprintf(stderr, "xflickr crypto rotate: %v\n", err)
		return 1
	}
	return 0
}

func runConfiguredRotation(ctx context.Context, targets []crypto.Target, batchSize int, stdout io.Writer) error {
	return runConfiguredRotationWith(ctx, targets, batchSize, stdout, crypto.NewKeyringFromEnv, openConfiguredRotationStore, crypto.Rotate)
}

func runConfiguredRotationWith(ctx context.Context, targets []crypto.Target, batchSize int, stdout io.Writer, loadKeyring func() (*crypto.Keyring, error), openStore rotationStoreOpener, rotate rotationExecutor) error {
	keyring, err := loadKeyring()
	if err != nil {
		return err
	}
	pool, database, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	rotated, err := rotate(ctx, database, keyring, targets, batchSize)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "rotated %d sealed value(s)\n", rotated)
	return err
}

func openConfiguredRotationStore(ctx context.Context) (rotationPool, crypto.RotationDB, error) {
	dbConfig, err := config.LoadDatabase()
	if err != nil {
		return nil, nil, err
	}
	pool, err := db.NewPool(ctx, dbConfig)
	if err != nil {
		return nil, nil, err
	}
	return pool, crypto.NewPGXRotationDB(pool), nil
}
