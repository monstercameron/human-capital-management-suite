package designeredit

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// HistoryDirection is the closed set of durable time-travel commands.
type HistoryDirection string

const (
	HistoryUndo HistoryDirection = "UNDO"
	HistoryRedo HistoryDirection = "REDO"
)

// SemanticChangeKind identifies the workflow surface affected by one change.
type SemanticChangeKind string

const (
	ChangeNode      SemanticChangeKind = "NODE"
	ChangeEdge      SemanticChangeKind = "EDGE"
	ChangeBinding   SemanticChangeKind = "BINDING"
	ChangeParameter SemanticChangeKind = "PARAMETER"
)

// SemanticChangeOperation describes how one semantic subject changed.
type SemanticChangeOperation string

const (
	ChangeAdded   SemanticChangeOperation = "ADDED"
	ChangeRemoved SemanticChangeOperation = "REMOVED"
	ChangeUpdated SemanticChangeOperation = "UPDATED"
)

// SemanticChange is a stable, presentation-safe workflow diff row. It never
// exposes the raw draft document.
type SemanticChange struct {
	Kind      SemanticChangeKind
	Operation SemanticChangeOperation
	SubjectID string
	Field     string
	Before    string
	After     string
}

// History is the persistence-neutral cursor returned by a durable store.
type History struct {
	Position     uint64
	Length       uint64
	CurrentLabel string
	Current      json.RawMessage
	Previous     json.RawMessage
}

// NavigateRequest advances the optimistic revision while restoring one
// adjacent history snapshot.
type NavigateRequest struct {
	DraftID          string
	AuthorRef        string
	ExpectedRevision uint64
	Direction        HistoryDirection
	At               time.Time
}

// HistoryStore is optional so pure edit-kernel tests can keep tiny memory
// stores. Production composition implements it; history commands fail closed
// when it is absent.
type HistoryStore interface {
	LoadHistory(context.Context, values.TenantId, string) (History, error)
	Navigate(context.Context, values.TenantId, NavigateRequest) (Draft, error)
}

// ImportRequest is the internal agent/import seam. It deliberately writes the
// exact same Draft artifact as the human editor; there is no privileged agent
// document or second persistence format to reconcile later.
type ImportRequest struct {
	Definition        workflow.Definition
	SemanticVersion   string
	BaseVersionDigest string
}

// Import creates an author-owned draft from a canonical definition that has
// no presentation coordinates. The product projection applies AUTO layout in
// exactly the same way as it does for human-created drafts.
func (s Service) Import(ctx context.Context, tenant values.TenantId, author string, request ImportRequest) (Change, error) {
	if s.Store == nil || s.NewID == nil || strings.TrimSpace(tenant.String()) == "" || strings.TrimSpace(author) == "" || strings.TrimSpace(request.Definition.WorkflowID) == "" {
		return Change{}, ErrInvalid
	}
	semanticVersion := strings.TrimSpace(request.SemanticVersion)
	if semanticVersion == "" {
		semanticVersion = "0.1.0"
	}
	if workflowversion.ValidateSemanticVersion(semanticVersion) != nil {
		return Change{}, ErrInvalid
	}
	id, err := s.NewID()
	if err != nil || strings.TrimSpace(id) == "" {
		return Change{}, fmt.Errorf("%w: mint draft id", ErrInvalid)
	}
	document, err := workflow.Marshal(request.Definition)
	if err != nil {
		return Change{}, fmt.Errorf("%w: marshal imported draft", ErrInvalid)
	}
	now := s.now()
	saved, err := s.Store.Save(ctx, tenant, SaveRequest{
		DraftID: id, WorkflowID: request.Definition.WorkflowID, AuthorRef: strings.TrimSpace(author),
		SemanticVersion: semanticVersion, BaseVersionDigest: strings.TrimSpace(request.BaseVersionDigest),
		CommandLabel: "Import workflow", Document: document, ExpiresAt: now.Add(s.ttl()), At: now,
	})
	if err != nil {
		return Change{}, err
	}
	view, err := s.projectDraft(ctx, tenant, saved)
	return Change{Draft: view}, err
}

