package designeredit

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

const (
	MetadataDisplayName = "hcmnext.designer.display_name"
	metadataOverlays    = "hcmnext.designer.template_overlays"
)

type ParameterKind string

const (
	ParameterText    ParameterKind = "TEXT"
	ParameterInteger ParameterKind = "INTEGER"
	ParameterEnum    ParameterKind = "ENUM"
)

type ParameterView struct {
	ID, Label, Value string
	Kind             ParameterKind
	Required         bool
	Minimum, Maximum int64
	Options          []string
}

type OverlayOperation string

const (
	OverlayAdd     OverlayOperation = "ADD"
	OverlayOmit    OverlayOperation = "OMIT"
	OverlayReplace OverlayOperation = "REPLACE"
)

type OverlayView struct {
	Operation    OverlayOperation `json:"operation"`
	TargetNodeID string           `json:"target_node_id,omitempty"`
	EntryID      string           `json:"entry_id,omitempty"`
	EntryVersion uint32           `json:"entry_version,omitempty"`
	Reason       string           `json:"reason,omitempty"`
}

type UpdateNodeParametersRequest struct {
	DraftID          string
	ExpectedRevision uint64
	NodeID           string
	Values           map[string]string
}

type ApplyTemplateOverlayRequest struct {
	DraftID          string
	ExpectedRevision uint64
	Operation        OverlayOperation
	TargetNodeID     string
	EntryID          string
	EntryVersion     uint32
	Reason           string
}

