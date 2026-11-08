package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/jooservices/xflickr/internal/platform/roles"
)

type checkpointRepairRunner func(context.Context, roles.CheckpointRepairOptions) (roles.CheckpointRepairReport, error)

func runCheckpointRepair(ctx context.Context, args []string, stdout, stderr io.Writer, run checkpointRepairRunner) int {
	flags := flag.NewFlagSet("xflickr repair-checkpoints", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := roles.CheckpointRepairOptions{}
	flags.IntVar(&options.BatchSize, "batch-size", 500, "interest rows per committed batch (1..500)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || options.BatchSize < 1 || options.BatchSize > 500 {
		fmt.Fprintln(stderr, "repair-checkpoints requires --batch-size 1..500; no positional arguments")
		return 2
	}
	report, err := run(ctx, options)
	if encodeErr := json.NewEncoder(stdout).Encode(report); encodeErr != nil {
		fmt.Fprintln(stderr, encodeErr)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
