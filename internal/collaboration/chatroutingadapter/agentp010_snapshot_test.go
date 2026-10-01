package chatroutingadapter

import (
	"context"
	"errors"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type routedSnapshotOwner struct {
	*leaseRecorder
	denied  bool
	request chat.ThreadSnapshotRequest
}

func (s *routedSnapshotOwner) CaptureThreadSnapshot(ctx context.Context, r chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error) {
	if err := s.record(ctx, "snapshot"); err != nil {
		return chat.ThreadSnapshot{}, err
	}
	s.request = r
	if s.denied {
		return chat.ThreadSnapshot{}, chat.ErrPermissionDenied
	}
	return chat.ThreadSnapshot{TenantID: r.TenantID, ConversationID: r.ConversationID, ThreadID: r.ThreadID, InvokingPostID: r.InvokingPostID, PrincipalID: r.Principal.SubjectID, SnapshotID: "snapshot", Digest: "digest"}, nil
}

func (s *routedSnapshotOwner) WithThreadSnapshotFence(ctx context.Context, _ chat.ThreadSnapshot, fn func() error) error {
	if err := s.record(ctx, "fence"); err != nil {
		return err
	}
	if s.denied {
		return chat.ErrPermissionDenied
	}
	return fn()
}

func TestTodo_AGENTP_010_RoutedSnapshotPreservesOwnerAndFence(t *testing.T) {
	owner := &routedSnapshotOwner{leaseRecorder: newLeaseRecorder()}
	routed, directory := newAdapter(t, owner)
	placeNamedRoute(t, directory, "room", "tenant", "chat-shard")
	extension, ok := any(routed).(chat.ThreadSnapshotStore)
	if !ok {
		t.Fatal("routing decorator discarded the atomic thread snapshot extension")
	}
	request := chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: "tenant", SubjectID: "invoker"}, TenantID: "tenant", ConversationID: "room", ThreadID: "thread", InvokingPostID: "post", Limit: 50}
	snapshot, err := extension.CaptureThreadSnapshot(context.Background(), request)
	if err != nil || snapshot.InvokingPostID != "post" || owner.request.Principal.SubjectID != "invoker" || owner.request.Limit != 50 {
		t.Fatalf("snapshot forwarding: %+v %v", snapshot, err)
	}
	if lease := owner.seen["snapshot"]; lease.Route.HostTenantID != "tenant" || lease.Route.ShardID != "chat-shard" {
		t.Fatalf("snapshot route: %+v", lease)
	}
	called := 0
	if err := extension.WithThreadSnapshotFence(context.Background(), snapshot, func() error { called++; return nil }); err != nil || called != 1 {
		t.Fatalf("fenced continuation: called=%d err=%v", called, err)
	}
	if lease := owner.seen["fence"]; lease.Route.HostTenantID != "tenant" || lease.Route.ConversationID != "room" {
		t.Fatalf("fence route: %+v", lease)
	}
	owner.denied = true
	if _, err := extension.CaptureThreadSnapshot(context.Background(), request); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("denied source: %v", err)
	}
	if err := extension.WithThreadSnapshotFence(context.Background(), snapshot, func() error { called++; return nil }); !errors.Is(err, chat.ErrPermissionDenied) || called != 1 {
		t.Fatalf("denied fence invoked continuation: called=%d err=%v", called, err)
	}
}

func TestTodo_AGENTP_010_RoutedSnapshotRejectsUnknownAndMovingRoutes(t *testing.T) {
	owner := &routedSnapshotOwner{leaseRecorder: newLeaseRecorder()}
	routed, directory := newAdapter(t, owner)
	placeNamedRoute(t, directory, "room", "tenant", "chat-shard")
	extension, ok := any(routed).(chat.ThreadSnapshotStore)
	if !ok {
		t.Fatal("routing decorator discarded atomic snapshots")
	}
	for _, request := range []chat.ThreadSnapshotRequest{{TenantID: "foreign", ConversationID: "room"}, {TenantID: "tenant", ConversationID: "missing"}} {
		if _, err := extension.CaptureThreadSnapshot(context.Background(), request); err == nil {
			t.Fatal("unknown route reached snapshot owner")
		}
	}
	if owner.calls != 0 {
		t.Fatal("foreign or unknown routes reached the snapshot owner")
	}
	if _, err := directory.BeginMove(context.Background(), "room", "tenant", 1, "new-shard"); err != nil {
		t.Fatal(err)
	}
	if _, err := extension.CaptureThreadSnapshot(context.Background(), chat.ThreadSnapshotRequest{TenantID: "tenant", ConversationID: "room"}); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("moving snapshot route: %v", err)
	}
	if err := extension.WithThreadSnapshotFence(context.Background(), chat.ThreadSnapshot{TenantID: "tenant", ConversationID: "room"}, func() error { t.Fatal("moving route invoked continuation"); return nil }); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("moving fence: %v", err)
	}
	if owner.calls != 0 {
		t.Fatal("moving route reached snapshot owner")
	}
	plain, _ := newAdapter(t, &fakeService{})
	missing := any(plain).(chat.ThreadSnapshotStore)
	if _, err := missing.CaptureThreadSnapshot(context.Background(), chat.ThreadSnapshotRequest{}); !errors.Is(err, chat.ErrThreadSnapshotUnavailable) {
		t.Fatalf("missing source: %v", err)
	}
}
