package designeredit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var ErrNotFound = errors.New("designeredit: draft or palette entry not found")

const defaultDraftTTL = 30 * 24 * time.Hour

// Draft is the persistence-neutral authoring record used by Service.
type Draft struct {
	DraftID, WorkflowID, AuthorRef, SemanticVersion, BaseVersionDigest string
	Revision                                                           uint64
	HistoryPosition, HistoryLength                                     uint64
	Document                                                           json.RawMessage
	ExpiresAt                                                          time.Time
}

// SaveRequest is one optimistic durable autosave.
type SaveRequest struct {
	DraftID, WorkflowID, AuthorRef, SemanticVersion, BaseVersionDigest string
	ExpectedRevision                                                   uint64
	CommandLabel                                                       string
	Document                                                           json.RawMessage
	ExpiresAt, At                                                      time.Time
}

type Store interface {
	Load(context.Context, values.TenantId, string) (Draft, error)
	Save(context.Context, values.TenantId, SaveRequest) (Draft, error)
}

type Catalog interface {
	List(context.Context, values.TenantId) []designerpalette.Entry
}

// Service applies authorized edit commands to durable drafts. The transport
// supplies the authenticated tenant and author; no method accepts either from
// request payload data.
type Service struct {
	Store   Store
	Catalog Catalog
	NewID   func() (string, error)
	Now     func() time.Time
	TTL     time.Duration
}

type CreateRequest struct {
	WorkflowID, Name, SemanticVersion, BaseVersionDigest, TemplateID string
	TemplateVersion                                                  uint32
}

type InsertRequest struct {
	DraftID          string
	ExpectedRevision uint64
	EntryID          string
	EntryVersion     uint32
}

// View is the safe draft projection returned to an author. The raw document
// stays server-side; the browser receives only graph and group metadata.
type View struct {
	DraftID, WorkflowID, Name, SemanticVersion, BaseVersionDigest string
	StartNodeID, DefinitionDigest                                 string
	Revision                                                      uint64
	HistoryPosition, HistoryLength                                uint64
	HistoryLabel                                                  string
	CanUndo, CanRedo                                              bool
	LayoutMode                                                    string
	ExpiresAt                                                     time.Time
	Nodes                                                         []NodeView
	Edges                                                         []workflow.Edge
	Groups                                                        []GroupView
	Overlays                                                      []OverlayView
	Changes                                                       []SemanticChange
}

type NodeView struct {
	ID, StepType, GroupID, Label, LockKind string
	Locked                                 bool
	Parameters                             []ParameterView
	Outcomes                               []OutcomeView
	Bindings                               []BindingView
}

type GroupView struct {
	ID, Name, EntryID string
	EntryVersion      uint32
	Collapsed         bool
	NodeIDs           []string
}

type Change struct {
	Draft           View
	InsertedNodeIDs []string
	GroupID         string
}

