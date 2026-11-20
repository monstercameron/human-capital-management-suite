package chatstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

const issue3787MentionU1 = `[{"Kind":"PERSON_MENTION","TenantID":"host","ID":"u-1","Display":"U1"}]`
const issue3787MentionU2 = `[{"Kind":"PERSON_MENTION","TenantID":"host","ID":"u-2","Display":"U2"}]`

func issue3787Post(id string, seq int, author, body, refs, parent, age string) string {
	if refs == "" {
		refs = "[]"
	}
	return `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json,parent_id,created_at) VALUES('` + id + `','host','c-1','` + author + `','host',` + itoa3787(seq) + `,'` + body + `','` + refs + `','` + parent + `',now()-interval '` + age + `')`
}

func itoa3787(n int) string {
	if n >= 10 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0' + n))
}

// issue3787Seed builds a channel with u-1 (the caller) and u-2.
func issue3787Seed(t *testing.T) (*Store, *RecipientStateStore, chatrecipient.Identity) {
	t.Helper()
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "c-1", TenantID: "host", Kind: "PUBLIC_CHANNEL", OwnerID: "u-1", Lifecycle: "ACTIVE", SettingsRevision: 1},
		[]Membership{{MemberID: "u-1", Role: "manager", State: "active"}, {MemberID: "u-2", Role: "member", State: "active"}})
	for _, q := range []string{
		`UPDATE chat_membership SET home_tenant_id='host' WHERE tenant_id='host'`,
		// u-2 asks u-1 something (owed).
		issue3787Post("ask", 1, "u-2", "@U1 can you confirm the swap?", issue3787MentionU1, "", "5 hours"),
		// u-2 asks u-1 and u-1 answers in the thread (not owed).
		issue3787Post("asked-answered", 2, "u-2", "@U1 lunch?", issue3787MentionU1, "", "5 hours"),
		issue3787Post("my-answer", 3, "u-1", "yes", "", "asked-answered", "4 hours"),
		// Too recent to nag about.
		issue3787Post("fresh", 4, "u-2", "@U1 ping", issue3787MentionU1, "", "5 minutes"),
		// u-1 asks u-2 (waiting) with a mention.
		issue3787Post("mine", 5, "u-1", "@U2 please confirm", issue3787MentionU2, "", "6 hours"),
		// u-1 asks a plain question nobody answers.
		issue3787Post("plainq", 6, "u-1", "Who has the key?", "", "", "6 hours"),
		// u-1 asks u-2 and u-2 replies in the thread (not waiting).
		issue3787Post("mine-answered", 7, "u-1", "@U2 ready?", issue3787MentionU2, "", "6 hours"),
		issue3787Post("their-answer", 8, "u-2", "ready", "", "mine-answered", "5 hours"),
		// Not a question, no mention: never tracked.
		issue3787Post("statement", 9, "u-1", "Done for today.", "", "", "6 hours"),
	} {
		if err := s.execTenant(ctx, "host", q); err != nil {
			t.Fatal(err)
		}
	}
	id := chatrecipient.Identity{HostTenantID: "host", HomeTenantID: "host", SubjectID: "u-1", ConversationID: "c-1"}
	return s, NewRecipientStateStore(s), id
}

func issue3787Index(rows []chatrecipient.FollowUp) map[string]chatrecipient.FollowUp {
	out := map[string]chatrecipient.FollowUp{}
	for _, row := range rows {
		out[row.Direction+"/"+row.PostID] = row
	}
	return out
}

