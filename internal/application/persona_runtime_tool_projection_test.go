package application

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

func TestTodo_AGENTP_008_RuntimeToolProjectionAdmitsOnlyProjectedProposal(t *testing.T) {
	if err := validatePersonaRunToolProjection(nil); err != nil {
		t.Fatalf("empty authorized tools refused text-only model: %v", err)
	}
	schema := agentmodel.ToolSchema{Name: personaDocumentSearchTool, InputSchema: personaDocumentSearchSchema}
	proposal := agentmodel.ToolProposal{ID: "call-a", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave"}`)}
	if !personaRunToolProposalProjected([]agentmodel.ToolSchema{schema}, proposal) {
		t.Fatal("projected proposal refused")
	}
	if personaRunToolProposalProjected(nil, proposal) {
		t.Fatal("empty tool set executed provider proposal")
	}
	proposal.Name = "provider_invented"
	if personaRunToolProposalProjected([]agentmodel.ToolSchema{schema}, proposal) {
		t.Fatal("unprojected proposal accepted")
	}
	if err := validatePersonaRunToolProjection([]agentmodel.ToolSchema{schema, schema}); err == nil {
		t.Fatal("duplicate tool name accepted")
	}
	schema.InputSchema = json.RawMessage(`{"type":"string"}`)
	if err := validatePersonaRunToolProjection([]agentmodel.ToolSchema{schema}); err == nil {
		t.Fatal("non-object tool schema accepted")
	}
}
