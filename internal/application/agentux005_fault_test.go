package application

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// failingDMService is the chat service of the preparation with its conversation
// creation refused, the way a store outage in the middle of the preparation
// refuses it.
type failingDMService struct {
	*chatcore.Service
	err error
}

func (f failingDMService) CreateConversation(context.Context, chatcore.CreateConversationRequest) (chatcore.Conversation, error) {
	return chatcore.Conversation{}, f.err
}

// A preparation that cannot finish says so and leaves nothing half made: with
// no chat store or service it refuses before touching anything, and when the
// store refuses the conversation the step returns the failure, no conversation
// exists for the administrator, and no policy row was written for one.
func TestTodo_AGENTUX_005_Fault(t *testing.T) {
	raw, service, ctx, admin := agentUXPrepareChatFixture(t)
	if id, _, err := ensureLocalAgentDemoDirectConversation(ctx, nil, service, "tenant-a", admin.SubjectID, "policy-helper"); !errors.Is(err, chatcore.ErrUnavailable) || id != "" {
		t.Fatalf("no chat store: id=%q err=%v", id, err)
	}
	if id, _, err := ensureLocalAgentDemoDirectConversation(ctx, raw, nil, "tenant-a", admin.SubjectID, "policy-helper"); !errors.Is(err, chatcore.ErrUnavailable) || id != "" {
		t.Fatalf("no chat service: id=%q err=%v", id, err)
	}
	outage := errors.New("store offline")
	id, _, err := ensureLocalAgentDemoDirectConversation(ctx, raw, failingDMService{Service: service, err: outage}, "tenant-a", admin.SubjectID, "policy-helper")
	if err == nil || id != "" {
		t.Fatalf("a refused conversation was reported as prepared: id=%q err=%v", id, err)
	}
	listed, err := service.ListConversations(ctx, chatcore.ListConversationsRequest{Principal: admin, TenantID: "tenant-a", Page: chatcore.Page{PageSize: 50}})
	if err != nil || len(listed.Conversations) != 0 {
		t.Fatalf("the failed preparation left conversations behind: %+v %v", listed.Conversations, err)
	}
	want, _ := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: admin.SubjectID}, {TenantID: "tenant-a", SubjectID: "policy-helper"}})
	if _, err := raw.CapturePersonaChannelPolicy(ctx, "tenant-a", want, ""); err == nil {
		t.Fatal("a policy row exists for a conversation that was never made")
	}
}

// An interrupted earlier preparation left the administrator's conversation with
// Policy Helper made but without the policy rows an agent conversation needs
// (so a question in it was refused). Running the preparation again repairs it
// in place: the same conversation, now with both policies, and a third run
// changes nothing.
func TestTodo_AGENTUX_005_Recovery(t *testing.T) {
	raw, service, ctx, admin := agentUXPrepareChatFixture(t)
	agent := chatcore.MemberRef{TenantID: "tenant-a", SubjectID: "policy-helper"}
	id, err := chatcore.DirectPairConversationID("tenant-a", []chatcore.MemberRef{{TenantID: "tenant-a", SubjectID: admin.SubjectID}, agent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateConversation(ctx, chatcore.CreateConversationRequest{Principal: admin, TenantID: "tenant-a", ConversationID: id, Kind: chatcore.Direct, Name: "Policy Helper", Members: []chatcore.MemberRef{agent}}); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.CapturePersonaChannelPolicy(ctx, "tenant-a", id, ""); err == nil {
		t.Fatal("the interrupted state already has a channel policy; the fixture is not the failure under test")
	}

	got, _, err := ensureLocalAgentDemoDirectConversation(ctx, raw, service, "tenant-a", admin.SubjectID, "policy-helper")
	if err != nil || got != id {
		t.Fatalf("recovery: id=%q want %q err=%v", got, id, err)
	}
	snapshot, err := raw.CapturePersonaChannelPolicy(ctx, "tenant-a", id, "")
	if err != nil || snapshot.Policy.PlacementClass != "ONE_TO_ONE_DM" || !snapshot.Policy.AlwaysPrivate {
		t.Fatalf("the channel policy was not repaired: %+v %v", snapshot, err)
	}
	// The audience policy exists now: writing a first one is a conflict.
	if _, err := raw.PutAudiencePolicy(ctx, "tenant-a", id, 0, personaDMaudiencePolicy()); !errors.Is(err, chatstore.ErrAudiencePolicyConflict) {
		t.Fatalf("the audience policy was not repaired: %v", err)
	}
	listed, err := service.ListConversations(ctx, chatcore.ListConversationsRequest{Principal: admin, TenantID: "tenant-a", Page: chatcore.Page{PageSize: 50}})
	if err != nil || len(listed.Conversations) != 1 || listed.Conversations[0].ID != id {
		t.Fatalf("recovery made a second conversation: %+v %v", listed.Conversations, err)
	}
	again, repaired, err := ensureLocalAgentDemoDirectConversation(ctx, raw, service, "tenant-a", admin.SubjectID, "policy-helper")
	if err != nil || again != id || repaired {
		t.Fatalf("a third run: id=%q repaired=%t err=%v", again, repaired, err)
	}
}
