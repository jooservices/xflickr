package http

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/sharing/app"
	"github.com/jooservices/xflickr/internal/sharing/domain"
)

type changeRepo struct{ err error }

func (r changeRepo) Set(_ context.Context, setting domain.Setting) (domain.Setting, error) {
	setting.ChangedAt = time.Unix(1, 0)
	return setting, r.err
}

func TestPutMeSharing(t *testing.T) {
	ctx := context.Background()
	valid := &gen.UpdateSharingRequest{Enabled: true}
	for _, tc := range []struct {
		name    string
		handler Handler
		request gen.PutMeSharingRequestObject
		want    any
		wantErr bool
	}{
		{name: "missing body", handler: Handler{}, want: gen.PutMeSharing400JSONResponse{}},
		{name: "no actor", handler: Handler{}, request: gen.PutMeSharingRequestObject{Body: valid}, want: gen.PutMeSharing401JSONResponse{}},
		{name: "unauthenticated", handler: Handler{ActorID: func(context.Context) (int64, bool) { return 0, false }}, request: gen.PutMeSharingRequestObject{Body: valid}, want: gen.PutMeSharing401JSONResponse{}},
		{name: "success", handler: Handler{Change: app.Change{Repository: changeRepo{}}, ActorID: func(context.Context) (int64, bool) { return 5, true }}, request: gen.PutMeSharingRequestObject{Body: valid}, want: gen.PutMeSharing200JSONResponse{}},
		{name: "repository error", handler: Handler{Change: app.Change{Repository: changeRepo{err: errors.New("db")}}, ActorID: func(context.Context) (int64, bool) { return 5, true }}, request: gen.PutMeSharingRequestObject{Body: valid}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.handler.PutMeSharing(ctx, tc.request)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if err == nil && fmt.Sprintf("%T", got) != fmt.Sprintf("%T", tc.want) {
				t.Fatalf("response type = %T, want %T", got, tc.want)
			}
		})
	}
}
