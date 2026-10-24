package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestAccountStatusTransitions(t *testing.T) {
	allowed := [][2]AccountStatus{
		{AccountOK, AccountOK},
		{AccountOK, AccountReconnectRequired},
		{AccountOK, AccountRevoked},
		{AccountReconnectRequired, AccountReconnectRequired},
		{AccountReconnectRequired, AccountOK},
		{AccountReconnectRequired, AccountRevoked},
		{AccountRevoked, AccountRevoked},
		{AccountRevoked, AccountOK},
	}
	for _, pair := range allowed {
		if err := pair[0].Transition(pair[1]); err != nil {
			t.Errorf("%s -> %s = %v", pair[0], pair[1], err)
		}
	}
	denied := [][2]AccountStatus{
		{AccountRevoked, AccountReconnectRequired},
		{AccountOK, "unknown"},
		{"unknown", AccountOK},
	}
	for _, pair := range denied {
		if err := pair[0].Transition(pair[1]); !errors.Is(err, ErrInvalidAccountTransition) {
			t.Errorf("%s -> %s = %v", pair[0], pair[1], err)
		}
	}
}

func TestAccountWithStatus(t *testing.T) {
	account := Account{ID: 3, Status: AccountOK}
	revoked, err := account.WithStatus(AccountRevoked)
	if err != nil || revoked.Status != AccountRevoked || account.Status != AccountOK {
		t.Fatalf("revoked = %+v, original = %+v, err = %v", revoked, account, err)
	}
	if _, err := revoked.WithStatus(AccountReconnectRequired); !errors.Is(err, ErrInvalidAccountTransition) {
		t.Fatalf("revoked -> reconnect_required = %v", err)
	}
}

func TestAccountAssociatedDataIsBoundToRowAndPurpose(t *testing.T) {
	if string(TokenAAD(9)) != "flickr_accounts:9:token" || string(TokenSecretAAD(9)) != "flickr_accounts:9:secret" {
		t.Fatalf("aad = %q %q", TokenAAD(9), TokenSecretAAD(9))
	}
	if string(RequestSecretAAD("abc")) != "flickr_oauth_state:abc" {
		t.Fatalf("request aad = %q", RequestSecretAAD("abc"))
	}
}

func TestNormalizeNSID(t *testing.T) {
	if got, err := NormalizeNSID(" 21207597@N07 "); err != nil || got != "21207597@N07" {
		t.Fatalf("nsid = %q %v", got, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("n", CredentialMaxLength+1)} {
		if _, err := NormalizeNSID(bad); !errors.Is(err, ErrInvalidCredential) {
			t.Errorf("NormalizeNSID(%d chars) = %v", len(bad), err)
		}
	}
}
