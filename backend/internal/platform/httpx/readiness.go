package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const readinessCheckTimeout = 2 * time.Second

// DependencyCheck tests whether one required service is reachable.
type DependencyCheck struct {
	Name  string
	Check func(context.Context) error
}

// DependencyStatus reports one dependency without exposing internal error details.
type DependencyStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// ReadinessStatus is the public response returned by GET /readyz.
type ReadinessStatus struct {
	Status       string             `json:"status"`
	Dependencies []DependencyStatus `json:"dependencies"`
}

type roleDependencies struct {
	configured bool
	stopped    bool
	checks     []DependencyCheck
}

// Readiness aggregates the readiness checks for the roles running in one process.
type Readiness struct {
	mu    sync.RWMutex
	roles map[string]roleDependencies
}

// NewReadiness creates an unready entry for each selected role.
func NewReadiness(roles ...string) *Readiness {
	readiness := &Readiness{roles: make(map[string]roleDependencies, len(roles))}
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role != "" {
			readiness.roles[role] = roleDependencies{}
		}
	}
	return readiness
}

// ConfigureRole registers the dependencies required before a role is ready.
func (r *Readiness) ConfigureRole(role string, checks ...DependencyCheck) {
	if r == nil {
		return
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return
	}
	configured := make([]DependencyCheck, 0, len(checks))
	seen := make(map[string]struct{}, len(checks))
	for _, check := range checks {
		name := strings.TrimSpace(check.Name)
		if name == "" || check.Check == nil {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		check.Name = name
		configured = append(configured, check)
	}
	sort.Slice(configured, func(i, j int) bool { return configured[i].Name < configured[j].Name })

	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles[role] = roleDependencies{configured: true, checks: configured}
}

// MarkStopped makes a role unready when it exits before process shutdown.
func (r *Readiness) MarkStopped(role string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.roles[role]
	state.stopped = true
	r.roles[role] = state
}

// Evaluate runs every selected role's required checks and returns a redacted snapshot.
func (r *Readiness) Evaluate(ctx context.Context) (ReadinessStatus, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil {
		return ReadinessStatus{
			Status:       "error",
			Dependencies: []DependencyStatus{{Name: "startup", Status: "error"}},
		}, false
	}

	r.mu.RLock()
	names := make([]string, 0, len(r.roles))
	states := make(map[string]roleDependencies, len(r.roles))
	for name, state := range r.roles {
		names = append(names, name)
		states[name] = state
	}
	r.mu.RUnlock()
	sort.Strings(names)

	type dependencyTask struct {
		resultIndex int
		role        string
		dependency  DependencyCheck
	}
	results := make([]DependencyStatus, 0)
	tasks := make([]dependencyTask, 0)
	for _, role := range names {
		state := states[role]
		switch {
		case state.stopped:
			results = append(results, DependencyStatus{Name: role + ".process", Status: "error"})
		case !state.configured:
			results = append(results, DependencyStatus{Name: role + ".startup", Status: "error"})
		default:
			for _, dependency := range state.checks {
				resultIndex := len(results)
				results = append(results, DependencyStatus{})
				tasks = append(tasks, dependencyTask{resultIndex: resultIndex, role: role, dependency: dependency})
			}
		}
	}

	var group sync.WaitGroup
	group.Add(len(tasks))
	for _, task := range tasks {
		task := task
		go func() {
			defer group.Done()
			checkCtx, cancel := context.WithTimeout(ctx, readinessCheckTimeout)
			defer cancel()
			status := "ok"
			if err := task.dependency.Check(checkCtx); err != nil {
				status = "error"
			}
			results[task.resultIndex] = DependencyStatus{
				Name:   task.role + "." + task.dependency.Name,
				Status: status,
			}
		}()
	}
	group.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	status := "ok"
	for _, result := range results {
		if result.Status != "ok" {
			status = "error"
			break
		}
	}
	return ReadinessStatus{Status: status, Dependencies: results}, status == "ok"
}

// NewProbeHandler exposes liveness and readiness for role and container probes.
func NewProbeHandler(version string, readiness *Readiness) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", NewHealthHandler(version))
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		result, ready := readiness.Evaluate(r.Context())
		w.Header().Set("Content-Type", "application/json")
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	return mux
}
