package chatstore

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

func issue3916Seed(t *testing.T) (*Store, *RecipientStateStore, chatrecipient.Identity) {
	t.Helper()
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversation(t, s, "host")
	for _, q := range []string{
		`UPDATE chat_membership SET home_tenant_id='host' WHERE tenant_id='host' AND conversation_id='c-1' AND member_id='u-1'`,
		`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('root-a','host','c-1','u-1','host',1,'first root')`,
		`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body) VALUES('root-b','host','c-1','u-1','host',2,'second root')`,
		`INSERT INTO chat_post(id,tenant_id,conversation_id,author_id,author_home_tenant_id,sequence,body,parent_id,created_at) VALUES('reply-a','host','c-1','u-1','host',3,'a reply','root-a',now()+interval '1 minute')`,
	} {
		if err := s.execTenant(ctx, "host", q); err != nil {
			t.Fatal(err)
		}
	}
	id := chatrecipient.Identity{HostTenantID: "host", HomeTenantID: "host", SubjectID: "u-1", ConversationID: "c-1"}
	return s, NewRecipientStateStore(s), id
}

func TestIssue3916_ListFollowed_Integration(t *testing.T) {
	s, r, id := issue3916Seed(t)
	ctx := context.Background()
	rows, err := r.ListFollowed(ctx, "host", "host", "u-1", 50)
	if err != nil || len(rows) != 0 {
		t.Fatalf("nothing followed yet: %+v err=%v", rows, err)
	}
	if _, err := r.PutFollow(ctx, id, chatrecipient.Follow{RootPostID: "root-b", Followed: true}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PutFollow(ctx, id, chatrecipient.Follow{RootPostID: "root-a", Followed: true}, 1); err != nil {
		t.Fatal(err)
	}
	rows, err = r.ListFollowed(ctx, "host", "host", "u-1", 50)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	// root-a has a newer reply, so it leads; root-b has none and uses its own time.
	if rows[0].RootPostID != "root-a" || rows[0].ReplyCount != 1 || rows[0].RootBody != "first root" || rows[0].RootSequence != 1 || rows[0].Revision != 2 {
		t.Fatalf("first row = %+v", rows[0])
	}
	if rows[1].RootPostID != "root-b" || rows[1].ReplyCount != 0 || rows[1].LastActivity.IsZero() {
		t.Fatalf("second row = %+v", rows[1])
	}
	// The thread header and the list read the same row: unfollowing removes it.
	if _, err := r.PutFollow(ctx, id, chatrecipient.Follow{RootPostID: "root-b", Followed: false}, 2); err != nil {
		t.Fatal(err)
	}
	if rows, _ = r.ListFollowed(ctx, "host", "host", "u-1", 50); len(rows) != 1 || rows[0].RootPostID != "root-a" {
		t.Fatalf("unfollowed thread still listed: %+v", rows)
	}
	// A deleted root stays listed, without its text.
	if err := s.execTenant(ctx, "host", `UPDATE chat_post SET tombstoned=true WHERE tenant_id='host' AND id='root-a'`); err != nil {
		t.Fatal(err)
	}
	if rows, _ = r.ListFollowed(ctx, "host", "host", "u-1", 50); len(rows) != 1 || !rows[0].RootRemoved || rows[0].RootBody != "" {
		t.Fatalf("deleted root: %+v", rows)
	}
}

func TestIssue3916_ListFollowed_Security(t *testing.T) {
	s, r, id := issue3916Seed(t)
	ctx := context.Background()
	if _, err := r.PutFollow(ctx, id, chatrecipient.Follow{RootPostID: "root-a", Followed: true}, 1); err != nil {
		t.Fatal(err)
	}
	// Another person, another company with the same subject id: neither sees it.
	if rows, err := r.ListFollowed(ctx, "host", "host", "someone-else", 50); err != nil || len(rows) != 0 {
		t.Fatalf("another person: %+v err=%v", rows, err)
	}
	if rows, err := r.ListFollowed(ctx, "host", "other-home", "u-1", 50); err != nil || len(rows) != 0 {
		t.Fatalf("another home company: %+v err=%v", rows, err)
	}
	if rows, err := r.ListFollowed(ctx, "other-host", "host", "u-1", 50); err != nil || len(rows) != 0 {
		t.Fatalf("another host company: %+v err=%v", rows, err)
	}
	// A root addressed to other people drops out of the list and out of the counts.
	if err := s.execTenant(ctx, "host", `INSERT INTO chat_post_audience(tenant_id,conversation_id,post_id,restricted,author_id,segments,member_ids) VALUES('host','c-1','root-a',true,'u-9',ARRAY['x'],ARRAY['u-9'])`); err != nil {
		t.Fatal(err)
	}
	if rows, err := r.ListFollowed(ctx, "host", "host", "u-1", 50); err != nil || len(rows) != 0 {
		t.Fatalf("restricted root listed: %+v err=%v", rows, err)
	}
	if err := s.execTenant(ctx, "host", `DELETE FROM chat_post_audience WHERE tenant_id='host' AND post_id='root-a'`); err != nil {
		t.Fatal(err)
	}
	// A reply addressed to others is not counted.
	if err := s.execTenant(ctx, "host", `INSERT INTO chat_post_audience(tenant_id,conversation_id,post_id,restricted,author_id,segments,member_ids) VALUES('host','c-1','reply-a',true,'u-9',ARRAY['x'],ARRAY['u-9'])`); err != nil {
		t.Fatal(err)
	}
	if rows, _ := r.ListFollowed(ctx, "host", "host", "u-1", 50); len(rows) != 1 || rows[0].ReplyCount != 0 {
		t.Fatalf("restricted reply counted: %+v", rows)
	}
	// Access loss removes the thread.
	if err := s.execTenant(ctx, "host", `UPDATE chat_membership SET state='removed' WHERE tenant_id='host' AND home_tenant_id='host' AND member_id='u-1'`); err != nil {
		t.Fatal(err)
	}
	if rows, err := r.ListFollowed(ctx, "host", "host", "u-1", 50); err != nil || len(rows) != 0 {
		t.Fatalf("removed member still lists: %+v err=%v", rows, err)
	}
}
