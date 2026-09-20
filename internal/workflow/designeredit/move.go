package designeredit

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// MoveDirection is the closed set of relative ordering commands shared by
// every workflow editor surface. The order is presentation metadata carried
// by Definition.Nodes; graph edges remain the sole source of execution flow.
type MoveDirection string

const (
	MoveEarlier MoveDirection = "EARLIER"
	MoveLater   MoveDirection = "LATER"
)

type MoveNodeRequest struct {
	DraftID          string
	ExpectedRevision uint64
	NodeID           string
	Direction        MoveDirection
}

// MoveNode changes only the durable node presentation order. Both the visual
// graph and the accessible outline call this command, so they cannot drift
// into competing edit models. Boundary moves are idempotent and preserve the
// optimistic revision.
func (s Service) MoveNode(ctx context.Context, tenant values.TenantId, author string, request MoveNodeRequest) (Change, error) {
	if request.ExpectedRevision == 0 || strings.TrimSpace(request.NodeID) == "" ||
		(request.Direction != MoveEarlier && request.Direction != MoveLater) {
		return Change{}, ErrInvalid
	}
	draft, definition, err := s.loadEditable(ctx, tenant, author, request.DraftID, request.ExpectedRevision)
	if err != nil {
		return Change{}, err
	}
	index := nodePosition(definition.Nodes, request.NodeID)
	if index < 0 {
		return Change{}, ErrNotFound
	}
	target := index - 1
	if request.Direction == MoveLater {
		target = index + 1
	}
	if target < 0 || target >= len(definition.Nodes) {
		view, projectErr := s.projectDraft(ctx, tenant, draft)
		return Change{Draft: view}, projectErr
	}
	definition.Nodes[index], definition.Nodes[target] = definition.Nodes[target], definition.Nodes[index]
	return s.saveDefinition(ctx, tenant, draft, definition, "Move "+request.NodeID)
}
