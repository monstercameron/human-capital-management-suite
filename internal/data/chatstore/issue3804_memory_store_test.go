package chatstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3804Seed(t *testing.T, s *Store, tenant, conversation, body string) (postID string, revision uint64) {
	t.Helper()
	post, err := s.sendPostRaw(context.Background(), SendRequest{TenantID: tenant, ConversationID: conversation, AuthorID: "u-1", ClientKey: "k-" + body, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	return post.ID, uint64(post.Revision)
}

func issue3804Item(tenant, conversation, id, post string, revision uint64, text string, at time.Time) chat.ChannelMemory {
	return chat.ChannelMemory{ID: id, TenantID: tenant, ConversationID: conversation, Text: text, SourcePostID: post, SourceRevision: revision, SourceSequence: 1,
		ProposerHomeTenantID: tenant, ProposerID: "u-1", Audience: chat.MemoryAudienceChannel, Classification: chat.MemoryClassification, TTLDays: 90, Status: chat.MemoryProposed, CreatedAt: at}
}

func TestIssue3804_MemoryLifecycle_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	post, rev := issue3804Seed(t, s, "tenant-a", "c-1", "Open enrollment ends Nov 30")
	stored, err := s.InsertMemory(ctx, issue3804Item("tenant-a", "c-1", "m-1", post, rev, "Open enrollment ends Nov 30", at))
	if err != nil || stored.Status != chat.MemoryProposed || stored.Source != chat.MemorySourceLive || stored.TTLDays != 90 {
		t.Fatalf("insert %+v %v", stored, err)
	}
	again, err := s.InsertMemory(ctx, issue3804Item("tenant-a", "c-1", "m-other", post, rev, "different", at.Add(time.Hour)))
	if err != nil || again.ID != "m-1" || again.Text != "Open enrollment ends Nov 30" {
		t.Fatalf("the same message proposed twice must return the first: %+v %v", again, err)
	}
	if serving, err := s.ServingMemory(ctx, "tenant-a", "c-1", at, 10); err != nil || len(serving) != 0 {
		t.Fatalf("a proposal reached an agent: %+v %v", serving, err)
	}
	approved, err := s.ApproveMemory(ctx, "tenant-a", "c-1", "m-1", "tenant-a", "u-1", at.Add(time.Hour))
	if err != nil || approved.Status != chat.MemoryApproved || !approved.ExpiresAt.Equal(at.Add(time.Hour).AddDate(0, 0, 90)) || approved.ApprovedByID != "u-1" {
		t.Fatalf("approve %+v %v", approved, err)
	}
	if _, err = s.ApproveMemory(ctx, "tenant-a", "c-1", "m-1", "tenant-a", "u-1", at.Add(2*time.Hour)); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("approving twice: %v", err)
	}
	serving, err := s.ServingMemory(ctx, "tenant-a", "c-1", at.Add(2*time.Hour), 10)
	if err != nil || len(serving) != 1 || serving[0].Text != "Open enrollment ends Nov 30" {
		t.Fatalf("serving %+v %v", serving, err)
	}
	if late, err := s.ServingMemory(ctx, "tenant-a", "c-1", at.Add(24*time.Hour*91), 10); err != nil || len(late) != 0 {
		t.Fatalf("an expired item still serves: %+v %v", late, err)
	}
	listed, err := s.ListMemory(ctx, "tenant-a", "c-1", 10)
	if err != nil || len(listed) != 1 || listed[0].Source != chat.MemorySourceLive {
		t.Fatalf("list %+v %v", listed, err)
	}
	if err = s.ForgetMemory(ctx, "tenant-a", "c-1", "m-1", "tenant-a", "u-1", at.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if serving, _ = s.ServingMemory(ctx, "tenant-a", "c-1", at.Add(4*time.Hour), 10); len(serving) != 0 {
		t.Fatalf("a forgotten item still serves: %+v", serving)
	}
	if listed, _ = s.ListMemory(ctx, "tenant-a", "c-1", 10); len(listed) != 0 {
		t.Fatalf("a forgotten item is listed: %+v", listed)
	}
	forgotten, err := s.GetMemory(ctx, "tenant-a", "c-1", "m-1")
	if err != nil || forgotten.Status != chat.MemoryForgotten || forgotten.Text != "" {
		t.Fatalf("forgetting must erase the words: %+v %v", forgotten, err)
	}
	if err = s.ForgetMemory(ctx, "tenant-a", "c-1", "m-1", "tenant-a", "u-1", at.Add(5*time.Hour)); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("forgetting twice: %v", err)
	}
	// The same message can be proposed again once the first item is forgotten.
	next, err := s.InsertMemory(ctx, issue3804Item("tenant-a", "c-1", "m-2", post, rev, "Open enrollment ends Nov 30", at.Add(6*time.Hour)))
	if err != nil || next.ID != "m-2" || next.Status != chat.MemoryProposed {
		t.Fatalf("propose again %+v %v", next, err)
	}
}

