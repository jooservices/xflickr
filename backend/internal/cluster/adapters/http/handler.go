package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jooservices/xflickr/internal/cluster/app"
	"github.com/jooservices/xflickr/internal/cluster/domain"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type Service interface {
	Create(context.Context, string, []string) (domain.Node, string, error)
	AuthenticateToken(context.Context, string) (domain.Node, error)
	Heartbeat(context.Context, string, app.HeartbeatInput) (domain.Node, error)
}

type nodeContextKey struct{}

type Handler struct {
	Service Service
	Logger  *slog.Logger
	Now     func() time.Time
}

func NodeFromContext(ctx context.Context) (domain.Node, bool) {
	node, ok := ctx.Value(nodeContextKey{}).(domain.Node)
	return node, ok && node.Name != ""
}

// NodeAuthMiddleware authenticates the opaque bearer token and adds the node
// identity to the request context without logging the credential.
func NodeAuthMiddleware(service Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if service == nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "Internal server error")
				return
			}
			scheme, token, found := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
			if !found || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "Node token required")
				return
			}
			node, err := service.AuthenticateToken(r.Context(), token)
			if errors.Is(err, app.ErrInvalidToken) {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "Node token is invalid")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "Internal server error")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), nodeContextKey{}, node)))
		})
	}
}

// PublicInternalPathBlocker keeps internal endpoints off the public API
// listener, even if a future contract adds another internal operation.
func PublicInternalPathBlocker(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal" || strings.HasPrefix(r.URL.Path, "/internal/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// InternalHeartbeatOnly restricts the node listener to its single operation.
func InternalHeartbeatOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/nodes/heartbeat" {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (handler Handler) CreateAdminNode(ctx context.Context, request gen.CreateAdminNodeRequestObject) (gen.CreateAdminNodeResponseObject, error) {
	if handler.Service == nil || request.Body == nil {
		return gen.CreateAdminNode400JSONResponse{Code: "invalid_request", Message: "A node name and at least one role are required"}, nil
	}
	node, token, err := handler.Service.Create(ctx, request.Body.Name, request.Body.Roles)
	switch {
	case errors.Is(err, app.ErrInvalidNode):
		return gen.CreateAdminNode400JSONResponse{Code: "invalid_node", Message: "The node name or roles are invalid"}, nil
	case errors.Is(err, app.ErrNodeExists):
		return gen.CreateAdminNode409JSONResponse{Code: "node_exists", Message: "A node with this name already exists"}, nil
	case err != nil:
		return nil, err
	default:
		return gen.CreateAdminNode201JSONResponse(gen.CreatedNode{
			Name: node.Name, Roles: node.Roles, State: gen.CreatedNodeState(node.EffectiveState(handler.now())), Token: token,
		}), nil
	}
}

func (handler Handler) HeartbeatNode(ctx context.Context, request gen.HeartbeatNodeRequestObject) (gen.HeartbeatNodeResponseObject, error) {
	node, ok := NodeFromContext(ctx)
	if !ok {
		return gen.HeartbeatNode401JSONResponse{Code: "unauthenticated", Message: "Node token required"}, nil
	}
	if handler.Service == nil || request.Body == nil {
		return gen.HeartbeatNode400JSONResponse{Code: "invalid_request", Message: "Heartbeat body is required"}, nil
	}
	input := app.HeartbeatInput{}
	if request.Body.Version != nil {
		input.Version = *request.Body.Version
	}
	if request.Body.EgressIP != nil {
		input.EgressIP = *request.Body.EgressIP
	}
	if request.Body.Metrics != nil {
		encoded, err := json.Marshal(*request.Body.Metrics)
		if err != nil {
			return gen.HeartbeatNode400JSONResponse{Code: "invalid_node", Message: "Heartbeat metrics are invalid"}, nil
		}
		input.Metrics = encoded
	}
	updated, err := handler.Service.Heartbeat(ctx, node.Name, input)
	if errors.Is(err, app.ErrInvalidNode) {
		return gen.HeartbeatNode400JSONResponse{Code: "invalid_node", Message: "Heartbeat metadata is invalid"}, nil
	}
	if errors.Is(err, app.ErrInvalidToken) {
		return gen.HeartbeatNode401JSONResponse{Code: "unauthenticated", Message: "Node is disabled or no longer registered"}, nil
	}
	if err != nil {
		return nil, err
	}
	if updated.LastHeartbeatAt == nil {
		return nil, errors.New("cluster: repository returned a heartbeat without a timestamp")
	}
	state := updated.EffectiveState(handler.now())
	logger := handler.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.InfoContext(ctx, "node.heartbeat", "node", updated.Name, "state", string(state), "version", updated.Version)
	return gen.HeartbeatNode200JSONResponse(gen.NodeHeartbeatResponse{
		Name: updated.Name, State: gen.NodeHeartbeatResponseState(state), LastHeartbeatAt: *updated.LastHeartbeatAt,
	}), nil
}

func (handler Handler) now() time.Time {
	if handler.Now != nil {
		return handler.Now().UTC()
	}
	return time.Now().UTC()
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gen.Error{Code: code, Message: message})
}
