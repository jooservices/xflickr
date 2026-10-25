package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jooservices/xflickr/internal/crawl/domain"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

const defaultChangeCheckInterval = 24 * time.Hour

var errCycleIncomplete = errors.New("crawl: cycle dependencies are required")

// CycleSelection is the next eligible person and the cursor to persist after
// scheduling that person's check.
type CycleSelection struct {
	Person  domain.Person
	Cursor  string
	Wrapped bool
	Found   bool
}

// CycleStore schedules one due person, reopens that person's completed check
// item when needed, and advances the durable cursor atomically.
type CycleStore interface {
	ScheduleNext(context.Context, int64, time.Time, time.Duration) (bool, error)
}

// CycleUserReader lists users with at least one person to check.
type CycleUserReader interface {
	UsersWithInterests(context.Context) ([]int64, error)
}

// Cycle checks a user's effective interval and schedules the next due person.
type Cycle struct {
	Store    CycleStore
	Users    CycleUserReader
	Settings EffectiveSettings
	Now      func() time.Time
}

// RunOnce schedules the next due person for each user with crawl interests.
func (c Cycle) RunOnce(ctx context.Context) error {
	if ctx == nil || c.Users == nil {
		return errCycleIncomplete
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	users, err := c.Users.UsersWithInterests(ctx)
	if err != nil {
		return err
	}
	for _, userID := range users {
		if _, err := c.RunUser(ctx, userID); err != nil {
			return err
		}
	}
	return nil
}

// RunUser schedules at most one due person's check for userID.
func (c Cycle) RunUser(ctx context.Context, userID int64) (bool, error) {
	if ctx == nil || c.Store == nil || c.Settings == nil || userID <= 0 {
		return false, errCycleIncomplete
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	effective, err := c.Settings.Effective(ctx, userID)
	if err != nil {
		return false, err
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	return c.Store.ScheduleNext(ctx, userID, now, EffectiveCheckInterval(effective))
}

// SelectCyclePerson advances through all people in NSID order. A cursor only
// wraps after the last person has been visited, so a large cycle survives
// scheduler restarts without repeatedly favoring the first people.
func SelectCyclePerson(people []domain.Person, cursor string) CycleSelection {
	if len(people) == 0 {
		return CycleSelection{}
	}
	ordered := append([]domain.Person(nil), people...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].NSID < ordered[j].NSID })
	for _, person := range ordered {
		if person.NSID > cursor {
			return CycleSelection{Person: person, Cursor: person.NSID, Found: true}
		}
	}
	return CycleSelection{Person: ordered[0], Cursor: ordered[0].NSID, Wrapped: true, Found: true}
}

// EffectiveCheckInterval applies the user's configured interval, retaining
// the product default when an old or invalid effective value is absent.
func EffectiveCheckInterval(settings settingsdomain.Effective) time.Duration {
	if settings.ChangeCheckInterval <= 0 {
		return defaultChangeCheckInterval
	}
	return settings.ChangeCheckInterval
}

// CycleDue checks whether the user's next full cycle may start.
func CycleDue(lastCycle, now time.Time, settings settingsdomain.Effective) bool {
	return CheckIntervalDue(lastCycle, now, EffectiveCheckInterval(settings))
}
