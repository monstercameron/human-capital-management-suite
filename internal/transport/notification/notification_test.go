package notification

import (
	"context"
	"errors"
	"testing"

	notificationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
)

type fakeHandler struct {
	err error

	listReq    *notificationv1.ListNotificationsRequest
	readReq    *notificationv1.MarkNotificationReadRequest
	archiveReq *notificationv1.ArchiveNotificationRequest
	pinReq     *notificationv1.PinNotificationRequest
}

func (f *fakeHandler) ListNotifications(_ context.Context, req *notificationv1.ListNotificationsRequest) (*notificationv1.ListNotificationsResponse, error) {
	f.listReq = req
	return &notificationv1.ListNotificationsResponse{}, f.err
}

func (f *fakeHandler) MarkNotificationRead(_ context.Context, req *notificationv1.MarkNotificationReadRequest) (*notificationv1.MarkNotificationReadResponse, error) {
	f.readReq = req
	return &notificationv1.MarkNotificationReadResponse{}, f.err
}

func (f *fakeHandler) ArchiveNotification(_ context.Context, req *notificationv1.ArchiveNotificationRequest) (*notificationv1.ArchiveNotificationResponse, error) {
	f.archiveReq = req
	return &notificationv1.ArchiveNotificationResponse{}, f.err
}

func (f *fakeHandler) PinNotification(_ context.Context, req *notificationv1.PinNotificationRequest) (*notificationv1.PinNotificationResponse, error) {
	f.pinReq = req
	return &notificationv1.PinNotificationResponse{}, f.err
}

// TestTodo_NAAS_004 proves Service is a pure forward to the Notification
// handler port: every method delivers the caller's exact request to the
// handler and surfaces the handler's error instead of a fabricated
// success. It also pins compile-time conformance to the generated server
// interface, so a proto change that adds an RPC fails here rather than
// silently unserved.
func TestTodo_NAAS_004(t *testing.T) {
	var _ notificationv1.NotificationServiceServer = (*Service)(nil)

	fake := &fakeHandler{}
	svc := &Service{Handler: fake}
	ctx := context.Background()

	listReq := &notificationv1.ListNotificationsRequest{}
	if _, err := svc.ListNotifications(ctx, listReq); err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if fake.listReq != listReq {
		t.Fatal("ListNotifications did not forward the caller's request")
	}

	readReq := &notificationv1.MarkNotificationReadRequest{}
	if _, err := svc.MarkNotificationRead(ctx, readReq); err != nil {
		t.Fatalf("MarkNotificationRead: %v", err)
	}
	if fake.readReq != readReq {
		t.Fatal("MarkNotificationRead did not forward the caller's request")
	}

	archiveReq := &notificationv1.ArchiveNotificationRequest{}
	if _, err := svc.ArchiveNotification(ctx, archiveReq); err != nil {
		t.Fatalf("ArchiveNotification: %v", err)
	}
	if fake.archiveReq != archiveReq {
		t.Fatal("ArchiveNotification did not forward the caller's request")
	}

	pinReq := &notificationv1.PinNotificationRequest{}
	if _, err := svc.PinNotification(ctx, pinReq); err != nil {
		t.Fatalf("PinNotification: %v", err)
	}
	if fake.pinReq != pinReq {
		t.Fatal("PinNotification did not forward the caller's request")
	}

	sentinel := errors.New("handler refused")
	fake.err = sentinel
	if _, err := svc.ListNotifications(ctx, listReq); !errors.Is(err, sentinel) {
		t.Fatalf("ListNotifications error = %v, want the handler's error", err)
	}
	if _, err := svc.MarkNotificationRead(ctx, readReq); !errors.Is(err, sentinel) {
		t.Fatalf("MarkNotificationRead error = %v, want the handler's error", err)
	}
	if _, err := svc.ArchiveNotification(ctx, archiveReq); !errors.Is(err, sentinel) {
		t.Fatalf("ArchiveNotification error = %v, want the handler's error", err)
	}
	if _, err := svc.PinNotification(ctx, pinReq); !errors.Is(err, sentinel) {
		t.Fatalf("PinNotification error = %v, want the handler's error", err)
	}
}
