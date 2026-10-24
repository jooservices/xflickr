package http

import (
	"context"
	"errors"

	"github.com/jooservices/xflickr/internal/identity/app"
	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// AdminUserService is the use case behind the /admin/users endpoints.
type AdminUserService interface {
	List(context.Context, app.UserQuery) (app.UserPage, error)
	Update(ctx context.Context, actorID, id int64, change domain.UserChange) (domain.User, error)
}

func (h Handler) ListAdminUsers(ctx context.Context, request gen.ListAdminUsersRequestObject) (gen.ListAdminUsersResponseObject, error) {
	if _, ok := adminPrincipal(ctx); !ok {
		return gen.ListAdminUsers401JSONResponse(unauthenticatedError()), nil
	}
	if h.AdminUsers == nil {
		return nil, errors.New("identity: admin user service is required")
	}
	query := app.UserQuery{}
	if request.Params.Q != nil {
		query.Search = *request.Params.Q
	}
	if request.Params.Page != nil {
		query.Page = *request.Params.Page
	}
	if request.Params.PageSize != nil {
		query.PageSize = *request.Params.PageSize
	}
	page, err := h.AdminUsers.List(ctx, query)
	if err != nil {
		return nil, err
	}
	items := make([]gen.AdminUser, 0, len(page.Items))
	for _, u := range page.Items {
		items = append(items, apiAdminUser(u))
	}
	return gen.ListAdminUsers200JSONResponse(gen.AdminUserPage{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize}), nil
}

func (h Handler) UpdateAdminUser(ctx context.Context, request gen.UpdateAdminUserRequestObject) (gen.UpdateAdminUserResponseObject, error) {
	actor, ok := adminPrincipal(ctx)
	if !ok {
		return gen.UpdateAdminUser401JSONResponse(unauthenticatedError()), nil
	}
	if h.AdminUsers == nil {
		return nil, errors.New("identity: admin user service is required")
	}
	if request.Body == nil {
		return gen.UpdateAdminUser400JSONResponse{Code: "invalid_request", Message: "Request body is required"}, nil
	}
	change := domain.UserChange{Disabled: request.Body.Disabled}
	if request.Body.Role != nil {
		role := domain.Role(*request.Body.Role)
		change.Role = &role
	}
	user, err := h.AdminUsers.Update(ctx, actor.User.ID, request.ID, change)
	switch {
	case errors.Is(err, app.ErrNoUserChange), errors.Is(err, app.ErrInvalidRole):
		return gen.UpdateAdminUser400JSONResponse{Code: "invalid_request", Message: "Send a valid role or disabled value"}, nil
	case errors.Is(err, app.ErrUserNotFound):
		return gen.UpdateAdminUser404JSONResponse{Code: "not_found", Message: "User not found"}, nil
	case errors.Is(err, domain.ErrLastAdmin):
		return gen.UpdateAdminUser409JSONResponse{Code: "last_admin", Message: "The last active administrator cannot be disabled or demoted"}, nil
	case err != nil:
		return nil, err
	}
	return gen.UpdateAdminUser200JSONResponse(apiAdminUser(user)), nil
}

func adminPrincipal(ctx context.Context) (Principal, bool) {
	principal, ok := PrincipalFrom(ctx)
	return principal, ok && principal.User.ID != 0
}

func unauthenticatedError() gen.Error {
	return gen.Error{Code: codeUnauthenticated, Message: "Authentication required"}
}

func apiAdminUser(u domain.User) gen.AdminUser {
	return gen.AdminUser{ID: u.ID, Email: openapi_types.Email(u.Email), Role: gen.AdminUserRole(u.Role), Locale: u.Locale, Disabled: u.Disabled, CreatedAt: u.CreatedAt}
}
