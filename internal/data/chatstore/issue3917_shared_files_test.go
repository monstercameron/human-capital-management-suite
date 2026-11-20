package chatstore

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestIssue3917_SharedFilesListIsScopedPagedAndPrivate_Integration pins what the
// Shared files list of Conversation details relies on: a search for kind:file in
// one conversation, with no words, lists the attachments of that conversation
// only (replies in threads included), newest first, page by page, and never a
// file of a removed message, of another conversation, or for a person who is not
// a member.
func TestIssue3917_SharedFilesListIsScopedPagedAndPrivate_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "design", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: "design", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"},
		{MemberID: "bob", HomeTenantID: "tenant", Role: "member", State: "active"},
	})
	seedConversationRow(t, s, Conversation{ID: "finance", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: "finance", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"},
	})
	refs := func(id, name string) json.RawMessage {
		raw, _ := json.Marshal([]chat.Reference{{Kind: chat.MediaAttachment, ID: id, Display: name}})
		return raw
	}
	send := func(room, author, key, parent, mediaID, name string) Post {
		post, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: room, AuthorID: author, ClientKey: key, ParentID: parent, Body: "see " + key, References: refs(mediaID, name)})
		if err != nil {
			t.Fatal(err)
		}
		return post
	}
	first := send("design", "bob", "one", "", "m1", "brand-guide.pdf")
	send("design", "alice", "two", "", "m2", "logo.png")
	send("design", "bob", "reply", first.ID, "m3", "reply-notes.txt")
	gone := send("design", "bob", "gone", "", "m4", "withdrawn.pdf")
	send("finance", "alice", "other", "", "m5", "salaries.xlsx")
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=true WHERE tenant_id='tenant' AND id=$1`, gone.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	registry := chatsearch.NewRegistry()
	if err := s.RegisterChatSearch(registry, func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	list := func(person, cursor string, limit int) chatsearch.Response {
		t.Helper()
		got, err := registry.Search(ctx, chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: person}, Query: "in:design kind:file", Cursor: cursor, Limit: limit, At: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Unavailable) != 0 {
			t.Fatalf("a source was unavailable: %v", got.Unavailable)
		}
		return got
	}
	names := func(got chatsearch.Response) []string {
		var out []string
		for _, g := range got.Groups {
			for _, row := range g.Rows {
				if row.Target.ConversationID != "design" {
					t.Fatalf("a file of %q leaked into the design list: %+v", row.Target.ConversationID, row)
				}
				out = append(out, fmt.Sprint(row.Text, "|", row.AuthorID, "|", row.Target.ItemID, "|", row.Target.ThreadID != ""))
			}
		}
		return out
	}
	all := names(list("bob", "", 50))
	want := []string{"reply-notes.txt|bob|m3|true", "logo.png|alice|m2|false", "brand-guide.pdf|bob|m1|false"}
	if fmt.Sprint(all) != fmt.Sprint(want) {
		t.Fatalf("shared files = %v, want newest first, threads included, removed and other rooms left out: %v", all, want)
	}
	// Paging: one file a page, every file once, the last page ends the list.
	var paged []string
	cursor := ""
	for page := 0; page < 5; page++ {
		got := list("alice", cursor, 1)
		paged = append(paged, names(got)...)
		if cursor = got.NextCursor; cursor == "" {
			break
		}
	}
	if fmt.Sprint(paged) != fmt.Sprint(want) {
		t.Fatalf("paged shared files = %v, want %v", paged, want)
	}
	// A file name narrows the list on the server.
	narrow, err := registry.Search(ctx, chatsearch.Request{Actor: chatsearch.Actor{TenantID: "tenant", HomeTenantID: "tenant", PersonID: "bob"}, Query: "in:design kind:file brand", Limit: 50, At: time.Now().UTC()})
	if err != nil || fmt.Sprint(names(narrow)) != "[brand-guide.pdf|bob|m1|false]" {
		t.Fatalf("name filter = %v, %v", names(narrow), err)
	}
	// A person who is not in the room learns nothing: no rows, no cursor.
	outsider := list("carol", "", 50)
	if len(outsider.Groups) != 0 || outsider.NextCursor != "" {
		t.Fatalf("a non-member was shown shared files: %+v", outsider)
	}
	// Another tenant's person with the same id is not a member either.
	foreign, err := registry.Search(ctx, chatsearch.Request{Actor: chatsearch.Actor{TenantID: "other-tenant", HomeTenantID: "other-tenant", PersonID: "bob"}, Query: "in:design kind:file", Limit: 50, At: time.Now().UTC()})
	if err == nil && len(foreign.Groups) != 0 {
		t.Fatalf("another tenant was shown shared files: %+v", foreign)
	}
}
