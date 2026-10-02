package chatstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chatremoveRunFixture struct{}

func (chatremoveRunFixture) RunForModerationPost(context.Context, string, string, string) (string, error) {
	return "run:fixture", nil
}

type chatremoveSkillFixture struct{ reviewer chat.Principal }

func (f chatremoveSkillFixture) AuthorizeModerationSummary(context.Context, chat.Principal, string, string) (chat.Principal, error) {
	return f.reviewer, nil
}

func TestTodo_CHATMOD_005_Security(t *testing.T) {
	s, p, post := chatremoveDB(t)
	ctx := context.Background()
	if err := s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN'),($1,'outsider','WORKSPACE_ADMIN')`, p.TenantID, p.SubjectID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_moderation_report(tenant_id,report_id,conversation_id,target_id,reporter_id,reason,created_at,state) VALUES($1,'case','room',$2,'reporter','spam',now(),'OPEN')`, p.TenantID, post.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	outsider := chat.Principal{TenantID: p.TenantID, SubjectID: "outsider"}
	items, err := s.SearchModeration(ctx, outsider, p.TenantID, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("outside private count=%d err=%v", len(items), err)
	}
	if err = s.ResolveModeration(ctx, outsider, p.TenantID, "report:case", "remove", "reason", time.Now()); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("private outsider resolve", err)
	}
	agent := chat.Principal{TenantID: p.TenantID, SubjectID: "agent"}
	now := time.Now()
	verified, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(p.TenantID), Subject: agent.SubjectID, SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "fixture", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	agentCtx := trust.WithPrincipal(ctx, verified)
	if _, err = s.QueueTool(agentCtx, agent, p.TenantID, "review", nil); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("missing skill allowed", err)
	}
	items, err = s.QueueTool(agentCtx, agent, p.TenantID, "review", chatremoveSkillFixture{reviewer: p})
	if err != nil || len(items) != 1 || items[0].ReporterID != "" {
		t.Fatalf("tool=%+v err=%v", items, err)
	}
	if err = s.FlagQueueTool(agentCtx, agent, p.TenantID, "review", "room", post.ID, "flagged for human review", chatremoveSkillFixture{reviewer: p}); err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveModeration(agentCtx, agent, p.TenantID, "report:case", "remove", "reason", time.Now()); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("agent removed", err)
	}
	items, err = s.SearchModeration(ctx, p, p.TenantID, "")
	if err != nil || len(items) != 2 {
		t.Fatalf("flag=%+v err=%v", items, err)
	}
}

