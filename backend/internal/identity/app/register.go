package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

var ErrRegistrationClosed = errors.New("identity: registration is closed")
var ErrEmailExists = errors.New("identity: email already registered")
var ErrInvalidInput = errors.New("identity: email or password length is invalid")

type RegisterRepository interface {
	Register(context.Context, domain.User, bool) (domain.User, error)
	UsersExist(context.Context) (bool, error)
}

type RegistrationPolicy interface {
	RegistrationOpen(context.Context) (bool, error)
}

type Register struct {
	Repository RegisterRepository
	Policy     RegistrationPolicy
	Params     domain.Argon2Params
}

func (usecase Register) Execute(ctx context.Context, email, password, locale string) (domain.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if invalidRegistrationInput(email, password) {
		return domain.User{}, ErrInvalidInput
	}
	if locale == "" {
		locale = "en"
	}
	if usecase.Policy == nil {
		return domain.User{}, errors.New("identity: registration policy is required")
	}
	open, err := usecase.Policy.RegistrationOpen(ctx)
	if err != nil {
		return domain.User{}, err
	}
	// Closed registration still allows the first account. Reject before hashing
	// only when an account already exists; the repository decides again while
	// holding the registration lock.
	if !open {
		exists, err := usecase.Repository.UsersExist(ctx)
		if err != nil {
			return domain.User{}, err
		}
		if exists {
			return domain.User{}, ErrRegistrationClosed
		}
	}
	params := usecase.Params
	if params.Memory == 0 {
		params = domain.DefaultArgon2Params
	}
	hash, err := domain.HashPassword(password, params)
	if err != nil {
		return domain.User{}, err
	}
	return usecase.Repository.Register(ctx, domain.User{Email: email, PasswordHash: hash, Locale: locale}, open)
}

func invalidRegistrationInput(email, password string) bool {
	return email == "" || len(email) > MaxEmailBytes || len(password) < MinPasswordBytes || len(password) > MaxPasswordBytes
}
