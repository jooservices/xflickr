package domain

import (
	"math"
	"math/rand/v2"
	"sort"
	"testing"
	"time"
)

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

func TestScoreRejectsNonFiniteAndOverflow(t *testing.T) {
	if _, err := Score(ScoreInput{}, ScoreWeights{PlanWeight: math.NaN()}); err == nil {
		t.Fatal("Score accepted NaN weight")
	}
	if _, err := Score(ScoreInput{}, ScoreWeights{AgeMinutes: math.Inf(1)}); err == nil {
		t.Fatal("Score accepted infinite weight")
	}
	if _, err := Score(ScoreInput{AgeMinutes: math.MaxFloat64}, ScoreWeights{AgeMinutes: math.MaxFloat64}); err == nil {
		t.Fatal("Score accepted overflow to infinity")
	}
	if _, err := Score(ScoreInput{PlanWeight: math.MaxInt}, ScoreWeights{PlanWeight: math.MaxFloat64}); err == nil {
		t.Fatal("Score accepted base overflow to infinity")
	}
	created := time.Unix(1_700_000_000, 0).UTC()
	if _, err := StaticScore(StaticScoreInput{CreatedAt: created, Depth: -1, MaxDepth: 0}, ScoreWeights{}); err == nil {
		t.Fatal("StaticScore accepted negative depth")
	}
	if _, err := StaticScore(StaticScoreInput{CreatedAt: created, PlanWeight: math.MaxInt}, ScoreWeights{PlanWeight: math.MaxFloat64}); err == nil {
		t.Fatal("StaticScore accepted base overflow to infinity")
	}
	if _, err := StaticScore(StaticScoreInput{CreatedAt: time.Unix(120, 0).UTC()}, ScoreWeights{AgeMinutes: math.MaxFloat64}); err == nil {
		t.Fatal("StaticScore accepted age-term overflow to infinity")
	}
}

func TestStaticScoreOrderingMatchesAgedScore(t *testing.T) {
	rng := rand.New(rand.NewPCG(370, 1))
	weights := ScoreWeights{PlanWeight: 10, DirectAction: 100, Starred: 20, Depth: 5, AgeMinutes: .1}
	type row struct {
		id    int
		input StaticScoreInput
	}
	rows := make([]row, 1000)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range rows {
		rows[i] = row{id: i, input: StaticScoreInput{
			PlanWeight: rng.IntN(5) + 1, DirectAction: rng.IntN(2) == 0, Starred: rng.IntN(2) == 0,
			Depth: rng.IntN(4), MaxDepth: 4, CreatedAt: base.Add(time.Duration(rng.IntN(60*24*30)) * time.Minute),
		}}
	}
	for trial := 0; trial < 20; trial++ {
		// "now" is always after every created_at so AgeMinutes stays non-negative.
		now := base.Add(60 * 24 * 60 * time.Minute).Add(time.Duration(rng.IntN(60*24*30)) * time.Minute)
		agedOrder := append([]row(nil), rows...)
		staticOrder := append([]row(nil), rows...)
		byScore := func(scoreI, scoreJ float64, idI, idJ int) bool {
			const eps = 1e-6
			if math.Abs(scoreI-scoreJ) > eps {
				return scoreI > scoreJ
			}
			return idI < idJ
		}
		sort.SliceStable(agedOrder, func(i, j int) bool {
			// Use whole-second age so the proof matches StaticScore's Unix epoch term.
			ageI := float64(now.Unix()-agedOrder[i].input.CreatedAt.Unix()) / 60.0
			ageJ := float64(now.Unix()-agedOrder[j].input.CreatedAt.Unix()) / 60.0
			scoreI, err := Score(ScoreInput{
				PlanWeight: agedOrder[i].input.PlanWeight, DirectAction: agedOrder[i].input.DirectAction,
				Starred: agedOrder[i].input.Starred, Depth: agedOrder[i].input.Depth,
				MaxDepth: agedOrder[i].input.MaxDepth, AgeMinutes: ageI,
			}, weights)
			if err != nil {
				t.Fatal(err)
			}
			scoreJ, err := Score(ScoreInput{
				PlanWeight: agedOrder[j].input.PlanWeight, DirectAction: agedOrder[j].input.DirectAction,
				Starred: agedOrder[j].input.Starred, Depth: agedOrder[j].input.Depth,
				MaxDepth: agedOrder[j].input.MaxDepth, AgeMinutes: ageJ,
			}, weights)
			if err != nil {
				t.Fatal(err)
			}
			return byScore(scoreI, scoreJ, agedOrder[i].id, agedOrder[j].id)
		})
		sort.SliceStable(staticOrder, func(i, j int) bool {
			scoreI, err := StaticScore(staticOrder[i].input, weights)
			if err != nil {
				t.Fatal(err)
			}
			scoreJ, err := StaticScore(staticOrder[j].input, weights)
			if err != nil {
				t.Fatal(err)
			}
			return byScore(scoreI, scoreJ, staticOrder[i].id, staticOrder[j].id)
		})
		for i := range rows {
			if agedOrder[i].id != staticOrder[i].id {
				t.Fatalf("trial %d index %d: aged id=%d static id=%d", trial, i, agedOrder[i].id, staticOrder[i].id)
			}
		}
	}
}

func TestStaticScoreChangesOnlyWhenInputsChange(t *testing.T) {
	weights := ScoreWeights{PlanWeight: 10, DirectAction: 100, Starred: 20, Depth: 5, AgeMinutes: .1}
	created := time.Unix(1_700_000_000, 0).UTC()
	base := StaticScoreInput{PlanWeight: 2, DirectAction: true, Starred: false, Depth: 1, MaxDepth: 4, CreatedAt: created}
	first, err := StaticScore(base, weights)
	if err != nil {
		t.Fatal(err)
	}
	sameLater, err := StaticScore(base, weights)
	if err != nil || sameLater != first {
		t.Fatalf("identical inputs changed score: %v -> %v (%v)", first, sameLater, err)
	}
	changed := base
	changed.Starred = true
	second, err := StaticScore(changed, weights)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("star change left static score unchanged")
	}
	weightChange := weights
	weightChange.PlanWeight = 11
	third, err := StaticScore(base, weightChange)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("weight change left static score unchanged")
	}
	if _, err := StaticScore(StaticScoreInput{}, weights); err == nil {
		t.Fatal("StaticScore accepted zero created_at")
	}
}

func TestWeightsFingerprintStableUntilInputsChange(t *testing.T) {
	weights := ScoreWeights{PlanWeight: 1, DirectAction: 2, Starred: 3, Depth: 4, AgeMinutes: .5}
	first := WeightsFingerprint(weights, 4)
	if first != WeightsFingerprint(weights, 4) {
		t.Fatal("fingerprint changed for identical inputs")
	}
	if first == WeightsFingerprint(weights, 5) {
		t.Fatal("max depth change left fingerprint unchanged")
	}
	weights.AgeMinutes = .6
	if first == WeightsFingerprint(weights, 4) {
		t.Fatal("age weight change left fingerprint unchanged")
	}
	if math.IsNaN(1) {
		t.Fatal("unreachable")
	}
}
