package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jooservices/xflickr/internal/cluster/domain"
)

func TestCreateNodeStoresOnlyTokenHash(t *testing.T) {
	token := testToken()
	repository := &nodeRepositoryFake{}
	service := Nodes{Repository: repository, GenerateToken: func() (string, error) { return token, nil }}
	node, raw, err := service.Create(t.Context(), "  worker-1 ", []string{"crawl", "media"})
	if err != nil {
		t.Fatal(err)
	}
	if raw != token || node.Name != "worker-1" || node.State != domain.StateOffline {
		t.Fatalf("created node = %+v, token returned = %t", node, raw == token)
	}
	if !bytes.Equal(repository.tokenHash, domain.HashNodeToken(token)) || bytes.Contains(repository.tokenHash, []byte(token)) {
		t.Fatal("repository did not receive a one-way token hash")
	}
	if len(node.Roles) != 2 || repository.created.Name != node.Name {
		t.Fatalf("node data = %+v", node)
	}
}

func TestCreateNodeRejectsInvalidInputBeforeGeneratingToken(t *testing.T) {
	for _, test := range []struct {
		name  string
		roles []string
	}{
		{name: "", roles: []string{"crawl"}},
		{name: "valid", roles: nil},
		{name: "valid", roles: []string{"crawl", "crawl"}},
		{name: "valid", roles: []string{"Crawl"}},
		{name: "bad\nname", roles: []string{"crawl"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			generated := false
			service := Nodes{Repository: &nodeRepositoryFake{}, GenerateToken: func() (string, error) {
				generated = true
				return testToken(), nil
			}}
			if _, _, err := service.Create(t.Context(), test.name, test.roles); !errors.Is(err, ErrInvalidNode) {
				t.Fatalf("Create() error = %v", err)
			}
			if generated {
				t.Fatal("invalid input generated a token")
			}
		})
	}
}

func TestAuthenticateNodeTokenValidatesFormatAndHashes(t *testing.T) {
	token := testToken()
	repository := &nodeRepositoryFake{}
	service := Nodes{Repository: repository}
	if _, err := service.AuthenticateToken(t.Context(), "invalid"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("invalid token error = %v", err)
	}
	if repository.authenticatedHash != nil {
		t.Fatal("malformed token reached repository")
	}
	if _, err := service.AuthenticateToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(repository.authenticatedHash, domain.HashNodeToken(token)) {
		t.Fatal("repository did not receive the token hash")
	}
}

func TestHeartbeatValidatesAndNormalizesPayload(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	repository := &nodeRepositoryFake{}
	service := Nodes{Repository: repository, Now: func() time.Time { return now }}
	node, err := service.Heartbeat(t.Context(), "node-1", HeartbeatInput{Version: " v1 ", EgressIP: " 192.0.2.1 ", Metrics: json.RawMessage(`{"cpu":0.5}`)})
	if err != nil {
		t.Fatal(err)
	}
	if node.Name != "node-1" || !repository.heartbeatAt.Equal(now) || repository.heartbeatInput.Version != "v1" || repository.heartbeatInput.EgressIP != "192.0.2.1" {
		t.Fatalf("heartbeat input = %+v at %s", repository.heartbeatInput, repository.heartbeatAt)
	}
	for _, input := range []HeartbeatInput{
		{EgressIP: "not-an-ip"},
		{Version: string(bytes.Repeat([]byte{'x'}, 65))},
		{Metrics: json.RawMessage("[]")},
		{Metrics: json.RawMessage(`{"broken"`)},
		{Metrics: json.RawMessage("null")},
	} {
		if _, err := service.Heartbeat(t.Context(), "node-1", input); !errors.Is(err, ErrInvalidNode) {
			t.Errorf("Heartbeat(%+v) error = %v", input, err)
		}
	}
}

func TestHeartbeatWrapsRepositoryErrors(t *testing.T) {
	want := errors.New("database unavailable")
	service := Nodes{Repository: &nodeRepositoryFake{heartbeatErr: want}}
	_, err := service.Heartbeat(t.Context(), "node-1", HeartbeatInput{})
	if !errors.Is(err, want) {
		t.Fatalf("Heartbeat() error = %v", err)
	}
}

type nodeRepositoryFake struct {
	created           domain.Node
	tokenHash         []byte
	authenticatedHash []byte
	heartbeatInput    HeartbeatInput
	heartbeatAt       time.Time
	heartbeatErr      error
}

func (repository *nodeRepositoryFake) Create(_ context.Context, node domain.Node, hash []byte) (domain.Node, error) {
	repository.created = node
	repository.tokenHash = append([]byte(nil), hash...)
	return node, nil
}

func (repository *nodeRepositoryFake) AuthenticateToken(_ context.Context, hash []byte) (domain.Node, error) {
	repository.authenticatedHash = append([]byte(nil), hash...)
	return domain.Node{Name: "node-1"}, nil
}

func (repository *nodeRepositoryFake) Heartbeat(_ context.Context, name string, input HeartbeatInput, at time.Time) (domain.Node, error) {
	repository.heartbeatInput = input
	repository.heartbeatAt = at
	if repository.heartbeatErr != nil {
		return domain.Node{}, repository.heartbeatErr
	}
	return domain.Node{Name: name, State: domain.StateOnline, LastHeartbeatAt: &at, Metrics: input.Metrics}, nil
}

func testToken() string {
	return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
}
