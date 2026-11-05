package journey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// overlapNotificationEngine rendezvouses the two inbox reads: each reader
// announces its arrival on its own channel, then waits for the other's
// announcement before answering. Run one after the other, the first reader
// waits out its whole timeout and fails; run together, both arrive and both
// answer.
type overlapNotificationEngine struct {
	*fakeEngine
	approvalArrived chan struct{}
	statusArrived   chan struct{}
}

func (e *overlapNotificationEngine) meet(ctx context.Context, self string, mine chan<- struct{}, other <-chan struct{}) error {
	select {
	case mine <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-other:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return errors.New(self + " read never overlapped the other inbox read")
	}
}

func (e *overlapNotificationEngine) WorkflowNotifications(ctx context.Context, _ []workspace.JourneySummary) ([]workspace.WorkflowNotification, error) {
	if err := e.meet(ctx, "approval", e.approvalArrived, e.statusArrived); err != nil {
		return nil, err
	}
	return []workspace.WorkflowNotification{{ID: "notice", Purpose: "APPROVAL", Status: "ASSIGNED",
		CreatedAt: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)}}, nil
}

func (e *overlapNotificationEngine) WorkflowStatusNotifications(ctx context.Context, _ []workspace.JourneySummary) ([]workspace.WorkflowNotification, error) {
	if err := e.meet(ctx, "status", e.statusArrived, e.approvalArrived); err != nil {
		return nil, err
	}
	return []workspace.WorkflowNotification{{ID: "status", Purpose: "UPDATE", Status: "FINISHED",
		CreatedAt: time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)}}, nil
}

// TestTodo_UXBLIND_089_NotificationOverlap is the RED for the cold-load
// window's slowest leg: ListJourneys read its two inboxes one after the
// other, paying two sequential transactions on every cold load. The reads are
// independent, so they must overlap; the merged answer keeps the exact shape
// the sequential read produced.
func TestTodo_UXBLIND_089_NotificationOverlap(t *testing.T) {
	engine := &overlapNotificationEngine{fakeEngine: newFakeEngine(),
		approvalArrived: make(chan struct{}, 1), statusArrived: make(chan struct{}, 1)}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	resp, err := client.ListJourneys(testContext(t), &journeyv1.ListJourneysRequest{})
	if err != nil {
		t.Fatalf("ListJourneys: %v", err)
	}
	if resp.GetNotificationsUnavailable() {
		t.Fatal("the inbox reads did not overlap: one failed waiting for the other")
	}
	if len(resp.GetNotifications()) != 2 {
		t.Fatalf("notifications = %d, want the approval and the status notice", len(resp.GetNotifications()))
	}
	if resp.GetNotifications()[0].GetNotificationId() != "status" {
		t.Fatalf("first notice = %q, want the newer status notice first", resp.GetNotifications()[0].GetNotificationId())
	}
}
