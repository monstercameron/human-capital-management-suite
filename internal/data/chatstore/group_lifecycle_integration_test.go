package chatstore

import (
	"context"
	"testing"
	"time"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

func TestTodo_CHAT_016_Integration(t *testing.T) {
	store := adapterDB(t)
	ctx := context.Background()
	owner := chat.Principal{TenantID: "chat-016-tenant", SubjectID: "alice"}
	dm := chat.Conversation{ID: "chat-016-dm", TenantID: owner.TenantID, Kind: chat.Direct, Name: "", OwnerID: owner.SubjectID, Revision: 1}
	if _, err := store.CreateConversation(ctx, dm, []chat.Membership{
		{ConversationID: dm.ID, TenantID: dm.TenantID, HomeTenantID: owner.TenantID, SubjectID: "alice", Role: chat.Manager, HistoryVisibility: chat.FullHistory},
		{ConversationID: dm.ID, TenantID: dm.TenantID, HomeTenantID: owner.TenantID, SubjectID: "bob", Role: chat.Member, HistoryVisibility: chat.FullHistory},
	}, ""); err != nil {
		t.Fatal(err)
	}
	dmPost, err := store.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: owner.TenantID, ConversationID: dm.ID, IdempotencyKey: "chat-016-dm-post"}, chat.Post{AuthorID: owner.SubjectID, Body: "DM history stays private"})
	if err != nil {
		t.Fatal(err)
	}

	service := chat.NewService(store, func() time.Time { return time.Unix(200, 0).UTC() })
	service.SetAuthority(forwardingAuthority{store: store})
	group, err := service.CreatePrivateGroup(ctx, chat.CreateConversationRequest{
		Principal: owner, TenantID: owner.TenantID, ConversationID: "chat-016-group", Kind: chat.Group, Name: "Payroll launch",
		Members: []chat.MemberRef{{TenantID: owner.TenantID, SubjectID: "bob"}, {TenantID: owner.TenantID, SubjectID: "carol"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if group.ID == dm.ID || group.Kind != chat.Group {
		t.Fatalf("group=%+v; expected a separate GROUP conversation", group)
	}
	if _, err := service.RenamePrivateGroup(ctx, chat.UpdateConversationRequest{Principal: owner, Conversation: chat.Conversation{ID: group.ID, TenantID: group.TenantID, Name: "Payroll launch crew"}, ExpectedRevision: group.Revision}); err != nil {
		t.Fatal(err)
	}
	invited, err := service.InviteGroupMember(ctx, chat.AddMembershipRequest{Principal: owner, Membership: chat.Membership{ConversationID: group.ID, TenantID: group.TenantID, HomeTenantID: group.TenantID, SubjectID: "dana", Role: chat.Manager, HistoryVisibility: chat.FullHistory}})
	if err != nil || invited.Role != chat.Member || invited.HistoryVisibility != chat.FromJoin {
		t.Fatalf("invited=%+v err=%v", invited, err)
	}
	groupPost, err := service.SendPost(ctx, chat.SendPostRequest{Principal: owner, TenantID: group.TenantID, ConversationID: group.ID, Body: "new group timeline", IdempotencyKey: "chat-016-group-post"})
	if err != nil {
		t.Fatal(err)
	}
	groupHistory, err := store.ListPosts(ctx, owner, group.TenantID, group.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(groupHistory.Posts) != 1 || groupHistory.Posts[0].ID != groupPost.ID || groupHistory.Posts[0].ID == dmPost.ID {
		t.Fatalf("group history=%+v err=%v; expected only its own post", groupHistory, err)
	}
	dmHistory, err := store.ListPosts(ctx, owner, dm.TenantID, dm.ID, 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(dmHistory.Posts) != 1 || dmHistory.Posts[0].ID != dmPost.ID {
		t.Fatalf("DM history=%+v err=%v; expected its original post", dmHistory, err)
	}
	if _, err := service.RevokeGroupMember(ctx, chat.RemoveMembershipRequest{Principal: owner, TenantID: group.TenantID, HomeTenantID: invited.HomeTenantID, ConversationID: group.ID, SubjectID: invited.SubjectID, ExpectedRevision: invited.Revision}); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetMembership(ctx, group.TenantID, group.ID, group.TenantID, invited.SubjectID)
	if err != nil || current.LeftAt == nil {
		t.Fatalf("revoked membership=%+v err=%v; want inactive", current, err)
	}
}
