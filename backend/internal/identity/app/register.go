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
	Register(context.Context, domain.User) (domain.User, error)
}

type Register struct {
	Repository RegisterRepository
	Params     domain.Argon2Params
}

func (usecase Register) Execute(ctx context.Context, email, password, locale string) (domain.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return domain.User{}, errors.New("identity: email and password are required")
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
	return usecase.Repository.Register(ctx, domain.User{Email: email, PasswordHash: hash, Locale: locale})
}