// NavigateHistory performs durable undo or redo. Restoring a snapshot always
// advances Draft.Revision, so an in-flight command can never become valid
// again after time travel.
func (s Service) NavigateHistory(ctx context.Context, tenant values.TenantId, author string, request NavigateRequest) (Change, error) {
	if request.ExpectedRevision == 0 || (request.Direction != HistoryUndo && request.Direction != HistoryRedo) {
		return Change{}, ErrInvalid
	}
	draft, err := s.owned(ctx, tenant, author, request.DraftID)
	if err != nil {
		return Change{}, err
	}
	if draft.Revision != request.ExpectedRevision {
		return Change{}, ErrConflict
	}
	historyStore, ok := s.Store.(HistoryStore)
	if !ok {
		return Change{}, ErrInvalid
	}
	request.DraftID = draft.DraftID
	request.AuthorRef = draft.AuthorRef
	request.At = s.now()
	saved, err := historyStore.Navigate(ctx, tenant, request)
	if err != nil {
		return Change{}, err
	}
	view, err := s.projectDraft(ctx, tenant, saved)
	return Change{Draft: view}, err
}

func (s Service) projectDraft(ctx context.Context, tenant values.TenantId, draft Draft) (View, error) {
	view, err := project(draft)
	if err != nil {
		return View{}, err
	}
	return s.withHistory(ctx, tenant, view)
}

func (s Service) withHistory(ctx context.Context, tenant values.TenantId, view View) (View, error) {
	historyStore, ok := s.Store.(HistoryStore)
	if !ok {
		return view, nil
	}
	history, err := historyStore.LoadHistory(ctx, tenant, view.DraftID)
	if err != nil {
		return View{}, err
	}
	if history.Position == 0 || history.Length == 0 || history.Position > history.Length {
		return View{}, ErrInvalid
	}
	view.HistoryPosition = history.Position
	view.HistoryLength = history.Length
	view.HistoryLabel = strings.TrimSpace(history.CurrentLabel)
	view.CanUndo = history.Position > 1
	view.CanRedo = history.Position < history.Length
	if len(history.Previous) == 0 {
		return view, nil
	}
	before, err := workflow.Load(history.Previous)
	if err != nil {
		return View{}, fmt.Errorf("%w: previous history definition", ErrInvalid)
	}
	after, err := workflow.Load(history.Current)
	if err != nil {
		return View{}, fmt.Errorf("%w: current history definition", ErrInvalid)
	}
	view.Changes = SemanticDiff(before, after)
	return view, nil
}

