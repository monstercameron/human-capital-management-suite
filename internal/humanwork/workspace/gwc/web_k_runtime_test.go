package gwc

import (
	"strings"
	"testing"
	"time"

	contract "github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
)

// TestServedRendererBridge proves the document boundary used by the
// workspace server executes the governed renderer bridge before serving the
// already-authorized GWC projection, and remains deterministic.
func TestServedRendererBridge(t *testing.T) {
	source := contract.SourceRecord{
		WorkspaceID: "served-runtime-contract",
		Title:       "Promotion review",
		WorkerID:    "worker-1",
		WorkerName:  "Avery Okafor",
		AllFields: []contract.RequestField{{
			ID: "reason", Label: "Business reason", Kind: contract.FieldKindTextarea, Value: "Expanded scope",
		}},
		Simulation: contract.SimulationResult{
			Status:      contract.SimulationReady,
			Summary:     "Ready for review",
			GeneratedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
		},
	}
	workspace := contract.NewWorkspaceContract(source, contract.Allow("reason"), contract.Allow())
	first, err := Document(workspace)
	if err != nil {
		t.Fatalf("served GWC document: %v", err)
	}
	second, err := Document(workspace)
	if err != nil {
		t.Fatalf("served GWC document second render: %v", err)
	}
	if first != second {
		t.Fatal("served GWC document changed between identical renders")
	}
	for _, want := range []string{"Promotion review", `id="reason"`, "@media (forced-colors:active)", "@media print"} {
		if !strings.Contains(first, want) {
			t.Fatalf("served GWC document missing %q", want)
		}
	}
}
