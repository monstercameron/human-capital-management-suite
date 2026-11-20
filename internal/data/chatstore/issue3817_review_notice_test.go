package chatstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestIssue3817_ReviewNotice_Integration: a waiting join request reaches the
// channel's managers as a private notice, once per level, never the applicant,
// never another workspace, and an administrator outside a private channel is
// told nothing of it.
func TestIssue3817_ReviewNotice_Integration(t *testing.T) {
	adapter := adapterDB(t)
	moderated := NewModeratedAdapter(adapter)
	repo := &GateRepository{Store: adapter.Store}
	ctx := t.Context()
	chatmodChannel(t, moderated, "private", "payroll", chat.PrivateChannel, false, "lead", "insider")
	chatmodChannel(t, moderated, "public", "general", chat.PublicChannel, false, "pub-lead", "walt")
	if err := adapter.Store.RunTenantTx(ctx, "tenant-a", func(tx dbport.Tx) error {
		for _, who := range []string{"boss", "insider"} {
			if _, e := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES('tenant-a',$1,'WORKSPACE_ADMIN')`, who); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	notices := func(subject string) []chat.ModerationNotice {
		t.Helper()
		out, err := moderated.ModerationNotices(ctx, chat.Principal{TenantID: "tenant-a", SubjectID: subject}, "tenant-a")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	send := func(conversation, person, level string) {
		t.Helper()
		if err := repo.NotifyReview(ctx, chatgate.ReviewNotice{Scope: chatgate.Scope{Tenant: "tenant-a", Conversation: conversation}, SubmissionID: "sub-1", Person: person, Level: level}); err != nil {
			t.Fatal(err)
		}
	}

	send("private", "ana", chatgate.ReviewNoticeNew)
	send("private", "ana", chatgate.ReviewNoticeNew) // a replay is the same notice
	got := notices("lead")
	if len(got) != 1 || got[0].Outcome != chat.ModerationOutcomeGateReview || got[0].ConversationID != "private" || got[0].Reason != "" || got[0].PostID != "" {
		t.Fatalf("manager notices: %+v", got)
	}
	for _, other := range []string{"ana", "boss", "insider", "walt"} {
		if rows := notices(other); len(rows) != 0 {
			t.Errorf("%s was told of a new request: %+v", other, rows)
		}
	}

	send("private", "ana", chatgate.ReviewNoticeOverdue)
	send("private", "ana", chatgate.ReviewNoticeEscalated)
	if got = notices("lead"); len(got) != 3 {
		t.Fatalf("manager should hold one notice per level, has %+v", got)
	}
	if rows := notices("boss"); len(rows) != 0 {
		t.Fatalf("an administrator outside a private channel learned it exists: %+v", rows)
	}
	if rows := notices("insider"); len(rows) != 1 || rows[0].Outcome != chat.ModerationOutcomeGateEscalated {
		t.Fatalf("an administrator inside the channel is told only on escalation: %+v", rows)
	}

	// A public channel's escalation reaches every workspace administrator.
	if err := repo.NotifyReview(ctx, chatgate.ReviewNotice{Scope: chatgate.Scope{Tenant: "tenant-a", Conversation: "public"}, SubmissionID: "sub-2", Person: "walt", Level: chatgate.ReviewNoticeEscalated}); err != nil {
		t.Fatal(err)
	}
	if rows := notices("boss"); len(rows) != 1 || rows[0].ConversationID != "public" {
		t.Fatalf("workspace administrator: %+v", rows)
	}
	// The applicant is never told about their own request, even as a manager.
	if err := repo.NotifyReview(ctx, chatgate.ReviewNotice{Scope: chatgate.Scope{Tenant: "tenant-a", Conversation: "public"}, SubmissionID: "sub-3", Person: "pub-lead", Level: chatgate.ReviewNoticeNew}); err != nil {
		t.Fatal(err)
	}
	for _, row := range notices("pub-lead") {
		if row.ID == "gate-review:new:sub-3" {
			t.Fatalf("the applicant was told of their own request: %+v", row)
		}
	}
	// Another workspace's channel of the same id tells nobody here, and a bad
	// level is refused.
	if err := repo.NotifyReview(ctx, chatgate.ReviewNotice{Scope: chatgate.Scope{Tenant: "tenant-b", Conversation: "private"}, SubmissionID: "sub-1", Person: "ana", Level: chatgate.ReviewNoticeNew}); err != nil {
		t.Fatal(err)
	}
	if got = notices("lead"); len(got) != 3 {
		t.Fatalf("another workspace changed this one's notices: %+v", got)
	}
	if err := repo.NotifyReview(context.Background(), chatgate.ReviewNotice{Scope: chatgate.Scope{Tenant: "tenant-a", Conversation: "private"}, SubmissionID: "x", Level: "bogus"}); err == nil {
		t.Fatal("an unknown level was accepted")
	}
}
