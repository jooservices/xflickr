// Package app coordinates plan administration and user entitlement lookup.
package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jooservices/xflickr/internal/platform/audit"
	"github.com/jooservices/xflickr/internal/schedule/domain"
)

const planCacheTTL = 30 * time.Second

var (
	ErrPlanNotFound = errors.New("schedule: plan not found")
	ErrUserNotFound = errors.New("schedule: user not found")
)

type Store interface {
	ListPlans(context.Context) ([]domain.Plan, error)
	SavePlans(context.Context, []domain.Plan) error
	PlanForUser(context.Context, int64) (domain.Plan, error)
	AssignUserPlan(context.Context, int64, int64) error
}

type cachedPlan struct {
	plan    domain.Plan
	expires time.Time
}

// Plans provides admin plan management and a short lived per-user lookup cache.
type Plans struct {
	Store Store
	Audit audit.Recorder
	Now   func() time.Time

	mu    sync.Mutex
	cache map[int64]cachedPlan
}

func (s *Plans) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Plans) List(ctx context.Context) ([]domain.Plan, error) {
	if s.Store == nil {
		return nil, errors.New("schedule: plan store is required")
	}
	return s.Store.ListPlans(ctx)
}

// Replace updates plan definitions as one catalog operation and invalidates cached entitlements.
func (s *Plans) Replace(ctx context.Context, actorID int64, plans []domain.Plan) error {
	if s.Store == nil || s.Audit == nil {
		return errors.New("schedule: plan store and audit recorder are required")
	}
	if len(plans) == 0 {
		return domain.ErrInvalidPlan
	}
	seen := make(map[string]struct{}, len(plans))
	for _, p := range plans {
		if err := p.Validate(); err != nil {
			return err
		}
		if _, ok := seen[p.Name]; ok {
			return domain.ErrInvalidPlan
		}
		seen[p.Name] = struct{}{}
	}
	if err := s.Store.SavePlans(ctx, plans); err != nil {
		return err
	}
	s.invalidate()
	values := append([]domain.Plan(nil), plans...)
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return s.Audit.Record(ctx, userRef(actorID), "plans.update", "plans", map[string]any{"plans": values})
}

// ForUser returns the user's plan, keeping a successful result for 30 seconds.
func (s *Plans) ForUser(ctx context.Context, userID int64) (domain.Plan, error) {
	if s.Store == nil {
		return domain.Plan{}, errors.New("schedule: plan store is required")
	}
	now := s.now()
	s.mu.Lock()
	if entry, ok := s.cache[userID]; ok && now.Before(entry.expires) {
		s.mu.Unlock()
		return entry.plan, nil
	}
	s.mu.Unlock()
	plan, err := s.Store.PlanForUser(ctx, userID)
	if err != nil {
		return domain.Plan{}, err
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = make(map[int64]cachedPlan)
	}
	s.cache[userID] = cachedPlan{plan: plan, expires: now.Add(planCacheTTL)}
	s.mu.Unlock()
	return plan, nil
}

// Assign changes a user's plan and records the assignment in the audit trail.
func (s *Plans) Assign(ctx context.Context, actorID, userID, planID int64) error {
	if s.Store == nil || s.Audit == nil {
		return errors.New("schedule: plan store and audit recorder are required")
	}
	if planID <= 0 || userID <= 0 {
		return domain.ErrInvalidPlan
	}
	if err := s.Store.AssignUserPlan(ctx, userID, planID); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.cache, userID)
	s.mu.Unlock()
	if err := s.Audit.Record(ctx, userRef(actorID), "user.plan.assign", userRef(userID), map[string]any{"plan_id": planID}); err != nil {
		return fmt.Errorf("schedule: audit plan assignment: %w", err)
	}
	return nil
}

func (s *Plans) invalidate() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
}

func userRef(id int64) string { return "user:" + strconv.FormatInt(id, 10) }
