package http

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	metricsapp "github.com/jooservices/xflickr/internal/platform/metrics/app"
)

const (
	metricsSeriesName = "queue.jobs_available"
	metricsStep       = "1m"
	metricsQueryError = "Invalid metrics query"
)

// GetAdminMetrics returns stored minute samples for one queue, role, and node.
func (h Handler) GetAdminMetrics(ctx context.Context, request gen.GetAdminMetricsRequestObject) (gen.GetAdminMetricsResponseObject, error) {
	if h.ActorID == nil {
		return gen.GetAdminMetrics401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if _, ok := h.ActorID(ctx); !ok {
		return gen.GetAdminMetrics401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	from, to, ok := parseMetricsQuery(request.Params, h.now())
	if !ok {
		return invalidMetricsQuery(), nil
	}
	if h.Metrics == nil {
		return internalMetricsError(), nil
	}
	points, err := h.Metrics.ReadSeries(ctx, metricsapp.SeriesQuery{
		Queue: request.Params.Queue,
		Role:  request.Params.Role,
		Node:  request.Params.Node,
		From:  from,
		To:    to,
	})
	if err != nil {
		return internalMetricsError(), nil
	}
	responsePoints := make([]gen.MetricPoint, 0, len(points))
	for _, point := range points {
		responsePoints = append(responsePoints, gen.MetricPoint{At: point.At.UTC(), Value: point.Value})
	}
	return gen.GetAdminMetrics200JSONResponse(gen.MetricSeries{
		Series: gen.MetricSeriesSeriesQueueJobsAvailable,
		Queue:  request.Params.Queue,
		Role:   request.Params.Role,
		Node:   request.Params.Node,
		Step:   gen.MetricSeriesStepN1M,
		Points: responsePoints,
	}), nil
}

func (h Handler) now() time.Time {
	if h.Now == nil {
		return time.Now()
	}
	return h.Now()
}

func parseMetricsQuery(params gen.GetAdminMetricsParams, now time.Time) (time.Time, time.Time, bool) {
	if string(params.Series) != metricsSeriesName || string(params.Step) != metricsStep ||
		!validMetricDimension(params.Queue) || !validMetricDimension(params.Role) || !validMetricDimension(params.Node) {
		return time.Time{}, time.Time{}, false
	}
	from := params.From
	if !minuteBoundary(from) {
		return time.Time{}, time.Time{}, false
	}
	to := params.To
	if !minuteBoundary(to) {
		return time.Time{}, time.Time{}, false
	}
	from = from.UTC()
	to = to.UTC()
	if !from.Before(to) || to.Sub(from) > time.Hour || to.After(now.UTC().Truncate(time.Minute)) {
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

func validMetricDimension(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value)
}

func minuteBoundary(value time.Time) bool {
	return value.Second() == 0 && value.Nanosecond() == 0
}

func invalidMetricsQuery() gen.GetAdminMetrics400JSONResponse {
	return gen.GetAdminMetrics400JSONResponse(gen.Error{Code: "invalid_request", Message: metricsQueryError})
}

func internalMetricsError() gen.GetAdminMetrics500JSONResponse {
	return gen.GetAdminMetrics500JSONResponse(gen.Error{Code: "internal_error", Message: "Internal server error"})
}
