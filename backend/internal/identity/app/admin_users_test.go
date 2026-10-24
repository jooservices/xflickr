package app

import (
	"context"
	"errors"
	"testing"

	"github.com/jooservices/xflickr/internal/identity/domain"
)

type fakeAdminStore struct {
	users    map[int64]domain.User
	listErr  error
	gotLimit int
	gotOff   int
	gotQ     string
}

func (f *fakeAdminStore) ListUsers(_ context.Context, search string, limit, offset int) ([]domain.User, int64, error) {
	f.gotQ, f.gotLimit, f.gotOff = search, limit, offset
	return []domain.User{{ID: 1}}, 7, f.listErr
}

func (f *fakeAdminStore) UpdateUser(_ context.Context, id int64, decide func(domain.User, int) (domain.User, error)) (domain.User, domain.User, error) {
	current, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.User{}, ErrUserNotFound
	}
	admins := 0
	for _, u := range f.users {
		if u.IsActiveAdmin() {
			admins++
		}
	}
	next, err := decide(current, admins)
	if err != nil {
		return domain.User{}, domain.User{}, err
	}
	f.users[id] = next
	return current, next, nil
}

type auditEntry struct {
	actor, action, target string
	detail                map[string]any
}

type fakeAudit struct {
	entries []auditEntry
	err     error
}

func (f *fakeAudit) Record(_ context.Context, actor, action, target string, detail map[string]any) error {
	f.entries = append(f.entries, auditEntry{actor, action, target, detail})
	return f.err
}

func ptr[T any](v T) *T { return &v }

func TestAdminUsersListClampsPaging(t *testing.T) {
	store := &fakeAdminStore{}
	page, err := AdminUsers{Store: store}.List(context.Background(), UserQuery{Search: "  ann ", Page: 0, PageSize: 500})
	if err != nil {
		t.Fatal(err)
	}
	if store.gotQ != "ann" || store.gotLimit != MaxUserPageSize || store.gotOff != 0 || page.Total != 7 || page.Page != 1 {
		t.Fatalf("unexpected query %+v page %+v", store, page)
	}
	if _, err = (AdminUsers{Store: store}).List(context.Background(), UserQuery{Page: 3}); err != nil || store.gotLimit != DefaultUserPageSize || store.gotOff != 2*DefaultUserPageSize {
		t.Fatalf("default paging wrong: %v %+v", err, store)
	}
	store.listErr = errors.New("db down")
	if _, err = (AdminUsers{Store: store}).List(context.Background(), UserQuery{}); err == nil {
		t.Fatal("store error was swallowed")
	}
	if _, err = (AdminUsers{}).List(context.Background(), UserQuery{}); err == nil {
		t.Fatal("missing store accepted")
	}
}

func TestAdminUsersUpdateAudits(t *testing.T) {
	store := &fakeAdminStore{users: map[int64]domain.User{1: {ID: 1, Role: domain.RoleAdmin}, 2: {ID: 2, Role: domain.RoleUser}}}
	rec := &fakeAudit{}
	uc := AdminUsers{Store: store, Audit: rec}
	got, err := uc.Update(context.Background(), 1, 2, domain.UserChange{Role: ptr(domain.RoleAdmin)})
	if err != nil || got.Role != domain.RoleAdmin {
		t.Fatalf("promote: %v %+v", err, got)
	}
	if len(rec.entries) != 1 || rec.entries[0].actor != "user:1" || rec.entries[0].target != "user:2" || rec.entries[0].action != "user.update" {
		t.Fatalf("audit entries = %+v", rec.entries)
	}
}

func TestAdminUsersUpdateRejects(t *testing.T) {
	store := &fakeAdminStore{users: map[int64]domain.User{1: {ID: 1, Role: domain.RoleAdmin}}}
	rec := &fakeAudit{}
	uc := AdminUsers{Store: store, Audit: rec}
	ctx := context.Background()
	if _, err := uc.Update(ctx, 1, 1, domain.UserChange{Disabled: ptr(true)}); !errors.Is(err, domain.ErrLastAdmin) {
		t.Fatalf("disable last admin: %v", err)
	}
	if _, err := uc.Update(ctx, 1, 1, domain.UserChange{Role: ptr(domain.RoleUser)}); !errors.Is(err, domain.ErrLastAdmin) {
		t.Fatalf("demote last admin: %v", err)
	}
	if _, err := uc.Update(ctx, 1, 9, domain.UserChange{Disabled: ptr(true)}); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	if _, err := uc.Update(ctx, 1, 1, domain.UserChange{}); !errors.Is(err, ErrNoUserChange) {
		t.Fatalf("empty change: %v", err)
	}
	if _, err := uc.Update(ctx, 1, 1, domain.UserChange{Role: ptr(domain.Role("root"))}); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("bad role: %v", err)
	}
	if len(rec.entries) != 0 || store.users[1].Role != domain.RoleAdmin || store.users[1].Disabled {
		t.Fatalf("rejected changes leaked: %+v %+v", rec.entries, store.users[1])
	}
	if _, err := (AdminUsers{}).Update(ctx, 1, 1, domain.UserChange{Disabled: ptr(true)}); err == nil {
		t.Fatal("missing dependencies accepted")
	}
}

func TestAdminUsersUpdateAuditFailure(t *testing.T) {
	store := &fakeAdminStore{users: map[int64]domain.User{2: {ID: 2, Role: domain.RoleUser}}}
	uc := AdminUsers{Store: store, Audit: &fakeAudit{err: errors.New("audit down")}}
	if _, err := uc.Update(context.Background(), 1, 2, domain.UserChange{Disabled: ptr(true)}); err == nil {
		t.Fatal("audit failure was swallowed")
	}
}
