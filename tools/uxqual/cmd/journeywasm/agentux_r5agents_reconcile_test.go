package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestAgentUXR5Agents_ReconcilePreservesDocumentUse(t *testing.T) {
	previous := productui.AgentTask{
		ID:         "leave",
		AnswerText: "Old answer",
		Documents:  []productui.AgentTaskDocumentReference{{DocumentID: "old", Label: "Old document"}},
	}
	projection := productui.AgentTask{
		ID:                 "leave",
		State:              productui.AgentTaskCompleted,
		ResultPreview:      "The leave policy allows this.",
		DocumentUsageState: productui.AgentDocumentUsageUsed,
		UsedDocuments:      []productui.AgentTaskDocumentReference{{DocumentID: "leave-policy", Label: "Leave policy", SectionAnchor: "carry-over"}},
	}

	merged := mergeAgentTask(previous, projection)
	if merged.DocumentUsageState != productui.AgentDocumentUsageUsed || len(merged.UsedDocuments) != 1 || merged.UsedDocuments[0].DocumentID != "leave-policy" || merged.UsedDocuments[0].SectionAnchor != "carry-over" {
		t.Fatalf("document use was lost while reconciling a task: %#v", merged)
	}
	if merged.AnswerText != "Old answer" || merged.ResultPreview != projection.ResultPreview {
		t.Fatalf("reconciliation did not retain the answer or the refreshed preview: %#v", merged)
	}
}
