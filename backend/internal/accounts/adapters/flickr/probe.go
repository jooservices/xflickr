// Package flickr checks a Flickr API key with one flickr.test.echo call.
package flickr

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jooservices/xflickr/internal/accounts/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

const (
	echoMethod = "flickr.test.echo"
	// EchoTimeout bounds the key check so a stalled Flickr call cannot hang the request.
	EchoTimeout = 10 * time.Second
)

// Probe validates a public API key. It never sends the API secret.
type Probe struct {
	client  *flickrclient.Client
	timeout time.Duration
}

// NewProbe calls Flickr at baseURL. An empty baseURL uses Flickr's REST endpoint.
func NewProbe(baseURL string, httpClient *http.Client, observers ...flickrclient.CallObserver) Probe {
	client := flickrclient.NewClient(baseURL, httpClient)
	if len(observers) > 0 {
		client.SetCallObserver(observers[0])
	}
	return Probe{client: client, timeout: EchoTimeout}
}

// Echo returns app.ErrKeyInvalid when Flickr rejects the key. Other failures
// are returned without the key so callers can avoid storing or logging it.
func (p Probe) Echo(ctx context.Context, apiKey string) error {
	if p.client == nil {
		return errors.New("accounts: flickr client is required")
	}
	if ctx == nil {
		return errors.New("accounts: context is required")
	}
	timeout := p.timeout
	if timeout <= 0 {
		timeout = EchoTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, _, err := p.client.Call(ctx, flickrclient.Credentials{APIKey: apiKey}, echoMethod, nil)
	if err == nil {
		return nil
	}
	if errors.Is(err, flickrclient.ErrInvalidKey) {
		return app.ErrKeyInvalid
	}
	var call *flickrclient.CallError
	if errors.As(err, &call) && call.Code == 100 {
		return app.ErrKeyInvalid
	}
	return errors.New("accounts: flickr.test.echo failed")
}
