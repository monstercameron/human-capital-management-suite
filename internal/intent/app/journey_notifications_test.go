package app

import (
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

func TestTodo_NAAS_001_Security(t *testing.T) {
	tenant, instance, itemID := uuid.New(), uuid.New(), uuid.New()
	record := inbox.WorkflowRecord{Record: inbox.Record{TenantID: tenant, SubjectRef: "owner", InboxRecordID: uuid.New(), ReadState: inbox.Unread}, WorkItemID: itemID, InstanceID: instance, Purpose: "APPROVAL"}
	visible := map[string]workspace.JourneySummary{instance.String(): {IntentID: "journey", WorkerName: "Authorized employee"}}
	item := workitem.WorkItem{TenantID: tenant, WorkItemID: itemID, WorkflowInstanceID: instance, Status: workitem.StatusAssigned, Assignment: workitem.Assignment{ChosenOwner: "owner"}}
	for _, tc := range []struct {
		name, subject string
		mutate        func(*workitem.WorkItem)
		hide          bool
		want          int
	}{
		{"recipient", "owner", nil, false, 1}, {"other recipient", "other", nil, false, 0}, {"anonymous", "", nil, false, 0}, {"access revoked", "owner", nil, true, 0},
		{"reassigned", "owner", func(w *workitem.WorkItem) { w.Assignment.ChosenOwner = "new-owner" }, false, 0},
		{"other tenant", "owner", func(w *workitem.WorkItem) { w.TenantID = uuid.New() }, false, 0},
		{"other instance", "owner", func(w *workitem.WorkItem) { w.WorkflowInstanceID = uuid.New() }, false, 0},
		{"completed", "owner", func(w *workitem.WorkItem) { w.Status = workitem.StatusCompleted }, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := item
			if tc.mutate != nil {
				tc.mutate(&candidate)
			}
			views := visible
			if tc.hide {
				views = nil
			}
			got := projectWorkflowNotifications(tc.subject, []inbox.WorkflowRecord{record}, views, map[uuid.UUID]workitem.WorkItem{itemID: candidate})
			if len(got) != tc.want {
				t.Fatalf("notices=%+v want %d", got, tc.want)
			}
			if len(got) > 0 && (got[0].JourneyID != "journey" || got[0].Status != string(candidate.Status) || got[0].Read) {
				t.Fatalf("incorrect projection: %+v", got)
			}
		})
	}
}
