package chatstore

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func issue3809Seed(t *testing.T) *Store {
	t.Helper()
	s, _ := chatFixture(t)
	ctx := context.Background()
	two := []Membership{{MemberID: "u-1", Role: "member", State: "active"}, {MemberID: "u-2", Role: "member", State: "active"}}
	seedConversationRow(t, s, Conversation{ID: "pub-a", TenantID: "host", Kind: "PUBLIC_CHANNEL", Name: "incidents", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1}, two)
	seedConversationRow(t, s, Conversation{ID: "pub-b", TenantID: "host", Kind: "PUBLIC_CHANNEL", Name: "random", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1}, two)
	seedConversationRow(t, s, Conversation{ID: "dm-1", TenantID: "host", Kind: "DIRECT", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1}, two)
	seedConversationRow(t, s, Conversation{ID: "priv-1", TenantID: "host", Kind: "PRIVATE_CHANNEL", Name: "secret", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1}, two)
	seedConversationRow(t, s, Conversation{ID: "pub-old", TenantID: "host", Kind: "PUBLIC_CHANNEL", Name: "old", OwnerID: "u-2", Lifecycle: "ACTIVE", SettingsRevision: 1}, two)
	seedConversationRow(t, s, Conversation{ID: "pub-x", TenantID: "other", Kind: "PUBLIC_CHANNEL", Name: "elsewhere", OwnerID: "u-9", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{{MemberID: "u-9", Role: "member", State: "active"}})
	if err := s.execTenant(ctx, "host", `UPDATE chat_conversation SET lifecycle='ARCHIVED' WHERE id='pub-old'`); err != nil {
		t.Fatal(err)
	}
	return s
}

// issue3809Post inserts a post and the outbox row its commit writes.
func issue3809Post(t *testing.T, s *Store, tenant, conv, id string, seq int, body, age string) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,created_at) VALUES('` + id + `','` + tenant + `','` + conv + `','u-2','` + tenant + `',` + itoa3787(seq) + `,'` + body + `',now()-interval '` + age + `')`,
		`INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES('` + tenant + `','` + id + `','post.created','{"ConversationID":"` + conv + `","TargetID":"` + id + `"}')`,
	} {
		if err := s.execTenant(ctx, tenant, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func issue3809IDs(p WebhookPostPage) map[string]bool {
	out := map[string]bool{}
	for _, post := range p.Posts {
		out[post.PostID] = true
	}
	return out
}

func TestIssue3809_AllowedPublicChannelsOnly_Integration_Security(t *testing.T) {
	s := issue3809Seed(t)
	ctx := context.Background()
	// Only a public, active channel of this company may be allowed.
	for _, id := range []string{"dm-1", "priv-1", "pub-old"} {
		if _, err := s.AllowWebhookChannel(ctx, "host", id, "admin-1"); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Fatalf("%s allowed: %v", id, err)
		}
	}
	if _, err := s.AllowWebhookChannel(ctx, "host", "missing", "admin-1"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("unknown channel: %v", err)
	}
	// Another company's channel is not found from this one.
	if _, err := s.AllowWebhookChannel(ctx, "host", "pub-x", "admin-1"); !errors.Is(err, chat.ErrNotFound) {
		t.Fatalf("foreign channel: %v", err)
	}
	if _, err := s.AllowWebhookChannel(ctx, "host", "pub-a", " "); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no actor: %v", err)
	}
	if list, _ := s.ListWebhookChannels(ctx, "host"); len(list) != 0 {
		t.Fatalf("a refused allowance left a row: %+v", list)
	}
	added, err := s.AllowWebhookChannel(ctx, "host", "pub-a", "admin-1")
	if err != nil || !added {
		t.Fatalf("allow: %v %v", added, err)
	}
	if again, err := s.AllowWebhookChannel(ctx, "host", "pub-a", "admin-2"); err != nil || again {
		t.Fatalf("allowing twice: %v %v", again, err)
	}
	list, err := s.ListWebhookChannels(ctx, "host")
	if err != nil || len(list) != 1 || list[0].ConversationID != "pub-a" || list[0].Name != "incidents" || list[0].AllowedBy != "admin-1" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if other, err := s.ListWebhookChannels(ctx, "other"); err != nil || len(other) != 0 {
		t.Fatalf("another company read the allowance: %+v %v", other, err)
	}
	var audited int
	if err := s.RunTenantTx(ctx, "host", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM chat_audit_event WHERE tenant_id='host' AND action='chat.webhook_channel.allowed' AND target_id='pub-a' AND actor_id='admin-1'`).Scan(&audited)
	}); err != nil || audited != 1 {
		t.Fatalf("audit entries: %d %v", audited, err)
	}
	removed, err := s.RemoveWebhookChannel(ctx, "other", "pub-a", "admin-9")
	if err != nil || removed {
		t.Fatalf("another company removed the allowance: %v %v", removed, err)
	}
	if list, _ := s.ListWebhookChannels(ctx, "host"); len(list) != 1 {
		t.Fatalf("allowance lost: %+v", list)
	}
	if removed, err := s.RemoveWebhookChannel(ctx, "host", "pub-a", "admin-1"); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if removed, _ := s.RemoveWebhookChannel(ctx, "host", "pub-a", "admin-1"); removed {
		t.Fatal("removed twice")
	}
}

