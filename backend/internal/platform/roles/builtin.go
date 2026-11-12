package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	clusterdomain "github.com/jooservices/xflickr/internal/cluster/domain"
	identityhttp "github.com/jooservices/xflickr/internal/identity/adapters/http"
	opsdomain "github.com/jooservices/xflickr/internal/ops/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
	"github.com/jooservices/xflickr/internal/platform/config"
	"github.com/jooservices/xflickr/internal/platform/db"
)

// openRoleDatabase keeps process startup injectable in unit tests without
// connecting to a shared or live database.
var openRoleDatabase = db.NewPool

var builtinRoleNames = []string{"api", "scheduler", "indexer", "node-agent", "crawl", "media", "sync", "ai-worker"}

// NewBuiltinRegistry creates the role set supported by the xflickr binary.
func NewBuiltinRegistry(version string) *Registry {
	registry := NewRegistry()
	registerBuiltin(registry, newConfiguredAPIRole(apiAddress(), version))
	for _, name := range builtinRoleNames[1:] {
		if name == "scheduler" {
			registerBuiltin(registry, schedulerRole{})
			continue
		}
		if name == "crawl" {
			registerBuiltin(registry, crawlRole{})
			continue
		}
		if name == "sync" {
			registerBuiltin(registry, syncRole{})
			continue
		}
		if name == "media" {
			registerBuiltin(registry, mediaRole{})
			continue
		}
		registerBuiltin(registry, metricsWrappedRole{inner: idleRole{name: name}, roleName: name})
	}
	return registry
}

func registerBuiltin(registry *Registry, role Role) {
	if err := registry.Register(role); err != nil {
		panic(err)
	}
}

type idleRole struct {
	name string
}

func (r idleRole) Name() string { return r.name }

func (r idleRole) Run(ctx context.Context) error {
	configureRoleReadiness(ctx, r.name)
	<-ctx.Done()
	return nil
}

type duplicateEgressChecker struct {
	nodes     duplicateEgressFinder
	incidents duplicateIncidentOpener
}

type duplicateEgressFinder interface {
	FindDuplicateEgress(context.Context, clusterdomain.Node, time.Time) (string, error)
}

type duplicateIncidentOpener interface {
	Open(context.Context, opsdomain.Incident) (opsdomain.Incident, error)
}

func (checker duplicateEgressChecker) CheckDuplicate(ctx context.Context, node clusterdomain.Node, at time.Time) error {
	if node.EgressIP == "" || node.EffectiveState(at) != clusterdomain.StateOnline || !slices.Contains(node.Roles, "crawl") {
		return nil
	}
	duplicate, err := checker.nodes.FindDuplicateEgress(ctx, node, at)
	if err != nil {
		return fmt.Errorf("cluster: check duplicate egress IP: %w", err)
	}
	if duplicate == "" {
		return nil
	}
	names := []string{node.Name, duplicate}
	sort.Strings(names)
	_, err = checker.incidents.Open(ctx, opsdomain.Incident{
		Kind:     "duplicate_egress_ip",
		Scope:    fmt.Sprintf("%d:%s|%d:%s", len(names[0]), names[0], len(names[1]), names[1]),
		Severity: opsdomain.SeverityWarning,
		Title:    "Duplicate egress IP",
		Detail:   json.RawMessage("{}"),
	})
	if err != nil {
		return fmt.Errorf("cluster: open duplicate egress incident: %w", err)
	}
	return nil
}

// nodeID names this node in quota reports: NODE_ID, or the host name.
func nodeID(cfg config.Config) string {
	return nodeIDWithHostname(cfg, os.Hostname)
}

func nodeIDWithHostname(cfg config.Config, hostname func() (string, error)) string {
	if id := strings.TrimSpace(cfg.Node.ID); id != "" {
		return id
	}
	if host, err := hostname(); err == nil && host != "" {
		return host
	}
	return "unknown"
}

var _ gen.StrictServerInterface = apiServer{}

func userIDFromPrincipal(ctx context.Context) (int64, bool) {
	principal, ok := identityhttp.PrincipalFrom(ctx)
	if !ok || principal.User.ID == 0 {
		return 0, false
	}
	return principal.User.ID, true
}
