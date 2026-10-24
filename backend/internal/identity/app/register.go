package app

import (
	"context"
	"errors"
	"strings"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

var ErrRegistrationClosed = errors.New("identity: registration is closed")
var ErrEmailExists = errors.New("identity: email already registered")

type RegisterRepository interface {
	Register(context.Context, domain.User, bool) (domain.User, error)
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
	if email == "" || len(email) > MaxEmailBytes || len(password) < MinPasswordBytes || len(password) > MaxPasswordBytes {
		return domain.User{}, errors.New("identity: email or password length is invalid")
	}
	if locale == "" {
		locale = "en"
	}
	params := usecase.Params
	if params.Memory == 0 {
		params = domain.DefaultArgon2Params
	}
	hash, err := domain.HashPassword(password, params)
	if err != nil {
		return domain.User{}, err
	}
	if usecase.Policy == nil {
		return domain.User{}, errors.New("identity: registration policy is required")
	}
	open, err := usecase.Policy.RegistrationOpen(ctx)
	if err != nil {
		return domain.User{}, err
	}
	return usecase.Repository.Register(ctx, domain.User{Email: email, PasswordHash: hash, Locale: locale}, open)
}
