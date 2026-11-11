package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
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

func TestCreateNodeRequiresRepository(t *testing.T) {
	if _, _, err := (Nodes{}).Create(t.Context(), "node-1", []string{"crawl"}); err == nil {
		t.Fatal("Create() accepted a missing repository")
	}
}

func TestCreateNodePropagatesGeneratorAndRepositoryErrors(t *testing.T) {
	t.Run("generator failure", func(t *testing.T) {
		want := errors.New("random source unavailable")
		service := Nodes{Repository: &nodeRepositoryFake{}, GenerateToken: func() (string, error) { return "", want }}
		if _, _, err := service.Create(t.Context(), "node-1", []string{"crawl"}); !errors.Is(err, want) {
			t.Fatalf("Create() error = %v, want %v", err, want)
		}
	})

	t.Run("invalid generated token", func(t *testing.T) {
		service := Nodes{Repository: &nodeRepositoryFake{}, GenerateToken: func() (string, error) { return "invalid", nil }}
		if _, _, err := service.Create(t.Context(), "node-1", []string{"crawl"}); err == nil {
			t.Fatal("Create() accepted an invalid generated token")
		}
	})

	t.Run("repository failure", func(t *testing.T) {
		want := errors.New("database unavailable")
		service := Nodes{
			Repository:    &nodeRepositoryFake{createErr: want},
			GenerateToken: func() (string, error) { return testToken(), nil },
		}
		if _, _, err := service.Create(t.Context(), "node-1", []string{"crawl"}); !errors.Is(err, want) {
			t.Fatalf("Create() error = %v, want %v", err, want)
		}
	})

	t.Run("default token generator", func(t *testing.T) {
		if _, token, err := (Nodes{Repository: &nodeRepositoryFake{}}).Create(t.Context(), "node-1", []string{"crawl"}); err != nil || !domain.ValidNodeToken(token) {
			t.Fatalf("Create() token valid = %t, error = %v", domain.ValidNodeToken(token), err)
		}
	})
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
	checker := &duplicateCheckerFake{}
	service := Nodes{Repository: &nodeRepositoryFake{heartbeatErr: want}, DuplicateChecker: checker}
	_, err := service.Heartbeat(t.Context(), "node-1", HeartbeatInput{})
	if !errors.Is(err, want) {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if checker.calls != 0 {
		t.Fatal("duplicate checker ran after the heartbeat write failed")
	}
}

func TestHeartbeatChecksDuplicateAfterSave(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 123456000, time.UTC)
	events := make([]string, 0, 2)
	repository := &nodeRepositoryFake{events: &events}
	checker := &duplicateCheckerFake{events: &events}
	service := Nodes{
		Repository:       repository,
		DuplicateChecker: checker,
		Now:              func() time.Time { return now },
	}

	updated, err := service.Heartbeat(t.Context(), "node-1", HeartbeatInput{EgressIP: "192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "heartbeat,duplicate" {
		t.Fatalf("heartbeat events = %v, want save followed by duplicate check", events)
	}
	if checker.calls != 1 || checker.node.Name != updated.Name || checker.node.EgressIP != updated.EgressIP || !checker.at.Equal(now) {
		t.Fatalf("duplicate check received node %+v at %s, want saved node %+v at %s", checker.node, checker.at, updated, now)
	}
}

func TestHeartbeatSurvivesDuplicateCheckFailure(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	token := "token-" + testToken()
	repository := &nodeRepositoryFake{}
	checker := &duplicateCheckerFake{err: errors.New("database failure " + token)}
	var logs bytes.Buffer
	service := Nodes{
		Repository:       repository,
		DuplicateChecker: checker,
		Logger:           slog.New(slog.NewJSONHandler(&logs, nil)),
		Now:              func() time.Time { return now },
	}

	updated, err := service.Heartbeat(t.Context(), "node-1", HeartbeatInput{})
	if err != nil || updated.Name != "node-1" {
		t.Fatalf("Heartbeat() = %+v, %v; duplicate failure must not fail the saved heartbeat", updated, err)
	}
	if checker.calls != 1 {
		t.Fatalf("duplicate checker calls = %d, want 1", checker.calls)
	}
	if strings.Contains(logs.String(), token) {
		t.Fatal("duplicate check log contains token material")
	}
	if !strings.Contains(logs.String(), "duplicate egress check failed") {
		t.Fatalf("duplicate check failure was not logged: %s", logs.String())
	}
}

func TestAssignInitialValidatesInputAndUsesClock(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 123456000, time.UTC)
	want := Assignment{KeyID: 7, Node: "node-1", Since: now, Reason: "initial"}

	t.Run("invalid input does not reach repository", func(t *testing.T) {
		for _, test := range []struct {
			name  string
			keyID int64
			node  string
		}{
			{name: "nonpositive key", keyID: 0, node: "node-1"},
			{name: "empty node", keyID: 7, node: ""},
		} {
			t.Run(test.name, func(t *testing.T) {
				repository := &assignmentRepositoryFake{}
				service := Assignments{Repository: repository}
				if _, err := service.AssignInitial(t.Context(), test.keyID, test.node); !errors.Is(err, ErrAssignmentUnavailable) {
					t.Fatalf("AssignInitial() error = %v", err)
				}
				if repository.assignCalls != 0 {
					t.Fatal("invalid input reached the repository")
				}
			})
		}
	})

	t.Run("repository is required", func(t *testing.T) {
		if _, err := (Assignments{}).AssignInitial(t.Context(), 7, "node-1"); err == nil {
			t.Fatal("AssignInitial() accepted a nil repository")
		}
	})

	t.Run("injected clock", func(t *testing.T) {
		repository := &assignmentRepositoryFake{assignResult: want}
		service := Assignments{Repository: repository, Now: func() time.Time { return now }}
		got, err := service.AssignInitial(t.Context(), want.KeyID, want.Node)
		if err != nil {
			t.Fatal(err)
		}
		if got != want || !repository.assignNow.Equal(now) {
			t.Fatalf("assignment = %+v at %s, want %+v at %s", got, repository.assignNow, want, now)
		}
	})

	t.Run("default clock", func(t *testing.T) {
		repository := &assignmentRepositoryFake{assignResult: Assignment{KeyID: 7, Node: "node-1", Reason: "initial"}}
		if _, err := (Assignments{Repository: repository}).AssignInitial(t.Context(), 7, "node-1"); err != nil {
			t.Fatal(err)
		}
		if repository.assignNow.IsZero() {
			t.Fatal("AssignInitial() did not use its default clock")
		}
	})

	t.Run("repository error is preserved", func(t *testing.T) {
		wantErr := context.Canceled
		service := Assignments{Repository: &assignmentRepositoryFake{assignErr: wantErr}, Now: func() time.Time { return now }}
		if _, err := service.AssignInitial(t.Context(), 7, "node-1"); !errors.Is(err, wantErr) {
			t.Fatalf("AssignInitial() error = %v, want %v", err, wantErr)
		}
	})
}

