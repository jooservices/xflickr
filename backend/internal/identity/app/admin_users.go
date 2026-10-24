package app

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jooservices/xflickr/internal/identity/domain"
	"github.com/jooservices/xflickr/internal/platform/audit"
)

const (
	// DefaultUserPageSize is used when the caller sends no page size.
	DefaultUserPageSize = 25
	// MaxUserPageSize caps one page of the admin user list.
	MaxUserPageSize = 100

	auditActionUserUpdate = "user.update"
)

// ErrNoUserChange means an update named neither a role nor a disabled state.
var ErrNoUserChange = errors.New("identity: user update changes nothing")

// ErrInvalidRole means an update named a role that does not exist.
var ErrInvalidRole = errors.New("identity: unknown role")

// UserQuery selects one page of users. Search matches part of the email.
type UserQuery struct {
	Search   string
	Page     int
	PageSize int
}

// UserPage is one page of users plus the total across all pages.
type UserPage struct {
	Items    []domain.User
	Total    int64
	Page     int
	PageSize int
}

// AdminUserStore persists administrator-facing user changes.
type AdminUserStore interface {
	// ListUsers returns users newest first and the total matching the search.
	ListUsers(ctx context.Context, search string, limit, offset int) ([]domain.User, int64, error)
	// UpdateUser loads the user and the number of active administrators under a
	// lock, passes both to decide, and stores the user decide returns. It returns
	// ErrUserNotFound for an unknown id and the error decide returns unchanged.
	UpdateUser(ctx context.Context, id int64, decide func(current domain.User, activeAdmins int) (domain.User, error)) (before, after domain.User, err error)
}

// AdminUsers lists, disables and promotes users on behalf of administrators.
type AdminUsers struct {
	Store AdminUserStore
	Audit audit.Recorder
}

// List returns the requested page. Out-of-range paging values fall back to defaults.
func (a AdminUsers) List(ctx context.Context, query UserQuery) (UserPage, error) {
	if a.Store == nil {
		return UserPage{}, errors.New("identity: admin user store is required")
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = DefaultUserPageSize
	}
	if query.PageSize > MaxUserPageSize {
		query.PageSize = MaxUserPageSize
	}
	search := strings.TrimSpace(query.Search)
	items, total, err := a.Store.ListUsers(ctx, search, query.PageSize, (query.Page-1)*query.PageSize)
	if err != nil {
		return UserPage{}, err
	}
	return UserPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

// Update applies change to the user id as actor and records the result in the
// audit trail. It returns domain.ErrLastAdmin when the change would leave no
// active administrator.
func (a AdminUsers) Update(ctx context.Context, actorID, id int64, change domain.UserChange) (domain.User, error) {
	if a.Store == nil || a.Audit == nil {
		return domain.User{}, errors.New("identity: admin user store and audit recorder are required")
	}
	if change.Role == nil && change.Disabled == nil {
		return domain.User{}, ErrNoUserChange
	}
	if change.Role != nil && *change.Role != domain.RoleAdmin && *change.Role != domain.RoleUser {
		return domain.User{}, ErrInvalidRole
	}
	before, after, err := a.Store.UpdateUser(ctx, id, func(current domain.User, activeAdmins int) (domain.User, error) {
		return current.Apply(change, activeAdmins)
	})
	if err != nil {
		return domain.User{}, err
	}
	detail := map[string]any{
		"role":     map[string]any{"from": string(before.Role), "to": string(after.Role)},
		"disabled": map[string]any{"from": before.Disabled, "to": after.Disabled},
	}
	if err := a.Audit.Record(ctx, userRef(actorID), auditActionUserUpdate, userRef(id), detail); err != nil {
		return domain.User{}, err
	}
	return after, nil
}

func userRef(id int64) string { return "user:" + strconv.FormatInt(id, 10) }
