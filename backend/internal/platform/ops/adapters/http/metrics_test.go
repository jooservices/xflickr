package http

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	metricsapp "github.com/jooservices/xflickr/internal/platform/metrics/app"
)

type seriesReaderStub struct {
	points []metricsapp.Point
	err    error
	query  metricsapp.SeriesQuery
	calls  int
}

func (s *seriesReaderStub) ReadSeries(_ context.Context, query metricsapp.SeriesQuery) ([]metricsapp.Point, error) {
	s.calls++
	s.query = query
	return s.points, s.err
}

func TestGetAdminMetrics(t *testing.T) {
	from := time.Date(2026, 11, 11, 8, 20, 0, 0, time.FixedZone("UTC+3", 3*60*60))
	to := from.Add(time.Hour)
	now := time.Date(2026, 11, 11, 9, 20, 45, 0, time.UTC)
	first := from.UTC()
	second := first.Add(time.Minute)
	reader := &seriesReaderStub{points: []metricsapp.Point{
		{At: first, Value: 12},
		{At: second, Value: 0},
	}}
	handler := Handler{ActorID: actor, Metrics: reader, Now: func() time.Time { return now }}
	request := validMetricsRequest()
	request.Params.From = from
	request.Params.To = to
	response, err := handler.GetAdminMetrics(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := response.(gen.GetAdminMetrics200JSONResponse)
	if !ok {
		t.Fatalf("response=%T, want 200", response)
	}
	if body.Series != gen.MetricSeriesSeriesQueueJobsAvailable || body.Queue != "default" || body.Role != "scheduler" || body.Node != "node-1" || body.Step != gen.MetricSeriesStepN1M {
		t.Fatalf("series metadata=%+v", body)
	}
	if len(body.Points) != 2 || body.Points[0].At != first || body.Points[0].Value != 12 || body.Points[1].At != second || body.Points[1].Value != 0 {
		t.Fatalf("points=%+v", body.Points)
	}
	if reader.calls != 1 || reader.query.Queue != "default" || reader.query.Role != "scheduler" || reader.query.Node != "node-1" || !reader.query.From.Equal(from.UTC()) || !reader.query.To.Equal(to.UTC()) {
		t.Fatalf("reader calls=%d query=%+v", reader.calls, reader.query)
	}
}

func TestGetAdminMetricsEmptyAndErrors(t *testing.T) {
	t.Run("empty points are an empty array", func(t *testing.T) {
		response, err := (Handler{ActorID: actor, Metrics: &seriesReaderStub{}, Now: fixedMetricsNow}).GetAdminMetrics(t.Context(), validMetricsRequest())
		if err != nil {
			t.Fatal(err)
		}
		body, ok := response.(gen.GetAdminMetrics200JSONResponse)
		if !ok || body.Points == nil || len(body.Points) != 0 {
			t.Fatalf("response=%T points=%v", response, body.Points)
		}
	})

	t.Run("storage errors are generic", func(t *testing.T) {
		reader := &seriesReaderStub{err: errors.New("database details must not escape")}
		response, err := (Handler{ActorID: actor, Metrics: reader, Now: fixedMetricsNow}).GetAdminMetrics(t.Context(), validMetricsRequest())
		body, ok := response.(gen.GetAdminMetrics500JSONResponse)
		if err != nil || !ok || body.Code != "internal_error" || body.Message != "Internal server error" || strings.Contains(body.Message, "database") {
			t.Fatalf("response=%+v type=%T err=%v", response, response, err)
		}
	})

	t.Run("missing dependencies return a generic server error", func(t *testing.T) {
		response, err := (Handler{ActorID: actor, Now: fixedMetricsNow}).GetAdminMetrics(t.Context(), validMetricsRequest())
		if _, ok := response.(gen.GetAdminMetrics500JSONResponse); err != nil || !ok {
			t.Fatalf("response=%T err=%v", response, err)
		}
	})

	t.Run("missing actor is unauthorized", func(t *testing.T) {
		response, err := (Handler{Metrics: &seriesReaderStub{}}).GetAdminMetrics(t.Context(), validMetricsRequest())
		if _, ok := response.(gen.GetAdminMetrics401JSONResponse); err != nil || !ok {
			t.Fatalf("response=%T err=%v", response, err)
		}
	})
}

func TestGetAdminMetricsValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*gen.GetAdminMetricsParams)
	}{
		{name: "missing series", change: func(p *gen.GetAdminMetricsParams) { p.Series = "" }},
		{name: "unsupported series", change: func(p *gen.GetAdminMetricsParams) { p.Series = "queue.jobs_running" }},
		{name: "missing queue", change: func(p *gen.GetAdminMetricsParams) { p.Queue = "" }},
		{name: "queue exceeds code point limit", change: func(p *gen.GetAdminMetricsParams) { p.Queue = strings.Repeat("q", 129) }},
		{name: "queue is invalid UTF-8", change: func(p *gen.GetAdminMetricsParams) { p.Queue = "\xff" }},
		{name: "missing role", change: func(p *gen.GetAdminMetricsParams) { p.Role = "" }},
		{name: "role exceeds code point limit", change: func(p *gen.GetAdminMetricsParams) { p.Role = strings.Repeat("r", 129) }},
		{name: "missing node", change: func(p *gen.GetAdminMetricsParams) { p.Node = "" }},
		{name: "node exceeds code point limit", change: func(p *gen.GetAdminMetricsParams) { p.Node = strings.Repeat("n", 129) }},
		{name: "missing from", change: func(p *gen.GetAdminMetricsParams) { p.From = time.Time{} }},
		{name: "from not at minute boundary", change: func(p *gen.GetAdminMetricsParams) { p.From = time.Date(2026, 11, 11, 8, 20, 1, 0, time.UTC) }},
		{name: "missing to", change: func(p *gen.GetAdminMetricsParams) { p.To = time.Time{} }},
		{name: "to not at minute boundary", change: func(p *gen.GetAdminMetricsParams) { p.To = time.Date(2026, 11, 11, 9, 20, 1, 0, time.UTC) }},
		{name: "zero range", change: func(p *gen.GetAdminMetricsParams) { p.To = p.From }},
		{name: "reversed range", change: func(p *gen.GetAdminMetricsParams) { p.From, p.To = p.To, p.From }},
		{name: "range exceeds one hour", change: func(p *gen.GetAdminMetricsParams) { p.From = time.Date(2026, 11, 11, 8, 19, 0, 0, time.UTC) }},
		{name: "to is after current minute", change: func(p *gen.GetAdminMetricsParams) { p.To = time.Date(2026, 11, 11, 9, 21, 0, 0, time.UTC) }},
		{name: "missing step", change: func(p *gen.GetAdminMetricsParams) { p.Step = "" }},
		{name: "unsupported step", change: func(p *gen.GetAdminMetricsParams) { p.Step = "5m" }},
	}
	runInvalidMetricsQueries(t, tests)
}

