package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type fakeAdminService struct {
	page    app.UserPage
	user    domain.User
	err     error
	query   app.UserQuery
	actor   int64
	target  int64
	changed domain.UserChange
}

func (f *fakeAdminService) List(_ context.Context, q app.UserQuery) (app.UserPage, error) {
	f.query = q
	return f.page, f.err
}

func (f *fakeAdminService) Update(_ context.Context, actor, id int64, change domain.UserChange) (domain.User, error) {
	f.actor, f.target, f.changed = actor, id, change
	return f.user, f.err
}

func adminCtx() context.Context {
	return WithPrincipal(context.Background(), Principal{User: domain.User{ID: 5, Role: domain.RoleAdmin}})
}

func TestListAdminUsersHandler(t *testing.T) {
	svc := &fakeAdminService{page: app.UserPage{Items: []domain.User{{ID: 1, Email: "a@example.com", Role: domain.RoleUser, Disabled: true}}, Total: 1, Page: 2, PageSize: 10}}
	h := Handler{AdminUsers: svc}
	q, page, size := "a@", 2, 10
	resp, err := h.ListAdminUsers(adminCtx(), gen.ListAdminUsersRequestObject{Params: gen.ListAdminUsersParams{Q: &q, Page: &page, PageSize: &size}})
	if err != nil {
		t.Fatal(err)
	}
	ok, isOK := resp.(gen.ListAdminUsers200JSONResponse)
	if !isOK || len(ok.Items) != 1 || !ok.Items[0].Disabled || ok.Total != 1 || svc.query != (app.UserQuery{Search: "a@", Page: 2, PageSize: 10}) {
		t.Fatalf("response = %#v query = %#v", resp, svc.query)
	}
	if resp, _ := h.ListAdminUsers(context.Background(), gen.ListAdminUsersRequestObject{}); resp == nil {
		t.Fatal("anonymous request not answered")
	} else if _, ok := resp.(gen.ListAdminUsers401JSONResponse); !ok {
		t.Fatalf("anonymous response = %T", resp)
	}
	if _, err := (Handler{}).ListAdminUsers(adminCtx(), gen.ListAdminUsersRequestObject{}); err == nil {
		t.Fatal("missing service accepted")
	}
	svc.err = errors.New("boom")
	if _, err := h.ListAdminUsers(adminCtx(), gen.ListAdminUsersRequestObject{}); err == nil {
		t.Fatal("service error swallowed")
	}
}

func TestUpdateAdminUserHandler(t *testing.T) {
	svc := &fakeAdminService{user: domain.User{ID: 9, Email: "u@example.com", Role: domain.RoleAdmin}}
	h := Handler{AdminUsers: svc}
	role := gen.UpdateAdminUserRequestRoleAdmin
	off := true
	resp, err := h.UpdateAdminUser(adminCtx(), gen.UpdateAdminUserRequestObject{ID: 9, Body: &gen.UpdateAdminUserRequest{Role: &role, Disabled: &off}})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := resp.(gen.UpdateAdminUser200JSONResponse); !ok || got.ID != 9 {
		t.Fatalf("response = %#v", resp)
	}
	if svc.actor != 5 || svc.target != 9 || *svc.changed.Role != domain.RoleAdmin || !*svc.changed.Disabled {
		t.Fatalf("service call = %+v", svc)
	}

	cases := []struct {
		name string
		err  error
		body *gen.UpdateAdminUserRequest
		want any
	}{
		{"nil body", nil, nil, gen.UpdateAdminUser400JSONResponse{}},
		{"empty change", app.ErrNoUserChange, &gen.UpdateAdminUserRequest{}, gen.UpdateAdminUser400JSONResponse{}},
		{"bad role", app.ErrInvalidRole, &gen.UpdateAdminUserRequest{}, gen.UpdateAdminUser400JSONResponse{}},
		{"missing user", app.ErrUserNotFound, &gen.UpdateAdminUserRequest{}, gen.UpdateAdminUser404JSONResponse{}},
		{"last admin", domain.ErrLastAdmin, &gen.UpdateAdminUserRequest{}, gen.UpdateAdminUser409JSONResponse{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := Handler{AdminUsers: &fakeAdminService{err: tc.err}}.UpdateAdminUser(adminCtx(), gen.UpdateAdminUserRequestObject{ID: 1, Body: tc.body})
			if err != nil {
				t.Fatal(err)
			}
			if typeName(resp) != typeName(tc.want) {
				t.Fatalf("response = %T, want %T", resp, tc.want)
			}
		})
	}
	if _, err := (Handler{AdminUsers: &fakeAdminService{err: errors.New("boom")}}).UpdateAdminUser(adminCtx(), gen.UpdateAdminUserRequestObject{Body: &gen.UpdateAdminUserRequest{}}); err == nil {
		t.Fatal("unexpected error swallowed")
	}
	if resp, _ := h.UpdateAdminUser(context.Background(), gen.UpdateAdminUserRequestObject{}); typeName(resp) != typeName(gen.UpdateAdminUser401JSONResponse{}) {
		t.Fatalf("anonymous response = %T", resp)
	}
	if _, err := (Handler{}).UpdateAdminUser(adminCtx(), gen.UpdateAdminUserRequestObject{}); err == nil {
		t.Fatal("missing service accepted")
	}
}

func typeName(v any) string { return fmt.Sprintf("%T", v) }

func TestProtectRoutesRequiresAdminUnderAdmin(t *testing.T) {
	user := &fakeSessions{auth: app.Authenticated{User: domain.User{ID: 2, Role: domain.RoleUser}}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for _, method := range []string{http.MethodGet, http.MethodPatch} {
		req := httptest.NewRequest(method, "/admin/users/3", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "present"})
		rec := httptest.NewRecorder()
		ProtectRoutes(user)(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s as user status = %d", method, rec.Code)
		}
	}
	anon := httptest.NewRecorder()
	ProtectRoutes(&fakeSessions{authErr: app.ErrUnauthenticated})(next).ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/admin/users", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", anon.Code)
	}
	admin := &fakeSessions{auth: app.Authenticated{User: domain.User{ID: 1, Role: domain.RoleAdmin}}}
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "present"})
	ok := httptest.NewRecorder()
	ProtectRoutes(admin)(next).ServeHTTP(ok, req)
	if ok.Code != http.StatusNoContent {
		t.Fatalf("admin status = %d", ok.Code)
	}
}
