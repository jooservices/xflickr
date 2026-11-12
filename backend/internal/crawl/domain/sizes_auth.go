package domain

import "context"

// SizesAuthCandidate is one connected account that may view a restricted photo.
// Tokens stay in the accounts adapter; this value only identifies the caller.
type SizesAuthCandidate struct {
	AccountID int64
	UserID    int64
	KeyID     int64
	NSID      string
	Owner     bool
}

// SizesAuthenticator returns ordered accounts for a restricted sizes retry:
// the photo owner's connected account first, then the requesting user's
// accounts. Implementations must not return token material.
type SizesAuthenticator interface {
	SizesAuthCandidates(ctx context.Context, ownerNSID string, requestingUserID int64) ([]SizesAuthCandidate, error)
}
