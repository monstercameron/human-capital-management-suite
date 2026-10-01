package chatroutingadapter

import (
	"context"
	"testing"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
)

type ephemeralLeaseRecorder struct {
	*leaseRecorder
	calls int
	lease chatrouting.WriteLease
}

func (f *ephemeralLeaseRecorder) SendEphemeralPost(ctx context.Context, r chat.SendEphemeralPostRequest) (chat.EphemeralPost, error) {
	f.calls++
	lease, ok := chatrouting.WriteLeaseFromContext(ctx)
	if !ok {
		return chat.EphemeralPost{}, chat.ErrUnavailable
	}
	f.lease = lease
	return chat.EphemeralPost{ConversationID: r.ConversationID, DurableCopyConversationID: r.DurableCopyConversationID}, nil
}

func placeNamedRoute(t *testing.T, d *chatrouting.MemoryDirectory, id, tenant, shard string) {
	t.Helper()
	route, err := d.Reserve(context.Background(), chatrouting.ReserveRequest{ConversationID: id, HostTenantID: tenant, ShardID: shard, IdempotencyKey: id + "-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Activate(context.Background(), id, tenant, route.Epoch); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_AGENTP_011_EphemeralLeaseUsesCanonicalDM(t *testing.T) {
	f := &ephemeralLeaseRecorder{leaseRecorder: newLeaseRecorder()}
	s, d := newAdapter(t, f)
	placeNamedRoute(t, d, "source-room", "tenant-a", "source-shard")
	placeNamedRoute(t, d, "canonical-dm", "tenant-a", "dm-shard")

	_, err := s.SendEphemeralPost(context.Background(), chat.SendEphemeralPostRequest{
		Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "invoker"},
		TenantID:  "tenant-a", ConversationID: "source-room", ThreadID: "source-room",
		DurableCopyConversationID: "canonical-dm", Body: "private", IdempotencyKey: "reply-1",
	})
	if err != nil {
		t.Fatalf("send ephemeral: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("ephemeral calls = %d, want 1", f.calls)
	}
	if f.lease.Route.ConversationID != "canonical-dm" || f.lease.Route.ShardID != "dm-shard" {
		t.Fatalf("lease route = %+v, want canonical DM", f.lease.Route)
	}
}

func TestTodo_AGENTP_011_EphemeralLeaseRefusesMissingCanonicalDM(t *testing.T) {
	f := &ephemeralLeaseRecorder{leaseRecorder: newLeaseRecorder()}
	s, d := newAdapter(t, f)
	placeNamedRoute(t, d, "source-room", "tenant-a", "source-shard")

	_, err := s.SendEphemeralPost(context.Background(), chat.SendEphemeralPostRequest{
		Principal: chat.Principal{TenantID: "tenant-a", SubjectID: "invoker"},
		TenantID:  "tenant-a", ConversationID: "source-room", ThreadID: "source-room",
		Body: "private", IdempotencyKey: "reply-2",
	})
	if err != chat.ErrInvalidArgument {
		t.Fatalf("missing canonical DM error = %v, want %v", err, chat.ErrInvalidArgument)
	}
	if f.calls != 0 {
		t.Fatalf("missing canonical DM reached inner service %d times", f.calls)
	}
}
