package cell

import (
	notificationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	transportnotification "github.com/monstercameron/human-capital-management-suite/internal/transport/notification"
	"google.golang.org/grpc"
)

// RegisterNotificationFeed adds the served recipient-scoped notification
// handler to the already configured main gRPC server.
func RegisterNotificationFeed(server *grpc.Server, handler transport.NotificationHandler) {
	notificationv1.RegisterNotificationServiceServer(server, &transportnotification.Service{Handler: handler})
}
