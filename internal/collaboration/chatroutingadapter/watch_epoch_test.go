package chatroutingadapter

import (
	"context"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrouting"
)

type watchEpochChat struct{ chatcore.ConversationService }

func TestTodo_CHAT_019_RouteEpochTracksTheDirectoryFence(t *testing.T) {
	directory := chatrouting.NewMemoryDirectory()
	service, err := New(watchEpochChat{}, Options{Directory: directory, DefaultShard: "shard-a"})
	if err != nil {
		t.Fatal(err)
	}

	if epoch, err := service.RouteEpoch(context.Background(), "tenant", "legacy"); err != nil || epoch != 0 {
		t.Fatalf("legacy route epoch = %d, err=%v; want zero sentinel", epoch, err)
	}
	route, err := directory.Reserve(context.Background(), chatrouting.ReserveRequest{ConversationID: "c", HostTenantID: "tenant", ShardID: "shard-a", IdempotencyKey: "create-c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = directory.Activate(context.Background(), "c", "tenant", route.Epoch); err != nil {
		t.Fatal(err)
	}
	if epoch, err := service.RouteEpoch(context.Background(), "tenant", "c"); err != nil || epoch != 1 {
		t.Fatalf("active route epoch = %d, err=%v; want 1", epoch, err)
	}
	if _, err = directory.BeginMove(context.Background(), "c", "tenant", 1, "shard-b"); err != nil {
		t.Fatal(err)
	}
	if epoch, err := service.RouteEpoch(context.Background(), "tenant", "c"); err != nil || epoch != 2 {
		t.Fatalf("moving route epoch = %d, err=%v; want 2", epoch, err)
	}
}
