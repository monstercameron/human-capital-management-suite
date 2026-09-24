package journey

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

// A persisted manager relationship names the manager by the stored worker
// key (the subject), while the listing's worker_ref is a display slug. The
// manager must still resolve, and the endpoint disclosed is the slug.
func TestProjectAuthorizedManagersResolvesTheManagerSubject(t *testing.T) {
	workers := []workspace.WorkerSummary{
		{WorkerRef: "mateo-1a2b3c4d", WorkerID: "0e9c-mateo", SubjectID: "hc-002-mateo-alvarez", ManagerDisposition: workspace.ManagerRelationshipRoot},
		{WorkerRef: "camila-5e6f7a8b", WorkerID: "0e9c-camila", SubjectID: "hc-043-camila-morales", ManagerRef: "hc-002-mateo-alvarez"},
	}
	got := projectAuthorizedManagers(workers, workers)
	if got[1].ManagerDisposition != workspace.ManagerRelationshipVisible || got[1].ManagerWorkerRef != "mateo-1a2b3c4d" {
		t.Fatalf("manager = %v %q, want the visible manager's reference", got[1].ManagerDisposition, got[1].ManagerWorkerRef)
	}
}
