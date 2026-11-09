package app

import (
	catalog "github.com/jooservices/xflickr/internal/catalog/domain"
	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
)

// SizesAuthKind is the Flickr credentials choice for one sizes attempt.
type SizesAuthKind int

const (
	// SizesAuthAnonymous uses the API key without an OAuth token.
	SizesAuthAnonymous SizesAuthKind = iota
	// SizesAuthAccount signs the call with one connected account token.
	SizesAuthAccount
	// SizesAuthNone makes no Flickr call (no eligible account remains).
	SizesAuthNone
)

// SizesAuthDecision is the result of ChooseSizesAuth.
type SizesAuthDecision struct {
	Kind           SizesAuthKind
	Candidate      crawldomain.SizesAuthCandidate
	UserID         int64
	ExistingStatus catalog.SizesStatus
}

// SizesAuthInput feeds ChooseSizesAuth. Restricted means an unauthenticated
// attempt already failed as not-found or permission denied. Placeholder means
// the listing used Flickr's restricted server, so anonymous getSizes is skipped.
type SizesAuthInput struct {
	Placeholder bool
	Restricted  bool
	OnDemand    bool
	Candidates  []crawldomain.SizesAuthCandidate
	// Blocked reports an account already restricted for this photo/facet.
	Blocked func(crawldomain.SizesAuthCandidate) bool
}

// ChooseSizesAuth picks unauthenticated first, then one authenticated account
// when the photo is restricted. Bulk jobs only use the owner's account;
// on-demand jobs may also use the requesting user's accounts. At most one
// account is chosen per decision so a failed retry cannot loop in one handle.
func ChooseSizesAuth(in SizesAuthInput) SizesAuthDecision {
	if !in.Placeholder && !in.Restricted {
		return SizesAuthDecision{Kind: SizesAuthAnonymous}
	}
	for _, candidate := range in.Candidates {
		if !in.OnDemand && !candidate.Owner {
			continue
		}
		if in.Blocked != nil && in.Blocked(candidate) {
			continue
		}
		return SizesAuthDecision{Kind: SizesAuthAccount, Candidate: candidate}
	}
	return SizesAuthDecision{Kind: SizesAuthNone}
}
