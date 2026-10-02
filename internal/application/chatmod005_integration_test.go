package application

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type chatmodReport struct {
	ConversationID, PostID, ReasonCode, Note string
}

func (f *chatmodServed) report(subject, post, reason, note string) int {
	f.t.Helper()
	response := f.call(subject, http.MethodPost, ChatModerationPath+"/report", chatremoveInput{ConversationID: "room", PostID: post, ReasonCode: reason, Note: note})
	return response.Code
}

func (f *chatmodServed) resolve(subject, caseID, action, reason, note string) int {
	f.t.Helper()
	return f.call(subject, http.MethodPost, ChatModerationPath+"/resolve", chatremoveInput{CaseID: caseID, Action: action, ReasonCode: reason, Note: note}).Code
}

// flagged records a hit of a rule whose action is "flag for review", the way the
// filter service records it when a message is sent: before the message exists in
// the queue, with the rule, the author and the channel, and nothing of the text
// but a digest and the mask.
func (f *chatmodServed) flagged(author, rule string) {
	f.t.Helper()
	digest := "sha256:" + strings.Repeat("ab", 32)
	hit := chatfilter.Record{Tenant: f.tenant, Channel: "room", Subject: author, At: time.Now(), Hit: chatfilter.Hit{RuleID: "rule-falcon", RuleName: rule, Version: "1.2.0", Action: "flag", Digest: digest, Masked: "[removed word]"}}
	if err := chatstore.NewFilterStore(f.raw).RecordHits(f.t.Context(), f.tenant, []chatfilter.Record{hit}); err != nil {
		f.t.Fatal(err)
	}
}

func chatmodItem(t *testing.T, q chatmodQueue, kind string) chat.ModerationItem {
	t.Helper()
	for _, item := range q.Items {
		if item.Kind == kind {
			return item
		}
	}
	t.Fatalf("no %q item in the queue: %+v", kind, q.Items)
	return chat.ModerationItem{}
}