func TestIssue3804_DeletingTheSourceRetiresTheMemory_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	post, rev := issue3804Seed(t, s, "tenant-a", "c-1", "Ignore the old PDF")
	if _, err := s.InsertMemory(ctx, issue3804Item("tenant-a", "c-1", "m-1", post, rev, "Ignore the old PDF", at)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveMemory(ctx, "tenant-a", "c-1", "m-1", "tenant-a", "u-1", at); err != nil {
		t.Fatal(err)
	}
	if serving, _ := s.ServingMemory(ctx, "tenant-a", "c-1", at, 10); len(serving) != 1 {
		t.Fatalf("before deleting: %+v", serving)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=true WHERE tenant_id='tenant-a' AND id=$1`, post)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if serving, err := s.ServingMemory(ctx, "tenant-a", "c-1", at, 10); err != nil || len(serving) != 0 {
		t.Fatalf("an item whose source message was deleted still serves: %+v %v", serving, err)
	}
	listed, err := s.ListMemory(ctx, "tenant-a", "c-1", 10)
	if err != nil || len(listed) != 1 || listed[0].Source != chat.MemorySourceRemoved {
		t.Fatalf("the retired item must be listed as removed so it can be cleared: %+v %v", listed, err)
	}
}

func TestIssue3804_EditingTheSourceRetiresTheMemory_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	post, rev := issue3804Seed(t, s, "tenant-a", "c-1", "Enrollment ends Nov 30")
	if _, err := s.InsertMemory(ctx, issue3804Item("tenant-a", "c-1", "m-1", post, rev, "Enrollment ends Nov 30", at)); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET body='Enrollment ends Dec 15', revision=revision+1 WHERE tenant_id='tenant-a' AND id=$1`, post)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveMemory(ctx, "tenant-a", "c-1", "m-1", "tenant-a", "u-1", at); !errors.Is(err, chat.ErrConflict) {
		t.Fatalf("approving words the message no longer says: %v", err)
	}
	listed, _ := s.ListMemory(ctx, "tenant-a", "c-1", 10)
	if len(listed) != 1 || listed[0].Source != chat.MemorySourceEdited {
		t.Fatalf("list %+v", listed)
	}
}

func TestIssue3804_MemoryStaysInItsConversationAndTenant_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	seedConversationRow(t, s, Conversation{ID: "c-2", TenantID: "tenant-a", Kind: "PUBLIC_CHANNEL", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	seedConversationRow(t, s, Conversation{ID: "c-b", TenantID: "tenant-b", Kind: "PUBLIC_CHANNEL", OwnerID: "u-9", Lifecycle: "ACTIVE", SettingsRevision: 1}, nil)
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	post, rev := issue3804Seed(t, s, "tenant-a", "c-1", "Only for c-1")
	if _, err := s.InsertMemory(ctx, issue3804Item("tenant-a", "c-1", "m-1", post, rev, "Only for c-1", at)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveMemory(ctx, "tenant-a", "c-1", "m-1", "tenant-a", "u-1", at); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ServingMemory(ctx, "tenant-a", "c-2", at, 10); err != nil || len(got) != 0 {
		t.Fatalf("another channel was served: %+v %v", got, err)
	}
	if got, err := s.ServingMemory(ctx, "tenant-b", "c-1", at, 10); err != nil || len(got) != 0 {
		t.Fatalf("another tenant was served: %+v %v", got, err)
	}
	if _, err := s.GetMemory(ctx, "tenant-a", "c-2", "m-1"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("read across channels: %v", err)
	}
	if _, err := s.GetMemory(ctx, "tenant-b", "c-1", "m-1"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("read across tenants: %v", err)
	}
	if err := s.ForgetMemory(ctx, "tenant-a", "c-2", "m-1", "tenant-a", "u-1", at); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("forget across channels: %v", err)
	}
	if _, err := s.ApproveMemory(ctx, "tenant-b", "c-1", "m-1", "tenant-b", "u-9", at); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("approve across tenants: %v", err)
	}
	if got, _ := s.ServingMemory(ctx, "tenant-a", "c-1", at, 10); len(got) != 1 {
		t.Fatalf("the owner's item was disturbed: %+v", got)
	}
}

