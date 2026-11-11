package app

import (
	"context"
	"errors"
	"time"
)

// ErrAssignmentUnavailable indicates that a key or eligible node is unavailable.
var ErrAssignmentUnavailable = errors.New("cluster: assignment unavailable")

// Assignment is the immutable initial node assignment for an API key.
type Assignment struct {
	KeyID  int64
	Node   string
	Since  time.Time
	Reason string
}

// AssignmentRepository stores and reads key-to-node assignments.
type AssignmentRepository interface {
	AssignInitial(context.Context, int64, string, time.Time) (Assignment, error)
	GetAssignment(context.Context, int64) (Assignment, error)
}

// Assignments provides the internal key assignment and queue lookup API.
type Assignments struct {
	Repository AssignmentRepository
	Now        func() time.Time
}

// AssignInitial stores the initial assignment to an eligible node. Existing
// assignments are returned unchanged.
func (service Assignments) AssignInitial(ctx context.Context, keyID int64, node string) (Assignment, error) {
	if service.Repository == nil {
		return Assignment{}, errors.New("cluster: assignment repository is required")
	}
	if keyID <= 0 || node == "" {
		return Assignment{}, ErrAssignmentUnavailable
	}
	now := service.Now
	if now == nil {
		now = time.Now
	}
	return service.Repository.AssignInitial(ctx, keyID, node, now())
}

// QueueFor returns the logical queue name for a key's stored assignment.
func (service Assignments) QueueFor(ctx context.Context, keyID int64) (string, error) {
	if service.Repository == nil {
		return "", errors.New("cluster: assignment repository is required")
	}
	if keyID <= 0 {
		return "", ErrAssignmentUnavailable
	}
	assignment, err := service.Repository.GetAssignment(ctx, keyID)
	if err != nil {
		return "", err
	}
	return "crawl@" + assignment.Node, nil
}
