package chatstore

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3733Post(conv, id string, seq int, author, body, refs, parent, age string) string {
	if refs == "" {
		refs = "[]"
	}
	return `INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,references_json,parent_id,created_at) VALUES('` + id + `','host','` + conv + `','` + author + `','host',` + itoa3787(seq) + `,'` + body + `','` + refs + `','` + parent + `',now()-interval '` + age + `')`
}

func issue3733Index(rows []chatrecipient.Notification) map[string]chatrecipient.Notification {
	out := map[string]chatrecipient.Notification{}
	for _, row := range rows {
		out[row.PostID] = row
	}
	return out
}

// issue3733Seed builds the rooms one person (u-1) is in, and ones they are not.
func issue3733Seed(t *testing.T) (*Store, *RecipientStateStore) {
	t.Helper()
	s, _ := chatFixture(t)
	ctx := context.Background()
	two := []Membership{{MemberID: "u-1", Role: "member", State: "active"}, {MemberID: "u-2", Role: "member", State: "active"}}
	seedConversationRow(t, s, Conversation{ID: "c-1", TenantID: "host", Kind: "PUBLIC_CHANNEL", Name: "ops", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1}, two)
	seedConversationRow(t, s, Conversation{ID: "dm-1", TenantID: "host", Kind: "DIRECT", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1}, two)
	seedConversationRow(t, s, Conversation{ID: "c-muted", TenantID: "host", Kind: "PUBLIC_CHANNEL", Name: "quiet", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1}, two)
	seedConversationRow(t, s, Conversation{ID: "c-mo", TenantID: "host", Kind: "PRIVATE_CHANNEL", Name: "focus", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1}, two)
	seedConversationRow(t, s, Conversation{ID: "c-away", TenantID: "host", Kind: "PRIVATE_CHANNEL", Name: "secret", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1},
		[]Membership{{MemberID: "u-2", Role: "member", State: "active"}, {MemberID: "u-3", Role: "member", State: "active"}})
	seedConversationRow(t, s, Conversation{ID: "c-left", TenantID: "host", Kind: "PRIVATE_CHANNEL", Name: "gone", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1},
		[]Membership{{MemberID: "u-2", Role: "member", State: "active"}, {MemberID: "u-1", Role: "member", State: "left"}})
	seedConversationRow(t, s, Conversation{ID: "c-old", TenantID: "host", Kind: "PUBLIC_CHANNEL", Name: "old", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1}, two)
	for _, q := range []string{
		`UPDATE chat_membership SET home_tenant_id='host' WHERE tenant_id='host'`,
		issue3733Post("c-1", "mention", 1, "u-2", "@U1 can you look", issue3787MentionU1, "", "3 hours"),
		issue3733Post("c-1", "plain", 2, "u-2", "lunch anyone", "", "", "3 hours"),
		issue3733Post("c-1", "mention-of-other", 3, "u-2", "@U2 yours", issue3787MentionU2, "", "3 hours"),
		issue3733Post("c-1", "own", 4, "u-1", "@U1 note to self", issue3787MentionU1, "", "3 hours"),
		issue3733Post("c-1", "gone", 5, "u-2", "@U1 oops", issue3787MentionU1, "", "3 hours"),
		`UPDATE chat_post SET tombstoned=true WHERE id='gone'`,
		// A thread u-1 follows, and one they do not.
		issue3733Post("c-1", "root-f", 6, "u-1", "my thread", "", "", "5 hours"),
		issue3733Post("c-1", "reply-f", 7, "u-2", "reply to followed", "", "root-f", "2 hours"),
		issue3733Post("c-1", "root-n", 8, "u-2", "other thread", "", "", "5 hours"),
		issue3733Post("c-1", "reply-n", 9, "u-2", "reply to other", "", "root-n", "2 hours"),
		`INSERT INTO chat_thread_follow(tenant_id,home_tenant_id,member_id,conversation_id,root_post_id,followed) VALUES('host','host','u-1','c-1','root-f',true)`,
		`INSERT INTO chat_thread_follow(tenant_id,home_tenant_id,member_id,conversation_id,root_post_id,followed) VALUES('host','host','u-1','c-1','root-n',false)`,
		// A post held back from u-1 by its audience.
		issue3733Post("c-1", "held", 10, "u-2", "@U1 for others", issue3787MentionU1, "", "1 hour"),
		`INSERT INTO chat_post_audience(tenant_id,conversation_id,post_id,author_id,segments,restricted,member_ids) VALUES('host','c-1','held','u-2','{s}',true,'{u-2,u-3}')`,
		// A direct message.
		issue3733Post("dm-1", "dm", 1, "u-2", "got a minute", "", "", "1 hour"),
		// A muted conversation, a mentions-only one.
		issue3733Post("c-muted", "muted-mention", 1, "u-2", "@U1 hi", issue3787MentionU1, "", "1 hour"),
		`INSERT INTO chat_preference(tenant_id,home_tenant_id,member_id,conversation_id,marker,value) VALUES('host','host','u-1','c-muted','notification','{"Muted":true,"MentionsOnly":false}')`,
		issue3733Post("c-mo", "mo-mention", 1, "u-2", "@U1 hi", issue3787MentionU1, "", "1 hour"),
		issue3733Post("c-mo", "mo-plain", 2, "u-2", "chatter", "", "", "1 hour"),
		`INSERT INTO chat_preference(tenant_id,home_tenant_id,member_id,conversation_id,marker,value) VALUES('host','host','u-1','c-mo','notification','{"Muted":false,"MentionsOnly":true}')`,
		// Rooms u-1 is not in, has left, or that are archived.
		issue3733Post("c-away", "away", 1, "u-2", "@U1 hello?", issue3787MentionU1, "", "1 hour"),
		issue3733Post("c-left", "left", 1, "u-2", "@U1 hello?", issue3787MentionU1, "", "1 hour"),
		issue3733Post("c-old", "archived", 1, "u-2", "@U1 hello?", issue3787MentionU1, "", "1 hour"),
		`UPDATE chat_conversation SET lifecycle='ARCHIVED' WHERE id='c-old'`,
		// Older than the window.
		issue3733Post("c-1", "ancient", 11, "u-2", "@U1 long ago", issue3787MentionU1, "", "60 days"),
	} {
		if err := s.execTenant(ctx, "host", q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return s, NewRecipientStateStore(s)
}

func issue3733List(t *testing.T, r *RecipientStateStore, host, subject string) map[string]chatrecipient.Notification {
	t.Helper()
	rows, err := r.ListNotifications(context.Background(), host, "host", subject, time.Now().Add(-chatrecipient.NotificationsWindow), 50)
	if err != nil {
		t.Fatal(err)
	}
	return issue3733Index(rows)
}

func TestIssue3733_ListNotifications_Integration(t *testing.T) {
	_, r := issue3733Seed(t)
	got := issue3733List(t, r, "host", "u-1")
	want := map[string]string{"mention": "mention", "reply-f": "reply", "dm": "direct", "mo-mention": "mention"}
	for id, kind := range want {
		n, ok := got[id]
		if !ok || n.Kind != kind || n.Read || n.AuthorID != "u-2" || n.Body == "" || n.ConversationID == "" || n.Sequence == 0 {
			t.Fatalf("%s = %+v ok=%v, want an unread %s from u-2", id, n, ok, kind)
		}
	}
	for id := range got {
		if _, ok := want[id]; !ok {
			t.Fatalf("%s must not be a notification: %+v", id, got)
		}
	}
	if n := got["reply-f"]; n.ThreadRootID != "root-f" || n.ThreadRootSequence != 6 || n.ConversationName != "ops" {
		t.Fatalf("reply = %+v, want it to name its thread and channel", n)
	}
	if got["dm"].ConversationKind != "DIRECT" {
		t.Fatalf("dm = %+v", got["dm"])
	}
}

func TestIssue3733_NotificationsAreOnlyForThePerson_Security(t *testing.T) {
	_, r := issue3733Seed(t)
	// u-3 is in a room u-1 is not in, and is not mentioned anywhere else.
	if got := issue3733List(t, r, "host", "u-3"); len(got) != 0 {
		t.Fatalf("u-3 sees %+v", got)
	}
	// Another company's view of the same people sees nothing.
	if got := issue3733List(t, r, "other-tenant", "u-1"); len(got) != 0 {
		t.Fatalf("another tenant sees %+v", got)
	}
}

func TestIssue3733_MarkRead_Integration(t *testing.T) {
	s, r := issue3733Seed(t)
	ctx := context.Background()
	since := time.Now().Add(-chatrecipient.NotificationsWindow)
	stored := func() int {
		var n int
		if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM chat_notification_read`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// A message the caller has no notification for is skipped and not revealed:
	// "away" is in a room they are not in, "plain" is no notification.
	n, err := r.MarkNotificationsRead(ctx, "host", "host", "u-1", since, []string{"away", "plain", "no-such-post"}, false)
	if err != nil || n != 0 || stored() != 0 {
		t.Fatalf("mark of non-notifications = %d, %v, stored %d; want 0", n, err, stored())
	}
	n, err = r.MarkNotificationsRead(ctx, "host", "host", "u-1", since, []string{"mention", "dm"}, false)
	if err != nil || n != 2 {
		t.Fatalf("mark two = %d, %v", n, err)
	}
	got := issue3733List(t, r, "host", "u-1")
	if !got["mention"].Read || !got["dm"].Read || got["reply-f"].Read {
		t.Fatalf("read state = %+v", got)
	}
	if n, err = r.MarkNotificationsRead(ctx, "host", "host", "u-1", since, []string{"mention"}, false); err != nil || n != 0 {
		t.Fatalf("repeat = %d, %v", n, err)
	}
	// Another person's read state is theirs.
	if other := issue3733List(t, r, "host", "u-2"); other["mention"].Read {
		t.Fatalf("u-1's read state leaked: %+v", other)
	}
	n, err = r.MarkNotificationsRead(ctx, "host", "host", "u-1", since, nil, true)
	if err != nil || n != 2 {
		t.Fatalf("mark all = %d, %v; want the other two", n, err)
	}
	for id, row := range issue3733List(t, r, "host", "u-1") {
		if !row.Read {
			t.Fatalf("%s still unread", id)
		}
	}
}

// Reading a conversation reads its notifications too, in every session: the
// read position is the one the conversation already keeps.
func TestIssue3733_ReadPositionReadsNotifications_Integration(t *testing.T) {
	s, r := issue3733Seed(t)
	ctx := context.Background()
	if err := s.execTenant(ctx, "host", `INSERT INTO chat_cursor(tenant_id,home_tenant_id,member_id,conversation_id,last_sequence) VALUES('host','host','u-1','c-1',7)`); err != nil {
		t.Fatal(err)
	}
	got := issue3733List(t, r, "host", "u-1")
	if !got["mention"].Read || !got["reply-f"].Read {
		t.Fatalf("a conversation read to its seventh message still has unread notifications: %+v", got)
	}
	if got["dm"].Read {
		t.Fatalf("another conversation's notification was read: %+v", got["dm"])
	}
}
