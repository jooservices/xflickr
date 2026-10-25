package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/jooservices/xflickr/internal/crawl/domain"
)

// Registry holds the facets the engine can run, keyed by name.
type Registry struct {
	mu     sync.RWMutex
	facets map[string]domain.Facet
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{facets: map[string]domain.Facet{}}
}

// Register adds a facet. Names are unique and the lane must be supported.
func (r *Registry) Register(facet domain.Facet) error {
	if facet == nil || strings.TrimSpace(facet.Name()) == "" || !facet.Lane().Valid() {
		return domain.ErrInvalidFacet
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.facets[facet.Name()]; exists {
		return fmt.Errorf("%w: %q is already registered", domain.ErrInvalidFacet, facet.Name())
	}
	r.facets[facet.Name()] = facet
	return nil
}

// Get returns the facet registered under name.
func (r *Registry) Get(name string) (domain.Facet, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	facet, ok := r.facets[name]
	return facet, ok
}

// Names lists registered facet names in order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.facets))
	for name := range r.facets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Plan asks every facet which work it wants for person. Specs without a kind
// are stamped with the planning facet's name; invalid specs are dropped.
func (r *Registry) Plan(ctx context.Context, person domain.Person) []domain.WorkSpec {
	var specs []domain.WorkSpec
	for _, name := range r.Names() {
		facet, _ := r.Get(name)
		for _, spec := range facet.Plan(ctx, person) {
			if spec.Kind == "" {
				spec.Kind = name
			}
			if spec.Validate() == nil {
				specs = append(specs, spec)
			}
		}
	}
	return specs
}
