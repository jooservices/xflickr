package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/platform/audit"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

type fakeStore struct {
	plans                                []domain.Plan
	userPlan                             domain.Plan
	listErr, saveErr, userErr, assignErr error
	listCalls, userCalls, assignCalls    int
	assignedUser, assignedPlan           int64
	event                                audit.Event
}

func (f *fakeStore) ListPlans(context.Context) ([]domain.Plan, error) {
	f.listCalls++
	return append([]domain.Plan(nil), f.plans...), f.listErr
}

func (f *fakeStore) SavePlans(_ context.Context, p []domain.Plan, event audit.Event) error {
	f.event = event
	f.plans = append([]domain.Plan(nil), p...)
	return f.saveErr
}

func (f *fakeStore) PlanForUser(context.Context, int64) (domain.Plan, error) {
	f.userCalls++
	return f.userPlan, f.userErr
}

func (f *fakeStore) AssignUserPlan(_ context.Context, u, p int64, event audit.Event) error {
	f.assignCalls++
	f.assignedUser = u
	f.assignedPlan = p
	f.event = event
	return f.assignErr
}

func TestPlansListAndMissingStore(t *testing.T) {
	if _, err := (&Plans{}).List(context.Background()); err == nil {
		t.Fatal("List accepted a missing store")
	}
	store := &fakeStore{plans: []domain.Plan{{Name: "free", Weight: 1}}}
	got, err := (&Plans{Store: store}).List(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("List = %v, %v", got, err)
	}
	store.listErr = errors.New("db")
	if _, err := (&Plans{Store: store}).List(context.Background()); err == nil {
		t.Fatal("List swallowed store error")
	}
}

func TestReplaceValidatesPersistsAuditsAndInvalidatesCache(t *testing.T) {
	store := &fakeStore{userPlan: domain.Plan{Name: "old", Weight: 1}}
	now := time.Unix(100, 0)
	svc := &Plans{Store: store, Now: func() time.Time { return now }}
	if _, err := svc.ForUser(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	plans := []domain.Plan{{Name: "pro", Weight: 4, ResourceShare: .8}, {Name: "free", Weight: 1, ResourceShare: .2}}
	if err := svc.Replace(context.Background(), 7, plans); err != nil {
		t.Fatal(err)
	}
	if store.event.Actor != "user:7" || store.event.Action != "plans.update" || store.event.Target != "plans" {
		t.Fatalf("audit event = %+v", store.event)
	}
	if len(store.plans) != 2 {
		t.Fatalf("saved plans = %+v", store.plans)
	}
	store.userPlan = domain.Plan{Name: "new", Weight: 2}
	if got, err := svc.ForUser(context.Background(), 10); err != nil || got.Name != "new" || store.userCalls != 2 {
		t.Fatalf("cache not invalidated: %+v calls=%d err=%v", got, store.userCalls, err)
	}
}

func TestReplaceRejectsBadInputAndPropagatesFailures(t *testing.T) {
	plan := domain.Plan{Name: "free", Weight: 1, ResourceShare: .2}
	if err := (&Plans{}).Replace(context.Background(), 1, []domain.Plan{plan}); err == nil {
		t.Fatal("Replace accepted missing dependencies")
	}
	store := &fakeStore{}
	svc := &Plans{Store: store}
	for _, plans := range [][]domain.Plan{nil, {{Name: "", Weight: 1}}, {{Name: "same", Weight: 1}, {Name: "same", Weight: 2}}} {
		if err := svc.Replace(context.Background(), 1, plans); !errors.Is(err, domain.ErrInvalidPlan) {
			t.Errorf("Replace(%+v) error=%v", plans, err)
		}
	}
	store.saveErr = errors.New("save")
	if err := svc.Replace(context.Background(), 1, []domain.Plan{plan}); err == nil {
		t.Fatal("Replace swallowed save error")
	}
	store.saveErr = nil
}

func TestForUserCachesExpiresAndPropagatesErrors(t *testing.T) {
	now := time.Unix(100, 0)
	store := &fakeStore{userPlan: domain.Plan{Name: "free", Weight: 1}}
	svc := &Plans{Store: store, Now: func() time.Time { return now }}
	for range 2 {
		if _, err := svc.ForUser(context.Background(), 5); err != nil {
			t.Fatal(err)
		}
	}
	if store.userCalls != 1 {
		t.Fatalf("calls before expiry=%d", store.userCalls)
	}
	now = now.Add(planCacheTTL)
	if _, err := svc.ForUser(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if store.userCalls != 2 {
		t.Fatalf("calls after expiry=%d", store.userCalls)
	}
	if _, err := (&Plans{}).ForUser(context.Background(), 1); err == nil {
		t.Fatal("ForUser accepted missing store")
	}
	store.userErr = ErrUserNotFound
	if _, err := svc.ForUser(context.Background(), 8); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("ForUser error=%v", err)
	}
}

func TestAssignInvalidatesAndAudits(t *testing.T) {
	store := &fakeStore{userPlan: domain.Plan{Name: "free", Weight: 1}}
	svc := &Plans{Store: store}
	if _, err := svc.ForUser(context.Background(), 9); err != nil {
		t.Fatal(err)
	}
	if err := svc.Assign(context.Background(), 2, 9, 4); err != nil {
		t.Fatal(err)
	}
	if store.assignedUser != 9 || store.assignedPlan != 4 || store.event.Action != "user.plan.assign" || store.event.Actor != "user:2" || store.event.Target != "user:9" {
		t.Fatalf("assignment=%+v", store)
	}
	store.userPlan = domain.Plan{Name: "pro", Weight: 4}
	if got, err := svc.ForUser(context.Background(), 9); err != nil || got.Name != "pro" || store.userCalls != 2 {
		t.Fatalf("assignment cache: %+v calls=%d err=%v", got, store.userCalls, err)
	}
	if err := (&Plans{}).Assign(context.Background(), 1, 1, 1); err == nil {
		t.Fatal("Assign accepted missing dependencies")
	}
	for _, ids := range [][2]int64{{0, 1}, {1, 0}} {
		if err := svc.Assign(context.Background(), 1, ids[0], ids[1]); !errors.Is(err, domain.ErrInvalidPlan) {
			t.Errorf("Assign(%v) error=%v", ids, err)
		}
	}
	store.assignErr = ErrPlanNotFound
	if err := svc.Assign(context.Background(), 1, 9, 4); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("Assign store error=%v", err)
	}
	store.assignErr = nil
}