func TestQueueForAssignment(t *testing.T) {
	t.Run("returns logical node queue", func(t *testing.T) {
		repository := &assignmentRepositoryFake{getResult: Assignment{KeyID: 7, Node: "node-1"}}
		queue, err := (Assignments{Repository: repository}).QueueFor(t.Context(), 7)
		if err != nil {
			t.Fatal(err)
		}
		if queue != "crawl@node-1" || repository.getKeyID != 7 {
			t.Fatalf("QueueFor() = %q for key %d", queue, repository.getKeyID)
		}
	})

	t.Run("missing assignment", func(t *testing.T) {
		repository := &assignmentRepositoryFake{getErr: ErrAssignmentUnavailable}
		if _, err := (Assignments{Repository: repository}).QueueFor(t.Context(), 7); !errors.Is(err, ErrAssignmentUnavailable) {
			t.Fatalf("QueueFor() error = %v", err)
		}
	})

	t.Run("repository cancellation", func(t *testing.T) {
		repository := &assignmentRepositoryFake{getErr: context.Canceled}
		if _, err := (Assignments{Repository: repository}).QueueFor(t.Context(), 7); !errors.Is(err, context.Canceled) {
			t.Fatalf("QueueFor() error = %v", err)
		}
	})

	t.Run("rejects invalid key before lookup", func(t *testing.T) {
		repository := &assignmentRepositoryFake{}
		if _, err := (Assignments{Repository: repository}).QueueFor(t.Context(), 0); !errors.Is(err, ErrAssignmentUnavailable) {
			t.Fatalf("QueueFor() error = %v", err)
		}
		if repository.getCalls != 0 {
			t.Fatal("invalid key reached the repository")
		}
	})

	t.Run("repository is required", func(t *testing.T) {
		if _, err := (Assignments{}).QueueFor(t.Context(), 7); err == nil {
			t.Fatal("QueueFor() accepted a nil repository")
		}
	})
}

type assignmentRepositoryFake struct {
	assignResult Assignment
	getResult    Assignment
	assignErr    error
	getErr       error
	assignCalls  int
	getCalls     int
	assignKeyID  int64
	assignNode   string
	assignNow    time.Time
	getKeyID     int64
}

func (repository *assignmentRepositoryFake) AssignInitial(_ context.Context, keyID int64, node string, now time.Time) (Assignment, error) {
	repository.assignCalls++
	repository.assignKeyID = keyID
	repository.assignNode = node
	repository.assignNow = now
	return repository.assignResult, repository.assignErr
}

func (repository *assignmentRepositoryFake) GetAssignment(_ context.Context, keyID int64) (Assignment, error) {
	repository.getCalls++
	repository.getKeyID = keyID
	return repository.getResult, repository.getErr
}

type nodeRepositoryFake struct {
	created           domain.Node
	createErr         error
	tokenHash         []byte
	authenticatedHash []byte
	heartbeatInput    HeartbeatInput
	heartbeatAt       time.Time
	heartbeatErr      error
	events            *[]string
}

type duplicateCheckerFake struct {
	node   domain.Node
	at     time.Time
	err    error
	calls  int
	events *[]string
}

func (checker *duplicateCheckerFake) CheckDuplicate(_ context.Context, node domain.Node, at time.Time) error {
	checker.calls++
	checker.node = node
	checker.at = at
	if checker.events != nil {
		*checker.events = append(*checker.events, "duplicate")
	}
	return checker.err
}

func (repository *nodeRepositoryFake) Create(_ context.Context, node domain.Node, hash []byte) (domain.Node, error) {
	repository.created = node
	repository.tokenHash = append([]byte(nil), hash...)
	return node, repository.createErr
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
	if repository.events != nil {
		*repository.events = append(*repository.events, "heartbeat")
	}
	return domain.Node{Name: name, EgressIP: input.EgressIP, State: domain.StateOnline, LastHeartbeatAt: &at, Metrics: input.Metrics}, nil
}

func testToken() string {
	return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
}
