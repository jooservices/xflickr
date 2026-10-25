package app

import "errors"

// CreditInput describes the independent limits that bound a dispatch batch.
type CreditInput struct {
	RemainingQuota int
	PerKeyReadyCap int
	NodeCap        int
	Inflight       int
	WorkerCap      int
	WorkerInflight int
}

// Credits returns the number of rows that may be dispatched for one key and
// worker queue during this pump tick.
func Credits(input CreditInput) (int, error) {
	if input.RemainingQuota < 0 || input.PerKeyReadyCap < 0 || input.NodeCap < 0 || input.Inflight < 0 || input.WorkerCap < 0 || input.WorkerInflight < 0 {
		return 0, errors.New("schedule: capacity values cannot be negative")
	}
	keyCredits := min(input.RemainingQuota, input.PerKeyReadyCap)
	nodeFree := max(0, input.NodeCap-input.Inflight)
	workerFree := max(0, input.WorkerCap-input.WorkerInflight)
	return min(keyCredits, nodeFree, workerFree), nil
}
