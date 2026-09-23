package app

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/inbox"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/notifyplan"
)

// TestTodo_WF_NOTIFY_001_Security proves a status notice is disclosed only to its
// recipient and only while they can still see the request, and that how it
// ended is read from the request's current stage rather than stored copy.
func TestTodo_WF_NOTIFY_001_Security(t *testing.T) {
	tenant, seen, hidden := uuid.New(), uuid.New(), uuid.New()
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	record := func(instance uuid.UUID, subject, event string) inbox.WorkflowStatusRecord {
		return inbox.WorkflowStatusRecord{Record: inbox.Record{TenantID: tenant, InboxRecordID: uuid.New(), SubjectRef: subject, ReadState: inbox.Unread, CreatedAt: at},
			InstanceID: instance, Event: event, EventRef: "ref"}
	}
	records := []inbox.WorkflowStatusRecord{
		record(seen, "principal:rafael", inbox.StatusFinished),
		record(seen, "principal:rafael", inbox.StatusInReview),
		record(hidden, "principal:rafael", inbox.StatusFinished),
		record(seen, "principal:someone-else", inbox.StatusFinished),
	}
	visible := []workspace.JourneySummary{{IntentID: "intent-1", InstanceID: seen.String(), WorkerName: "Adrian Cole", Stage: workspace.JourneyStageRejected}, {IntentID: "intent-unstarted"}}

	got := projectWorkflowStatusNotifications("principal:rafael", records, visible)
	if len(got) != 1 {
		t.Fatalf("disclosed %d notices, want the latest one for the recipient's visible request: %+v", len(got), got)
	}
	for i, want := range []string{notifyplan.StatusFinishedDeclined} {
		if got[i].Status != want || got[i].Purpose != notifyplan.PurposeUpdate || got[i].JourneyID != "intent-1" || got[i].WorkerName != "Adrian Cole" || got[i].WorkItemID != "" || got[i].Read {
			t.Fatalf("notice %d: %+v, want status %s", i, got[i], want)
		}
	}
	if earlier := projectWorkflowStatusNotifications("principal:rafael", records[1:], visible); len(earlier) != 1 || earlier[0].Status != notifyplan.StatusSentForReview {
		t.Fatalf("before the request finished: %+v", earlier)
	}
	if len(projectWorkflowStatusNotifications("", records, visible)) != 0 {
		t.Fatal("an anonymous caller was shown notices")
	}
	if len(projectWorkflowStatusNotifications("principal:rafael", records, nil)) != 0 {
		t.Fatal("notices outlived the caller's sight of the request")
	}
	for stage, want := range map[workspace.JourneyStage]string{
		workspace.JourneyStageCompleted: notifyplan.StatusFinishedApproved, workspace.JourneyStageRecorded: notifyplan.StatusFinishedApproved,
		workspace.JourneyStageRejected: notifyplan.StatusFinishedDeclined, workspace.JourneyStageFailed: notifyplan.StatusFinishedFailed,
		workspace.JourneyStageRepairRequired: notifyplan.StatusFinishedFailed, workspace.JourneyStageProposed: notifyplan.StatusFinished,
	} {
		if got := workspace.FinishedNotificationStatus(stage); got != want {
			t.Fatalf("stage %s worded as %s, want %s", stage, got, want)
		}
	}
}
