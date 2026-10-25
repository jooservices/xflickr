package domain

import "time"

// State summarizes how much of a key's hourly quota is left.
type State string

const (
	// StateOK means the key can still make calls in the current window.
	StateOK State = "ok"
	// StateCooling means Flickr rejected the key and it waits for the window to end.
	StateCooling State = "cooling"
	// StateExhausted means the hourly limit is spent.
	StateExhausted State = "exhausted"
)

// Trend compares a key's usage with the previous window.
type Trend string

const (
	// TrendRising means usage runs above the previous window's pace.
	TrendRising Trend = "rising"
	// TrendSteady means usage matches the previous window's pace.
	TrendSteady Trend = "steady"
	// TrendFalling means usage runs below the previous window's pace.
	TrendFalling Trend = "falling"
)

// trendTolerance is the share by which usage may differ from the previous
// window's pace before the trend counts as rising or falling.
const trendTolerance = 0.1

// Classify maps a window's counters to a State. A cooldown wins over the
// counter because Flickr itself refused the key.
func Classify(used, limit int64, cooling bool) State {
	switch {
	case cooling:
		return StateCooling
	case used >= limit:
		return StateExhausted
	default:
		return StateOK
	}
}

// TrendOf compares used, reached after elapsed time in the current window,
// with previous, the full usage of the preceding window. Without a previous
// window any usage counts as rising.
func TrendOf(used, previous int64, hasPrevious bool, elapsed time.Duration, window time.Duration) Trend {
	if !hasPrevious || window <= 0 {
		if used > 0 {
			return TrendRising
		}
		return TrendSteady
	}
	elapsed = min(max(elapsed, 0), window)
	pace := float64(previous) * float64(elapsed) / float64(window)
	switch {
	case float64(used) > pace*(1+trendTolerance) && used > 0:
		return TrendRising
	case float64(used) < pace*(1-trendTolerance):
		return TrendFalling
	default:
		return TrendSteady
	}
}
