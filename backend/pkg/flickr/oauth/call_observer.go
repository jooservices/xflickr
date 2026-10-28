package oauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

type callContextKey struct{}

// CallContext carries non-sensitive attribution for one upstream request.
type CallContext struct {
	KeyID         int64
	UserID        *int64
	AccountID     *int64
	FlickrAccount string
	Lane          string
	Facet         string
	Attempt       int
	NodeID        string
	RequestID     string
}

// WithCallContext adds call attribution while preserving an earlier request ID.
func WithCallContext(ctx context.Context, metadata CallContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if current, ok := CallContextFrom(ctx); ok && metadata.RequestID == "" {
		metadata.RequestID = current.RequestID
	}
	return context.WithValue(ctx, callContextKey{}, metadata)
}

// CallContextFrom returns call attribution attached to ctx.
func CallContextFrom(ctx context.Context) (CallContext, bool) {
	if ctx == nil {
		return CallContext{}, false
	}
	metadata, ok := ctx.Value(callContextKey{}).(CallContext)
	return metadata, ok
}

// CallRecord is the safe, non-sensitive summary of one Flickr transport call.
type CallRecord struct {
	CallContext
	Method          string
	StartedAt       time.Time
	Latency         time.Duration
	HTTPStatus      int
	FlickrStat      string
	FlickrErrorCode int
	ErrorMessage    string
	ResponseBytes   int64
	Outcome         string
}

// CallObserver receives completed calls. Implementations should not block.
type CallObserver interface {
	RecordCall(context.Context, CallRecord)
}

// CallObserverFunc adapts a function to CallObserver.
type CallObserverFunc func(context.Context, CallRecord)

func (f CallObserverFunc) RecordCall(ctx context.Context, record CallRecord) {
	if f != nil {
		f(ctx, record)
	}
}

// NewRequestID returns a random UUID for correlation.
func NewRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}
