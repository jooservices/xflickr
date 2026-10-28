package app

import (
	"context"
	"errors"
	"time"
)

// CallStats is the administrator view of observed Flickr calls and completed
// crawl work within one rolling window.
type CallStats struct {
	Window           string
	CallsPerHour     []CallOutcomeHour
	TopErrorCodes    []CallErrorCode
	LatencyByMethod  []CallMethodLatency
	RetriesByFacet   []CallFacetRetries
	CompletedPerHour []CallCompletedHour
}

type CallOutcomeHour struct {
	Hour    time.Time
	Outcome string
	Calls   int64
}

type CallErrorCode struct {
	Code  int64
	Calls int64
}

type CallMethodLatency struct {
	Method string
	P50MS  float64
	P95MS  float64
}

type CallFacetRetries struct {
	Facet   string
	Retries int64
}

type CallCompletedHour struct {
	Hour   time.Time
	Photos int64
	People int64
}

// CallStatsReader loads the saved call statistics for a validated window.
type CallStatsReader interface {
	Stats(context.Context, string) (CallStats, error)
}

// ReadCallStats validates and reads the requested rolling window.
func ReadCallStats(ctx context.Context, reader CallStatsReader, window string) (CallStats, error) {
	if ctx == nil || reader == nil {
		return CallStats{}, errors.New("quota: call stats reader and context are required")
	}
	if window != "1h" && window != "24h" {
		return CallStats{}, errors.New("quota: window must be 1h or 24h")
	}
	return reader.Stats(ctx, window)
}