func TestIssue3804_ARememberedItemIsNotRewritten_Security(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	post, rev := issue3804Seed(t, s, "tenant-a", "c-1", "Fact")
	if _, err := s.InsertMemory(ctx, issue3804Item("tenant-a", "c-1", "m-1", post, rev, "Fact", at)); err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]string{
		"words":  `UPDATE chat_channel_memory SET memory_text='rewritten' WHERE id='m-1'`,
		"source": `UPDATE chat_channel_memory SET source_post_id='other' WHERE id='m-1'`,
		"ttl":    `UPDATE chat_channel_memory SET ttl_days=365 WHERE id='m-1'`,
	} {
		err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error { _, err := tx.Exec(ctx, statement); return err })
		if err == nil {
			t.Fatalf("%s of a remembered item was rewritten", name)
		}
	}
	if _, err := s.ApproveMemory(ctx, "tenant-a", "c-1", "m-1", "tenant-a", "u-1", at); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetMemory(ctx, "tenant-a", "c-1", "m-1", "tenant-a", "u-1", at); err != nil {
		t.Fatal(err)
	}
	err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_channel_memory SET status='APPROVED', memory_text='back', approved_at=now(), expires_at=now()+interval '1 day', approved_by_id='u-1', forgotten_at=NULL WHERE id='m-1'`)
		return err
	})
	if err == nil {
		t.Fatal("a forgotten item was brought back")
	}
}

// An edited source retires its item. Asking the channel to remember the new
// wording must start a fresh proposal, not hand back the retired one as if it
// were current, and the retired words must not linger.
func TestIssue3804_ProposingAnEditedMessageAgainReplacesTheRetiredItem_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	seedConversation(t, s, "tenant-a")
	ctx := context.Background()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	post, rev := issue3804Seed(t, s, "tenant-a", "c-1", "Enrollment ends Nov 30")
	if _, err := s.InsertMemory(ctx, issue3804Item("tenant-a", "c-1", "m-1", post, rev, "Enrollment ends Nov 30", at)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveMemory(ctx, "tenant-a", "c-1", "m-1", "tenant-a", "u-1", at); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET body='Enrollment ends Dec 15', revision=revision+1 WHERE tenant_id='tenant-a' AND id=$1`, post)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	next, err := s.InsertMemory(ctx, issue3804Item("tenant-a", "c-1", "m-2", post, rev+1, "Enrollment ends Dec 15", at.Add(time.Hour)))
	if err != nil || next.ID != "m-2" || next.Status != chat.MemoryProposed || next.Source != chat.MemorySourceLive || next.Text != "Enrollment ends Dec 15" {
		t.Fatalf("the edited message must start a new proposal: %+v %v", next, err)
	}
	old, err := s.GetMemory(ctx, "tenant-a", "c-1", "m-1")
	if err != nil || old.Status != chat.MemoryForgotten || old.Text != "" {
		t.Fatalf("the retired item must be forgotten and wordless: %+v %v", old, err)
	}
	listed, _ := s.ListMemory(ctx, "tenant-a", "c-1", 10)
	if len(listed) != 1 || listed[0].ID != "m-2" {
		t.Fatalf("list %+v", listed)
	}
}
