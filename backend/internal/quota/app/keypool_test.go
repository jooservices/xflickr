package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/quota/domain"
)

type fakeInterests struct {
	users []int64
	err   error
}

func (f fakeInterests) InterestedUsers(context.Context, string) ([]int64, error) {
	return f.users, f.err
}

type fakeKeys struct {
	byUser map[int64][]PoolKey
	err    error
}

func (f fakeKeys) KeysOf(_ context.Context, userID int64) ([]PoolKey, error) {
	return f.byUser[userID], f.err
}

type fakeQuota struct {
	remaining map[KeyID]int
	errs      map[KeyID]error
}

func (f fakeQuota) Capacity(_ context.Context, id int64, _ time.Duration) (int, error) {
	if err := f.errs[KeyID(id)]; err != nil {
		return 0, err
	}
	return f.remaining[KeyID(id)], nil
}

var poolNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func newPool(users []int64, keys map[int64][]PoolKey, quota fakeQuota) KeyPool {
	return KeyPool{
		Interests: fakeInterests{users: users},
		Keys:      fakeKeys{byUser: keys},
		Quota:     quota,
		Now:       func() time.Time { return poolNow },
	}
}

func TestChoosePicksMostRemainingQuota(t *testing.T) {
	pool := newPool([]int64{1, 2}, map[int64][]PoolKey{
		1: {{ID: 10, Valid: true}},
		2: {{ID: 20, Valid: true}, {ID: 21, Valid: true}},
	}, fakeQuota{remaining: map[KeyID]int{10: 50, 20: 300, 21: 100}})
	got, err := pool.Choose(context.Background(), "p", domain.Person)
	if err != nil || got != 20 {
		t.Fatalf("got %d, %v; want 20", got, err)
	}
}

func TestChooseTieBreaksByLeastRecentUse(t *testing.T) {
	pool := newPool([]int64{1}, map[int64][]PoolKey{1: {
		{ID: 10, Valid: true, LastUsedAt: poolNow.Add(-time.Minute)},
		{ID: 11, Valid: true, LastUsedAt: poolNow.Add(-time.Hour)},
		{ID: 12, Valid: true},
	}}, fakeQuota{remaining: map[KeyID]int{10: 5, 11: 5, 12: 1}})
	got, err := pool.Choose(context.Background(), "p", domain.Photo)
	if err != nil || got != 11 {
		t.Fatalf("got %d, %v; want 11", got, err)
	}
}

func TestChooseTieOnUseBreaksByLowestID(t *testing.T) {
	pool := newPool([]int64{1}, map[int64][]PoolKey{1: {{ID: 12, Valid: true}, {ID: 11, Valid: true}}},
		fakeQuota{remaining: map[KeyID]int{11: 5, 12: 5}})
	got, err := pool.Choose(context.Background(), "p", domain.Interactive)
	if err != nil || got != 11 {
		t.Fatalf("got %d, %v; want 11", got, err)
	}
}

func TestChooseSkipsIneligibleKeys(t *testing.T) {
	pool := newPool([]int64{1}, map[int64][]PoolKey{1: {
		{ID: 10, Valid: false},
		{ID: 11, Valid: true, CooldownUntil: poolNow.Add(time.Minute)},
		{ID: 12, Valid: true},
		{ID: 13, Valid: true},
		{ID: 14, Valid: true},
		{ID: 15, Valid: true, CooldownUntil: poolNow.Add(-time.Second)},
	}}, fakeQuota{
		remaining: map[KeyID]int{10: 999, 11: 999, 12: 0, 13: 999, 14: 999, 15: 7},
		errs:      map[KeyID]error{13: ErrKeyUnavailable, 14: nil},
	})
	got, err := pool.Choose(context.Background(), "p", domain.Person)
	if err != nil || got != 14 {
		t.Fatalf("got %d, %v; want 14", got, err)
	}
}

func TestChooseNeverUsesKeysOfUninterestedUsers(t *testing.T) {
	pool := newPool([]int64{1}, map[int64][]PoolKey{
		1: {{ID: 10, Valid: true}},
		2: {{ID: 20, Valid: true}},
	}, fakeQuota{remaining: map[KeyID]int{10: 1, 20: 1000}})
	got, err := pool.Choose(context.Background(), "p", domain.Person)
	if err != nil || got != 10 {
		t.Fatalf("got %d, %v; want 10", got, err)
	}
}

func TestChooseNoInterestOrNoEligibleKeyIsErrNoKey(t *testing.T) {
	keys := map[int64][]PoolKey{1: {{ID: 10, Valid: true}}}
	for name, pool := range map[string]KeyPool{
		"no interest": newPool(nil, keys, fakeQuota{remaining: map[KeyID]int{10: 10}}),
		"no quota":    newPool([]int64{1}, keys, fakeQuota{remaining: map[KeyID]int{10: 0}}),
		"no keys":     newPool([]int64{3}, keys, fakeQuota{}),
	} {
		if _, err := pool.Choose(context.Background(), "p", domain.Person); !errors.Is(err, ErrNoKey) {
			t.Errorf("%s: err = %v, want ErrNoKey", name, err)
		}
	}
}

func TestChooseDeduplicatesUsersAndKeys(t *testing.T) {
	pool := newPool([]int64{1, 1, 2}, map[int64][]PoolKey{
		1: {{ID: 10, Valid: true}},
		2: {{ID: 10, Valid: true}},
	}, fakeQuota{remaining: map[KeyID]int{10: 3}})
	if got, err := pool.Choose(context.Background(), "p", domain.Person); err != nil || got != 10 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestChooseDefaultsClock(t *testing.T) {
	pool := newPool([]int64{1}, map[int64][]PoolKey{1: {{ID: 10, Valid: true}}}, fakeQuota{remaining: map[KeyID]int{10: 3}})
	pool.Now = nil
	if got, err := pool.Choose(context.Background(), "p", domain.Person); err != nil || got != 10 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestChooseErrors(t *testing.T) {
	boom := errors.New("boom")
	keys := map[int64][]PoolKey{1: {{ID: 10, Valid: true}}}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := map[string]struct {
		pool  KeyPool
		ctx   context.Context
		class domain.Class
		want  error
	}{
		"missing deps":    {KeyPool{}, context.Background(), domain.Person, nil},
		"invalid class":   {newPool(nil, nil, fakeQuota{}), context.Background(), "bogus", domain.ErrInvalidClass},
		"cancelled":       {newPool(nil, nil, fakeQuota{}), cancelled, domain.Person, context.Canceled},
		"interests error": {KeyPool{Interests: fakeInterests{err: boom}, Keys: fakeKeys{}, Quota: fakeQuota{}}, context.Background(), domain.Person, boom},
		"keys error":      {KeyPool{Interests: fakeInterests{users: []int64{1}}, Keys: fakeKeys{err: boom}, Quota: fakeQuota{}}, context.Background(), domain.Person, boom},
		"quota error":     {newPool([]int64{1}, keys, fakeQuota{errs: map[KeyID]error{10: boom}}), context.Background(), domain.Person, boom},
	}
	for name, c := range cases {
		_, err := c.pool.Choose(c.ctx, "p", c.class)
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}
