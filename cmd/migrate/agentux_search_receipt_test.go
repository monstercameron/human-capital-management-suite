package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
)

func TestAgentUXSearch_PreparationReceipt(t *testing.T) {
	summary := application.LocalAgentDemoSummary{Version: 6, State: "PUBLISHED", AssistantVersion: 2, AssistantState: "PUBLISHED", WorkspaceIndex: application.WorkspaceIndexPreparation{Model: "static:potion-base-8M", DocumentsIndexed: 663, SectionsIndexed: 1200, Workspace: application.WorkspaceDocumentSearchStatus{WorkspaceDocuments: 600, WorkspacePending: 3, WorkspaceIndexedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}}}
	text := formatAgentDemoSummary(summary)
	for _, want := range []string{"Assistant v2: PUBLISHED", "documents_indexed=663", "sections_indexed=1200", "workspace_documents=600", "waiting=3", "indexed_at=2026-10-01T12:00:00Z"} {
		if !strings.Contains(text, want) {
			t.Fatalf("receipt lacks %s: %s", want, text)
		}
	}
	summary.WorkspaceIndex.DocumentsIndexed, summary.WorkspaceIndex.SectionsIndexed = 0, 0
	if text := formatAgentDemoSummary(summary); !strings.Contains(text, "already prepared; no changes") || !strings.Contains(text, "documents_indexed=0") {
		t.Fatal(text)
	}
	summary.WorkspaceIndex.Unavailable = "search by meaning unavailable: model files not installed. To turn it on, install tokenizer.json and model.safetensors"
	summary.WorkspaceDocumentsShared = 2
	if text := formatAgentDemoSummary(summary); !strings.Contains(text, "Warning: search by meaning unavailable: model files not installed") || !strings.Contains(text, "install tokenizer.json") || !strings.Contains(text, "documents_shared_with_workspace=2") || strings.Contains(text, "Preparation incomplete") {
		t.Fatal(text)
	}
}
