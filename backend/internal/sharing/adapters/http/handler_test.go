package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/sharing/app"
	"github.com/jooservices/xflickr/internal/sharing/domain"
)

type sharingRepo struct{ err error }

func (r sharingRepo) Set(_ context.Context, setting domain.Setting) (domain.Setting, error) {
	setting.ChangedAt = time.Unix(1, 0).UTC()
	return setting, r.err
}

func (r sharingRepo) Get(_ context.Context, userID int64) (domain.Setting, error) {
	return domain.Setting{UserID: userID, Enabled: true, ChangedAt: time.Unix(1, 0).UTC()}, r.err
}

func actor(id int64) func(context.Context) (int64, bool) {
	return func(context.Context) (int64, bool) { return id, id > 0 }
}

func assertSharingResponse(t *testing.T, response interface {
	VisitPutMeSharingResponse(http.ResponseWriter) error
}, status int, contentType, bodyPart string,
) {
	t.Helper()
	recorder := httptest.NewRecorder()
	if err := response.VisitPutMeSharingResponse(recorder); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != status || recorder.Header().Get("Content-Type") != contentType || !strings.Contains(recorder.Body.String(), bodyPart) {
		t.Fatalf("response = %d %q %s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
}

func TestPutMeSharing(t *testing.T) {
	ctx := context.Background()
	valid := &gen.UpdateSharingRequest{Enabled: true}
	for _, tc := range []struct {
		name     string
		handler  Handler
		request  gen.PutMeSharingRequestObject
		status   int
		content  string
		bodyPart string
		wantErr  bool
	}{
		{name: "missing body", handler: Handler{}, status: http.StatusBadRequest, content: "application/problem+json", bodyPart: "/problems/malformed-request"},
		{name: "no actor", handler: Handler{}, request: gen.PutMeSharingRequestObject{Body: valid}, status: http.StatusUnauthorized, content: "application/problem+json", bodyPart: "/problems/unauthenticated"},
		{name: "unauthenticated", handler: Handler{ActorID: actor(0)}, request: gen.PutMeSharingRequestObject{Body: valid}, status: http.StatusUnauthorized, content: "application/problem+json", bodyPart: "/problems/unauthenticated"},
		{name: "success", handler: Handler{Change: app.Change{Repository: sharingRepo{}}, ActorID: actor(5)}, request: gen.PutMeSharingRequestObject{Body: valid}, status: http.StatusOK, content: "application/json", bodyPart: `"enabled":true`},
		{name: "repository error", handler: Handler{Change: app.Change{Repository: sharingRepo{err: errors.New("db")}}, ActorID: actor(5)}, request: gen.PutMeSharingRequestObject{Body: valid}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.handler.PutMeSharing(ctx, tc.request)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if err == nil {
				assertSharingResponse(t, got, tc.status, tc.content, tc.bodyPart)
			}
		})
	}
}

func TestGetMeSharing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		handler  Handler
		status   int
		content  string
		bodyPart string
		wantErr  bool
	}{
		{name: "no actor", status: http.StatusUnauthorized, content: "application/problem+json", bodyPart: "/problems/unauthenticated"},
		{name: "success", handler: Handler{Read: app.Read{Repository: sharingRepo{}}, ActorID: actor(7)}, status: http.StatusOK, content: "application/json", bodyPart: `"enabled":true`},
		{name: "repository error", handler: Handler{Read: app.Read{Repository: sharingRepo{err: errors.New("db")}}, ActorID: actor(7)}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.handler.GetMeSharing(context.Background(), gen.GetMeSharingRequestObject{})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if err == nil {
				recorder := httptest.NewRecorder()
				if err := got.VisitGetMeSharingResponse(recorder); err != nil {
					t.Fatal(err)
				}
				if recorder.Code != tc.status || recorder.Header().Get("Content-Type") != tc.content || !strings.Contains(recorder.Body.String(), tc.bodyPart) {
					t.Fatalf("response = %d %q %s", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
				}
				var envelope map[string]any
				if tc.status == http.StatusOK && json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil {
					t.Fatalf("invalid response envelope: %s", recorder.Body.String())
				}
			}
		})
	}
}
