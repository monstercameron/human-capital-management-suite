// Package designeredit applies server-resolved workflow palette entries to a
// mutable draft definition. It owns no persistence or authorization; callers
// must resolve an entry from the tenant-filtered catalog before invoking it.
package designeredit

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designerpalette"
)

var (
	// ErrInvalid reports a malformed draft or palette expansion.
	ErrInvalid = errors.New("designeredit: invalid edit")
	// ErrConflict reports an insertion that collides with the current draft.
	ErrConflict = errors.New("designeredit: edit conflict")
)

const (
	MetadataGroupID        = "hcmnext.designer.group_id"
	MetadataGroupName      = "hcmnext.designer.group_name"
	MetadataGroupEntry     = "hcmnext.designer.group_entry"
	MetadataGroupVersion   = "hcmnext.designer.group_version"
	MetadataGroupCollapsed = "hcmnext.designer.group_collapsed"
)

var nonID = regexp.MustCompile(`[^a-z0-9]+`)

// Result is the edited definition and stable identities the UI may announce
// and focus. GroupID is populated only for fragment insertions.
type Result struct {
	Definition      workflow.Definition
	InsertedNodeIDs []string
	GroupID         string
}

// Insert applies one already-authorized registry entry. Templates replace an
// empty draft, fragments expand to nodes and internal edges under one group,
// and blocks append a single typed node. The input and registry entry are
// defensively cloned and never mutated.
func Insert(current workflow.Definition, entry designerpalette.Entry) (Result, error) {
	definition, err := cloneDefinition(current)
	if err != nil || strings.TrimSpace(entry.ID) == "" || entry.Version == 0 {
		return Result{}, ErrInvalid
	}
	switch entry.Kind {
	case designerpalette.KindTemplate:
		if entry.Expansion.Template == nil {
			return Result{}, ErrInvalid
		}
		if len(definition.Nodes) != 0 || len(definition.Edges) != 0 {
			return Result{}, ErrConflict
		}
		template, cloneErr := cloneDefinition(*entry.Expansion.Template)
		if cloneErr != nil {
			return Result{}, ErrInvalid
		}
		ids := nodeIDs(template.Nodes)
		return Result{Definition: template, InsertedNodeIDs: ids}, nil
	case designerpalette.KindFragment:
		return insertFragment(definition, entry)
	case designerpalette.KindBlock:
		return insertBlock(definition, entry)
	default:
		return Result{}, ErrInvalid
	}
}

func insertBlock(definition workflow.Definition, entry designerpalette.Entry) (Result, error) {
	if entry.StepType == "" {
		return Result{}, ErrInvalid
	}
	base := safeID(entry.Name)
	if base == "" {
		base = safeID(entry.ID)
	}
	id := nextID(base, definition.Nodes)
	node := workflow.Node{ID: id, Type: entry.StepType, DeclaredEffect: entry.EffectClass}
	definition.Nodes = append(definition.Nodes, node)
	if definition.StartNodeID == "" {
		definition.StartNodeID = id
	}
	return Result{Definition: definition, InsertedNodeIDs: []string{id}}, nil
}

func insertFragment(definition workflow.Definition, entry designerpalette.Entry) (Result, error) {
	if len(entry.Expansion.Nodes) == 0 {
		return Result{}, ErrInvalid
	}
	groupID := nextGroupID(safeID(entry.Name), definition.Nodes)
	remap := make(map[string]string, len(entry.Expansion.Nodes))
	seen := make(map[string]bool, len(entry.Expansion.Nodes))
	// safeID is lossy: it folds every non-alphanumeric run to "_", so distinct
	// fragment node ids such as "a-b" and "a_b" can remap onto one id. Left
	// undetected that silently merges two nodes and reroutes the second one's
	// edges and mappings onto the first.
	taken := make(map[string]bool, len(entry.Expansion.Nodes))
	for _, node := range entry.Expansion.Nodes {
		if strings.TrimSpace(node.ID) == "" || seen[node.ID] {
			return Result{}, ErrInvalid
		}
		seen[node.ID] = true
		remapped := groupID + "__" + safeID(node.ID)
		if taken[remapped] {
			return Result{}, ErrInvalid
		}
		taken[remapped] = true
		remap[node.ID] = remapped
	}
	for _, edge := range entry.Expansion.Edges {
		if remap[edge.From] == "" || remap[edge.To] == "" || strings.TrimSpace(edge.RouteKey) == "" {
			return Result{}, ErrInvalid
		}
	}
	var mergeErr error
	definition.ApprovalRequirements, mergeErr = mergeApprovalRequirements(definition.ApprovalRequirements, entry.Expansion.ApprovalRequirements)
	if mergeErr != nil {
		return Result{}, mergeErr
	}
	definition.Obligations, mergeErr = mergeObligations(definition.Obligations, entry.Expansion.Obligations)
	if mergeErr != nil {
		return Result{}, mergeErr
	}
	inserted := make([]string, 0, len(entry.Expansion.Nodes))
	for _, original := range entry.Expansion.Nodes {
		node, cloneErr := cloneNode(original)
		if cloneErr != nil {
			return Result{}, ErrInvalid
		}
		node.ID = remap[original.ID]
		remapNodeReferences(&node, remap)
		if node.Metadata == nil {
			node.Metadata = make(map[string]string, 5)
		}
		node.Metadata[MetadataGroupID] = groupID
		node.Metadata[MetadataGroupName] = strings.TrimSpace(entry.Name)
		node.Metadata[MetadataGroupEntry] = entry.ID
		node.Metadata[MetadataGroupVersion] = fmt.Sprint(entry.Version)
		node.Metadata[MetadataGroupCollapsed] = "true"
		definition.Nodes = append(definition.Nodes, node)
		inserted = append(inserted, node.ID)
	}
	for _, edge := range entry.Expansion.Edges {
		definition.Edges = append(definition.Edges, workflow.Edge{From: remap[edge.From], To: remap[edge.To], RouteKey: edge.RouteKey})
	}
	if definition.StartNodeID == "" {
		definition.StartNodeID = inserted[0]
	}
	return Result{Definition: definition, InsertedNodeIDs: inserted, GroupID: groupID}, nil
}

