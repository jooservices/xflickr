package app

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestFuseRRFCombinesRanks(t *testing.T) {
	bm25 := []string{"both", "text", "bm25-only"}
	vector := []string{"both", "vector-only", "text"}

	got, err := FuseRRF(bm25, vector)
	if err != nil {
		t.Fatalf("FuseRRF() error = %v", err)
	}

	want := []RankedHit{
		{ID: "both", Score: 1.0/61 + 1.0/61, BM25Rank: 1, VectorRank: 1},
		{ID: "text", Score: 1.0/62 + 1.0/63, BM25Rank: 2, VectorRank: 3},
		{ID: "vector-only", Score: 1.0 / 62, VectorRank: 2},
		{ID: "bm25-only", Score: 1.0 / 63, BM25Rank: 3},
	}
	assertRankedHits(t, got, want)
}

func TestFuseRRFTiesAreOrderedByID(t *testing.T) {
	got, err := FuseRRF([]string{"zeta"}, []string{"alpha"})
	if err != nil {
		t.Fatalf("FuseRRF() error = %v", err)
	}

	assertRankedHits(t, got, []RankedHit{
		{ID: "alpha", Score: 1.0 / 61, VectorRank: 1},
		{ID: "zeta", Score: 1.0 / 61, BM25Rank: 1},
	})
}

func TestFuseRRFEmptyBranches(t *testing.T) {
	tests := []struct {
		name   string
		bm25   []string
		vector []string
		want   []RankedHit
	}{
		{name: "both empty", want: []RankedHit{}},
		{
			name: "empty vector",
			bm25: []string{"bm25-only"},
			want: []RankedHit{{ID: "bm25-only", Score: 1.0 / 61, BM25Rank: 1}},
		},
		{
			name:   "empty bm25",
			vector: []string{"vector-only"},
			want:   []RankedHit{{ID: "vector-only", Score: 1.0 / 61, VectorRank: 1}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := FuseRRF(test.bm25, test.vector)
			if err != nil {
				t.Fatalf("FuseRRF() error = %v", err)
			}
			if test.want != nil && got == nil {
				t.Fatal("FuseRRF() returned a nil slice, want an empty or populated slice")
			}
			assertRankedHits(t, got, test.want)
		})
	}
}

func TestFuseRRFRejectsInvalidRanking(t *testing.T) {
	tests := []struct {
		name      string
		bm25      []string
		vector    []string
		sensitive string
		invalid   bool
	}{
		{name: "empty bm25 ID", bm25: []string{""}, invalid: true},
		{name: "duplicate bm25 ID", bm25: []string{"private-hit", "private-hit"}, sensitive: "private-hit", invalid: true},
		{name: "empty vector ID", vector: []string{""}, invalid: true},
		{name: "duplicate vector ID", vector: []string{"private-hit", "private-hit"}, sensitive: "private-hit", invalid: true},
		{name: "same ID across branches is valid", bm25: []string{"shared"}, vector: []string{"shared"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := FuseRRF(test.bm25, test.vector)
			if test.invalid {
				if !errors.Is(err, ErrInvalidRanking) {
					t.Fatalf("FuseRRF() error = %v, want ErrInvalidRanking", err)
				}
				if test.sensitive != "" && strings.Contains(err.Error(), test.sensitive) {
					t.Fatalf("FuseRRF() error exposed an input ID: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FuseRRF() error = %v", err)
			}
			if len(got) != 1 || got[0].BM25Rank != 1 || got[0].VectorRank != 1 {
				t.Fatalf("FuseRRF() = %#v, want one shared hit at rank 1 in each branch", got)
			}
		})
	}
}

func TestFuseRRFDoesNotMutateInputs(t *testing.T) {
	bm25 := []string{"one", "two"}
	vector := []string{"two", "three"}
	bm25Before := append([]string(nil), bm25...)
	vectorBefore := append([]string(nil), vector...)

	if _, err := FuseRRF(bm25, vector); err != nil {
		t.Fatalf("FuseRRF() error = %v", err)
	}
	if !reflect.DeepEqual(bm25, bm25Before) {
		t.Fatalf("FuseRRF() mutated bm25 input: got %v, want %v", bm25, bm25Before)
	}
	if !reflect.DeepEqual(vector, vectorBefore) {
		t.Fatalf("FuseRRF() mutated vector input: got %v, want %v", vector, vectorBefore)
	}
}

func assertRankedHits(t *testing.T, got, want []RankedHit) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("FuseRRF() returned %d hits, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		if got[index].ID != want[index].ID || got[index].BM25Rank != want[index].BM25Rank || got[index].VectorRank != want[index].VectorRank {
			t.Errorf("hit %d = %#v, want %#v", index, got[index], want[index])
		}
		if math.Abs(got[index].Score-want[index].Score) > 1e-12 {
			t.Errorf("hit %d score = %.15f, want %.15f", index, got[index].Score, want[index].Score)
		}
	}
}
