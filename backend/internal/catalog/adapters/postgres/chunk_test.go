package postgres

import (
	"errors"
	"testing"
)

func TestForEachChunkSplitsAndStopsOnError(t *testing.T) {
	boom := errors.New("chunk failed")
	cases := []struct {
		name      string
		count     int
		size      int
		failAt    int // 1-based chunk index that should fail; 0 means none
		wantCalls int
		wantErr   bool
	}{
		{name: "empty", count: 0, size: 500, wantCalls: 0},
		{name: "one item", count: 1, size: 500, wantCalls: 1},
		{name: "exact batch", count: 500, size: 500, wantCalls: 1},
		{name: "one over", count: 501, size: 500, wantCalls: 2},
		{name: "two full batches", count: 1000, size: 500, wantCalls: 2},
		{name: "stops on error", count: 1000, size: 500, failAt: 1, wantCalls: 1, wantErr: true},
		{name: "error on second chunk", count: 1000, size: 500, failAt: 2, wantCalls: 2, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := make([]int, tc.count)
			for i := range items {
				items[i] = i
			}
			var calls []int
			err := forEachChunk(items, tc.size, func(chunk []int) error {
				calls = append(calls, len(chunk))
				if tc.failAt > 0 && len(calls) == tc.failAt {
					return boom
				}
				return nil
			})
			if tc.wantErr {
				if !errors.Is(err, boom) {
					t.Fatalf("error = %v, want %v", err, boom)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(calls) != tc.wantCalls {
				t.Fatalf("calls = %v, want %d chunks", calls, tc.wantCalls)
			}
			if tc.count == 0 {
				return
			}
			total := 0
			for i, n := range calls {
				total += n
				if n > tc.size {
					t.Fatalf("chunk %d length %d exceeds size %d", i, n, tc.size)
				}
			}
			if !tc.wantErr && total != tc.count {
				t.Fatalf("covered %d items, want %d", total, tc.count)
			}
		})
	}
	if err := forEachChunk([]int{1}, 0, func([]int) error { return nil }); err == nil {
		t.Fatal("non-positive size was accepted")
	}
}