func (s Service) Create(ctx context.Context, tenant values.TenantId, author string, request CreateRequest) (_ Change, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.designer.create", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.DoneWith(op, retErr) }()
	if s.Store == nil || s.Catalog == nil || s.NewID == nil || strings.TrimSpace(tenant.String()) == "" || strings.TrimSpace(author) == "" {
		return Change{}, ErrInvalid
	}
	id, err := s.NewID()
	if err != nil || strings.TrimSpace(id) == "" {
		return Change{}, fmt.Errorf("%w: mint draft id", ErrInvalid)
	}
	workflowID := strings.TrimSpace(request.WorkflowID)
	if workflowID == "" {
		workflowID = "customer.workflow." + strings.ReplaceAll(id, "-", "")
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = "Untitled workflow"
	}
	semanticVersion := strings.TrimSpace(request.SemanticVersion)
	if semanticVersion == "" {
		semanticVersion = "0.1.0"
	}
	if workflowversion.ValidateSemanticVersion(semanticVersion) != nil {
		return Change{}, ErrInvalid
	}
	baseVersionDigest := strings.TrimSpace(request.BaseVersionDigest)
	if baseVersionDigest != "" && strings.TrimSpace(request.WorkflowID) == "" {
		return Change{}, ErrInvalid
	}
	definition := workflow.Definition{WorkflowID: workflowID, Version: 1, Name: name}
	change := Result{Definition: definition}
	if strings.TrimSpace(request.TemplateID) != "" || request.TemplateVersion != 0 {
		entry, found := findEntry(s.Catalog.List(ctx, tenant), request.TemplateID, request.TemplateVersion, designerpalette.KindTemplate)
		if !found {
			return Change{}, ErrNotFound
		}
		change, err = Insert(definition, entry)
		if err != nil {
			return Change{}, err
		}
	}
	document, err := workflow.Marshal(change.Definition)
	if err != nil {
		return Change{}, fmt.Errorf("%w: marshal draft", ErrInvalid)
	}
	now := s.now()
	saved, err := s.Store.Save(ctx, tenant, SaveRequest{
		DraftID: id, WorkflowID: change.Definition.WorkflowID, AuthorRef: strings.TrimSpace(author),
		SemanticVersion: semanticVersion, BaseVersionDigest: baseVersionDigest,
		CommandLabel: "Create workflow", Document: document, ExpiresAt: now.Add(s.ttl()), At: now,
	})
	if err != nil {
		return Change{}, err
	}
	view, err := s.projectDraft(ctx, tenant, saved)
	return Change{Draft: view, InsertedNodeIDs: change.InsertedNodeIDs, GroupID: change.GroupID}, err
}

func (s Service) Get(ctx context.Context, tenant values.TenantId, author, draftID string) (_ View, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.designer.get", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.DoneWith(op, retErr) }()
	draft, err := s.owned(ctx, tenant, author, draftID)
	if err != nil {
		return View{}, err
	}
	return s.projectDraft(ctx, tenant, draft)
}

func (s Service) Insert(ctx context.Context, tenant values.TenantId, author string, request InsertRequest) (_ Change, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.designer.insert", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.DoneWith(op, retErr) }()
	if request.ExpectedRevision == 0 || strings.TrimSpace(request.EntryID) == "" || request.EntryVersion == 0 {
		return Change{}, ErrInvalid
	}
	draft, err := s.owned(ctx, tenant, author, request.DraftID)
	if err != nil {
		return Change{}, err
	}
	if draft.Revision != request.ExpectedRevision {
		return Change{}, ErrConflict
	}
	entry, found := findEntry(s.Catalog.List(ctx, tenant), request.EntryID, request.EntryVersion, "")
	if !found {
		return Change{}, ErrNotFound
	}
	definition, err := workflow.Load(draft.Document)
	if err != nil {
		return Change{}, fmt.Errorf("%w: stored definition", ErrInvalid)
	}
	result, err := Insert(definition, entry)
	if err != nil {
		return Change{}, err
	}
	document, err := workflow.Marshal(result.Definition)
	if err != nil {
		return Change{}, fmt.Errorf("%w: marshal edit", ErrInvalid)
	}
	now := s.now()
	expiresAt := draft.ExpiresAt
	if expiresAt.IsZero() || !expiresAt.After(now) {
		expiresAt = now.Add(s.ttl())
	}
	saved, err := s.Store.Save(ctx, tenant, SaveRequest{
		DraftID: draft.DraftID, WorkflowID: result.Definition.WorkflowID, AuthorRef: draft.AuthorRef,
		SemanticVersion: draft.SemanticVersion, BaseVersionDigest: draft.BaseVersionDigest, ExpectedRevision: draft.Revision,
		CommandLabel: "Add " + entry.Name, Document: document, ExpiresAt: expiresAt, At: now,
	})
	if err != nil {
		return Change{}, err
	}
	view, err := s.projectDraft(ctx, tenant, saved)
	return Change{Draft: view, InsertedNodeIDs: result.InsertedNodeIDs, GroupID: result.GroupID}, err
}

