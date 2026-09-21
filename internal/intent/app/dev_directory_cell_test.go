package app

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

type devDirectoryStub struct {
	snapshot workspace.DevDirectorySnapshot
}

func (stub devDirectoryStub) DevDirectorySnapshot() workspace.DevDirectorySnapshot {
	return stub.snapshot
}

// TestCellDevDirectoryCarriesTheComposedReaderOrNothing pins the accessor
// internal/transport/cell reads when it builds the workspace handler. A cell
// composed without a development directory must report none: that nil is the
// production shape, and the workspace handler refuses the reader outright
// unless the dev browser login is also on.
func TestCellDevDirectoryCarriesTheComposedReaderOrNothing(t *testing.T) {
	var production Cell
	if production.DevDirectory() != nil {
		t.Fatal("a cell composed with no development directory reported one")
	}
	stub := devDirectoryStub{snapshot: workspace.DevDirectorySnapshot{
		Units:     []workspace.DevDirectoryUnit{{Code: "harborcare", Name: "HarborCare Health Services"}},
		Employees: []workspace.DevDirectoryEmployee{{PersonaID: "employee:hc-001", WorkerKey: "hc-001", Name: "Amina Rahman"}},
	}}
	local := Cell{devDirectory: stub}
	got := local.DevDirectory()
	if got == nil {
		t.Fatal("a composed development directory was dropped")
	}
	snapshot := got.DevDirectorySnapshot()
	if len(snapshot.Units) != 1 || len(snapshot.Employees) != 1 || snapshot.Employees[0].Name != "Amina Rahman" {
		t.Fatalf("directory snapshot = %+v", snapshot)
	}
}
