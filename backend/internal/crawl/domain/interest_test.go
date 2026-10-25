package domain

import (
	"errors"
	"testing"
)

func TestInterestValidate(t *testing.T) {
	valid := Interest{UserID: 1, PersonNSID: "person", Depth: 0, Via: "account"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for name, interest := range map[string]Interest{
		"missing user":   {PersonNSID: "person", Via: "account"},
		"missing person": {UserID: 1, Via: "account"},
		"negative depth": {UserID: 1, PersonNSID: "person", Depth: -1, Via: "account"},
		"missing via":    {UserID: 1, PersonNSID: "person"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := interest.Validate(); !errors.Is(err, ErrInvalidInterest) {
				t.Fatalf("Validate() error = %v, want ErrInvalidInterest", err)
			}
		})
	}
}

func TestKeepLowest(t *testing.T) {
	for _, test := range []struct{ existing, candidate, want int }{
		{1, 3, 1}, {3, 1, 1}, {2, 2, 2},
	} {
		if got := KeepLowest(test.existing, test.candidate); got != test.want {
			t.Fatalf("KeepLowest(%d, %d) = %d, want %d", test.existing, test.candidate, got, test.want)
		}
	}
}

func TestIsExpandableUsesEachUsersDepth(t *testing.T) {
	interests := []InterestDepth{{UserID: 1, Depth: 1}, {UserID: 2, Depth: 3}}
	if !IsExpandable(interests, map[int64]int{1: 2, 2: 3}) {
		t.Fatal("person should expand for user 1 even though user 2 reached their max")
	}
	if IsExpandable(interests, map[int64]int{1: 1, 2: 3}) {
		t.Fatal("person should not expand when all users reached their own max")
	}
	if IsExpandable(interests, map[int64]int{}) {
		t.Fatal("missing user's max depth should not make person expandable")
	}
}