func TestIssue3809_PostsOfAllowedChannels_Integration_Security(t *testing.T) {
	s := issue3809Seed(t)
	ctx := context.Background()
	// History from before the allowance, in the channel that is about to be allowed.
	issue3809Post(t, s, "host", "pub-a", "before", 1, "older than the allowance", "2 hours")
	if _, err := s.AllowWebhookChannel(ctx, "host", "pub-a", "admin-1"); err != nil {
		t.Fatal(err)
	}
	issue3809Post(t, s, "host", "pub-a", "shown", 2, "disk full on db-3", "0 seconds")
	issue3809Post(t, s, "host", "pub-b", "not-allowed", 1, "channel not allowed", "0 seconds")
	issue3809Post(t, s, "host", "dm-1", "dm", 1, "private words", "0 seconds")
	issue3809Post(t, s, "host", "priv-1", "private-channel", 1, "private channel words", "0 seconds")
	issue3809Post(t, s, "host", "pub-a", "gone", 3, "deleted words", "0 seconds")
	issue3809Post(t, s, "host", "pub-a", "held", 4, "for a few people", "0 seconds")
	for _, q := range []string{
		`UPDATE chat_post SET tombstoned=true WHERE id='gone'`,
		`INSERT INTO chat_post_audience(tenant_id,conversation_id,post_id,author_id,segments,restricted,member_ids) VALUES('host','pub-a','held','u-2','{s}',true,'{u-2}')`,
	} {
		if err := s.execTenant(ctx, "host", q); err != nil {
			t.Fatal(err)
		}
	}
	// Another company's post in a channel with the same name is not read here.
	if _, err := s.AllowWebhookChannel(ctx, "other", "pub-x", "admin-9"); err != nil {
		t.Fatal(err)
	}
	issue3809Post(t, s, "other", "pub-x", "foreign", 1, "someone else", "0 seconds")

	page, err := s.ListWebhookPosts(ctx, "host", 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	ids := issue3809IDs(page)
	if len(page.Posts) != 1 || !ids["shown"] {
		t.Fatalf("posts read: %+v", page.Posts)
	}
	got := page.Posts[0]
	if got.ConversationID != "pub-a" || got.ConversationName != "incidents" || got.Body != "disk full on db-3" || got.Sequence != 2 || got.Author != "u-2" || got.OutboxID == 0 || got.PostedAt.IsZero() {
		t.Fatalf("post: %+v", got)
	}
	if page.Through < got.OutboxID {
		t.Fatalf("cursor %d is behind the post %d", page.Through, got.OutboxID)
	}
	// Skipped events still move the cursor: the next read has nothing to scan.
	next, err := s.ListWebhookPosts(ctx, "host", page.Through, 50)
	if err != nil || len(next.Posts) != 0 || next.Through != page.Through {
		t.Fatalf("second read: %+v %v", next, err)
	}
	// A new post after the cursor is the only thing returned, in order.
	issue3809Post(t, s, "host", "pub-a", "later", 5, "recovered", "0 seconds")
	next, err = s.ListWebhookPosts(ctx, "host", page.Through, 50)
	if err != nil || len(next.Posts) != 1 || next.Posts[0].PostID != "later" || next.Through <= page.Through {
		t.Fatalf("resume: %+v %v", next, err)
	}
	// Edited text goes out as it reads now; a post deleted after the commit does not go out.
	if err := s.execTenant(ctx, "host", `UPDATE chat_post SET body='disk replaced',revision=2 WHERE id='shown'`); err != nil {
		t.Fatal(err)
	}
	if err := s.execTenant(ctx, "host", `UPDATE chat_post SET tombstoned=true WHERE id='later'`); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListWebhookPosts(ctx, "host", 0, 50)
	if len(all.Posts) != 1 || all.Posts[0].Body != "disk replaced" || all.Posts[0].Revision != 2 {
		t.Fatalf("stale content: %+v", all.Posts)
	}
	// An archived channel stops publishing; removing the allowance does too.
	if err := s.execTenant(ctx, "host", `UPDATE chat_conversation SET lifecycle='ARCHIVED' WHERE id='pub-a'`); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.ListWebhookPosts(ctx, "host", 0, 50); len(all.Posts) != 0 {
		t.Fatalf("archived channel published: %+v", all.Posts)
	}
	foreign, err := s.ListWebhookPosts(ctx, "other", 0, 50)
	if err != nil || len(foreign.Posts) != 1 || foreign.Posts[0].PostID != "foreign" {
		t.Fatalf("other company: %+v %v", foreign, err)
	}
	if _, err := s.ListWebhookPosts(ctx, "", 0, 10); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("no company: %v", err)
	}
}
