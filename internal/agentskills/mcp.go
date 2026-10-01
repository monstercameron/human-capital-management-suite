package agentskills

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MCPTool is the tools/list projection of one published skill version. The
// schemas are the exact schemas from the pinned SkillDefinition.
type MCPTool struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	InputSchema  json.RawMessage    `json:"inputSchema"`
	OutputSchema json.RawMessage    `json:"outputSchema"`
	Annotations  MCPToolAnnotations `json:"annotations"`
}

type MCPToolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
}

type MCPToolsList struct {
	Tools []MCPTool `json:"tools"`
}

// MCPToolName makes the version part of the wire identity. A task that pins
// v1 therefore cannot silently receive v2's schema through discovery.
func MCPToolName(key SkillKey) string { return key.String() }

// MCPToolsList projects all active skill versions in deterministic order.
// Deprecated versions remain available through ResolvePin for existing tasks,
// but are not recommended to newly-discovering agents.
func (r *Registry) MCPToolsList() MCPToolsList {
	records := r.List()
	tools := make([]MCPTool, 0, len(records))
	for _, record := range records {
		if record.Status != StatusActive {
			continue
		}
		tools = append(tools, projectTool(record))
	}
	return MCPToolsList{Tools: tools}
}

// MCPToolsForPins projects the exact versions admitted to a task. It fails
// closed on an unknown, stale, or retired pin.
func (r *Registry) MCPToolsForPins(pins []SkillPin) (MCPToolsList, error) {
	records := make([]SkillRecord, 0, len(pins))
	seen := make(map[SkillKey]struct{}, len(pins))
	for _, pin := range pins {
		key := pin.Key()
		if _, exists := seen[key]; exists {
			return MCPToolsList{}, fmt.Errorf("%w: %s", ErrDuplicateSkillPin, key)
		}
		seen[key] = struct{}{}
		record, err := r.ResolvePin(pin)
		if err != nil {
			return MCPToolsList{}, err
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return MCPToolName(records[i].Definition.Key()) < MCPToolName(records[j].Definition.Key())
	})
	tools := make([]MCPTool, 0, len(records))
	for _, record := range records {
		tools = append(tools, projectTool(record))
	}
	return MCPToolsList{Tools: tools}, nil
}

func projectTool(record SkillRecord) MCPTool {
	return MCPTool{
		Name:         MCPToolName(record.Definition.Key()),
		Description:  record.Definition.Description,
		InputSchema:  append(json.RawMessage(nil), record.Definition.InputSchema...),
		OutputSchema: append(json.RawMessage(nil), record.Definition.OutputSchema...),
		Annotations: MCPToolAnnotations{
			ReadOnlyHint:    record.HighestCapabilityTier == TierRead && record.Definition.SideEffectTier == TierRead,
			DestructiveHint: record.Definition.SideEffectTier >= TierSubmitGoverned,
		},
	}
}

// Conformance checks this registry's projection against the pinned MCP
// tools/list shape. It validates the wire object as well as the typed values,
// so accidental JSON-tag or nil-schema changes are caught before publication.
func (r *Registry) Conformance(protocol string) error {
	return ValidateMCPProjection(protocol, r.MCPToolsList())
}

func ValidateMCPProjection(protocol string, projection MCPToolsList) error {
	if protocol != MCPProtocolRevision {
		return fmt.Errorf("agentskills: unsupported MCP protocol revision %q", protocol)
	}
	if projection.Tools == nil {
		return fmt.Errorf("agentskills: MCP tools must be a non-nil array")
	}
	encoded, err := json.Marshal(projection)
	if err != nil {
		return fmt.Errorf("agentskills: marshal MCP projection: %w", err)
	}
	var wire struct {
		Tools []struct {
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			InputSchema  json.RawMessage `json:"inputSchema"`
			OutputSchema json.RawMessage `json:"outputSchema"`
			Annotations  struct {
				ReadOnlyHint    bool `json:"readOnlyHint"`
				DestructiveHint bool `json:"destructiveHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return fmt.Errorf("agentskills: decode MCP projection: %w", err)
	}
	seen := make(map[string]struct{}, len(projection.Tools))
	for index, tool := range wire.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return fmt.Errorf("agentskills: tool %d has no name", index)
		}
		if _, exists := seen[tool.Name]; exists {
			return fmt.Errorf("agentskills: duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = struct{}{}
		if strings.TrimSpace(tool.Description) == "" {
			return fmt.Errorf("agentskills: tool %q has no description", tool.Name)
		}
		if !isMCPObjectSchema(tool.InputSchema) || !isMCPObjectSchema(tool.OutputSchema) {
			return fmt.Errorf("agentskills: tool %q has a non-object MCP JSON schema", tool.Name)
		}
	}
	return nil
}
