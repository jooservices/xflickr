package roles

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Role is one long-running XFlickr process role.
type Role interface {
	Name() string
	Run(ctx context.Context) error
}

// Registry stores role implementations by their command-line names.
type Registry struct {
	roles map[string]Role
}

// NewRegistry creates an empty role registry.
func NewRegistry() *Registry {
	return &Registry{roles: make(map[string]Role)}
}

// Register adds a role and rejects invalid or duplicate names.
func (r *Registry) Register(role Role) error {
	if role == nil {
		return fmt.Errorf("cannot register a nil role")
	}
	name := strings.TrimSpace(role.Name())
	if name == "" || strings.ContainsAny(name, ", \t\n") {
		return fmt.Errorf("invalid role name %q", name)
	}
	if _, exists := r.roles[name]; exists {
		return fmt.Errorf("role %q is already registered", name)
	}
	r.roles[name] = role
	return nil
}

// Get returns the role with name, if it is registered.
func (r *Registry) Get(name string) (Role, bool) {
	role, ok := r.roles[name]
	return role, ok
}

// Names returns the registered role names in stable alphabetical order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.roles))
	for name := range r.roles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidRoles formats the names accepted by this registry.
func (r *Registry) ValidRoles() string {
	return strings.Join(r.Names(), ", ")
}

// ParseRoleList resolves comma-separated role names and removes duplicates.
func ParseRoleList(raw string, registry *Registry) ([]Role, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("a role is required; valid roles: %s", registry.ValidRoles())
	}

	selected := make([]Role, 0)
	seen := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		role, ok := registry.Get(name)
		if !ok {
			return nil, fmt.Errorf("unknown role %q; valid roles: %s", name, registry.ValidRoles())
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		selected = append(selected, role)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("a role is required; valid roles: %s", registry.ValidRoles())
	}
	return selected, nil
}
