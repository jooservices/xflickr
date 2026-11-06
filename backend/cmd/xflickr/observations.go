package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/jooservices/xflickr/internal/platform/roles"
)

type backfillRunner func(context.Context, roles.ObservationBackfillOptions) (roles.ObservationBackfillReport, error)

func runObservationBackfill(ctx context.Context, args []string, stdout, stderr io.Writer, run backfillRunner) int {
	flags := flag.NewFlagSet("xflickr backfill-observations", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := roles.ObservationBackfillOptions{}
	flags.Int64Var(&options.UserID, "user-id", 0, "explicit sole XFlickr user ID")
	flags.Int64Var(&options.AccountID, "account-id", 0, "explicit sole connected Flickr account ID")
	flags.IntVar(&options.BatchSize, "batch-size", 500, "records per committed batch (1..10000)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 || options.UserID <= 0 || options.AccountID <= 0 || options.BatchSize < 1 || options.BatchSize > 10000 {
		fmt.Fprintln(stderr, "backfill-observations requires --user-id, --account-id and --batch-size 1..10000; no positional arguments")
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
