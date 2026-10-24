package roles

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// Run starts each selected role and waits for all of them to stop.
func Run(ctx context.Context, selected []Role) error {
	if len(selected) == 0 {
		return fmt.Errorf("no roles selected")
	}
	for _, role := range selected {
		if role == nil {
			return fmt.Errorf("cannot run a nil role")
		}
	}

	group, roleCtx := errgroup.WithContext(ctx)
	for _, role := range selected {
		role := role
		group.Go(func() error { return role.Run(roleCtx) })
	}
	return group.Wait()
}
