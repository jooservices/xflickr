package domain

import "testing"

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
