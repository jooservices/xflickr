package domain

import (
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	for name, tc := range map[string]struct {
		used, limit int64
		cooling     bool
		want        State
	}{
		"ok":                {used: 5, limit: 10, want: StateOK},
		"just below":        {used: 9, limit: 10, want: StateOK},
		"exhausted":         {used: 10, limit: 10, want: StateExhausted},
		"over":              {used: 12, limit: 10, want: StateExhausted},
		"cooling wins":      {used: 10, limit: 10, cooling: true, want: StateCooling},
		"cooling below cap": {used: 1, limit: 10, cooling: true, want: StateCooling},
	} {
		t.Run(name, func(t *testing.T) {
			if got := Classify(tc.used, tc.limit, tc.cooling); got != tc.want {
				t.Fatalf("Classify = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrendOf(t *testing.T) {
	const window = time.Hour
	for name, tc := range map[string]struct {
		used, previous int64
		hasPrevious    bool
		elapsed        time.Duration
		want           Trend
	}{
		"no history idle":     {want: TrendSteady},
		"no history active":   {used: 3, want: TrendRising},
		"same pace":           {used: 50, previous: 100, hasPrevious: true, elapsed: 30 * time.Minute, want: TrendSteady},
		"within tolerance":    {used: 54, previous: 100, hasPrevious: true, elapsed: 30 * time.Minute, want: TrendSteady},
		"faster":              {used: 80, previous: 100, hasPrevious: true, elapsed: 30 * time.Minute, want: TrendRising},
		"slower":              {used: 20, previous: 100, hasPrevious: true, elapsed: 30 * time.Minute, want: TrendFalling},
		"idle after busy":     {previous: 100, hasPrevious: true, elapsed: window, want: TrendFalling},
		"just started":        {used: 2, previous: 100, hasPrevious: true, elapsed: 0, want: TrendRising},
		"just started idle":   {previous: 100, hasPrevious: true, elapsed: 0, want: TrendSteady},
		"elapsed clamped":     {used: 100, previous: 100, hasPrevious: true, elapsed: 3 * window, want: TrendSteady},
		"negative elapsed":    {previous: 100, hasPrevious: true, elapsed: -time.Minute, want: TrendSteady},
		"previous idle":       {used: 5, previous: 0, hasPrevious: true, elapsed: window, want: TrendRising},
		"previous idle, idle": {previous: 0, hasPrevious: true, elapsed: window, want: TrendSteady},
	} {
		t.Run(name, func(t *testing.T) {
			if got := TrendOf(tc.used, tc.previous, tc.hasPrevious, tc.elapsed, window); got != tc.want {
				t.Fatalf("TrendOf = %q, want %q", got, tc.want)
			}
		})
	}
	if got := TrendOf(5, 5, true, time.Minute, 0); got != TrendRising {
		t.Fatalf("zero window = %q", got)
	}
}
