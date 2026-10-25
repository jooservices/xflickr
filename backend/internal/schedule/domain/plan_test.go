package domain

import (
	"errors"
	"testing"
)

func TestPlanValidate(t *testing.T) {
	valid := Plan{Name: "free", Weight: 1, MaxDepthCap: 1, MaxContacts: 100, AIQuotaPerDay: 0, ZipTTLHours: 24, ResourceShare: .2}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.Name = "" }, func(p *Plan) { p.Weight = 0 }, func(p *Plan) { p.MaxDepthCap = -1 },
		func(p *Plan) { p.MaxContacts = -1 }, func(p *Plan) { p.AIQuotaPerDay = -1 },
		func(p *Plan) { p.ZipTTLHours = -1 }, func(p *Plan) { p.ResourceShare = -0.1 }, func(p *Plan) { p.ResourceShare = 1.1 },
	} {
		p := valid
		mutate(&p)
		if !errors.Is(p.Validate(), ErrInvalidPlan) {
			t.Errorf("Validate(%+v) did not return ErrInvalidPlan", p)
		}
	}
}
