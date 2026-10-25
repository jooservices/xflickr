package domain

import (
	"errors"
	"testing"
)

func TestClassValid(t *testing.T) {
	for _, class := range []Class{Interactive, Person, Photo} {
		if !class.Valid() {
			t.Errorf("%q should be valid", class)
		}
	}
	if Class("unknown").Valid() {
		t.Fatal("unknown class should be invalid")
	}
}

func TestBudgetValidate(t *testing.T) {
	valid := Budget{HourlyLimit: 100, PerSecondRate: 4, Burst: 5, Reserve: 10, PersonLaneShare: 0.3}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid budget: %v", err)
	}
	for name, budget := range map[string]Budget{
		"hourly":    {PerSecondRate: 1, Burst: 1},
		"rate":      {HourlyLimit: 1, Burst: 1},
		"burst":     {HourlyLimit: 1, PerSecondRate: 1},
		"reserve":   {HourlyLimit: 1, PerSecondRate: 1, Burst: 1, Reserve: -1},
		"share low": {HourlyLimit: 1, PerSecondRate: 1, Burst: 1, PersonLaneShare: -0.1},
		"share high": {
			HourlyLimit: 1, PerSecondRate: 1, Burst: 1,
			PersonLaneShare: 1.1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := budget.Validate(); !errors.Is(err, ErrInvalidBudget) {
				t.Fatalf("Validate() = %v", err)
			}
		})
	}
}

func TestQuotaError(t *testing.T) {
	wrapped := errors.Join(errors.New("request failed"), errors.New("quota exceeded"))
	if QuotaError(Meta{Err: wrapped}, errors.New("quota exceeded")) {
		t.Fatal("different sentinel errors should not match")
	}
	marker := errors.New("quota")
	if !QuotaError(Meta{Err: fmtWrap(marker)}, marker) {
		t.Fatal("wrapped quota error was not recognized")
	}
	if QuotaError(Meta{}, marker) {
		t.Fatal("empty result was recognized as quota error")
	}
}

func fmtWrap(err error) error { return errors.Join(errors.New("call failed"), err) }
