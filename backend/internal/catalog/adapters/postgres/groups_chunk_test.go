package postgres

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestGroupsBatchChunksLargeInputsWithoutSizeError(t *testing.T) {
	ctx := context.Background()
	db := &repositoryTestDB{batch: &repositoryTestBatch{}}
	repo, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 9, 4, 0, 0, 0, time.UTC)
	for _, count := range []int{501, 1000, 5000} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			db.batch = &repositoryTestBatch{}
			groups := make([]Group, count)
			memberships := make([]PersonGroup, count)
			for i := 0; i < count; i++ {
				nsid := "g" + strconv.Itoa(i) + "@N00"
				groups[i] = Group{NSID: nsid, Name: "Group", MembersCount: 1, PoolCount: 1, SeenAt: now}
				memberships[i] = PersonGroup{PersonNSID: "p@N01", GroupNSID: nsid, SeenAt: now}
			}
			if err := repo.UpsertGroupsBatch(ctx, groups); err != nil {
				t.Fatalf("UpsertGroupsBatch(%d): %v", count, err)
			}
			if err := repo.UpsertPersonGroupsBatch(ctx, memberships); err != nil {
				t.Fatalf("UpsertPersonGroupsBatch(%d): %v", count, err)
			}
			if db.batch.index != count*2 {
				t.Fatalf("executed %d statements, want %d", db.batch.index, count*2)
			}
		})
	}
}
