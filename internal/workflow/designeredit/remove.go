package designeredit

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// maxDraftNameRunes bounds the durable draft name. The editor's name field and
// every list that renders it share this one limit.
const maxDraftNameRunes = 120

type RemoveNodeRequest struct {
	DraftID          string
	ExpectedRevision uint64
	NodeID           string
}

type ClearOutcomeRequest struct {
	DraftID          string
	ExpectedRevision uint64
	FromNodeID       string
	RouteKey         string
	ToNodeID         string
}

type RenameRequest struct {
	DraftID          string
	ExpectedRevision uint64
	Name             string
}

// RemoveNode deletes one authored node together with every edge that touches
// it and every input mapping that consumed its output. This is plain
// authoring deletion, not a justified template overlay: locked phases stay
// locked and nothing is recorded against the template. The single exception to
// the lock rule is the start node of a one-node draft, which must be
// deletable or a draft created from the wrong block can never be emptied.
func (s Service) RemoveNode(ctx context.Context, tenant values.TenantId, author string, request RemoveNodeRequest) (_ Change, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.designer.remove_node", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.DoneWith(op, retErr) }()
	if request.ExpectedRevision == 0 || strings.TrimSpace(request.NodeID) == "" {
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
	nodeID := definition.Nodes[index].ID
	// Emptying the draft is the one deletion the START lock must not refuse.
	// Withholding the start identity from the lock rule keeps every other
	// lock kind -- governance, revalidation, reconciliation, closure -- in
	// force for that last node.
	startID := definition.StartNodeID
	if len(definition.Nodes) == 1 {
		startID = ""
	}
	if locked, _ := lockedNode(definition.Nodes[index], startID); locked {
		return Change{}, ErrInvalid
	}
	definition.Nodes = append(definition.Nodes[:index:index], definition.Nodes[index+1:]...)
	edges := make([]workflow.Edge, 0, len(definition.Edges))
	for _, edge := range definition.Edges {
		if edge.From != nodeID && edge.To != nodeID {
			edges = append(edges, edge)
		}
	}
	definition.Edges = edges
	for i := range definition.Nodes {
		definition.Nodes[i].InputMappings = withoutSourceNode(definition.Nodes[i].InputMappings, nodeID)
	}
	if definition.StartNodeID == nodeID {
		definition.StartNodeID = ""
	}
	return s.saveDefinition(ctx, tenant, draft, definition, "Remove "+nodeID)
}

// ClearOutcome disconnects a declared outcome port. An empty ToNodeID clears
// every target on that route; naming a target clears only that one. Clearing
// a route that is already empty is a successful no-op, so a double click or a
// retried request never invents a revision.
func (s Service) ClearOutcome(ctx context.Context, tenant values.TenantId, author string, request ClearOutcomeRequest) (_ Change, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.designer.clear_outcome", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.DoneWith(op, retErr) }()
	if request.ExpectedRevision == 0 {
		return Change{}, ErrInvalid
	}
	draft, definition, err := s.loadEditable(ctx, tenant, author, request.DraftID, request.ExpectedRevision)
	if err != nil {
		return Change{}, err
	}
	fromIndex := nodePosition(definition.Nodes, request.FromNodeID)
	if fromIndex < 0 || strings.TrimSpace(request.RouteKey) == "" {
		return Change{}, ErrNotFound
	}
	from := definition.Nodes[fromIndex]
	if !containsString(workflow.OutcomeRoutes(from), request.RouteKey) {
		return Change{}, ErrInvalid
	}
	target := strings.TrimSpace(request.ToNodeID)
	kept := make([]workflow.Edge, 0, len(definition.Edges))
	removed := 0
	for _, edge := range definition.Edges {
		if edge.From == from.ID && edge.RouteKey == request.RouteKey && (target == "" || edge.To == target) {
			removed++
			continue
		}
		kept = append(kept, edge)
	}
	if removed == 0 {
		view, projectErr := s.projectDraft(ctx, tenant, draft)
		return Change{Draft: view}, projectErr
	}
	definition.Edges = kept
	return s.saveDefinition(ctx, tenant, draft, definition, "Clear "+from.ID+" "+request.RouteKey)
}

// Rename sets the durable draft name. Renaming to the name the draft already
// carries resolves to the stored document and is therefore a revision-
// preserving no-op, exactly like every other idempotent authoring command.
func (s Service) Rename(ctx context.Context, tenant values.TenantId, author string, request RenameRequest) (_ Change, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.designer.rename", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.DoneWith(op, retErr) }()
	name := strings.TrimSpace(request.Name)
	length := utf8.RuneCountInString(name)
	if request.ExpectedRevision == 0 || length == 0 || length > maxDraftNameRunes {
		return Change{}, ErrInvalid
	}
	draft, definition, err := s.loadEditable(ctx, tenant, author, request.DraftID, request.ExpectedRevision)
	if err != nil {
		return Change{}, err
	}
	definition.Name = name
	return s.saveDefinition(ctx, tenant, draft, definition, "Rename "+name)
}

// withoutSourceNode drops the mappings that read a removed node's output. A
// mapping left pointing at a deleted producer would compile into a dangling
// reference the author never sees in the inspector.
func withoutSourceNode(mappings []workflow.Mapping, nodeID string) []workflow.Mapping {
	kept := make([]workflow.Mapping, 0, len(mappings))
	for _, mapping := range mappings {
		if mapping.Source.Kind == workflow.SourceNodeOutput && mapping.Source.NodeID == nodeID {
			continue
		}
		kept = append(kept, mapping)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
