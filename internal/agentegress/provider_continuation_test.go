package agentegress

import (
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestTodo_AGENT_020_ToolContinuationBindsArguments(t *testing.T) {
	_, _, _, request := providerDispatchFixture(t)
	request.Model.Messages = append(request.Model.Messages, agentmodel.ModelMessage{Role: agentmodel.RoleAssistant, ToolCallID: "call-1", ToolName: "search", ToolArguments: json.RawMessage(`{"query":"leave"}`)}, agentmodel.ModelMessage{Role: agentmodel.RoleTool, ToolCallID: "call-1", Content: `{"hits":[]}`})
	request.Outbound.DeclaredFields = append(request.Outbound.DeclaredFields, "model.message.1", "model.message.2")
	request.Outbound.Fields = append(request.Outbound.Fields, Field{Name: "model.message.1", Value: `{"query":"leave"}`, Class: trustdlp.ClassPublic}, Field{Name: "model.message.2", Value: `{"hits":[]}`, Class: trustdlp.ClassPublic})
	if err := validateMessageBinding(request); err != nil {
		t.Fatalf("valid continuation=%v", err)
	}
	request.Outbound.Fields[1].Value = `{"query":"payroll"}`
	if err := validateMessageBinding(request); err == nil {
		t.Fatal("changed model arguments passed egress binding")
	}
}
