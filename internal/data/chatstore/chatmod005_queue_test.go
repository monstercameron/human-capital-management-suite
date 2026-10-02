package chatstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func chatmodRecord(author, rule, action string, dry bool) chatfilter.Record {
	return chatfilter.Record{Tenant: "tenant-a", Channel: "room", Subject: author, At: time.Now(), Hit: chatfilter.Hit{RuleID: "rule-" + rule, RuleName: rule, Version: "1.0.0", Action: action, DryRun: dry, Digest: "sha256:" + strings.Repeat("cd", 32), Masked: "[removed word]"}}
}

// TestTodo_CHATMOD_005_FilterQueue: only a hit flagged for review, enforced, is
// an open item; it is matched to the author's message, shown with the rule, and
// decided once with a recorded actor and reason.
func TestTodo_CHATMOD_005_FilterQueue(t *testing.T) {
	s, p, post := chatremoveDB(t)
	ctx := context.Background()
	filters := NewFilterModeration(s)
	if err := NewFilterStore(s.Store).RecordHits(ctx, p.TenantID, []chatfilter.Record{
		chatmodRecord("author", "Project Falcon", "flag", false),
		chatmodRecord("author", "Dry run only", "flag", true),
		chatmodRecord("author", "Masked word", "mask", false),
		chatmodRecord("author", "Blocked word", "block", false),
	}); err != nil {
		t.Fatal(err)
	}
	items, err := filters.SearchModeration(ctx, p, p.TenantID, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("open hits=%+v err=%v", items, err)
	}
	hit := items[0]
	if hit.Kind != "filter" || hit.Rule != "Project Falcon 1.0.0" || hit.PostID != post.ID || hit.AuthorID != "author" || !hit.CanRemove || hit.Message.Body != post.Body || len(hit.Context) != 1 || !strings.HasPrefix(hit.ID, "filter:") || hit.State != "OPEN" {
		t.Fatalf("hit=%+v", hit)
	}
	if n, err := filters.CountModeration(ctx, p, p.TenantID); err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	// A person outside the private room is shown nothing, counted for nothing,
	// and cannot decide it.
	outsider := chat.Principal{TenantID: p.TenantID, SubjectID: "outsider"}
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,'outsider','WORKSPACE_ADMIN')`, p.TenantID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if seen, err := filters.SearchModeration(ctx, outsider, p.TenantID, ""); err != nil || len(seen) != 0 {
		t.Fatalf("outsider sees %+v err=%v", seen, err)
	}
	if n, err := filters.CountModeration(ctx, outsider, p.TenantID); err != nil || n != 0 {
		t.Fatalf("outsider is counted for %d err=%v", n, err)
	}
	if err = filters.ResolveModeration(ctx, outsider, p.TenantID, hit.ID, "dismiss", "no_action", time.Now()); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("an outsider decided a hit", err)
	}
	if err = filters.ResolveModeration(ctx, p, p.TenantID, "filter:999999", "dismiss", "no_action", time.Now()); !errors.Is(err, chat.ErrNotFound) {
		t.Fatal("an unknown hit", err)
	}
	if err = filters.ResolveModeration(ctx, p, p.TenantID, "report:1", "dismiss", "no_action", time.Now()); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatal("not a filter item", err)
	}
	if err = filters.ResolveModeration(ctx, p, p.TenantID, hit.ID, "dismiss", "", time.Now()); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatal("a decision without a reason", err)
	}
	if cid, err := filters.ConversationOf(ctx, p.TenantID, hit.ID); err != nil || cid != "room" {
		t.Fatalf("conversation=%q err=%v", cid, err)
	}

	// Remove: the message comes down with the author told, the hit closes, the
	// decision and its reason are recorded, and a second decision is refused.
	if err = filters.ResolveModeration(ctx, p, p.TenantID, hit.ID, "remove", "policy_violation — named a client", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetPost(ctx, p.TenantID, "room", post.ID); err != nil || !got.Deleted || got.Body != chat.RemovedByAdministrator {
		t.Fatalf("post=%+v err=%v", got, err)
	}
	var decided, actions int
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM chat_filter_hit_review WHERE tenant_id=$1 AND hit_id=$2 AND decision='remove' AND decided_by=$3 AND reason LIKE 'policy_violation%'`, p.TenantID, strings.TrimPrefix(hit.ID, "filter:"), p.SubjectID).Scan(&decided); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_moderation_action WHERE tenant_id=$1 AND case_id=$2 AND actor_id=$3`, p.TenantID, hit.ID, p.SubjectID).Scan(&actions)
	}); err != nil || decided != 1 || actions != 1 {
		t.Fatalf("decided=%d actions=%d err=%v", decided, actions, err)
	}
	if err = filters.ResolveModeration(ctx, p, p.TenantID, hit.ID, "dismiss", "no_action", time.Now()); !errors.Is(err, chat.ErrConflict) {
		t.Fatal("a closed hit decided again", err)
	}
	if open, err := filters.SearchModeration(ctx, p, p.TenantID, ""); err != nil || len(open) != 0 {
		t.Fatalf("open=%+v err=%v", open, err)
	}
	// The history of a removed message is read by the review permission, which a
	// workspace administrator holds and a channel manager does not.
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN')`, p.TenantID, p.SubjectID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	history, err := filters.SearchModeration(ctx, p, p.TenantID, "falcon")
	if err != nil || len(history) != 1 || history[0].State != "CLOSED" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	if notices, err := s.ModerationNotices(ctx, chat.Principal{TenantID: p.TenantID, SubjectID: "author"}, p.TenantID); err != nil || len(notices) != 1 || !strings.Contains(notices[0].Reason, "named a client") {
		t.Fatalf("author notices=%+v err=%v", notices, err)
	}
}

