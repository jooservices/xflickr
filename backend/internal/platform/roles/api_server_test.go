package roles

import (
	"context"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	metricsapp "github.com/jooservices/xflickr/internal/platform/metrics/app"
	"github.com/jooservices/xflickr/internal/platform/ops"
	opshttp "github.com/jooservices/xflickr/internal/platform/ops/adapters/http"
	sharinghttp "github.com/jooservices/xflickr/internal/sharing/adapters/http"
	sharingapp "github.com/jooservices/xflickr/internal/sharing/app"
	sharingdomain "github.com/jooservices/xflickr/internal/sharing/domain"
)

func TestAPIServerForwardsSharingAndOps(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	repository := &sharingRepositoryStub{}
	status := opsStatusStub{value: ops.Status{GeneratedAt: now, Counts: ops.Counts{People: 3}}}
	server := apiServer{
		Sharing: sharinghttp.Handler{
			Change:  sharingapp.Change{Repository: repository},
			ActorID: func(context.Context) (int64, bool) { return 17, true },
		},
		Ops: opshttp.Handler{
			Service: status,
			ActorID: func(context.Context) (int64, bool) { return 17, true },
		},
	}

	sharingResponse, err := server.PutMeSharing(context.Background(), gen.PutMeSharingRequestObject{Body: &gen.PutMeSharingJSONRequestBody{Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	sharing, ok := sharingResponse.(gen.PutMeSharing200JSONResponse)
	if !ok || !sharing.Enabled || !repository.got.Enabled || repository.got.UserID != 17 {
		t.Fatalf("sharing response=%T %+v saved=%+v", sharingResponse, sharing, repository.got)
	}

	opsResponse, err := server.GetAdminOpsStatus(context.Background(), gen.GetAdminOpsStatusRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if response, ok := opsResponse.(gen.GetAdminOpsStatus200JSONResponse); !ok || response.Counts.People != 3 {
		t.Fatalf("ops response=%T %+v", opsResponse, opsResponse)
	}
}

func TestAPIServerForwardsAdminMetrics(t *testing.T) {
	at := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	server := apiServer{Ops: opshttp.Handler{
		ActorID: func(context.Context) (int64, bool) { return 17, true },
		Metrics: apiServerMetricsReader{points: []metricsapp.Point{{At: at, Value: 12}}},
		Now:     func() time.Time { return time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC) },
	}}
	response, err := server.GetAdminMetrics(context.Background(), gen.GetAdminMetricsRequestObject{Params: gen.GetAdminMetricsParams{
		Series: gen.GetAdminMetricsParamsSeriesQueueJobsAvailable,
		Queue:  "default", Role: "scheduler", Node: "node-1",
		From: time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC),
		Step: gen.GetAdminMetricsParamsStepN1M,
	}})
	if err != nil {
		t.Fatal(err)
	}
	body, ok := response.(gen.GetAdminMetrics200JSONResponse)
	if !ok || len(body.Points) != 1 || body.Points[0].At != at || body.Points[0].Value != 12 {
		t.Fatalf("response=%T %+v", response, response)
	}
}

type sharingRepositoryStub struct{ got sharingdomain.Setting }

func (r *sharingRepositoryStub) Set(_ context.Context, setting sharingdomain.Setting) (sharingdomain.Setting, error) {
	r.got = setting
	return setting, nil
}

type opsStatusStub struct{ value ops.Status }

func (s opsStatusStub) Status(context.Context) (ops.Status, error) { return s.value, nil }

type apiServerMetricsReader struct{ points []metricsapp.Point }

func (r apiServerMetricsReader) ReadSeries(context.Context, metricsapp.SeriesQuery) ([]metricsapp.Point, error) {
	return r.points, nil
}
