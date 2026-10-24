package domain

import (
	"errors"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	params := Argon2Params{Memory: 32 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := HashPassword("correct horse battery staple", params)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" || !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("valid password did not verify")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("wrong password verified")
	}
}

func TestHashPasswordRejectsInvalidParameters(t *testing.T) {
	if _, err := HashPassword("password", Argon2Params{}); err == nil {
		t.Fatal("expected invalid parameters error")
	}
	if _, err := HashPassword("", DefaultArgon2Params); err == nil {
		t.Fatal("expected empty password error")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, encoded := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA", "$argon2id$v=19$m=1,t=1,p=1$%%%$%%%"} {
		if VerifyPassword(encoded, "password") {
			t.Errorf("VerifyPassword(%q) = true", encoded)
		}
	}
}

func TestUserApplyProtectsLastActiveAdmin(t *testing.T) {
	admin := User{ID: 1, Role: RoleAdmin}
	user := RoleUser
	off := true
	on := false
	tests := []struct {
		name    string
		user    User
		change  UserChange
		admins  int
		wantErr bool
	}{
		{"demote last admin", admin, UserChange{Role: &user}, 1, true},
		{"disable last admin", admin, UserChange{Disabled: &off}, 1, true},
		{"demote one of two", admin, UserChange{Role: &user}, 2, false},
		{"disable one of two", admin, UserChange{Disabled: &off}, 2, false},
		{"regular user disabled", User{ID: 2, Role: RoleUser}, UserChange{Disabled: &off}, 1, false},
		{"already disabled admin demoted", User{ID: 3, Role: RoleAdmin, Disabled: true}, UserChange{Role: &user}, 1, false},
		{"no-op on last admin", admin, UserChange{Disabled: &on}, 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.user.Apply(tc.change, tc.admins)
			if tc.wantErr {
				if !errors.Is(err, ErrLastAdmin) {
					t.Fatalf("err = %v, want ErrLastAdmin", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.change.Role != nil && got.Role != *tc.change.Role || tc.change.Disabled != nil && got.Disabled != *tc.change.Disabled {
				t.Fatalf("change not applied: %+v", got)
			}
		})
	}
}
