package app

import (
	"testing"

	crawldomain "github.com/jooservices/xflickr/internal/crawl/domain"
)

func TestChooseSizesAuth(t *testing.T) {
	owner := crawldomain.SizesAuthCandidate{AccountID: 1, UserID: 7, KeyID: 4, NSID: "owner@N00", Owner: true}
	other := crawldomain.SizesAuthCandidate{AccountID: 2, UserID: 7, KeyID: 5, NSID: "viewer@N00", Owner: false}
	blocked := map[int64]bool{}
	block := func(c crawldomain.SizesAuthCandidate) bool { return blocked[c.AccountID] }

	tests := []struct {
		name string
		in   SizesAuthInput
		kind SizesAuthKind
		id   int64
	}{
		{
			name: "anonymous first when unrestricted",
			in:   SizesAuthInput{Candidates: []crawldomain.SizesAuthCandidate{owner}},
			kind: SizesAuthAnonymous,
		},
		{
			name: "placeholder skips anonymous and uses owner",
			in: SizesAuthInput{
				Placeholder: true,
				Candidates:  []crawldomain.SizesAuthCandidate{owner, other},
				Blocked:     block,
			},
			kind: SizesAuthAccount,
			id:   1,
		},
		{
			name: "restricted retries with owner",
			in: SizesAuthInput{
				Restricted: true,
				Candidates: []crawldomain.SizesAuthCandidate{owner, other},
				Blocked:    block,
			},
			kind: SizesAuthAccount,
			id:   1,
		},
		{
			name: "bulk skips non-owner accounts",
			in: SizesAuthInput{
				Restricted: true,
				OnDemand:   false,
				Candidates: []crawldomain.SizesAuthCandidate{other},
				Blocked:    block,
			},
			kind: SizesAuthNone,
		},
		{
			name: "on demand uses requesting user after owner blocked",
			in: SizesAuthInput{
				Restricted: true,
				OnDemand:   true,
				Candidates: []crawldomain.SizesAuthCandidate{owner, other},
				Blocked: func(c crawldomain.SizesAuthCandidate) bool {
					return c.AccountID == owner.AccountID
				},
			},
			kind: SizesAuthAccount,
			id:   2,
		},
		{
			name: "no call when every candidate is blocked",
			in: SizesAuthInput{
				Placeholder: true,
				OnDemand:    true,
				Candidates:  []crawldomain.SizesAuthCandidate{owner, other},
				Blocked:     func(crawldomain.SizesAuthCandidate) bool { return true },
			},
			kind: SizesAuthNone,
		},
		{
			name: "no candidates yields none",
			in:   SizesAuthInput{Restricted: true, OnDemand: true},
			kind: SizesAuthNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ChooseSizesAuth(tt.in)
			if got.Kind != tt.kind {
				t.Fatalf("kind = %v, want %v", got.Kind, tt.kind)
			}
			if tt.kind == SizesAuthAccount && got.Candidate.AccountID != tt.id {
				t.Fatalf("account = %d, want %d", got.Candidate.AccountID, tt.id)
			}
		})
	}
}
