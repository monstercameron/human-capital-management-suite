package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type chatsaveFederatedAuthority struct{ forwardingAuthority }

func (a chatsaveFederatedAuthority) Authorize(ctx context.Context, p chat.Principal, c chat.Conversation, action chatpolicy.Action, now time.Time) (chatpolicy.Input, error) {
	in, err := a.forwardingAuthority.Authorize(ctx, p, c, action, now)
	if err == nil && c.TenantID != p.TenantID {
		in.Channel.Classification = "INTERNAL"
		in.Channel.Residency = "GLOBAL"
		in.HasGrant = true
		in.Grant = chatpolicy.Grant{ID: "fixture-consented", ConversationID: c.ID, HostTenant: c.TenantID, ConsumerTenant: p.TenantID, Version: 1, Scope: "conversation", Classification: "INTERNAL", Residency: "GLOBAL", Proposed: true, AcceptedByHost: true, AcceptedByConsumer: true, ExpiresAt: now.Add(time.Hour)}
	}
	return in, err
}

func TestTodo_CHATSAVE_001_Integration_CurrentAccess(t *testing.T) {
	s, p, item, _ := chatsaveDB(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "foreign-room", TenantID: "host-b", Kind: "PRIVATE_CHANNEL", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: p.SubjectID, HomeTenantID: p.TenantID, State: "active", Role: "member"}})
	post, err := s.sendPostRaw(ctx, SendRequest{TenantID: "host-b", HomeTenantID: p.TenantID, ConversationID: "foreign-room", AuthorID: p.SubjectID, ClientKey: "foreign-save", Body: "readable foreign message"})
	if err != nil {
		t.Fatal(err)
	}
	a := NewAdapter(s)
	service := chat.NewService(a, time.Now)
	service.SetAuthority(chatsaveFederatedAuthority{forwardingAuthority{store: a}})
	localReq := chat.SavedRequest{Principal: p, TenantID: item.TenantID, ConversationID: item.ConversationID, PostID: item.PostID}
	foreignReq := chat.SavedRequest{Principal: p, TenantID: "host-b", ConversationID: "foreign-room", PostID: post.ID}
	for _, req := range []chat.SavedRequest{localReq, foreignReq} {
		if _, err = service.SaveForLater(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = service.SetSavedNote(ctx, foreignReq, "private foreign note"); err != nil {
		t.Fatal(err)
	}
	page, err := service.ListSaved(ctx, chat.SavedListRequest{Principal: p, TenantID: p.TenantID, Tab: chat.SavedAll})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("home-tenant list=%+v %v", page, err)
	}
	find := func() chat.SavedItem {
		t.Helper()
		page, err := service.ListSaved(ctx, chat.SavedListRequest{Principal: p, TenantID: p.TenantID, Tab: chat.SavedAll})
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range page.Items {
			if x.TenantID == "host-b" {
				return x
			}
		}
		t.Fatal("foreign save missing")
		return chat.SavedItem{}
	}
	x := find()
	if x.Post == nil || x.Post.Body != "readable foreign message" || x.Note != "private foreign note" {
		t.Fatalf("foreign projection=%+v", x)
	}
	matches, err := service.SearchSaved(ctx, p, "foreign message")
	if err != nil || len(matches) != 1 {
		t.Fatal("current text search", err)
	}
	if err = s.RunTenantTx(ctx, "host-b", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET body='changed current message' WHERE id=$1`, post.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	matches, err = service.SearchSaved(ctx, p, "foreign message")
	if err != nil || len(matches) != 0 {
		t.Fatal("search retained copied original text", err)
	}
	if err = s.RunTenantTx(ctx, "host-b", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=true WHERE id=$1`, post.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	x = find()
	if x.Availability != "deleted" || x.Post != nil || x.Channel != "" {
		t.Fatalf("deleted projection=%+v", x)
	}
	if _, err = service.SaveForLater(ctx, foreignReq); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("saved deleted reference", err)
	}
	if err = s.RunTenantTx(ctx, "host-b", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=false WHERE id=$1`, post.ID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE chat_membership SET state='left',left_at=now() WHERE conversation_id='foreign-room' AND home_tenant_id=$1 AND member_id=$2`, p.TenantID, p.SubjectID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	x = find()
	if x.Availability != "no_access" || x.Post != nil {
		t.Fatal("left-channel body leaked")
	}
	matches, err = service.SearchSaved(ctx, p, "changed current")
	if err != nil || len(matches) != 0 {
		t.Fatal("revoked text search", err)
	}
	matches, err = service.SearchSaved(ctx, p, "private foreign note")
	if err != nil || len(matches) != 1 || matches[0].Post != nil {
		t.Fatal("owner note not searchable after access loss", err)
	}
	if err = s.EraseSavedPerson(ctx, p, p.TenantID); err != nil {
		t.Fatal(err)
	}
	page, err = service.ListSaved(ctx, chat.SavedListRequest{Principal: p, TenantID: p.TenantID, Tab: chat.SavedAll})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("foreign refs survived erasure", err)
	}
}