func runInvalidMetricsQueries(t *testing.T, tests []struct {
	name   string
	change func(*gen.GetAdminMetricsParams)
}) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &seriesReaderStub{}
			request := validMetricsRequest()
			test.change(&request.Params)
			response, err := (Handler{ActorID: actor, Metrics: reader, Now: fixedMetricsNow}).GetAdminMetrics(t.Context(), request)
			body, ok := response.(gen.GetAdminMetrics400JSONResponse)
			if err != nil || !ok || body.Code != "invalid_request" || body.Message != "Invalid metrics query" || reader.calls != 0 {
				t.Fatalf("response=%T %+v err=%v reader calls=%d", response, body, err, reader.calls)
			}
		})
	}
}

func TestGetAdminMetricsDimensionValidation(t *testing.T) {
	for _, dimension := range []string{"queue", "role", "node"} {
		for _, value := range []string{strings.Repeat("é", 128), strings.Repeat("é", 129), "", "\xff", " é "} {
			t.Run(dimension+"/"+fmt.Sprintf("%q", value), func(t *testing.T) {
				request := validMetricsRequest()
				fields := map[string]*string{"queue": &request.Params.Queue, "role": &request.Params.Role, "node": &request.Params.Node}
				*fields[dimension] = value
				reader := &seriesReaderStub{}
				response, err := (Handler{ActorID: actor, Metrics: reader, Now: fixedMetricsNow}).GetAdminMetrics(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				if value == "" || value == "\xff" || value == strings.Repeat("é", 129) {
					body, ok := response.(gen.GetAdminMetrics400JSONResponse)
					if !ok || body.Code != "invalid_request" || reader.calls != 0 {
						t.Fatalf("response=%T calls=%d", response, reader.calls)
					}
					return
				}
				if _, ok := response.(gen.GetAdminMetrics200JSONResponse); !ok || reader.calls != 1 || reader.query.Queue != request.Params.Queue || reader.query.Role != request.Params.Role || reader.query.Node != request.Params.Node {
					t.Fatalf("response=%T query=%+v calls=%d", response, reader.query, reader.calls)
				}
			})
		}
	}
}

func TestGetAdminMetricsPreservesWhitespace(t *testing.T) {
	t.Run("leading and trailing spaces are preserved", func(t *testing.T) {
		request := validMetricsRequest()
		request.Params.Queue = " queue "
		reader := &seriesReaderStub{}
		response, err := (Handler{ActorID: actor, Metrics: reader, Now: fixedMetricsNow}).GetAdminMetrics(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := response.(gen.GetAdminMetrics200JSONResponse); !ok || reader.query.Queue != " queue " {
			t.Fatalf("response=%T queue=%q", response, reader.query.Queue)
		}
	})
}

func TestGetAdminMetricsNormalizesTime(t *testing.T) {
	t.Run("current minute is accepted and offsets normalize to UTC", func(t *testing.T) {
		request := validMetricsRequest()
		zone := time.FixedZone("UTC+3", 3*60*60)
		request.Params.From = time.Date(2026, 11, 11, 11, 20, 0, 0, zone)
		request.Params.To = time.Date(2026, 11, 11, 12, 20, 0, 0, zone)
		reader := &seriesReaderStub{}
		response, err := (Handler{ActorID: actor, Metrics: reader, Now: fixedMetricsNow}).GetAdminMetrics(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := response.(gen.GetAdminMetrics200JSONResponse); !ok || reader.query.From.Location() != time.UTC || reader.query.To.Location() != time.UTC {
			t.Fatalf("response=%T from=%s to=%s", response, reader.query.From.Location(), reader.query.To.Location())
		}
	})
}

func TestGetAdminMetricsPreservesFiniteValues(t *testing.T) {
	t.Run("finite values pass through the reader boundary", func(t *testing.T) {
		reader := &seriesReaderStub{points: []metricsapp.Point{{At: time.Now(), Value: math.MaxFloat64}}}
		response, err := (Handler{ActorID: actor, Metrics: reader, Now: fixedMetricsNow}).GetAdminMetrics(t.Context(), validMetricsRequest())
		if err != nil {
			t.Fatal(err)
		}
		body, ok := response.(gen.GetAdminMetrics200JSONResponse)
		if !ok || len(body.Points) != 1 || body.Points[0].Value != math.MaxFloat64 {
			t.Fatalf("response=%T points=%+v", response, body.Points)
		}
	})
}

func validMetricsRequest() gen.GetAdminMetricsRequestObject {
	return gen.GetAdminMetricsRequestObject{Params: gen.GetAdminMetricsParams{
		Series: gen.GetAdminMetricsParamsSeriesQueueJobsAvailable,
		Queue:  "default",
		Role:   "scheduler",
		Node:   "node-1",
		From:   time.Date(2026, 11, 11, 8, 20, 0, 0, time.UTC),
		To:     time.Date(2026, 11, 11, 9, 20, 0, 0, time.UTC),
		Step:   gen.GetAdminMetricsParamsStepN1M,
	}}
}

func fixedMetricsNow() time.Time {
	return time.Date(2026, 11, 11, 9, 20, 45, 0, time.UTC)
}
