package flickr

import (
	"context"

	"github.com/jooservices/xflickr/pkg/flickr/oauth"
)

type (
	CallContext      = oauth.CallContext
	CallRecord       = oauth.CallRecord
	CallObserver     = oauth.CallObserver
	CallObserverFunc = oauth.CallObserverFunc
)

func WithCallContext(ctx context.Context, metadata CallContext) context.Context {
	return oauth.WithCallContext(ctx, metadata)
}

func CallContextFrom(ctx context.Context) (CallContext, bool) {
	return oauth.CallContextFrom(ctx)
}

func NewRequestID() string { return oauth.NewRequestID() }
