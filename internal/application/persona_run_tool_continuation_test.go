package application

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestPersonaRunToolContinuation_BindsUniqueStepAndTaintsToolResult(t *testing.T) {
	for _, source := range []string{"persona-invoking-post", "synthetic-fixture"} {
		t.Run(source, func(t *testing.T) { testPersonaRunToolContinuation(t, source) })
	}
}

func testPersonaRunToolContinuation(t *testing.T, source string) {
	request := executorAdapterRequest(t)
	request.ToolResultClass = trustdlp.ClassPII
	request.Model = agentmodel.ModelRequest{
		ContractVersion: agentmodel.ContractVersion, TaskProfile: "profile-1", ModelProfile: "model-profile",
		Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "summarize leave policy"}},
		Output:   agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: time.Date(2026, 9, 30, 13, 0, 0, 0, time.UTC),
		Limits: agentmodel.ModelLimits{MaxOutputTokens: 300, MaxCostMicros: 1000}, TraceID: "task-1",
		Processing: agentmodel.ProcessingPolicy{Residency: "us", Retention: "zero", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied},
	}
	request.Outbound.Fields = []agentegress.Field{{Name: "model.message.0", Value: "summarize leave policy", Class: trustdlp.ClassPublic, Taint: []string{"PERSONA_INVOKING_POST"}, Provenance: []string{"post:invoke-1"}}}
	request.Outbound.DeclaredFields = []string{"model.message.0"}
	request.FieldSources = map[string]string{"model.message.0": source}
	request.Route.TraceID = request.Model.TraceID
	request.Model.Tools = []agentmodel.ToolSchema{{Name: personaDocumentSearchTool, InputSchema: json.RawMessage(`{"type":"object"}`)}}
	if err := bindPersonaRunModelStep(&request, "task-1", 1); err != nil {
		t.Fatal(err)
	}
	firstStep := request.StepID
	if err := bindPersonaRunModelStep(&request, "task-1", 2); err != nil {
		t.Fatal(err)
	}
	if request.StepID == firstStep || request.StepID != request.Route.TraceID || request.StepID != request.Model.TraceID || request.Task.TaskID != "task-1" || request.Outbound.TaskID != "task-1" {
		t.Fatalf("step binding altered durable identity or reused step: req=%+v", request)
	}
	request.Model.Tools = nil
	proposal := agentmodel.ToolProposal{ID: "call-1", Name: personaDocumentSearchTool, Arguments: json.RawMessage(`{"query":"leave"}`)}
	result := []byte(`{"hits":[{"document_id":"policy-1"}]}`)
	if err := appendPersonaRunToolContinuation(&request, proposal, result); err != nil {
		t.Fatalf("append continuation: %v", err)
	}
	if err := validateExecutorRequest(request); err != nil {
		t.Fatalf("validate continuation: %v", err)
	}
	if request.FieldSources["model.message.0"] != source {
		t.Fatal("continuation replaced the owner's original source classification")
	}
	if err := agentmodel.ValidateModelRequest(request.Model); err != nil {
		t.Fatalf("validate transcript: %v", err)
	}
	if len(request.Model.Messages) != 3 || request.Model.Messages[1].ToolCallID != "call-1" || request.Model.Messages[2].Role != agentmodel.RoleTool || request.Model.Messages[2].ToolCallID != "call-1" || request.Model.Messages[2].Content != string(result) {
		t.Fatalf("provider-neutral continuation transcript=%+v", request.Model.Messages)
	}
	field := request.Outbound.Fields[2]
	if field.Name != "model.message.2" || field.Class != trustdlp.ClassPII || !strings.Contains(strings.Join(field.Taint, ","), "UNTRUSTED_TOOL_RESULT") || request.FieldSources[field.Name] != "persona-untrusted-tool-result" {
		t.Fatalf("tool result egress field lost taint/class/source: %+v sources=%v", field, request.FieldSources)
	}
}
