package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/jooservices/xflickr/internal/cluster/domain"
)

var (
	ErrInvalidNode  = errors.New("cluster: invalid node")
	ErrNodeExists   = errors.New("cluster: node already exists")
	ErrInvalidToken = errors.New("cluster: invalid node token")
)

const maxMetricsBytes = 64 * 1024

var rolePattern = regexp.MustCompile("^[a-z][a-z0-9_-]{0,63}$")

type HeartbeatInput struct {
	Version  string
	EgressIP string
	Metrics  json.RawMessage
}

type Repository interface {
	Create(context.Context, domain.Node, []byte) (domain.Node, error)
	AuthenticateToken(context.Context, []byte) (domain.Node, error)
	Heartbeat(context.Context, string, HeartbeatInput, time.Time) (domain.Node, error)
}

type Nodes struct {
	Repository    Repository
	Now           func() time.Time
	GenerateToken func() (string, error)
}

func (service Nodes) Create(ctx context.Context, name string, roles []string) (domain.Node, string, error) {
	if service.Repository == nil {
		return domain.Node{}, "", errors.New("cluster: node repository is required")
	}
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > 128 || strings.ContainsAny(name, "\x00\r\n") || !validRoles(roles) {
		return domain.Node{}, "", ErrInvalidNode
	}
	generate := service.GenerateToken
	if generate == nil {
		generate = domain.NewNodeToken
	}
	token, err := generate()
	if err != nil {
		return domain.Node{}, "", err
	}
	if !domain.ValidNodeToken(token) {
		return domain.Node{}, "", errors.New("cluster: token generator returned an invalid token")
	}
	created, err := service.Repository.Create(ctx, domain.Node{Name: name, Roles: append([]string(nil), roles...), State: domain.StateOffline, Metrics: json.RawMessage("{}")}, domain.HashNodeToken(token))
	if err != nil {
		return domain.Node{}, "", err
	}
	return created, token, nil
}

func (service Nodes) AuthenticateToken(ctx context.Context, token string) (domain.Node, error) {
	if service.Repository == nil || !domain.ValidNodeToken(token) {
		return domain.Node{}, ErrInvalidToken
	}
	return service.Repository.AuthenticateToken(ctx, domain.HashNodeToken(token))
}

func (service Nodes) Heartbeat(ctx context.Context, name string, input HeartbeatInput) (domain.Node, error) {
	if service.Repository == nil {
		return domain.Node{}, errors.New("cluster: node repository is required")
	}
	input.Version = strings.TrimSpace(input.Version)
	if len(input.Version) > 64 || strings.ContainsAny(input.Version, "\x00\r\n") {
		return domain.Node{}, ErrInvalidNode
	}
	input.EgressIP = strings.TrimSpace(input.EgressIP)
	if input.EgressIP != "" {
		address := net.ParseIP(input.EgressIP)
		if address == nil {
			return domain.Node{}, ErrInvalidNode
		}
		input.EgressIP = address.String()
	}
	if len(input.Metrics) == 0 {
		input.Metrics = json.RawMessage("{}")
	}
	if len(input.Metrics) > maxMetricsBytes || !json.Valid(input.Metrics) {
		return domain.Node{}, ErrInvalidNode
	}
	var metrics map[string]json.RawMessage
	if err := json.Unmarshal(input.Metrics, &metrics); err != nil || metrics == nil {
		return domain.Node{}, ErrInvalidNode
	}
	clock := service.Now
	if clock == nil {
		clock = time.Now
	}
	updated, err := service.Repository.Heartbeat(ctx, name, input, clock().UTC())
	if err != nil {
		return domain.Node{}, fmt.Errorf("cluster: record heartbeat: %w", err)
	}
	return updated, nil
}

func validRoles(roles []string) bool {
	if len(roles) == 0 || len(roles) > 32 {
		return false
	}
	seen := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if !rolePattern.MatchString(role) {
			return false
		}
		if _, exists := seen[role]; exists {
			return false
		}
		seen[role] = struct{}{}
	}
	return true
}