func (s Service) owned(ctx context.Context, tenant values.TenantId, author, draftID string) (Draft, error) {
	if s.Store == nil || strings.TrimSpace(tenant.String()) == "" || strings.TrimSpace(author) == "" || strings.TrimSpace(draftID) == "" {
		return Draft{}, ErrInvalid
	}
	draft, err := s.Store.Load(ctx, tenant, strings.TrimSpace(draftID))
	if err != nil {
		return Draft{}, err
	}
	if draft.DraftID != strings.TrimSpace(draftID) || draft.AuthorRef != strings.TrimSpace(author) {
		return Draft{}, ErrNotFound
	}
	return draft, nil
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) ttl() time.Duration {
	if s.TTL > 0 {
		return s.TTL
	}
	return defaultDraftTTL
}

func findEntry(entries []designerpalette.Entry, id string, version uint32, kind designerpalette.Kind) (designerpalette.Entry, bool) {
	id = strings.TrimSpace(id)
	for _, entry := range entries {
		if entry.ID == id && entry.Version == version && (kind == "" || entry.Kind == kind) {
			return entry, true
		}
	}
	return designerpalette.Entry{}, false
}

func project(draft Draft) (View, error) {
	definition, err := workflow.Load(draft.Document)
	if err != nil {
		return View{}, fmt.Errorf("%w: project stored definition", ErrInvalid)
	}
	view := View{
		DraftID: draft.DraftID, WorkflowID: draft.WorkflowID, Name: definition.Name,
		SemanticVersion: draft.SemanticVersion, BaseVersionDigest: draft.BaseVersionDigest, Revision: draft.Revision, ExpiresAt: draft.ExpiresAt,
		StartNodeID: definition.StartNodeID, DefinitionDigest: workflowversion.DefinitionDigest(definition), LayoutMode: "AUTO",
		Edges: append([]workflow.Edge(nil), definition.Edges...),
	}
	groups := make(map[string]*GroupView)
	for _, node := range definition.Nodes {
		groupID := ""
		if node.Metadata != nil {
			groupID = node.Metadata[MetadataGroupID]
		}
		locked, lockKind := lockedNode(node, definition.StartNodeID)
		label := ""
		if node.Metadata != nil {
			label = node.Metadata[MetadataDisplayName]
		}
		view.Nodes = append(view.Nodes, NodeView{ID: node.ID, StepType: string(node.Type), GroupID: groupID, Label: label, Locked: locked, LockKind: lockKind, Parameters: parameterViews(node), Outcomes: outcomeViews(definition, node), Bindings: bindingViews(definition, node)})
		if groupID == "" {
			continue
		}
		group := groups[groupID]
		if group == nil {
			group = &GroupView{ID: groupID, Name: node.Metadata[MetadataGroupName], EntryID: node.Metadata[MetadataGroupEntry], Collapsed: node.Metadata[MetadataGroupCollapsed] == "true"}
			_, _ = fmt.Sscan(node.Metadata[MetadataGroupVersion], &group.EntryVersion)
			groups[groupID] = group
		}
		group.NodeIDs = append(group.NodeIDs, node.ID)
	}
	if index := nodePosition(definition.Nodes, definition.StartNodeID); index >= 0 {
		view.Overlays = overlayViews(definition.Nodes[index])
	}
	for _, group := range groups {
		view.Groups = append(view.Groups, *group)
	}
	sortGroups(view.Groups)
	return view, nil
}

func sortGroups(groups []GroupView) {
	for i := 1; i < len(groups); i++ {
		for j := i; j > 0 && groups[j].ID < groups[j-1].ID; j-- {
			groups[j], groups[j-1] = groups[j-1], groups[j]
		}
	}
}
