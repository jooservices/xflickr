package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jooservices/xflickr/internal/platform/roles"
)

type requeueRunner func(context.Context, []int64, io.Writer) error

type idFlag []int64

func (ids *idFlag) String() string { return fmt.Sprint([]int64(*ids)) }

func (ids *idFlag) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id < 1 {
			return fmt.Errorf("invalid work item id %q", part)
		}
		*ids = append(*ids, id)
	}
	return nil
}

// runRequeue returns failed work items to pending: all of them, or only the
// ones given with --id.
func runRequeue(ctx context.Context, args []string, stdout, stderr io.Writer, run requeueRunner) int {
	flags := flag.NewFlagSet("xflickr requeue", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var ids idFlag
	flags.Var(&ids, "id", "repeatable or comma-separated failed work item id; all failed items when omitted")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "xflickr requeue: %v\n", err)
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "xflickr requeue: unexpected arguments %q\n", flags.Args())
		return 2
	}
	if err := run(ctx, ids, stdout); err != nil {
		fmt.Fprintf(stderr, "xflickr requeue: %v\n", err)
		return 1
	}
	return 0
}

func runConfiguredRequeue(ctx context.Context, ids []int64, stdout io.Writer) error {
	return runConfiguredRequeueWith(ctx, ids, stdout, roles.RequeueFailedWork)
}

func runConfiguredRequeueWith(ctx context.Context, ids []int64, stdout io.Writer, requeue func(context.Context, []int64) (int, error)) error {
	count, err := requeue(ctx, ids)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "requeued %d failed work item(s)\n", count)
	return err
}
