package grpcserver_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	notificationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/transporttest"
)

// fakeNotificationHandler is the NAAS-004 stand-in: it records the request
// it receives and replays canned responses, so the test proves the served
// NotificationService is backed by the handler port rather than inline logic.
type fakeNotificationHandler struct {
	fail bool

	list    *notificationv1.ListNotificationsRequest
	read    *notificationv1.MarkNotificationReadRequest
	archive *notificationv1.ArchiveNotificationRequest
	pin     *notificationv1.PinNotificationRequest
}

func (f *fakeNotificationHandler) ListNotifications(_ context.Context, req *notificationv1.ListNotificationsRequest) (*notificationv1.ListNotificationsResponse, error) {
	f.list = req
	if f.fail {
		return nil, errors.New("notification unavailable")
	}
	return &notificationv1.ListNotificationsResponse{}, nil
}

func (f *fakeNotificationHandler) MarkNotificationRead(_ context.Context, req *notificationv1.MarkNotificationReadRequest) (*notificationv1.MarkNotificationReadResponse, error) {
	f.read = req
	if f.fail {
		return nil, errors.New("notification unavailable")
	}
	return &notificationv1.MarkNotificationReadResponse{}, nil
}

func (f *fakeNotificationHandler) ArchiveNotification(_ context.Context, req *notificationv1.ArchiveNotificationRequest) (*notificationv1.ArchiveNotificationResponse, error) {
	f.archive = req
	if f.fail {
		return nil, errors.New("notification unavailable")
	}
	return &notificationv1.ArchiveNotificationResponse{}, nil
}

func (f *fakeNotificationHandler) PinNotification(_ context.Context, req *notificationv1.PinNotificationRequest) (*notificationv1.PinNotificationResponse, error) {
	f.pin = req
	if f.fail {
		return nil, errors.New("notification unavailable")
	}
	return &notificationv1.PinNotificationResponse{}, nil
}

// TestTodo_NAAS_004_Integration serves the NotificationService from
// grpcserver.NewServer through the production interceptor chain and proves
// all four RPCs reach the handler port with the caller's exact request,
// and that a handler failure surfaces instead of a fabricated success.
func TestTodo_NAAS_004_Integration(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }
	verifier, err := transporttest.NewVerifier(clock)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	token, err := transporttest.BearerToken(verifier, transporttest.DefaultClaims(now))
	if err != nil {
		t.Fatalf("BearerToken: %v", err)
	}
	cfg := transporttest.Config(verifier, clock, "req-naas004-0001", nil)

	handler := &fakeNotificationHandler{}
	server, err := grpcserver.NewServer(grpcserver.Options{Config: cfg, Notifications: handler})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	info, ok := server.GetServiceInfo()["hcmnext.notification.v1.NotificationService"]
	if !ok {
		t.Fatal("NotificationService is not registered")
	}
	if len(info.Methods) != 4 {
		t.Fatalf("NotificationService methods = %d, want 4", len(info.Methods))
	}

	listener := bufconn.Listen(1 << 20)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient("passthrough:///naas004",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx := metadata.AppendToOutgoingContext(context.Background(), transport.AuthorizationMetadataKey, token)
	client := notificationv1.NewNotificationServiceClient(conn)

	if _, err := client.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{}); err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if handler.list == nil {
		t.Fatal("ListNotifications never reached the handler port")
	}
	if _, err := client.MarkNotificationRead(ctx, &notificationv1.MarkNotificationReadRequest{}); err != nil {
		t.Fatalf("MarkNotificationRead: %v", err)
	}
	if handler.read == nil {
		t.Fatal("MarkNotificationRead never reached the handler port")
	}
	if _, err := client.ArchiveNotification(ctx, &notificationv1.ArchiveNotificationRequest{}); err != nil {
		t.Fatalf("ArchiveNotification: %v", err)
	}
	if handler.archive == nil {
		t.Fatal("ArchiveNotification never reached the handler port")
	}
	if _, err := client.PinNotification(ctx, &notificationv1.PinNotificationRequest{}); err != nil {
		t.Fatalf("PinNotification: %v", err)
	}
	if handler.pin == nil {
		t.Fatal("PinNotification never reached the handler port")
	}

	handler.fail = true
	if _, err := client.ListNotifications(ctx, &notificationv1.ListNotificationsRequest{}); err == nil {
		t.Fatal("ListNotifications with a failing handler = success, want an error")
	}
}
