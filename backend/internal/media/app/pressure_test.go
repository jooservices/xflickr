package app

import (
	"context"
	"errors"
	"testing"
)

type pressureStore struct {
	full bool
	err  error
}

func (s pressureStore) Full(context.Context) (bool, error) { return s.full, s.err }

func TestPressureCheck(t *testing.T) {
	for _, test := range []struct {
		name string
		p    Pressure
		ctx  context.Context
		want error
	}{
		{name: "available", p: Pressure{Store: pressureStore{}}, ctx: context.Background()},
		{name: "full", p: Pressure{Store: pressureStore{full: true}}, ctx: context.Background(), want: ErrTempStorageFull},
		{name: "store error", p: Pressure{Store: pressureStore{err: errors.New("stat failed")}}, ctx: context.Background()},
		{name: "missing store", ctx: context.Background()},
		{name: "missing context", p: Pressure{Store: pressureStore{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.p.Check(test.ctx)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("Check() error=%v, want %v", err, test.want)
			}
			if test.want == nil && test.name == "available" && err != nil {
				t.Fatalf("Check() error=%v", err)
			}
			if test.name != "available" && test.name != "full" && test.name != "store error" && err == nil {
				t.Fatal("Check() accepted missing dependency")
			}
		})
	}
	if full, err := (Pressure{Store: pressureStore{full: true}}).Full(context.Background()); err != nil || !full {
		t.Fatalf("Full() = %v, %v", full, err)
	}
	if full, err := (Pressure{Store: pressureStore{err: errors.New("failed")}}).Full(context.Background()); err == nil || full {
		t.Fatalf("Full() = %v, %v", full, err)
	}
}
