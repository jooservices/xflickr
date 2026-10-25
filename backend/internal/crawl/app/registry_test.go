package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

func TestRegistryRegisterAndLookup(t *testing.T) {
	registry := NewRegistry()
	facet := &fakeFacet{name: "contacts", lane: domain.LanePerson}
	if err := registry.Register(facet); err != nil {
		t.Fatal(err)
	}
	if got, ok := registry.Get("contacts"); !ok || got != domain.Facet(facet) {
		t.Fatal("registered facet not found")
	}
	if _, ok := registry.Get("missing"); ok {
		t.Fatal("unexpected facet")
	}
	if err := registry.Register(&fakeFacet{name: "sizes", lane: domain.LanePhoto}); err != nil {
		t.Fatal(err)
	}
	if got := registry.Names(); !reflect.DeepEqual(got, []string{"contacts", "sizes"}) {
		t.Fatalf("names = %v", got)
	}
}

func TestRegistryRejectsInvalidFacets(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(&fakeFacet{name: "contacts", lane: domain.LanePerson})
	for name, facet := range map[string]domain.Facet{
		"nil":       nil,
		"blank":     &fakeFacet{name: " ", lane: domain.LanePerson},
		"bad lane":  &fakeFacet{name: "x", lane: "other"},
		"duplicate": &fakeFacet{name: "contacts", lane: domain.LanePerson},
	} {
		if err := registry.Register(facet); !errors.Is(err, domain.ErrInvalidFacet) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestRegistryPlanStampsKindAndDropsInvalidSpecs(t *testing.T) {
	registry := NewRegistry()
	_ = registry.Register(&fakeFacet{name: "b", lane: domain.LanePerson, plan: []domain.WorkSpec{{Subject: "s", Method: "m"}, {Subject: "", Method: "m"}}})
	_ = registry.Register(&fakeFacet{name: "a", lane: domain.LanePerson, plan: []domain.WorkSpec{{Kind: "b", Subject: "s", Method: "m2"}}})
	got := registry.Plan(t.Context(), domain.Person{NSID: "s"})
	want := []domain.WorkSpec{{Kind: "b", Subject: "s", Method: "m2"}, {Kind: "b", Subject: "s", Method: "m"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan = %+v", got)
	}
}