func chatremoveDB(t *testing.T) (*ModeratedAdapter, chat.Principal, chat.Post) {
	t.Helper()
	s := NewModeratedAdapter(adapterDB(t))
	ctx := context.Background()
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "moderator"}
	c := chat.Conversation{ID: "room", TenantID: p.TenantID, Kind: chat.PrivateChannel, OwnerID: p.SubjectID, Revision: 1}
	var members []chat.Membership
	for _, id := range []string{"moderator", "author"} {
		role := chat.Member
		if id == "moderator" {
			role = chat.Manager
		}
		members = append(members, chat.Membership{TenantID: p.TenantID, ConversationID: c.ID, HomeTenantID: p.TenantID, SubjectID: id, Role: role, HistoryVisibility: chat.FullHistory})
	}
	if _, err := s.CreateConversation(ctx, c, members, ""); err != nil {
		t.Fatal(err)
	}
	post, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: p.TenantID, ConversationID: c.ID, IdempotencyKey: "first"}, chat.Post{AuthorID: "author", AuthorHomeTenantID: p.TenantID, Body: "retained evidence", References: []chat.Reference{{Kind: chat.MediaAttachment, ID: "media", Display: "secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	return s, p, post
}

func chatremoveApply(t *testing.T, s *ModeratedAdapter, p chat.Principal, post chat.Post, action string) {
	t.Helper()
	service := chat.ModerationService{Store: s}
	r := chat.RemovalRequest{Principal: p, TenantID: p.TenantID, Selection: chat.RemovalSelection{ConversationID: post.ConversationID, PostIDs: []string{post.ID}}, ReasonCode: "harassment", Note: "reviewed conduct", Action: action}
	preview, err := service.Preview(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	r.Confirmation, r.ConfirmedCount = preview.Confirmation, preview.Count
	if count, err := service.Apply(context.Background(), r); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestTodo_CHATMOD_004_Integration(t *testing.T) {
	s, p, post := chatremoveDB(t)
	s.RunTrace = chatremoveRunFixture{}
	ctx := context.Background()
	if err := s.PlaceRecordHold(ctx, p.TenantID, "hold", "matter", "preserve", "moderator", []string{"post:" + post.ID}); err != nil {
		t.Fatal(err)
	}
	chatremoveApply(t, s, p, post, "remove")
	retried, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: p.TenantID, ConversationID: "room", IdempotencyKey: "first"}, chat.Post{AuthorID: "author", AuthorHomeTenantID: p.TenantID, Body: post.Body, References: post.References})
	if err != nil || retried.Body != chat.RemovedByAdministrator || len(retried.References) != 0 {
		t.Fatalf("idempotent send leaked=%+v err=%v", retried, err)
	}
	deleted, err := s.DeletePost(ctx, chat.DeletePostRequest{Principal: chat.Principal{TenantID: p.TenantID, SubjectID: "author"}, TenantID: p.TenantID, ConversationID: "room", PostID: post.ID, ExpectedRevision: post.Revision})
	if err != nil || deleted.Body != chat.RemovedByAdministrator || len(deleted.References) != 0 {
		t.Fatalf("delete retry leaked=%+v err=%v", deleted, err)
	}
	var trace string
	if err := s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT run_ref FROM chat_admin_removal WHERE tenant_id=$1 AND post_id=$2`, p.TenantID, post.ID).Scan(&trace)
	}); err != nil || trace != "run:fixture" {
		t.Fatalf("trace=%q err=%v", trace, err)
	}
	got, err := s.GetPost(ctx, p.TenantID, "room", post.ID)
	if err != nil || !got.Deleted || got.Body != chat.RemovedByAdministrator || len(got.References) != 0 {
		t.Fatalf("reader=%+v err=%v", got, err)
	}
	page, err := s.ListPosts(ctx, p, p.TenantID, "room", 0, chat.Page{PageSize: 10}, chat.PostWindow{})
	if err != nil || len(page.Posts) != 1 || page.Posts[0].Body != chat.RemovedByAdministrator {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if deliver, err := s.CanDeliverPost(ctx, p.TenantID, "room", post.ID); err != nil || deliver {
		t.Fatalf("deliver=%v err=%v", deliver, err)
	}
	var body string
	var revisions, holds int
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT body FROM chat_post WHERE tenant_id=$1 AND id=$2`, p.TenantID, post.ID).Scan(&body); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_post_revision WHERE tenant_id=$1 AND post_id=$2`, p.TenantID, post.ID).Scan(&revisions); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_record_hold WHERE tenant_id=$1 AND released_at IS NULL`, p.TenantID).Scan(&holds)
	}); err != nil || body != post.Body || revisions != 1 || holds != 1 {
		t.Fatalf("body=%q revisions=%d holds=%d err=%v", body, revisions, holds, err)
	}
	if _, err = s.ReviewRemoved(ctx, p, p.TenantID, "room", post.ID, "investigation", time.Now()); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("channel manager read original", err)
	}
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN')`, p.TenantID, p.SubjectID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	original, err := s.ReviewRemoved(ctx, p, p.TenantID, "room", post.ID, "investigation", time.Now())
	if err != nil || original.Body != post.Body {
		t.Fatalf("original=%+v err=%v", original, err)
	}
	var auditReason string
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT reason FROM chat_audit_event WHERE tenant_id=$1 AND action='moderation.review' AND target_id=$2 ORDER BY sequence DESC LIMIT 1`, p.TenantID, post.ID).Scan(&auditReason)
	}); err != nil || auditReason != "investigation" {
		t.Fatalf("audit reason=%q err=%v", auditReason, err)
	}
	notices, err := s.ModerationNotices(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "author"}, p.TenantID)
	if err != nil || len(notices) != 1 || !notices[0].CanAppeal || !strings.Contains(notices[0].Reason, "harassment") {
		t.Fatalf("notices=%+v err=%v", notices, err)
	}
	chatremoveApply(t, s, p, post, "restore")
	restored, err := s.GetPost(ctx, p.TenantID, "room", post.ID)
	if err != nil || restored.Deleted || restored.Body != post.Body || len(restored.References) != 1 {
		t.Fatalf("restored=%+v err=%v", restored, err)
	}
}

func TestTodo_CHATMOD_004(t *testing.T) {
	s, p, post := chatremoveDB(t)
	ctx := context.Background()
	second, err := s.SendPost(ctx, chat.SendPostRequest{TenantID: p.TenantID, ConversationID: "room", IdempotencyKey: "second"}, chat.Post{AuthorID: "author", AuthorHomeTenantID: p.TenantID, Body: "second evidence", ParentID: post.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutPin(ctx, chat.Pin{TenantID: p.TenantID, ConversationID: "room", PostID: post.ID, PinnedBy: p.SubjectID, PinnedByHomeTenantID: p.TenantID}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutReaction(ctx, chat.Reaction{TenantID: p.TenantID, ConversationID: "room", PostID: post.ID, SubjectID: p.SubjectID, HomeTenantID: p.TenantID, Emoji: "ok"}); err != nil {
		t.Fatal(err)
	}
	service := chat.ModerationService{Store: s}
	r := chat.RemovalRequest{Principal: p, TenantID: p.TenantID, Selection: chat.RemovalSelection{ConversationID: "room", AuthorID: "author", AuthorHomeTenantID: p.TenantID, From: post.CreatedAt.Add(-time.Second), Until: second.CreatedAt.Add(time.Second)}, ReasonCode: "sensitive_information", Action: "remove"}
	recipients := NewRecipientStateStore(s.Store)
	identity := chatrecipient.Identity{HostTenantID: p.TenantID, HomeTenantID: p.TenantID, SubjectID: p.SubjectID, ConversationID: "room"}
	if counts, err := recipients.Counts(ctx, identity); err != nil || counts.Unread != 2 {
		t.Fatalf("before removal counts=%+v err=%v", counts, err)
	}
	preview, err := service.Preview(ctx, r)
	if err != nil || preview.Count != 2 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	r.Confirmation, r.ConfirmedCount = preview.Confirmation, preview.Count
	if count, err := service.Apply(ctx, r); err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if counts, err := recipients.Counts(ctx, identity); err != nil || counts.Unread != 0 || counts.Mentions != 0 {
		t.Fatalf("removed counts=%+v err=%v", counts, err)
	}
	if pins, err := s.ListPins(ctx, p.TenantID, "room"); err != nil || len(pins) != 0 {
		t.Fatalf("pins=%+v err=%v", pins, err)
	}
	if reactions, err := s.ListReactions(ctx, p, p.TenantID, "room", post.ID, chat.Page{}); err != nil || len(reactions.Reactions) != 0 {
		t.Fatalf("reactions=%+v err=%v", reactions, err)
	}
	if search, err := s.Search(ctx, chat.SearchRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room", Query: "evidence"}); err != nil || len(search.Results) != 0 {
		t.Fatalf("search=%+v err=%v", search, err)
	}
	got, err := s.GetPost(ctx, p.TenantID, "room", second.ID)
	if err != nil || got.ParentID != post.ID {
		t.Fatalf("thread=%+v err=%v", got, err)
	}
	page, err := s.ReadConversationEvents(ctx, chat.WatchConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room"}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	posts := 0
	for _, event := range page.Events {
		if event.Event.Post != nil {
			posts++
			if event.Event.Post.Body != chat.RemovedByAdministrator || len(event.Event.Post.References) != 0 {
				t.Fatalf("historical event leaked %+v", event.Event.Post)
			}
		}
		if event.Event.Reaction != nil || event.Event.Pin != nil {
			t.Fatal("removed decorations replayed")
		}
	}
	if posts < 2 {
		t.Fatalf("post events=%d", posts)
	}
	watchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	events, _, err := s.WatchWithErrors(watchCtx, chat.WatchConversationRequest{Principal: p, TenantID: p.TenantID, ConversationID: "room"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Event.Post != nil && event.Event.Post.Body == "retained evidence" {
			t.Fatal("watch leak")
		}
	case <-watchCtx.Done():
		t.Fatal("watch did not replay")
	}
}

func TestTodo_CHATMOD_004_Security(t *testing.T) {
	s, p, post := chatremoveDB(t)
	ctx := context.Background()
	outsider := chat.Principal{TenantID: p.TenantID, SubjectID: "outsider", Roles: []string{"WORKSPACE_ADMIN"}}
	if err := s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,'outsider','WORKSPACE_ADMIN')`, p.TenantID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r := chat.RemovalRequest{Principal: outsider, TenantID: p.TenantID, Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{post.ID}}, ReasonCode: "spam", Action: "remove"}
	if _, err := s.PreviewRemoval(ctx, r); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("private outsider admitted", err)
	}
	if _, err := s.ReviewRemoved(ctx, outsider, p.TenantID, "room", post.ID, "reason", time.Now()); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal(err)
	}
	got, _ := s.GetPost(ctx, p.TenantID, "room", post.ID)
	if got.Deleted {
		t.Fatal("refused removal mutated")
	}
	outsider = chat.Principal{TenantID: p.TenantID, SubjectID: "author", Roles: []string{"WORKSPACE_ADMIN"}}
	r.Principal = outsider
	if _, err := s.PreviewRemoval(ctx, r); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("untrusted role claim allowed removal", err)
	}
	if err := s.AssignModerationPermission(ctx, outsider, p.TenantID, "room", "MEMBER", chat.PermissionRemoveMessages, true); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("member assigned permission", err)
	}
	if err := s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN')`, p.TenantID, p.SubjectID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AssignModerationPermission(ctx, p, p.TenantID, "room", "MEMBER", chat.PermissionRemoveMessages, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewRemoval(ctx, r); err != nil {
		t.Fatal("explicit channel permission ignored", err)
	}
	preview, err := s.PreviewRemoval(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	r.ConfirmedCount, r.Confirmation = len(preview), chat.RemovalConfirmation(r, preview)
	if err := s.AssignModerationPermission(ctx, p, p.TenantID, "room", "MEMBER", chat.PermissionRemoveMessages, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitRemoval(ctx, r, time.Now()); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("revoked permission survived preview", err)
	}
}

