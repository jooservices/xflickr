// Package app coordinates operations metrics use cases.
package app

import (
	"context"
	"time"
)

// SeriesQuery identifies one queue metric series and a half-open time range.
type SeriesQuery struct {
	Queue, Role, Node string
	From, To          time.Time
}

// Point is one stored metric sample.
type Point struct {
	At    time.Time
	Value float64
}

// SeriesReader reads stored metric samples for one exact label set.
type SeriesReader interface {
	ReadSeries(context.Context, SeriesQuery) ([]Point, error)
}