// SemanticDiff compares two canonical definitions by workflow meaning rather
// than JSON formatting or slice allocation. Output order is deterministic for
// golden tests and for a stable review panel.
func SemanticDiff(before, after workflow.Definition) []SemanticChange {
	changes := make([]SemanticChange, 0)
	beforeNodes, afterNodes := indexNodes(before.Nodes), indexNodes(after.Nodes)
	for id, node := range beforeNodes {
		afterNode, exists := afterNodes[id]
		if !exists {
			changes = append(changes, SemanticChange{Kind: ChangeNode, Operation: ChangeRemoved, SubjectID: id, Before: string(node.Type)})
			continue
		}
		if node.Type != afterNode.Type {
			changes = append(changes, SemanticChange{Kind: ChangeNode, Operation: ChangeUpdated, SubjectID: id, Field: "step_type", Before: string(node.Type), After: string(afterNode.Type)})
		}
		changes = append(changes, parameterChanges(id, node, afterNode)...)
		changes = append(changes, bindingChanges(id, node, afterNode)...)
	}
	for id, node := range afterNodes {
		if _, exists := beforeNodes[id]; !exists {
			changes = append(changes, SemanticChange{Kind: ChangeNode, Operation: ChangeAdded, SubjectID: id, After: string(node.Type)})
		}
	}
	changes = append(changes, edgeChanges(before.Edges, after.Edges)...)
	sort.SliceStable(changes, func(i, j int) bool {
		left, right := changes[i], changes[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.SubjectID != right.SubjectID {
			return left.SubjectID < right.SubjectID
		}
		if left.Field != right.Field {
			return left.Field < right.Field
		}
		return left.Operation < right.Operation
	})
	return changes
}

func indexNodes(nodes []workflow.Node) map[string]workflow.Node {
	result := make(map[string]workflow.Node, len(nodes))
	for _, node := range nodes {
		result[node.ID] = node
	}
	return result
}

func parameterChanges(nodeID string, before, after workflow.Node) []SemanticChange {
	left, right := make(map[string]string), make(map[string]string)
	for _, parameter := range parameterViews(before) {
		left[parameter.ID] = parameter.Value
	}
	for _, parameter := range parameterViews(after) {
		right[parameter.ID] = parameter.Value
	}
	keys := unionKeys(left, right)
	result := make([]SemanticChange, 0)
	for _, key := range keys {
		if left[key] == right[key] {
			continue
		}
		operation := ChangeUpdated
		if _, ok := left[key]; !ok {
			operation = ChangeAdded
		} else if _, ok := right[key]; !ok {
			operation = ChangeRemoved
		}
		result = append(result, SemanticChange{Kind: ChangeParameter, Operation: operation, SubjectID: nodeID, Field: key, Before: left[key], After: right[key]})
	}
	return result
}

func bindingChanges(nodeID string, before, after workflow.Node) []SemanticChange {
	left, right := make(map[string]string), make(map[string]string)
	for _, mapping := range before.InputMappings {
		left[mapping.Target] = sourceSummary(mapping.Source)
	}
	for _, mapping := range after.InputMappings {
		right[mapping.Target] = sourceSummary(mapping.Source)
	}
	result := make([]SemanticChange, 0)
	for _, target := range unionKeys(left, right) {
		if left[target] == right[target] {
			continue
		}
		operation := ChangeUpdated
		if _, ok := left[target]; !ok {
			operation = ChangeAdded
		} else if _, ok := right[target]; !ok {
			operation = ChangeRemoved
		}
		result = append(result, SemanticChange{Kind: ChangeBinding, Operation: operation, SubjectID: nodeID, Field: target, Before: left[target], After: right[target]})
	}
	return result
}

func sourceSummary(source workflow.Source) string {
	parts := []string{string(source.Kind)}
	for _, value := range []string{source.NodeID, source.ContextKind, source.Path, source.Type.String(), source.Constant} {
		if strings.TrimSpace(value) != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ":")
}

func edgeChanges(before, after []workflow.Edge) []SemanticChange {
	left, right := make(map[string]workflow.Edge, len(before)), make(map[string]workflow.Edge, len(after))
	for _, edge := range before {
		left[edgeIdentity(edge)] = edge
	}
	for _, edge := range after {
		right[edgeIdentity(edge)] = edge
	}
	result := make([]SemanticChange, 0)
	for key, edge := range left {
		if _, exists := right[key]; !exists {
			result = append(result, SemanticChange{Kind: ChangeEdge, Operation: ChangeRemoved, SubjectID: edge.From, Field: edge.RouteKey, Before: edge.To})
		}
	}
	for key, edge := range right {
		if _, exists := left[key]; !exists {
			result = append(result, SemanticChange{Kind: ChangeEdge, Operation: ChangeAdded, SubjectID: edge.From, Field: edge.RouteKey, After: edge.To})
		}
	}
	return result
}

func edgeIdentity(edge workflow.Edge) string {
	return edge.From + "\x00" + edge.RouteKey + "\x00" + edge.To
}

func unionKeys(left, right map[string]string) []string {
	seen := make(map[string]bool, len(left)+len(right))
	for key := range left {
		seen[key] = true
	}
	for key := range right {
		seen[key] = true
	}
	result := make([]string, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