func (s Service) UpdateNodeParameters(ctx context.Context, tenant values.TenantId, author string, request UpdateNodeParametersRequest) (_ Change, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.designer.update_parameters", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.DoneWith(op, retErr) }()
	if request.ExpectedRevision == 0 || strings.TrimSpace(request.NodeID) == "" || len(request.Values) == 0 {
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
	if err := applyParameterValues(&definition.Nodes[index], request.Values); err != nil {
		return Change{}, err
	}
	return s.saveDefinition(ctx, tenant, draft, definition, "Update "+request.NodeID)
}

func (s Service) ApplyTemplateOverlay(ctx context.Context, tenant values.TenantId, author string, request ApplyTemplateOverlayRequest) (_ Change, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.designer.apply_overlay", observe.Attrs{observe.KeyTenant: tenant.String()})
	defer func() { observe.DoneWith(op, retErr) }()
	if request.ExpectedRevision == 0 {
		return Change{}, ErrInvalid
	}
	draft, definition, err := s.loadEditable(ctx, tenant, author, request.DraftID, request.ExpectedRevision)
	if err != nil {
		return Change{}, err
	}
	beforeLocked := lockedNodeIDs(definition)
	record := OverlayView{Operation: request.Operation, TargetNodeID: strings.TrimSpace(request.TargetNodeID), EntryID: strings.TrimSpace(request.EntryID), EntryVersion: request.EntryVersion, Reason: strings.TrimSpace(request.Reason)}
	change := Result{Definition: definition}
	switch request.Operation {
	case OverlayAdd:
		entry, found := findEntry(s.Catalog.List(ctx, tenant), record.EntryID, record.EntryVersion, "")
		if !found || entry.Kind == designerpalette.KindTemplate {
			return Change{}, ErrNotFound
		}
		change, err = Insert(definition, entry)
	case OverlayOmit:
		if record.Reason == "" {
			return Change{}, ErrInvalid
		}
		change.Definition, err = omitNode(definition, record.TargetNodeID)
	case OverlayReplace:
		if record.Reason == "" {
			return Change{}, ErrInvalid
		}
		entry, found := findEntry(s.Catalog.List(ctx, tenant), record.EntryID, record.EntryVersion, designerpalette.KindBlock)
		if !found {
			return Change{}, ErrNotFound
		}
		change.Definition, err = replaceNode(definition, record.TargetNodeID, entry)
	default:
		return Change{}, ErrInvalid
	}
	if err != nil {
		return Change{}, err
	}
	for id := range beforeLocked {
		if nodePosition(change.Definition.Nodes, id) < 0 {
			return Change{}, ErrInvalid
		}
	}
	if err := appendOverlay(&change.Definition, record); err != nil {
		return Change{}, err
	}
	label := "Change template"
	switch request.Operation {
	case OverlayAdd:
		label = "Add " + record.EntryID
	case OverlayOmit:
		label = "Omit " + record.TargetNodeID
	case OverlayReplace:
		label = "Replace " + record.TargetNodeID
	}
	saved, err := s.saveDefinition(ctx, tenant, draft, change.Definition, label)
	if err != nil {
		return Change{}, err
	}
	saved.InsertedNodeIDs, saved.GroupID = append([]string(nil), change.InsertedNodeIDs...), change.GroupID
	return saved, nil
}

func (s Service) loadEditable(ctx context.Context, tenant values.TenantId, author, draftID string, expected uint64) (Draft, workflow.Definition, error) {
	draft, err := s.owned(ctx, tenant, author, draftID)
	if err != nil {
		return Draft{}, workflow.Definition{}, err
	}
	if draft.Revision != expected {
		return Draft{}, workflow.Definition{}, ErrConflict
	}
	definition, err := workflow.Load(draft.Document)
	if err != nil {
		return Draft{}, workflow.Definition{}, fmt.Errorf("%w: stored definition", ErrInvalid)
	}
	return draft, definition, nil
}

func (s Service) saveDefinition(ctx context.Context, tenant values.TenantId, draft Draft, definition workflow.Definition, commandLabel string) (Change, error) {
	document, err := workflow.Marshal(definition)
	if err != nil {
		return Change{}, fmt.Errorf("%w: marshal edit", ErrInvalid)
	}
	// Every authoring surface shares this final persistence gate. A command
	// that resolves to the exact durable document is a successful no-op, not a
	// new revision; otherwise keyboard resubmission and network retries create
	// fictitious history and stale all other controls on the page.
	current, loadErr := workflow.Load(draft.Document)
	if loadErr != nil {
		return Change{}, fmt.Errorf("%w: compare stored edit", ErrInvalid)
	}
	// PostgreSQL stores the document as jsonb and therefore does not preserve
	// the whitespace emitted by workflow.Marshal. Compare the canonical
	// definition identity rather than transport bytes so the first no-op after
	// a database round trip remains a no-op too.
	if workflowversion.DefinitionDigest(current) == workflowversion.DefinitionDigest(definition) {
		view, projectErr := s.projectDraft(ctx, tenant, draft)
		return Change{Draft: view}, projectErr
	}
	now := s.now()
	expiresAt := draft.ExpiresAt
	if expiresAt.IsZero() || !expiresAt.After(now) {
		expiresAt = now.Add(s.ttl())
	}
	saved, err := s.Store.Save(ctx, tenant, SaveRequest{
		DraftID: draft.DraftID, WorkflowID: definition.WorkflowID, AuthorRef: draft.AuthorRef,
		SemanticVersion: draft.SemanticVersion, BaseVersionDigest: draft.BaseVersionDigest, ExpectedRevision: draft.Revision,
		CommandLabel: commandLabel, Document: document, ExpiresAt: expiresAt, At: now,
	})
	if err != nil {
		return Change{}, err
	}
	view, err := s.projectDraft(ctx, tenant, saved)
	return Change{Draft: view}, err
}

func parameterViews(node workflow.Node) []ParameterView {
	displayName := ""
	if node.Metadata != nil {
		displayName = node.Metadata[MetadataDisplayName]
	}
	result := []ParameterView{{ID: "display_name", Label: "Display name", Kind: ParameterText, Value: displayName, Maximum: 120}}
	if node.Retry != nil {
		result = append(result, ParameterView{ID: "retry_max_attempts", Label: "Maximum attempts", Kind: ParameterInteger, Value: strconv.FormatUint(uint64(node.Retry.MaxAttempts), 10), Required: true, Minimum: 1, Maximum: 20})
	}
	if node.Signal != nil {
		result = append(result,
			ParameterView{ID: "signal_event_type", Label: "Event type", Kind: ParameterText, Value: node.Signal.EventType, Required: true, Maximum: 160},
			ParameterView{ID: "signal_timeout_seconds", Label: "Close after seconds", Kind: ParameterInteger, Value: strconv.FormatUint(node.Signal.CloseAfterSeconds, 10), Minimum: 0, Maximum: 31536000},
		)
	}
	if node.Wait != nil {
		result = append(result,
			ParameterView{ID: "wait_wake_kind", Label: "Wake condition", Kind: ParameterEnum, Value: string(node.Wait.WakeKind), Required: true, Options: []string{string(workflow.WaitWakeAtInstant), string(workflow.WaitWakeAtLocalDate), string(workflow.WaitWakeAtLocalDateTime)}},
			ParameterView{ID: "wait_zone", Label: "Time zone", Kind: ParameterText, Value: node.Wait.ZoneID, Required: true, Maximum: 80},
			ParameterView{ID: "wait_calendar", Label: "Business calendar", Kind: ParameterText, Value: node.Wait.CalendarRef, Required: true, Maximum: 160},
		)
	}
	return result
}

func applyParameterValues(node *workflow.Node, values map[string]string) error {
	schema := parameterViews(*node)
	byID := make(map[string]ParameterView, len(schema))
	for _, field := range schema {
		byID[field.ID] = field
	}
	for id, raw := range values {
		field, ok := byID[id]
		if !ok {
			return ErrInvalid
		}
		value := strings.TrimSpace(raw)
		if field.Required && value == "" {
			return ErrInvalid
		}
		switch field.Kind {
		case ParameterText:
			if field.Maximum > 0 && int64(utf8.RuneCountInString(value)) > field.Maximum {
				return ErrInvalid
			}
		case ParameterInteger:
			parsed, err := strconv.ParseUint(value, 10, 64)
			if err != nil || int64(parsed) < field.Minimum || (field.Maximum > 0 && int64(parsed) > field.Maximum) {
				return ErrInvalid
			}
		case ParameterEnum:
			found := false
			for _, option := range field.Options {
				found = found || value == option
			}
			if !found {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		switch id {
		case "display_name":
			if node.Metadata == nil {
				node.Metadata = make(map[string]string)
			}
			if value == "" {
				delete(node.Metadata, MetadataDisplayName)
			} else {
				node.Metadata[MetadataDisplayName] = value
			}
		case "retry_max_attempts":
			parsed, _ := strconv.ParseUint(value, 10, 32)
			node.Retry.MaxAttempts = uint32(parsed)
		case "signal_event_type":
			node.Signal.EventType = value
		case "signal_timeout_seconds":
			parsed, _ := strconv.ParseUint(value, 10, 64)
			node.Signal.CloseAfterSeconds = parsed
		case "wait_wake_kind":
			node.Wait.WakeKind = workflow.WaitWakeKind(value)
		case "wait_zone":
			node.Wait.ZoneID = value
		case "wait_calendar":
			node.Wait.CalendarRef = value
		}
	}
	return nil
}

func lockedNode(node workflow.Node, startID string) (bool, string) {
	lower := strings.ToLower(node.ID)
	switch {
	case node.ID == startID:
		return true, "START"
	case node.Type == workflow.StepApproval || len(node.Governance.ApprovalRequirements) > 0:
		return true, "GOVERNANCE"
	case strings.Contains(lower, "revalid"):
		return true, "REVALIDATION"
	case strings.Contains(lower, "reconcil"):
		return true, "RECONCILIATION"
	case node.Type == workflow.StepEnd:
		return true, "CLOSURE"
	default:
		return false, ""
	}
}

func lockedNodeIDs(definition workflow.Definition) map[string]bool {
	result := make(map[string]bool)
	for _, node := range definition.Nodes {
		if locked, _ := lockedNode(node, definition.StartNodeID); locked {
			result[node.ID] = true
		}
	}
	return result
}

func omitNode(definition workflow.Definition, nodeID string) (workflow.Definition, error) {
	index := nodePosition(definition.Nodes, nodeID)
	if index < 0 {
		return workflow.Definition{}, ErrNotFound
	}
	if locked, _ := lockedNode(definition.Nodes[index], definition.StartNodeID); locked {
		return workflow.Definition{}, ErrInvalid
	}
	definition.Nodes = append(definition.Nodes[:index:index], definition.Nodes[index+1:]...)
	edges := definition.Edges[:0]
	for _, edge := range definition.Edges {
		if edge.From != nodeID && edge.To != nodeID {
			edges = append(edges, edge)
		}
	}
	definition.Edges = edges
	return definition, nil
}

func replaceNode(definition workflow.Definition, nodeID string, entry designerpalette.Entry) (workflow.Definition, error) {
	index := nodePosition(definition.Nodes, nodeID)
	if index < 0 {
		return workflow.Definition{}, ErrNotFound
	}
	if locked, _ := lockedNode(definition.Nodes[index], definition.StartNodeID); locked || entry.StepType == "" {
		return workflow.Definition{}, ErrInvalid
	}
	metadata := definition.Nodes[index].Metadata
	definition.Nodes[index] = workflow.Node{ID: nodeID, Type: entry.StepType, DeclaredEffect: entry.EffectClass, Metadata: metadata}
	return definition, nil
}

func nodePosition(nodes []workflow.Node, id string) int {
	id = strings.TrimSpace(id)
	for index := range nodes {
		if nodes[index].ID == id {
			return index
		}
	}
	return -1
}

func appendOverlay(definition *workflow.Definition, record OverlayView) error {
	index := nodePosition(definition.Nodes, definition.StartNodeID)
	if index < 0 {
		return ErrInvalid
	}
	if definition.Nodes[index].Metadata == nil {
		definition.Nodes[index].Metadata = make(map[string]string)
	}
	records := overlayViews(definition.Nodes[index])
	records = append(records, record)
	encoded, err := json.Marshal(records)
	if err != nil {
		return ErrInvalid
	}
	definition.Nodes[index].Metadata[metadataOverlays] = string(encoded)
	return nil
}

func overlayViews(start workflow.Node) []OverlayView {
	if start.Metadata == nil || strings.TrimSpace(start.Metadata[metadataOverlays]) == "" {
		return nil
	}
	var records []OverlayView
	if json.Unmarshal([]byte(start.Metadata[metadataOverlays]), &records) != nil {
		return nil
	}
	return records
}