// TestTodo_CHATMOD_005_Integration walks the one queue: a report from a member,
// a filter hit flagged for review, and an appeal, each opened by a moderator
// with the message in its context, decided with a recorded actor and reason,
// closed by the decision, and answered to the person who raised it.
func TestTodo_CHATMOD_005_Integration(t *testing.T) {
	f := chatmodSetup(t)
	post := f.post
	other := f.send("author", "another message by the same person", "second")

	// "Report message" is open to every member; a note is optional; the reason
	// comes from the short list.
	if code := f.report("reporter", post.ID, "harassment", "this was aimed at me"); code != http.StatusOK {
		t.Fatalf("a member's report: %d", code)
	}
	if code := f.report("reporter", post.ID, "not-a-reason", ""); code != http.StatusBadRequest {
		t.Fatalf("a reason outside the list: %d", code)
	}
	f.flagged("author", "Project Falcon")

	// Moderators see both, with the message in its context and the rule that matched.
	q := f.queue("owner")
	if len(q.Items) != 2 || q.Count != 2 {
		t.Fatalf("the manager's queue: %+v", q.Items)
	}
	report := chatmodItem(t, q, "report")
	if report.PostID != post.ID || !strings.Contains(report.Reason, "harassment") || !strings.Contains(report.Reason, "aimed at me") || report.ReporterID != "reporter" || !report.CanRemove || len(report.Context) < 2 || report.Message.Body != post.Body {
		t.Fatalf("the report in its context: %+v", report)
	}
	hit := chatmodItem(t, q, "filter")
	if hit.Rule != "Project Falcon 1.2.0" || hit.AuthorID != "author" || hit.ConversationID != "room" || !hit.CanRemove || hit.State != "OPEN" || hit.Message.Body == "" {
		t.Fatalf("the filter hit in its context: %+v", hit)
	}
	if q.Names["reporter"] != "Rae Reporter" || q.Names["author"] != "Alex Author" {
		t.Fatalf("names: %+v", q.Names)
	}
	// The page itself: Open with its count, one item of each kind with the message
	// as the conversation draws it, why it is here, who raised it, and one row of
	// buttons; the reporter is named to a moderator and to nobody else.
	queuePage := f.call("owner", http.MethodGet, ChatModerationPagePath+"?locale=en-US&tz=-240", nil)
	body := queuePage.Body.String()
	for _, want := range []string{"Open · 2", "Resolved", "Report", "Filter hit", "Rule that matched: Project Falcon 1.2.0", "Reported by Rae Reporter", "Alex Author", "the words to be removed", "#Room", `data-chatremove-act="dismiss"`, "action=remove", "action=message", `data-chatremove="filter"`} {
		if queuePage.Code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("the moderator's page misses %q: %d %s", want, queuePage.Code, body)
		}
	}
	if strings.Contains(body, "⟦") || strings.Contains(body, "<style") || strings.Contains(body, "reporter\"") && strings.Contains(body, ">reporter<") {
		t.Fatalf("the page prints a marker or an identifier: %s", body)
	}
	if s := f.summary("owner"); !s.Moderator || s.Open != 2 || len(s.Removable) != 1 || s.Removable[0] != "room" {
		t.Fatalf("the manager's count: %+v", s)
	}
	if s := f.summary("admin"); !s.Moderator || s.Open != 2 || !s.RemoveEverywhere || !s.ReviewEverywhere {
		t.Fatalf("the workspace administrator's count: %+v", s)
	}
	// The count is honest: nobody who cannot open an item is counted for it, and
	// a plain member has no queue at all.
	for _, subject := range []string{"member", "reporter", "author", "outsider"} {
		if s := f.summary(subject); s.Moderator || s.Open != 0 {
			t.Fatalf("%s is counted for items they cannot open: %+v", subject, s)
		}
		if q := f.queue(subject); len(q.Items) != 0 {
			t.Fatalf("%s sees queue items: %+v", subject, q.Items)
		}
	}

	// Dismiss the report: closes it, is recorded with the actor and a reason in
	// the moderator's words or the default, and the reporter is told.
	if code := f.resolve("owner", report.ID, "dismiss", "", ""); code != http.StatusOK {
		t.Fatalf("dismiss: %d", code)
	}
	if code := f.resolve("owner", report.ID, "dismiss", "", ""); code != http.StatusNotFound {
		t.Fatalf("a closed item decided again: %d", code)
	}
	if got := f.rows(`SELECT actor_id||'|'||action||'|'||reason FROM chat_moderation_action WHERE tenant_id=$1 AND case_id=$2`, f.tenant, report.ID); len(got) != 1 || got[0] != "owner|dismiss|no_action" {
		t.Fatalf("the dismissal is not recorded: %v", got)
	}
	told := f.notices("reporter")
	if len(told) != 1 || told[0].Outcome != "dismiss" || strings.HasPrefix(told[0].ID, "outcome:") == false {
		t.Fatalf("the reporter is not told the outcome: %+v", told)
	}
	if q = f.queue("owner"); len(q.Items) != 1 || q.Items[0].Kind != "filter" {
		t.Fatalf("after the dismissal: %+v", q.Items)
	}

	// Message the author on the filter hit needs words; with them it tells the
	// author and closes the item.
	if code := f.resolve("owner", hit.ID, "message_author", "", ""); code != http.StatusBadRequest {
		t.Fatalf("message the author without a note: %d", code)
	}
	if code := f.resolve("owner", hit.ID, "message_author", "", "Please keep client names out of the channel."); code != http.StatusOK {
		t.Fatalf("message the author: %d", code)
	}
	if got := f.notices("author"); len(got) != 1 || got[0].Outcome != "message_author" || !strings.Contains(got[0].Reason, "client names") {
		t.Fatalf("the author's message: %+v", got)
	}
	if q = f.queue("owner"); len(q.Items) != 0 {
		t.Fatalf("a decided hit is still open: %+v", q.Items)
	}

	// A second hit is removed from the queue: the matched message is taken down,
	// the decision is recorded, the author is told why, the hit is closed.
	f.flagged("author", "Client codenames")
	q = f.queue("owner")
	hit2 := chatmodItem(t, q, "filter")
	if hit2.PostID != other.ID && hit2.PostID != post.ID {
		t.Fatalf("the hit is not matched to a message by the same author: %+v", hit2)
	}
	if code := f.resolve("owner", hit2.ID, "remove", "policy_violation", "named a client"); code != http.StatusOK {
		t.Fatalf("remove from the queue: %d", code)
	}
	gone := f.core
	_ = gone
	page, err := f.core.ListPosts(f.as("member"), chat.ListPostsRequest{Principal: chat.Principal{TenantID: f.tenant, SubjectID: "member"}, TenantID: f.tenant, ConversationID: "room", Page: chat.Page{PageSize: 50}})
	if err != nil {
		t.Fatal(err)
	}
	removed := 0
	for _, p := range page.Posts {
		if p.ID == hit2.PostID && p.Deleted && p.Body == chat.RemovedByAdministrator {
			removed++
		}
	}
	if removed != 1 {
		t.Fatalf("the queue's removal is not visible to readers: %+v", page.Posts)
	}
	if got := f.rows(`SELECT decision||'|'||decided_by||'|'||reason FROM chat_filter_hit_review WHERE tenant_id=$1`, f.tenant); len(got) != 2 {
		t.Fatalf("filter decisions: %v", got)
	}
	if got := f.rows(`SELECT actor_id||'|'||action||'|'||reason FROM chat_moderation_action WHERE tenant_id=$1 AND case_id=$2`, f.tenant, hit2.ID); len(got) != 1 || !strings.HasPrefix(got[0], "owner|remove|policy_violation") {
		t.Fatalf("the removal from the queue is not recorded: %v", got)
	}
	if code := f.resolve("owner", hit2.ID, "dismiss", "", ""); code != http.StatusConflict {
		t.Fatalf("a closed filter item decided again: %d", code)
	}

	// The author asks for a review once; the appeal is an open item whose
	// message is the removed one, and Restore brings it back.
	removal := f.notices("author")[0]
	if removal.Outcome != "remove" || !removal.CanAppeal {
		t.Fatalf("the author's notice of the removal: %+v", removal)
	}
	appeal := chatremoveInput{ConversationID: removal.ConversationID, PostID: removal.PostID}
	if response := f.call("author", http.MethodPost, ChatModerationPath+"/appeal", appeal); response.Code != http.StatusOK {
		t.Fatalf("appeal: %d %s", response.Code, response.Body.String())
	}
	if response := f.call("author", http.MethodPost, ChatModerationPath+"/appeal", appeal); response.Code != http.StatusConflict {
		t.Fatalf("a second appeal: %d", response.Code)
	}
	q = f.queue("admin")
	item := chatmodItem(t, q, "appeal")
	if !item.CanRestore || item.CanRemove || item.PostID != removal.PostID || item.ReporterID != "author" {
		t.Fatalf("the appeal: %+v", item)
	}
	if code := f.resolve("owner", item.ID, "restore", "", ""); code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("a manager without the review permission decided an appeal: %d", code)
	}
	if code := f.resolve("admin", item.ID, "restore", "", "the context shows it was a joke between friends"); code != http.StatusOK {
		t.Fatalf("restore from the queue: %d", code)
	}
	if got := f.notices("author"); got[0].Outcome != "restore" || !strings.HasPrefix(got[0].ID, "outcome:") && got[0].Outcome != "restore" {
		t.Fatalf("the author is not told the appeal's outcome: %+v", got)
	}
	if s := f.summary("admin"); s.Open != 0 {
		t.Fatalf("the count after the last decision: %+v", s)
	}
	resolvedPage := f.call("admin", http.MethodGet, ChatModerationPagePath+"?locale=de-DE&tab=resolved&tz=120", nil)
	if resolvedPage.Code != http.StatusOK || !strings.Contains(resolvedPage.Body.String(), "Abgewiesen von Olive Owner") || !strings.Contains(resolvedPage.Body.String(), "Entfernt von Olive Owner") || strings.Contains(resolvedPage.Body.String(), `data-chatremove-act="dismiss"`) {
		t.Fatalf("the resolved tab: %d %s", resolvedPage.Code, resolvedPage.Body.String())
	}
	history := f.call("admin", http.MethodGet, ChatModerationPath+"?query="+url.QueryEscape("closed"), nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"Kind":"filter"`) || !strings.Contains(history.Body.String(), `"Kind":"report"`) {
		t.Fatalf("the queue's history is not searchable: %d %s", history.Code, history.Body.String())
	}
}

// TestTodo_CHATMOD_005_Security_Served: a moderator sees a reported message only
// where they may read it, and the reporter's identity goes to moderators only.
func TestTodo_CHATMOD_005_Security_Served(t *testing.T) {
	f := chatmodSetup(t)
	post := f.post
	if code := f.report("reporter", post.ID, "spam", ""); code != http.StatusOK {
		t.Fatalf("report: %d", code)
	}
	// The reporter's identity is in the queue a moderator reads; no one else is
	// given the queue, and the page a plain member opens names nobody.
	if q := f.queue("owner"); len(q.Items) != 1 || q.Items[0].ReporterID != "reporter" {
		t.Fatalf("moderator's queue: %+v", q.Items)
	}
	member := f.call("member", http.MethodGet, ChatModerationPagePath, nil)
	if member.Code != http.StatusOK || strings.Contains(member.Body.String(), "Rae Reporter") || strings.Contains(member.Body.String(), `data-chatremove="resolve"`) {
		t.Fatalf("a plain member's page: %d %s", member.Code, member.Body.String())
	}
	if code := f.resolve("member", "report:"+f.rows(`SELECT report_id FROM chat_moderation_report WHERE tenant_id=$1`, f.tenant)[0], "dismiss", "", ""); code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("a plain member decided a report: %d", code)
	}
	// A report of a message the reporter cannot see is refused.
	if code := f.report("outsider", post.ID, "spam", ""); code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("an outsider's report of a private message: %d", code)
	}

	// A private conversation: a workspace administrator who is not in it neither
	// sees the report nor is counted for it nor can decide it.
	owner := chat.Principal{TenantID: f.tenant, SubjectID: "owner"}
	if _, err := f.core.CreateConversation(t.Context(), chat.CreateConversationRequest{Principal: owner, TenantID: f.tenant, ConversationID: "secret", Kind: chat.PrivateChannel, Name: "Secret"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.core.AddMembership(t.Context(), chat.AddMembershipRequest{Principal: owner, Membership: chat.Membership{TenantID: f.tenant, ConversationID: "secret", HomeTenantID: f.tenant, SubjectID: "author", Role: chat.Member, HistoryVisibility: chat.FullHistory}}); err != nil {
		t.Fatal(err)
	}
	private, err := f.core.SendPost(t.Context(), chat.SendPostRequest{Principal: chat.Principal{TenantID: f.tenant, SubjectID: "author"}, TenantID: f.tenant, ConversationID: "secret", Body: "private words", IdempotencyKey: "private"})
	if err != nil {
		t.Fatal(err)
	}
	f.route("secret")
	if err = f.raw.RunTenantTx(t.Context(), f.tenant, func(tx dbport.Tx) error {
		_, e := tx.Exec(t.Context(), `INSERT INTO chat_moderation_report(tenant_id,report_id,conversation_id,target_id,reporter_id,reason,created_at,state) VALUES($1,'private-case','secret',$2,'owner','spam',now(),'OPEN')`, f.tenant, private.ID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	for _, item := range f.queue("admin").Items {
		if item.ConversationID == "secret" || strings.Contains(item.Message.Body, "private words") {
			t.Fatalf("the administrator outside the private channel reads it: %+v", item)
		}
	}
	if s := f.summary("admin"); s.Open != 1 {
		t.Fatalf("the administrator is counted for an item they cannot open: %+v", s)
	}
	if code := f.resolve("admin", "report:private-case", "remove", "spam", ""); code != http.StatusForbidden && code != http.StatusNotFound {
		t.Fatalf("an administrator outside a private channel decided its report: %d", code)
	}
	if code := f.resolve("owner", "report:private-case", "dismiss", "", ""); code != http.StatusOK {
		t.Fatalf("the channel's manager decided its report: %d", code)
	}
}