// TestTodo_CHATMOD_005_FilterQueueNoMessage: a hit no message can be matched to
// is still listed with its rule and can only be dismissed.
func TestTodo_CHATMOD_005_FilterQueueNoMessage(t *testing.T) {
	s, p, _ := chatremoveDB(t)
	ctx := context.Background()
	filters := NewFilterModeration(s)
	stale := chatmodRecord("author", "Old rule", "flag", false)
	stale.At = time.Now().Add(-time.Hour)
	if err := NewFilterStore(s.Store).RecordHits(ctx, p.TenantID, []chatfilter.Record{stale}); err != nil {
		t.Fatal(err)
	}
	items, err := filters.SearchModeration(ctx, p, p.TenantID, "")
	if err != nil || len(items) != 1 || items[0].PostID != "" || items[0].Rule != "Old rule 1.0.0" || items[0].CanRemove {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if err = filters.ResolveModeration(ctx, p, p.TenantID, items[0].ID, "remove", "spam", time.Now()); !errors.Is(err, chat.ErrConflict) {
		t.Fatal("removed a message that was never matched", err)
	}
	if err = filters.ResolveModeration(ctx, p, p.TenantID, items[0].ID, "dismiss", "no_action", time.Now()); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_CHATMOD_005_Summary: the one small read behind the sidebar entry and
// the message menu counts what the person can open, lists where they may remove
// and restore, carries their own notices, and writes no audit row.
func TestTodo_CHATMOD_005_Summary(t *testing.T) {
	s, p, post := chatremoveDB(t)
	ctx := context.Background()
	author := chat.Principal{TenantID: p.TenantID, SubjectID: "author"}
	var before int
	count := func() (n int) {
		if err := s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM chat_audit_event WHERE tenant_id=$1`, p.TenantID).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if sum, err := s.ModerationSummary(ctx, author, p.TenantID); err != nil || sum.Moderator || sum.Open != 0 || len(sum.Removable) != 0 || sum.NoticeCount != 0 {
		t.Fatalf("a plain member's summary=%+v err=%v", sum, err)
	}
	if _, err := s.ModerationSummary(ctx, chat.Principal{TenantID: "other", SubjectID: "author"}, p.TenantID); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatal("another workspace read the summary", err)
	}
	if err := s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO chat_moderation_report(tenant_id,report_id,conversation_id,target_id,reporter_id,reason,created_at,state) VALUES($1,'r1','room',$2,'author','spam',now(),'OPEN')`, p.TenantID, post.ID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	before = count()
	sum, err := s.ModerationSummary(ctx, p, p.TenantID)
	if err != nil || !sum.Moderator || sum.Open != 1 || len(sum.Removable) != 1 || sum.Removable[0] != "room" || len(sum.Reviewable) != 0 || sum.RemoveEverywhere || sum.ReviewEverywhere {
		t.Fatalf("the manager's summary=%+v err=%v", sum, err)
	}
	chatremoveApply(t, s, p, post, "remove")
	if count() <= before {
		t.Fatal("a removal wrote no audit row")
	}
	before = count()
	if sum, err = s.ModerationSummary(ctx, p, p.TenantID); err != nil || sum.Open != 0 {
		t.Fatalf("after the removal summary=%+v err=%v", sum, err)
	}
	if count() != before {
		t.Fatal("reading the summary wrote an audit row")
	}
	sum, err = s.ModerationSummary(ctx, author, p.TenantID)
	if err != nil || sum.Moderator || sum.NoticeCount != 2 || len(sum.Notices) != 2 {
		t.Fatalf("the author's summary=%+v err=%v", sum, err)
	}
	// A workspace administrator is a holder everywhere; a channel override
	// removes the manager's default.
	if err = s.removalTx(ctx, p.TenantID, func(tx dbport.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO chat_moderation_role(tenant_id,subject_id,role) VALUES($1,$2,'WORKSPACE_ADMIN')`, p.TenantID, p.SubjectID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if sum, err = s.ModerationSummary(ctx, p, p.TenantID); err != nil || !sum.RemoveEverywhere || !sum.ReviewEverywhere || len(sum.Reviewable) != 1 {
		t.Fatalf("the administrator's summary=%+v err=%v", sum, err)
	}
	target, err := s.ModerationTarget(ctx, p, p.TenantID, "room", post.ID)
	if err != nil || target.AuthorID != "author" || target.Body != chat.RemovedByAdministrator {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	if _, err = s.ModerationTarget(ctx, p, p.TenantID, "room", "missing"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatal("a missing message", err)
	}
}
