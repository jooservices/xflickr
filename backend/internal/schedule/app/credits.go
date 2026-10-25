package app

import "errors"

// CreditInput describes the independent limits that bound a dispatch batch.
type CreditInput struct {
	RemainingQuota int
	PerKeyReadyCap int
	KeyInflight    int
	NodeCap        int
	NodeInflight   int
	WorkerCap      int
	WorkerInflight int
}

// Credits returns the number of rows that may be dispatched for one key and
// worker queue during this pump tick.
func Credits(input CreditInput) (int, error) {
	if input.RemainingQuota < 0 || input.PerKeyReadyCap < 0 || input.KeyInflight < 0 || input.NodeCap < 0 || input.NodeInflight < 0 || input.WorkerCap < 0 || input.WorkerInflight < 0 {
		return 0, errors.New("schedule: capacity values cannot be negative")
	}
	keyFree := max(0, input.PerKeyReadyCap-input.KeyInflight)
	nodeFree := max(0, input.NodeCap-input.NodeInflight)
	workerFree := max(0, input.WorkerCap-input.WorkerInflight)
	return min(input.RemainingQuota, keyFree, nodeFree, workerFree), nil
}
