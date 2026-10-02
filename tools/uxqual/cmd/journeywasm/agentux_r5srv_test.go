package main

import (
	"testing"

	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestAgentUXR5Srv_DocumentUsageProjection(t *testing.T) {
	projection := &agentv1.AgentTaskProjection{
		TaskId: "task-a", State: "COMPLETED",
		DocumentUsageState:     agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_USED,
		UsedDocumentReferences: []*agentv1.AgentDocumentReference{{DocumentId: "policy-a", Label: "Leave policy", SectionAnchor: "eligibility"}},
	}
	task, ok := projectAgentTask(projection)
	if !ok || task.DocumentUsageState != productui.AgentDocumentUsageUsed || len(task.UsedDocuments) != 1 || task.UsedDocuments[0].DocumentID != "policy-a" || task.UsedDocuments[0].Label != "Leave policy" || task.UsedDocuments[0].SectionAnchor != "eligibility" {
		t.Fatalf("document usage = %+v, ok=%t", task, ok)
	}

	projection.DocumentUsageState = agentv1.AgentDocumentUsageState_AGENT_DOCUMENT_USAGE_STATE_NONE
	projection.UsedDocumentReferences = nil
	task, ok = projectAgentTask(projection)
	if !ok || task.DocumentUsageState != productui.AgentDocumentUsageNone || len(task.UsedDocuments) != 0 {
		t.Fatalf("no-document usage = %+v, ok=%t", task, ok)
	}
}
