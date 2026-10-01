package application

import (
	"testing"
	"time"

	"github.com/google/uuid"
	notificationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
)

// TestTodo_UXBLIND_056_DurableRead proves opening a notice survives a fresh
// feed read, while recipient isolation and a visibility revocation still hold.
func TestTodo_UXBLIND_056_DurableRead(t *testing.T) {
	fx := newNotificationFixture(t)
	instance := uuid.New()
	seed := fx.publish(t, "reader", "APPROVAL", "uxblind-056", instance, fx.now.Add(-time.Minute))
	ctx := fx.principal(t, "naas4", "reader")

	before, err := fx.feed.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil || len(before.Notifications) != 1 || before.Notifications[0].ReadState != "UNREAD" {
		t.Fatalf("before read = (%+v, %v), want one UNREAD notice", before, err)
	}
	if _, err := fx.feed.MarkNotificationRead(ctx, &notificationv1.MarkNotificationReadRequest{Id: seed.InboxRecordID.String(), ExpectedVersion: before.Notifications[0].Version}); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	after, err := fx.feed.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil || len(after.Notifications) != 1 || after.Notifications[0].ReadState != "READ" {
		t.Fatalf("fresh read = (%+v, %v), want one READ notice", after, err)
	}
	other, err := fx.feed.ListNotifications(fx.principal(t, "naas4", "other"), &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil || len(other.Notifications) != 0 {
		t.Fatalf("other recipient feed = (%+v, %v), want empty", other, err)
	}
	fx.vis.hidden[instance] = true
	revoked, err := fx.feed.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{PageSize: 10})
	if err != nil || len(revoked.Notifications) != 0 {
		t.Fatalf("revoked feed = (%+v, %v), want empty", revoked, err)
	}
}
