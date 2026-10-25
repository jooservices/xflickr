package domain

import "testing"

func TestScoreFormula(t *testing.T) {
	got, err := Score(ScoreInput{PlanWeight: 4, DirectAction: true, Starred: true, Depth: 2, MaxDepth: 4, AgeMinutes: 30}, ScoreWeights{PlanWeight: 10, DirectAction: 100, Starred: 20, Depth: 5, AgeMinutes: .1})
	if err != nil {
		t.Fatal(err)
	}
	if got != 173 {
		t.Fatalf("Score() = %v, want 173", got)
	}
}

func TestScoreRejectsNegativeValues(t *testing.T) {
	if _, err := Score(ScoreInput{AgeMinutes: -1}, ScoreWeights{}); err == nil {
		t.Fatal("Score accepted negative age")
	}
	if _, err := Score(ScoreInput{}, ScoreWeights{Depth: -1}); err == nil {
		t.Fatal("Score accepted negative weight")
	}
}
