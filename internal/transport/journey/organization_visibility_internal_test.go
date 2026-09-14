package journey

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

func TestTodo_UXAUDIT_004_PropertyAmbiguousIdentitiesAndCyclesFailClosed(t *testing.T) {
	fixtures := []workspace.WorkerSummary{
		{WorkerRef: "manager-a", WorkerID: "collision", PreferredName: "Manager A", ManagerDisposition: workspace.ManagerRelationshipRoot},
		{WorkerRef: "collision", WorkerID: "manager-b-id", PreferredName: "Manager B", ManagerDisposition: workspace.ManagerRelationshipRoot},
		{WorkerRef: "report", WorkerID: "report-id", PreferredName: "Report", ManagerRef: "collision"},
	}
	for rotation := 0; rotation < len(fixtures); rotation++ {
		workers := append([]workspace.WorkerSummary(nil), fixtures[rotation:]...)
		workers = append(workers, fixtures[:rotation]...)
		projected := projectAuthorizedManagers(workers, workers)
		for _, worker := range projected {
			if worker.WorkerRef == "report" && (worker.ManagerDisposition != workspace.ManagerRelationshipOrphan || worker.ManagerWorkerRef != "" || worker.ManagerRef != "") {
				t.Fatalf("rotation %d resolved ambiguous endpoint: %+v", rotation, worker)
			}
		}
	}

	cycle := []workspace.WorkerSummary{
		{WorkerRef: "cycle-a", WorkerID: "cycle-a-id", ManagerRef: "cycle-b"},
		{WorkerRef: "cycle-b", WorkerID: "cycle-b-id", ManagerRef: "cycle-a"},
		{WorkerRef: "cycle-child", WorkerID: "cycle-child-id", ManagerRef: "cycle-a"},
	}
	projected := projectAuthorizedManagers(cycle, cycle)
	for _, worker := range projected {
		switch worker.WorkerRef {
		case "cycle-a", "cycle-b":
			if worker.ManagerDisposition != workspace.ManagerRelationshipOrphan || worker.ManagerWorkerRef != "" || worker.ManagerRef != "" {
				t.Fatalf("cycle member retained invented edge: %+v", worker)
			}
		case "cycle-child":
			if worker.ManagerDisposition != workspace.ManagerRelationshipVisible || worker.ManagerWorkerRef != "cycle-a" {
				t.Fatalf("non-cycle report lost its valid edge: %+v", worker)
			}
		}
	}
}
