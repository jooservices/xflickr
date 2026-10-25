package app

import (
	"errors"
	"testing"
)

func TestUnavailableKeyErrorContract(t *testing.T) {
	var unavailable interface{ KeyUnavailable() bool }
	if !errors.As(ErrKeyUnavailable, &unavailable) || !unavailable.KeyUnavailable() {
		t.Fatal("ErrKeyUnavailable does not identify an unavailable key")
	}
	if !errors.Is(ErrKeyUnavailable, ErrKeyUnavailable) {
		t.Fatal("ErrKeyUnavailable does not match itself")
	}
	if ErrKeyUnavailable.Error() != "quota: key unavailable" {
		t.Fatal("ErrKeyUnavailable has an unexpected message")
	}
}
