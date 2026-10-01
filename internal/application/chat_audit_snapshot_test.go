package application

import (
	"context"
	"errors"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatroutingadapter"
)

type auditSnapshotOwner struct {
	chat.ConversationService
	request          chat.ThreadSnapshotRequest
	snapshot         chat.ThreadSnapshot
	denied           bool
	captures, fences int
}

func (s *auditSnapshotOwner) CaptureThreadSnapshot(_ context.Context, r chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error) {
	s.captures++
	s.request = r
	if s.denied {
		return chat.ThreadSnapshot{}, chat.ErrPermissionDenied
	}
	return s.snapshot, nil
}

func (s *auditSnapshotOwner) WithThreadSnapshotFence(_ context.Context, snapshot chat.ThreadSnapshot, fn func() error) error {
	s.fences++
	if s.denied || snapshot.Digest != s.snapshot.Digest {
		return chat.ErrPermissionDenied
	}
	return fn()
}

func TestTodo_AGENTP_010_ServedDecoratorsPreserveAtomicSnapshots(t *testing.T) {
	owner := &auditSnapshotOwner{snapshot: chat.ThreadSnapshot{TenantID: "tenant", ConversationID: "room", ThreadID: "thread", InvokingPostID: "post", PrincipalID: "invoker", SnapshotID: "snapshot", Digest: "digest"}}
	audited := &auditedChatService{ConversationService: owner}
	directory := chatrouting.NewMemoryDirectory()
	route, err := directory.Reserve(context.Background(), chatrouting.ReserveRequest{ConversationID: "room", HostTenantID: "tenant", ShardID: "chat-default", IdempotencyKey: "room"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directory.Activate(context.Background(), "room", "tenant", route.Epoch); err != nil {
		t.Fatal(err)
	}
	routed, err := chatroutingadapter.New(audited, chatroutingadapter.Options{Directory: directory, DefaultShard: "chat-default"})
	if err != nil {
		t.Fatal(err)
	}
	snapshots, ok := any(routed).(PersonaThreadSnapshotSource)
	if !ok {
		t.Fatal("served audit/routing composition lost the persona snapshot source")
	}
	request := chat.ThreadSnapshotRequest{Principal: chat.Principal{TenantID: "tenant", SubjectID: "invoker"}, TenantID: "tenant", ConversationID: "room", ThreadID: "thread", InvokingPostID: "post", Limit: 50}
	snapshot, err := snapshots.CaptureThreadSnapshot(context.Background(), request)
	if err != nil || snapshot.Digest != owner.snapshot.Digest || owner.request.Limit != 50 || owner.request.Principal.SubjectID != "invoker" {
		t.Fatalf("served snapshot: %+v %v", snapshot, err)
	}
	fence, ok := any(routed).(chat.ThreadSnapshotStore)
	if !ok {
		t.Fatal("served decorators lost the current snapshot fence")
	}
	called := 0
	if err := fence.WithThreadSnapshotFence(context.Background(), snapshot, func() error { called++; return nil }); err != nil || called != 1 || owner.fences != 1 {
		t.Fatalf("fenced callback: %d %v", called, err)
	}
	owner.denied = true
	if _, err := snapshots.CaptureThreadSnapshot(context.Background(), request); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("owner denial discarded: %v", err)
	}
	if err := fence.WithThreadSnapshotFence(context.Background(), snapshot, func() error { called++; return nil }); !errors.Is(err, chat.ErrPermissionDenied) || called != 1 {
		t.Fatalf("denied callback: %d %v", called, err)
	}
	missing, ok := any(&auditedChatService{ConversationService: auditConversation{}}).(chat.ThreadSnapshotStore)
	if !ok {
		t.Fatal("audit decorator does not declare snapshot extension")
	}
	if _, err := missing.CaptureThreadSnapshot(context.Background(), request); !errors.Is(err, chat.ErrThreadSnapshotUnavailable) {
		t.Fatalf("unsupported capture: %v", err)
	}
	if err := missing.WithThreadSnapshotFence(context.Background(), snapshot, func() error { t.Fatal("unsupported fence invoked callback"); return nil }); !errors.Is(err, chat.ErrThreadSnapshotUnavailable) {
		t.Fatalf("unsupported fence: %v", err)
	}
}