func TestIssue3787_ListFollowUps_Integration(t *testing.T) {
	s, r, _ := issue3787Seed(t)
	ctx := context.Background()
	rows, err := r.ListFollowUps(ctx, "host", "host", "u-1", time.Now(), chatrecipient.FollowUpMinAge, 50)
	if err != nil {
		t.Fatal(err)
	}
	got := issue3787Index(rows)
	for _, want := range []string{"owed/ask", "waiting/mine", "waiting/plainq"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing %s in %+v", want, rows)
		}
	}
	for _, not := range []string{"owed/asked-answered", "owed/fresh", "waiting/statement", "waiting/my-answer"} {
		if _, ok := got[not]; ok {
			t.Fatalf("%s must not be listed: %+v", not, rows)
		}
	}
	// A reply in the thread by the person asked is a possible answer, never a
	// silent close: it may answer a different question.
	if w, ok := got["waiting/mine-answered"]; !ok || w.Evidence != chatrecipient.FollowUpEvidenceReplied || w.EvidenceAt.IsZero() {
		t.Fatalf("answered question = %+v ok=%v, want it listed with a possible reply", w, ok)
	}
	if len(rows) != 4 {
		t.Fatalf("rows=%+v", rows)
	}
	if o := got["owed/ask"]; o.AuthorID != "u-2" || o.Evidence != "" || o.Body == "" || o.Sequence != 1 || o.ConversationID != "c-1" {
		t.Fatalf("owed row = %+v", o)
	}
	if w := got["waiting/mine"]; len(w.WaitingOn) != 1 || w.WaitingOn[0] != "u-2" {
		t.Fatalf("waiting row = %+v", w)
	}
	if w := got["waiting/plainq"]; len(w.WaitingOn) != 0 {
		t.Fatalf("plain question waits on nobody in particular: %+v", w)
	}
	// An acknowledgement is only evidence: the owed row stays, labelled.
	for _, q := range []string{
		`INSERT INTO chat_reaction(tenant_id,home_tenant_id,post_id,member_id,emoji) VALUES('host','host','ask','u-1','+1')`,
		// The other person only reacted: still waiting, labelled.
		`INSERT INTO chat_reaction(tenant_id,home_tenant_id,post_id,member_id,emoji) VALUES('host','host','mine','u-2','+1')`,
		// My very next post after the mention is a possible reply, not a closed question.
		issue3787Post("late-ask", 10, "u-2", "@U1 got a minute", issue3787MentionU1, "", "3 hours"),
		issue3787Post("quick", 11, "u-1", "unrelated news", "", "", "2 hours"),
		// A plain question whose next channel post is somebody else's.
		issue3787Post("q2", 12, "u-1", "Anyone free?", "", "", "100 minutes"),
		issue3787Post("other", 13, "u-2", "lunch plans", "", "", "90 minutes"),
	} {
		if err := s.execTenant(ctx, "host", q); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ = r.ListFollowUps(ctx, "host", "host", "u-1", time.Now(), chatrecipient.FollowUpMinAge, 50)
	got = issue3787Index(rows)
	for key, want := range map[string]string{
		"owed/ask":       chatrecipient.FollowUpEvidenceReacted,
		"owed/late-ask":  chatrecipient.FollowUpEvidencePostedLater,
		"waiting/mine":   chatrecipient.FollowUpEvidenceReacted,
		"waiting/q2":     chatrecipient.FollowUpEvidencePostedLater,
		"waiting/plainq": "",
	} {
		if row, ok := got[key]; !ok || row.Evidence != want {
			t.Fatalf("%s = %+v ok=%v, want evidence %q", key, row, ok, want)
		}
	}
	if got["owed/late-ask"].EvidenceAt.IsZero() {
		t.Fatal("evidence has no time")
	}
}

