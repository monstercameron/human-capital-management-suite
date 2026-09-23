// Package notification adapts the generated NotificationService server
// interface to the transport.NotificationHandler port. Every method is a
// forward: authentication, trusted-context construction, validation and
// error projection already happened in the interceptor chain, and nothing
// else belongs here.
package notification

import (
	"context"

	notificationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// Service adapts the generated NotificationService server interface to the
// transport.NotificationHandler port. The handler owns every rule; this
// type owns none.
type Service struct {
	notificationv1.UnimplementedNotificationServiceServer

	Handler transport.NotificationHandler
}

func (s *Service) ListNotifications(ctx context.Context, req *notificationv1.ListNotificationsRequest) (*notificationv1.ListNotificationsResponse, error) {
	return s.Handler.ListNotifications(ctx, req)
}

func (s *Service) MarkNotificationRead(ctx context.Context, req *notificationv1.MarkNotificationReadRequest) (*notificationv1.MarkNotificationReadResponse, error) {
	return s.Handler.MarkNotificationRead(ctx, req)
}

func (s *Service) ArchiveNotification(ctx context.Context, req *notificationv1.ArchiveNotificationRequest) (*notificationv1.ArchiveNotificationResponse, error) {
	return s.Handler.ArchiveNotification(ctx, req)
}

func (s *Service) PinNotification(ctx context.Context, req *notificationv1.PinNotificationRequest) (*notificationv1.PinNotificationResponse, error) {
	return s.Handler.PinNotification(ctx, req)
}