func mergeApprovalRequirements(current, inserted []workflow.ApprovalRequirement) ([]workflow.ApprovalRequirement, error) {
	result := append([]workflow.ApprovalRequirement(nil), current...)
	byID := make(map[string]workflow.ApprovalRequirement, len(result))
	for _, requirement := range result {
		byID[requirement.ID] = requirement
	}
	for _, requirement := range inserted {
		if strings.TrimSpace(requirement.ID) == "" {
			return nil, ErrInvalid
		}
		if existing, found := byID[requirement.ID]; found {
			if !equalJSON(existing, requirement) {
				return nil, ErrConflict
			}
			continue
		}
		result = append(result, requirement)
		byID[requirement.ID] = requirement
	}
	return result, nil
}

func mergeObligations(current, inserted []workflow.ObligationRequirement) ([]workflow.ObligationRequirement, error) {
	result := append([]workflow.ObligationRequirement(nil), current...)
	byID := make(map[string]workflow.ObligationRequirement, len(result))
	for _, requirement := range result {
		byID[requirement.ID] = requirement
	}
	for _, requirement := range inserted {
		if strings.TrimSpace(requirement.ID) == "" {
			return nil, ErrInvalid
		}
		if existing, found := byID[requirement.ID]; found {
			if !equalJSON(existing, requirement) {
				return nil, ErrConflict
			}
			continue
		}
		result = append(result, requirement)
		byID[requirement.ID] = requirement
	}
	return result, nil
}

func equalJSON(left, right any) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftBytes) == string(rightBytes)
}

func remapNodeReferences(node *workflow.Node, ids map[string]string) {
	for i := range node.InputMappings {
		if replacement := ids[node.InputMappings[i].Source.NodeID]; replacement != "" {
			node.InputMappings[i].Source.NodeID = replacement
		}
	}
	if replacement := ids[node.FailureRoute]; replacement != "" {
		node.FailureRoute = replacement
	}
	if node.Observe != nil {
		if replacement := ids[node.Observe.RetryExhaustionRoute]; replacement != "" {
			node.Observe.RetryExhaustionRoute = replacement
		}
	}
}

func nodeIDs(nodes []workflow.Node) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	sort.Strings(ids)
	return ids
}

func nextID(base string, nodes []workflow.Node) string {
	if base == "" {
		base = "step"
	}
	used := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		used[node.ID] = true
	}
	for ordinal := 1; ; ordinal++ {
		candidate := fmt.Sprintf("%s_%d", base, ordinal)
		if !used[candidate] {
			return candidate
		}
	}
}

func nextGroupID(base string, nodes []workflow.Node) string {
	if base == "" {
		base = "fragment"
	}
	used := make(map[string]bool)
	for _, node := range nodes {
		if node.Metadata != nil {
			used[node.Metadata[MetadataGroupID]] = true
		}
	}
	for ordinal := 1; ; ordinal++ {
		candidate := fmt.Sprintf("group_%s_%d", base, ordinal)
		if !used[candidate] {
			return candidate
		}
	}
}

func safeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(nonID.ReplaceAllString(value, "_"), "_")
	return value
}

func cloneDefinition(value workflow.Definition) (workflow.Definition, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return workflow.Definition{}, err
	}
	var cloned workflow.Definition
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return workflow.Definition{}, err
	}
	return cloned, nil
}

func cloneNode(value workflow.Node) (workflow.Node, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return workflow.Node{}, err
	}
	var cloned workflow.Node
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return workflow.Node{}, err
	}
	return cloned, nil
}
