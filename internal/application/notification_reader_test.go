package application

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

type notificationReaderStub struct{}

func (notificationReaderStub) WorkflowNotifications(_ context.Context, _ []workspace.JourneySummary) ([]workspace.WorkflowNotification, error) {
	return nil, nil
}

func TestRequiredWorkflowNotificationReaderPreservesAuthGate(t *testing.T) {
	reader := notificationReaderStub{}
	got, err := requiredWorkflowNotificationReader(reader, true)
	if err != nil || got != reader {
		t.Fatalf("configured journey reader = (%T, %v), want original reader and no error", got, err)
	}
	if got, err := requiredWorkflowNotificationReader(nil, false); err != nil || got != nil {
		t.Fatalf("disabled execution reader = (%T, %v), want nil reader and no error", got, err)
	}
	if got, err := requiredWorkflowNotificationReader(nil, true); err == nil || got != nil {
		t.Fatalf("configured execution without current authorization = (%T, %v), want fail-closed error", got, err)
	}
}