func TestTodo_CHATMOD_004_Integration_RestoreDeadline(t *testing.T) {
	s, p, post := chatremoveDB(t)
	ctx := context.Background()
	service := chat.ModerationService{Store: s, Clock: func() time.Time { return time.Now().Add(-31 * 24 * time.Hour) }}
	r := chat.RemovalRequest{Principal: p, TenantID: p.TenantID, Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{post.ID}}, ReasonCode: "spam", Action: "remove"}
	preview, err := service.Preview(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	r.Confirmation, r.ConfirmedCount = preview.Confirmation, preview.Count
	if _, err = service.Apply(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN')`, p.TenantID, p.SubjectID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r.Action = "restore"
	if _, err := service.Preview(ctx, r); !errors.Is(err, chat.ErrConflict) {
		t.Fatal("restore after thirty days admitted", err)
	}
	got, err := s.GetPost(ctx, p.TenantID, "room", post.ID)
	if err != nil || !got.Deleted {
		t.Fatalf("post=%+v err=%v", got, err)
	}
}

func TestTodo_CHATMOD_004_Integration_AuthorHomeTenant(t *testing.T) {
	s, p, _ := chatremoveDB(t)
	ctx := context.Background()
	guest := chat.Principal{TenantID: "guest-tenant", SubjectID: "guest-author"}
	member, err := s.PutMembership(ctx, p, chat.Membership{TenantID: p.TenantID, ConversationID: "room", HomeTenantID: guest.TenantID, SubjectID: guest.SubjectID, Role: chat.Member, HistoryVisibility: chat.FullHistory})
	if err != nil {
		t.Fatal(err)
	}
	post, err := s.SendPost(ctx, chat.SendPostRequest{Principal: guest, TenantID: p.TenantID, ConversationID: "room", IdempotencyKey: "guest"}, chat.Post{AuthorID: guest.SubjectID, AuthorHomeTenantID: guest.TenantID, Body: "guest message"})
	if err != nil {
		t.Fatal(err)
	}
	chatremoveApply(t, s, p, post, "remove")
	if _, err = s.RemoveMembership(ctx, p, p.TenantID, "room", guest.TenantID, guest.SubjectID, member.Revision); err != nil {
		t.Fatal(err)
	}
	notices, err := s.ModerationNotices(ctx, guest, p.TenantID)
	if err != nil || len(notices) != 1 || !notices[0].CanAppeal {
		t.Fatalf("guest notices=%+v err=%v", notices, err)
	}
	if err = s.AppealRemoval(ctx, guest, p.TenantID, "room", post.ID, time.Now()); err != nil {
		t.Fatal("author appeal after leaving", err)
	}
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN')`, p.TenantID, p.SubjectID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	items, err := s.SearchModeration(ctx, p, p.TenantID, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.Kind == "appeal" {
			found = true
			if err = s.ResolveModeration(ctx, p, p.TenantID, item.ID, "dismiss", "review completed", time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found {
		t.Fatal("guest appeal not queued")
	}
	notices, err = s.ModerationNotices(ctx, guest, p.TenantID)
	if err != nil || len(notices) != 2 {
		t.Fatalf("guest outcome=%+v err=%v", notices, err)
	}
	guest.SubjectID = "different-guest"
	notices, err = s.ModerationNotices(ctx, guest, p.TenantID)
	if err != nil || len(notices) != 0 {
		t.Fatalf("recipient leak=%+v err=%v", notices, err)
	}
}

func TestTodo_CHATMOD_004_Fault(t *testing.T) {
	s, p, post := chatremoveDB(t)
	ctx := context.Background()
	service := chat.ModerationService{Store: s}
	r := chat.RemovalRequest{Principal: p, TenantID: p.TenantID, Selection: chat.RemovalSelection{ConversationID: "room", PostIDs: []string{post.ID}}, ReasonCode: "spam", Action: "remove"}
	preview, err := service.Preview(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	r.Confirmation, r.ConfirmedCount = preview.Confirmation, preview.Count
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `CREATE FUNCTION chatremove_fail_notice() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected crash'; END $$; CREATE TRIGGER chatremove_fail BEFORE INSERT ON chat_moderation_notice FOR EACH ROW EXECUTE FUNCTION chatremove_fail_notice()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Apply(ctx, r); err == nil {
		t.Fatal("notice crash committed")
	}
	got, _ := s.GetPost(ctx, p.TenantID, "room", post.ID)
	if got.Deleted {
		t.Fatal("partial removal survived rollback")
	}
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DROP TRIGGER chatremove_fail ON chat_moderation_notice`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Apply(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Apply(ctx, r); !errors.Is(err, chat.ErrConflict) {
		t.Fatal("replay accepted", err)
	}
	notices, err := s.ModerationNotices(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "author"}, p.TenantID)
	if err != nil || len(notices) != 1 {
		t.Fatalf("notices=%+v err=%v", notices, err)
	}
}

func TestTodo_CHATMOD_005(t *testing.T) {
	s, p, post := chatremoveDB(t)
	ctx := context.Background()
	chatremoveApply(t, s, p, post, "remove")
	author := chat.Principal{TenantID: p.TenantID, SubjectID: "author"}
	if err := s.AppealRemoval(ctx, author, p.TenantID, "room", post.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.AppealRemoval(ctx, author, p.TenantID, "room", post.ID, time.Now()); !errors.Is(err, chat.ErrConflict) {
		t.Fatal("second appeal", err)
	}
	if err := s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN')`, p.TenantID, p.SubjectID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// CHATMOD-005: a removal is a decision already made, so it is history; the
	// open queue holds the appeal alone.
	items, err := s.SearchModeration(ctx, p, p.TenantID, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("queue=%+v err=%v", items, err)
	}
	var appeal chat.ModerationItem
	for _, item := range items {
		if item.Kind == "appeal" {
			appeal = item
		}
	}
	if appeal.ReporterID != "author" || appeal.Message.Body != post.Body || len(appeal.Context) != 1 {
		t.Fatalf("appeal=%+v", appeal)
	}
	if err = s.ResolveModeration(ctx, p, p.TenantID, appeal.ID, "dismiss", "review completed", time.Now()); err != nil {
		t.Fatal(err)
	}
	items, err = s.SearchModeration(ctx, p, p.TenantID, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("open=%+v err=%v", items, err)
	}
	history, err := s.SearchModeration(ctx, p, p.TenantID, "closed")
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	notices, err := s.ModerationNotices(ctx, author, p.TenantID)
	if err != nil || len(notices) != 2 || notices[0].Outcome != "dismiss" {
		t.Fatalf("outcome=%+v err=%v", notices, err)
	}
	if items, err = s.SearchModeration(ctx, author, p.TenantID, ""); err != nil || len(items) != 0 {
		t.Fatalf("author queue=%+v err=%v", items, err)
	}
	chatremoveApply(t, s, p, post, "restore")
	restored, err := s.GetPost(ctx, p.TenantID, "room", post.ID)
	if err != nil || restored.Deleted {
		t.Fatalf("restore action=%+v err=%v", restored, err)
	}
	for _, action := range []string{"message_author", "remove"} {
		caseID := "case-" + action
		if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_report(tenant_id,report_id,conversation_id,target_id,reporter_id,reason,created_at,state) VALUES($1,$2,'room',$3,'reporter','spam',now(),'OPEN')`, p.TenantID, caseID, post.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err = s.ResolveModeration(ctx, p, p.TenantID, "report:"+caseID, action, "human review completed", time.Now()); err != nil {
			t.Fatal(action, err)
		}
		if err = s.ResolveModeration(ctx, p, p.TenantID, "report:"+caseID, action, "replay", time.Now()); !errors.Is(err, chat.ErrConflict) {
			t.Fatal("replayed queue action", err)
		}
	}
	reporterNotices, err := s.ModerationNotices(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "reporter"}, p.TenantID)
	if err != nil || len(reporterNotices) != 2 {
		t.Fatalf("reporter notices=%+v err=%v", reporterNotices, err)
	}
	for _, notice := range reporterNotices {
		if notice.CanAppeal {
			t.Fatal("reporter offered author's appeal")
		}
	}
}
