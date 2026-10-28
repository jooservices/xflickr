package flickr

import (
	"context"
	"errors"
	"net/http"
	"time"

	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

const loginTimeout = 10 * time.Second

// LoginProbe verifies a Flickr OAuth token with flickr.test.login.
type LoginProbe struct {
	client  *flickrclient.Client
	timeout time.Duration
}

func NewLoginProbe(baseURL string, httpClient *http.Client) LoginProbe {
	return LoginProbe{client: flickrclient.NewClient(baseURL, httpClient), timeout: loginTimeout}
}

func (p LoginProbe) TestLogin(ctx context.Context, credentials flickrclient.Credentials) (flickrclient.LoginUser, error) {
	if p.client == nil {
		return flickrclient.LoginUser{}, errors.New("accounts: flickr login client is required")
	}
	if ctx == nil {
		return flickrclient.LoginUser{}, errors.New("accounts: context is required")
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = loginTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	user, err := p.client.TestLogin(ctx, credentials)
	return user, err
}