func TestIssue3787_FollowUpChoices_Integration(t *testing.T) {
	_, r, id := issue3787Seed(t)
	ctx := context.Background()
	now := time.Now()
	list := func(at time.Time) map[string]chatrecipient.FollowUp {
		rows, err := r.ListFollowUps(ctx, "host", "host", "u-1", at, chatrecipient.FollowUpMinAge, 50)
		if err != nil {
			t.Fatal(err)
		}
		return issue3787Index(rows)
	}
	// Resolved and dismissed rows stay hidden across reads.
	if err := r.PutFollowUpState(ctx, id, "ask", chatrecipient.FollowUpOwed, chatrecipient.FollowUpResolved, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := r.PutFollowUpState(ctx, id, "plainq", chatrecipient.FollowUpWaiting, chatrecipient.FollowUpDismissed, time.Time{}); err != nil {
		t.Fatal(err)
	}
	got := list(now)
	if _, ok := got["owed/ask"]; ok {
		t.Fatal("resolved row is listed")
	}
	if _, ok := got["waiting/plainq"]; ok {
		t.Fatal("dismissed row is listed")
	}
	if _, ok := got["waiting/mine"]; !ok {
		t.Fatal("untouched row vanished")
	}
	// Snoozed hides until the time passes.
	if err := r.PutFollowUpState(ctx, id, "mine", chatrecipient.FollowUpWaiting, chatrecipient.FollowUpSnoozed, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok := list(now)["waiting/mine"]; ok {
		t.Fatal("snoozed row is listed")
	}
	if _, ok := list(now.Add(3 * time.Hour))["waiting/mine"]; !ok {
		t.Fatal("snoozed row did not come back")
	}
	// Keeping a row open marks it and keeps it listed.
	if err := r.PutFollowUpState(ctx, id, "mine", chatrecipient.FollowUpWaiting, chatrecipient.FollowUpKept, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if w, ok := list(now)["waiting/mine"]; !ok || !w.Kept {
		t.Fatalf("kept row = %+v ok=%v", w, ok)
	}
}

func TestIssue3787_FollowUps_Security(t *testing.T) {
	s, r, id := issue3787Seed(t)
	ctx := context.Background()
	// Another person, another home company, another host company see nothing of u-1's.
	if rows, err := r.ListFollowUps(ctx, "host", "host", "u-2", time.Now(), chatrecipient.FollowUpMinAge, 50); err != nil {
		t.Fatal(err)
	} else {
		for _, row := range rows {
			if row.Direction == chatrecipient.FollowUpOwed && row.PostID != "mine" && row.PostID != "mine-answered" {
				t.Fatalf("u-2 saw %+v", row)
			}
			if row.Direction == chatrecipient.FollowUpWaiting && row.AuthorID != "u-2" {
				t.Fatalf("u-2 saw u-1's waiting row %+v", row)
			}
		}
	}
	for _, c := range []struct{ host, home string }{{"host", "other-home"}, {"other-host", "host"}} {
		if rows, err := r.ListFollowUps(ctx, c.host, c.home, "u-1", time.Now(), chatrecipient.FollowUpMinAge, 50); err != nil || len(rows) != 0 {
			t.Fatalf("%+v saw %+v err=%v", c, rows, err)
		}
	}
	// A row can be set only by a member of the conversation.
	other := chatrecipient.Identity{HostTenantID: "host", HomeTenantID: "host", SubjectID: "stranger", ConversationID: "c-1"}
	if err := r.PutFollowUpState(ctx, other, "ask", chatrecipient.FollowUpOwed, chatrecipient.FollowUpResolved, time.Time{}); err == nil {
		t.Fatal("a non-member stored a choice")
	}
	// A waiting row is only for the caller's own post.
	if err := r.PutFollowUpState(ctx, id, "ask", chatrecipient.FollowUpWaiting, chatrecipient.FollowUpResolved, time.Time{}); err == nil {
		t.Fatal("waiting state on somebody else's post")
	}
	if err := r.PutFollowUpState(ctx, id, "no-such-post", chatrecipient.FollowUpOwed, chatrecipient.FollowUpResolved, time.Time{}); err == nil {
		t.Fatal("state for an unknown post")
	}
	// A reply addressed to others is not evidence the caller may see: the
	// question stays listed and nothing about that reply shows.
	if err := s.execTenant(ctx, "host", `INSERT INTO chat_post_audience(tenant_id,conversation_id,post_id,restricted,author_id,segments,member_ids) VALUES('host','c-1','their-answer',true,'u-2',ARRAY['x'],ARRAY['u-2'])`); err != nil {
		t.Fatal(err)
	}
	rows, _ := r.ListFollowUps(ctx, "host", "host", "u-1", time.Now(), chatrecipient.FollowUpMinAge, 50)
	if w, ok := issue3787Index(rows)["waiting/mine-answered"]; !ok || w.Evidence != "" {
		t.Fatalf("a reply the caller cannot read must not show as evidence or reveal itself: %+v ok=%v", w, ok)
	}
	// A message addressed to others is neither listed nor can it be targeted.
	if err := s.execTenant(ctx, "host", `INSERT INTO chat_post_audience(tenant_id,conversation_id,post_id,restricted,author_id,segments,member_ids) VALUES('host','c-1','ask',true,'u-2',ARRAY['x'],ARRAY['u-2'])`); err != nil {
		t.Fatal(err)
	}
	rows, _ = r.ListFollowUps(ctx, "host", "host", "u-1", time.Now(), chatrecipient.FollowUpMinAge, 50)
	if _, ok := issue3787Index(rows)["owed/ask"]; ok {
		t.Fatal("restricted message listed")
	}
	if err := r.PutFollowUpState(ctx, id, "ask", chatrecipient.FollowUpOwed, chatrecipient.FollowUpResolved, time.Time{}); err == nil {
		t.Fatal("state for a message the caller cannot read")
	}
	// Access loss removes everything.
	if err := s.execTenant(ctx, "host", `UPDATE chat_membership SET state='removed' WHERE tenant_id='host' AND home_tenant_id='host' AND member_id='u-1'`); err != nil {
		t.Fatal(err)
	}
	if rows, err := r.ListFollowUps(ctx, "host", "host", "u-1", time.Now(), chatrecipient.FollowUpMinAge, 50); err != nil || len(rows) != 0 {
		t.Fatalf("removed member: %+v err=%v", rows, err)
	}
}
