// Package golden contains test-only helpers for evaluating search relevance.
package golden

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type goldenCase struct {
	Query    string   `json:"query"`
	Expected []string `json:"expected"`
	Actual   []string `json:"actual"`
}

type goldenFixture struct {
	Cases []goldenCase `json:"cases"`
}

func TestGoldenFixtureRecall(t *testing.T) {
	fixture, err := loadFixture(filepath.Join("testdata", "golden", "sample.json"))
	if err != nil {
		t.Fatalf("loadFixture() error = %v", err)
	}
	if len(fixture.Cases) != 2 {
		t.Fatalf("fixture cases = %d, want 2", len(fixture.Cases))
	}

	got, err := recallAt3(fixture.Cases)
	if err != nil {
		t.Fatalf("recallAt3() error = %v", err)
	}
	if math.Abs(got-5.0/6.0) > 1e-9 {
		t.Fatalf("recallAt3() = %v, want 5/6", got)
	}
	if !passesRecall(got) {
		t.Fatalf("passesRecall(%v) = false, want true", got)
	}
}

func TestGoldenRecallBelowThreshold(t *testing.T) {
	fixture, err := loadFixture(filepath.Join("testdata", "golden", "sample.json"))
	if err != nil {
		t.Fatalf("loadFixture() error = %v", err)
	}
	if len(fixture.Cases) != 2 {
		t.Fatalf("fixture cases = %d, want 2", len(fixture.Cases))
	}
	fixture.Cases[1].Actual = []string{"d", "x", "y"}

	got, err := recallAt3(fixture.Cases)
	if err != nil {
		t.Fatalf("recallAt3() error = %v", err)
	}
	if math.Abs(got-4.0/6.0) > 1e-9 {
		t.Fatalf("recallAt3() = %v, want 4/6", got)
	}
	if passesRecall(got) {
		t.Fatalf("passesRecall(%v) = true, want false", got)
	}
}

func TestRecallAt3(t *testing.T) {
	tests := []struct {
		name       string
		cases      []goldenCase
		wantRecall float64
		wantErr    bool
	}{
		{
			name:       "duplicate results count once",
			cases:      []goldenCase{{Query: "duplicate", Expected: []string{"a", "b", "c"}, Actual: []string{"a", "a", "b", "c"}}},
			wantRecall: 2.0 / 3.0,
		},
		{
			name:       "fourth result is excluded",
			cases:      []goldenCase{{Query: "fourth", Expected: []string{"a"}, Actual: []string{"x", "y", "z", "a"}}},
			wantRecall: 0,
		},
		{
			name:       "empty results are allowed",
			cases:      []goldenCase{{Query: "empty results", Expected: []string{"a"}}},
			wantRecall: 0,
		},
		{
			name:       "fewer than three results are allowed",
			cases:      []goldenCase{{Query: "short", Expected: []string{"a", "b"}, Actual: []string{"a"}}},
			wantRecall: 0.5,
		},
		{
			name: "recall is macro averaged",
			cases: []goldenCase{
				{Query: "one expected", Expected: []string{"a"}, Actual: []string{"a"}},
				{Query: "three expected", Expected: []string{"b", "c", "d"}, Actual: []string{"b"}},
			},
			wantRecall: 2.0 / 3.0,
		},
		{name: "no queries", wantErr: true},
		{
			name:    "empty expected list",
			cases:   []goldenCase{{Query: "empty expected", Actual: []string{"a"}}},
			wantErr: true,
		},
		{
			name:    "empty expected ID",
			cases:   []goldenCase{{Query: "empty expected ID", Expected: []string{""}}},
			wantErr: true,
		},
		{
			name:    "duplicate expected ID",
			cases:   []goldenCase{{Query: "duplicate expected ID", Expected: []string{"a", "a"}}},
			wantErr: true,
		},
		{
			name:    "empty result ID",
			cases:   []goldenCase{{Query: "empty result ID", Expected: []string{"a"}, Actual: []string{"a", ""}}},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := cloneCases(test.cases)
			got, err := recallAt3(test.cases)
			if test.wantErr {
				if err == nil {
					t.Fatal("recallAt3() error = nil, want an error")
				}
			} else {
				if err != nil {
					t.Fatalf("recallAt3() error = %v", err)
				}
				if math.Abs(got-test.wantRecall) > 1e-9 {
					t.Fatalf("recallAt3() = %v, want %v", got, test.wantRecall)
				}
			}
			if !reflect.DeepEqual(test.cases, before) {
				t.Fatalf("recallAt3() changed input: got %#v, want %#v", test.cases, before)
			}
		})
	}
}

func TestLoadFixtureErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.json")
		if _, err := loadFixture(path); err == nil {
			t.Fatal("loadFixture() error = nil, want an error")
		} else if !strings.Contains(err.Error(), "read fixture") || !strings.Contains(err.Error(), path) {
			t.Fatalf("loadFixture() error = %v, want read context for %q", err, path)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(path, []byte(`{"cases":[`), 0o600); err != nil {
			t.Fatalf("write invalid fixture: %v", err)
		}
		if _, err := loadFixture(path); err == nil {
			t.Fatal("loadFixture() error = nil, want an error")
		} else if !strings.Contains(err.Error(), "decode fixture") || !strings.Contains(err.Error(), path) {
			t.Fatalf("loadFixture() error = %v, want decode context for %q", err, path)
		}
	})
}

func TestPassesRecall(t *testing.T) {
	for _, test := range []struct {
		name   string
		recall float64
		want   bool
	}{
		{name: "threshold is inclusive", recall: 0.8, want: true},
		{name: "below threshold fails", recall: math.Nextafter(0.8, 0), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := passesRecall(test.recall); got != test.want {
				t.Fatalf("passesRecall(%v) = %t, want %t", test.recall, got, test.want)
			}
		})
	}
}

func cloneCases(cases []goldenCase) []goldenCase {
	if cases == nil {
		return nil
	}
	cloned := make([]goldenCase, len(cases))
	for i, testCase := range cases {
		cloned[i] = goldenCase{
			Query:    testCase.Query,
			Expected: append([]string(nil), testCase.Expected...),
			Actual:   append([]string(nil), testCase.Actual...),
		}
	}
	return cloned
}

func loadFixture(path string) (goldenFixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return goldenFixture{}, fmt.Errorf("golden: read fixture %q: %w", path, err)
	}

	var fixture goldenFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return goldenFixture{}, fmt.Errorf("golden: decode fixture %q: %w", path, err)
	}
	return fixture, nil
}

func recallAt3(cases []goldenCase) (float64, error) {
	if len(cases) == 0 {
		return 0, fmt.Errorf("golden: no query cases")
	}

	var totalRecall float64
	for i, testCase := range cases {
		if len(testCase.Expected) == 0 {
			return 0, fmt.Errorf("golden: query %d has no expected IDs", i+1)
		}

		expected := make(map[string]struct{}, len(testCase.Expected))
		for _, id := range testCase.Expected {
			if id == "" {
				return 0, fmt.Errorf("golden: query %d has an empty expected ID", i+1)
			}
			if _, exists := expected[id]; exists {
				return 0, fmt.Errorf("golden: query %d has a duplicate expected ID %q", i+1, id)
			}
			expected[id] = struct{}{}
		}

		for _, id := range testCase.Actual {
			if id == "" {
				return 0, fmt.Errorf("golden: query %d has an empty result ID", i+1)
			}
		}

		top := testCase.Actual
		if len(top) > 3 {
			top = top[:3]
		}
		found := make(map[string]struct{}, len(top))
		for _, id := range top {
			if _, relevant := expected[id]; relevant {
				found[id] = struct{}{}
			}
		}
		totalRecall += float64(len(found)) / float64(len(expected))
	}

	return totalRecall / float64(len(cases)), nil
}

func passesRecall(recall float64) bool { return recall >= 0.8 }
