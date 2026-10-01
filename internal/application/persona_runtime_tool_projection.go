package application

import (
	"encoding/json"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// validatePersonaRunToolProjection checks the server-owned projection before
// provider egress. Empty projections permit a text reply; they grant no tools.
func validatePersonaRunToolProjection(schemas []agentmodel.ToolSchema) error {
	if len(schemas) > 32 {
		return errPersonaRuntimeTools
	}
	seen := make(map[string]struct{}, len(schemas))
	for _, schema := range schemas {
		if schema.Name == "" || len(schema.Name) > 128 || strings.TrimSpace(schema.Name) != schema.Name || len(schema.InputSchema) == 0 || len(schema.InputSchema) > 64*1024 {
			return errPersonaRuntimeTools
		}
		if _, duplicate := seen[schema.Name]; duplicate {
			return errPersonaRuntimeTools
		}
		seen[schema.Name] = struct{}{}
		var body map[string]json.RawMessage
		if json.Unmarshal(schema.InputSchema, &body) != nil || body == nil {
			return errPersonaRuntimeTools
		}
		var kind string
		if json.Unmarshal(body["type"], &kind) != nil || kind != "object" {
			return errPersonaRuntimeTools
		}
	}
	return nil
}

// personaRunToolProposalProjected rejects any provider proposal that was not
// named in this exact model request's current server-owned tool projection.
func personaRunToolProposalProjected(schemas []agentmodel.ToolSchema, proposal agentmodel.ToolProposal) bool {
	if validatePersonaRunToolProjection(schemas) != nil || strings.TrimSpace(proposal.ID) == "" || proposal.ID != strings.TrimSpace(proposal.ID) || !personaRunJSONObject(proposal.Arguments) {
		return false
	}
	for _, schema := range schemas {
		if schema.Name == proposal.Name {
			return true
		}
	}
	return false
}
