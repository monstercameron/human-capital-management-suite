package gwc

import (
	"strings"
	"testing"
	"time"

	contract "github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
)

func TestDocumentRendersOnlyVisibleWorkspaceContent(t *testing.T) {
	source := contract.SourceRecord{
		WorkspaceID: "workspace-9",
		Title:       "Promotion <review>",
		WorkerID:    "worker-9",
		WorkerName:  "Rae Example",
		AllFields: []contract.RequestField{
			{ID: "reason", Label: "Business reason", Kind: contract.FieldKindTextarea, Value: "Expanded scope"},
			{ID: "secret", Label: "National ID", Kind: contract.FieldKindText, Value: "555-11-2222"},
		},
		Simulation: contract.SimulationResult{
			Status:      contract.SimulationReady,
			Summary:     "Ready for review",
			GeneratedAt: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
		},
	}
	workspace := contract.NewWorkspaceContract(
		source,
		contract.Allow("reason"),
		contract.Allow(),
	)
	document, err := Document(workspace)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	for _, want := range []string{
		"<!doctype html>",
		"Promotion &lt;review&gt;",
		`id="reason"`,
		"Ready for review",
		"--color-background",
		".layout-page-frame",
	} {
		if !strings.Contains(document, want) {
			t.Errorf("rendered document does not contain %q", want)
		}
	}
	for _, hidden := range []string{"National ID", "555-11-2222", `id="secret"`} {
		if strings.Contains(document, hidden) {
			t.Errorf("masked content %q leaked into the rendered document", hidden)
		}
	}
}
