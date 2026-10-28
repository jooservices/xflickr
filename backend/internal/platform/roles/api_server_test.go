package roles

import (
	"context"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
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

type sharingRepositoryStub struct{ got sharingdomain.Setting }

func (r *sharingRepositoryStub) Set(_ context.Context, setting sharingdomain.Setting) (sharingdomain.Setting, error) {
	r.got = setting
	return setting, nil
}

type opsStatusStub struct{ value ops.Status }

func (s opsStatusStub) Status(context.Context) (ops.Status, error) { return s.value, nil }
