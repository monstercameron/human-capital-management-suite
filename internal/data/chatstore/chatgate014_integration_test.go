package chatstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestTodo_CHATGATE_014_Integration: in the chat database a post restricted to
// some members raises no unread count, sidebar badge, mention count or live
// event for a member outside its audience, and raises all of them for the
// members inside it. The counts are read three ways, because each takes its posts
// from a different part of the counting statement: a row kept from before the
// post (the cached delta), a recount that ignores the cache, and a window so
// small that it saturates.
func TestTodo_CHATGATE_014_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	adapter := NewAdapter(s)
	recipients := NewRecipientStateStore(s)
	ctx := context.Background()
	const tenant, room, other = "tenant-a", "night", "plain"
	seedConversationRow(t, s, Conversation{ID: room, TenantID: tenant, Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, w8Members("owner", "ana", "ben", "cy"))
	seedConversationRow(t, s, Conversation{ID: other, TenantID: tenant, Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, w8Members("owner", "ben"))

	counts := func(person string, recount bool) chatrecipient.Counts {
		t.Helper()
		var got map[string]chatrecipient.Counts
		var err error
		if recount {
			got, err = recipients.ChatscaleRebuildCounts(ctx, tenant, tenant, person, []string{room, other})
		} else {
			got, err = recipients.ChatscaleSidebarCounts(ctx, tenant, tenant, person, []string{room, other})
		}
		if err != nil {
			t.Fatalf("counts for %s: %v", person, err)
		}
		return got[room]
	}
	// Every member's row is counted and cached before the posts exist, so what
	// follows is read through the delta over a cached row.
	for _, person := range []string{"ana", "ben", "cy"} {
		if got := counts(person, false); got.Unread != 0 || got.Mentions != 0 {
			t.Fatalf("%s starts with %+v", person, got)
		}
	}

	mention := func(person string) []byte {
		raw, err := json.Marshal([]chat.Reference{{Kind: chat.PersonMention, TenantID: tenant, ID: person, Display: person, ConversationID: room}})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	send := func(ctx context.Context, key, body string, refs []byte) Post {
		t.Helper()
		post, err := s.sendPostRaw(ctx, SendRequest{TenantID: tenant, HomeTenantID: tenant, ConversationID: room, AuthorID: "owner", ClientKey: key, Body: body, References: refs})
		if err != nil {
			t.Fatal(err)
		}
		return post
	}
	audience := chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Night shift"}, Restricted: true, Members: []string{"ana", "owner"}})
	// One restricted post that mentions both ana and ben, and one open post.
	restricted := send(audience, "r1", "Night shift only: the side door", mention("ana"))
	both, err := json.Marshal([]chat.Reference{
		{Kind: chat.PersonMention, TenantID: tenant, ID: "ana", Display: "ana", ConversationID: room},
		{Kind: chat.PersonMention, TenantID: tenant, ID: "ben", Display: "ben", ConversationID: room},
	})
	if err != nil {
		t.Fatal(err)
	}
	send(audience, "r2", "Night shift only: ask @ana and @ben", both)
	send(ctx, "o1", "For everyone: lunch is at noon", nil)
	// A message addressed to a segment and open to all is an ordinary post for the counts.
	send(chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Night shift"}}), "a1", "Night shift, everyone may read: the rota", nil)

	want := map[string]chatrecipient.Counts{
		"ana": {Unread: 4, Mentions: 2},
		"ben": {Unread: 2, Mentions: 0},
		"cy":  {Unread: 2, Mentions: 0},
	}
	for person, expected := range want {
		if got := counts(person, false); got != expected {
			t.Errorf("%s through the cached row: %+v, want %+v", person, got, expected)
		}
		if got := counts(person, true); got != expected {
			t.Errorf("%s recounted: %+v, want %+v", person, got, expected)
		}
		got, err := recipients.chatscaleReadCountsLimit(ctx, tenant, tenant, person, []string{room}, true, 3)
		if err != nil {
			t.Fatal(err)
		}
		saturated := expected
		if saturated.Unread > 3 {
			saturated.Unread = 3
		}
		if got[room].Unread != saturated.Unread {
			t.Errorf("%s with a window of three: %+v, want %d unread", person, got[room], saturated.Unread)
		}
	}
	// The one-conversation read the page makes when a room is opened.
	if got, err := recipients.Counts(ctx, chatrecipient.Identity{HostTenantID: tenant, HomeTenantID: tenant, SubjectID: "ben", ConversationID: room}); err != nil || got.Unread != 2 || got.Mentions != 0 {
		t.Fatalf("ben's count for the room: %+v %v", got, err)
	}
	// The author never counts their own posts, restricted or not.
	if got := counts("owner", true); got.Unread != 0 {
		t.Errorf("the author's own unread: %+v", got)
	}
	// A restricted post cannot be followed by somebody who cannot read it.
	if _, err := recipients.Follow(ctx, chatrecipient.Identity{HostTenantID: tenant, HomeTenantID: tenant, SubjectID: "ben", ConversationID: room}, restricted.ID); !errors.Is(err, chat.ErrNotFound) {
		t.Errorf("ben followed a thread he cannot read: %v", err)
	}
	if _, err := recipients.Follow(ctx, chatrecipient.Identity{HostTenantID: tenant, HomeTenantID: tenant, SubjectID: "ana", ConversationID: room}, restricted.ID); err != nil {
		t.Errorf("ana cannot follow a thread she can read: %v", err)
	}

	// An edit of a restricted post made after the person read the room does not
	// bring it back as an edited message for somebody outside the audience.
	for _, person := range []string{"ana", "ben", "cy"} {
		if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO chat_cursor(tenant_id,conversation_id,home_tenant_id,member_id,last_sequence) SELECT $1,$2,$1,$3,post_sequence FROM chat_conversation WHERE tenant_id=$1 AND id=$2 ON CONFLICT(tenant_id,conversation_id,home_tenant_id,member_id) DO UPDATE SET last_sequence=excluded.last_sequence,revision=chat_cursor.revision+1,updated_at=now()`, tenant, room, person)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, person := range []string{"ana", "ben", "cy"} {
		if got := counts(person, true); got.Unread != 0 || got.Mentions != 0 {
			t.Fatalf("%s after reading: %+v", person, got)
		}
	}
	if err := s.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE chat_post SET body=body||' (edited)',revision=revision+1,updated_at=now()+interval '1 minute' WHERE tenant_id=$1 AND id=$2`, tenant, restricted.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := counts("ben", true); got.Unread != 0 || got.Mentions != 0 {
		t.Errorf("an edit of a restricted post counts for ben: %+v", got)
	}
	if got := counts("ana", true); got.Mentions != 1 {
		t.Errorf("an edit of a restricted post that mentions ana does not count for her: %+v", got)
	}

	// The live events a member is notified from carry the restricted posts to the
	// audience only.
	events := func(person string) string {
		page, err := adapter.ReadConversationEvents(ctx, chat.WatchConversationRequest{TenantID: tenant, ConversationID: room, Principal: chat.Principal{TenantID: tenant, SubjectID: person}}, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(page.Events)
		return string(raw)
	}
	if got := events("ana"); !strings.Contains(got, "the side door") {
		t.Errorf("ana's events miss the restricted post: %s", got)
	}
	if got := events("ben"); strings.Contains(got, "the side door") || strings.Contains(got, "ask @ana") || !strings.Contains(got, "lunch is at noon") {
		t.Errorf("ben's events: %s", got)
	}
}

// TestTodo_CHATGATE_014_Integration_Tags: the tags of addressed messages come from
// the chat database as one reader may know them: the segments of an addressed post
// to everybody, a restricted post with how many it was delivered to to its
// audience only, and nothing of a restricted post to a reader outside it.
func TestTodo_CHATGATE_014_Integration_Tags(t *testing.T) {
	s, _ := chatFixture(t)
	adapter := NewAdapter(s)
	ctx := context.Background()
	const tenant, room = "tenant-a", "tags"
	seedConversationRow(t, s, Conversation{ID: room, TenantID: tenant, Kind: "PRIVATE_CHANNEL", OwnerID: "owner", Lifecycle: "ACTIVE", SettingsRevision: 1}, w8Members("owner", "ana", "ben"))
	send := func(ctx context.Context, key string) string {
		t.Helper()
		post, err := s.sendPostRaw(ctx, SendRequest{TenantID: tenant, HomeTenantID: tenant, ConversationID: room, AuthorID: "owner", ClientKey: key, Body: "message " + key})
		if err != nil {
			t.Fatal(err)
		}
		return post.ID
	}
	restricted := send(chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Night shift"}, Restricted: true, Members: []string{"ana", "owner"}}), "r")
	addressed := send(chat.WithPostAudience(ctx, chat.PostAudience{Segments: []string{"Denver office", "Night shift"}}), "a")
	open := send(ctx, "o")
	ids := []string{restricted, addressed, open}

	read := func(person string) map[string]chat.PostAudienceInfo {
		t.Helper()
		got, err := adapter.PostAudienceInfos(ctx, tenant, person, ids)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	ana := read("ana")
	if got := ana[restricted]; !got.Restricted || got.People != 2 || len(got.Segments) != 1 || got.Segments[0] != "Night shift" {
		t.Errorf("the audience's tag of the restricted post: %+v", got)
	}
	if got := ana[addressed]; got.Restricted || got.People != 0 || len(got.Segments) != 2 {
		t.Errorf("the tag of the addressed post: %+v", got)
	}
	if _, tagged := ana[open]; tagged {
		t.Error("a post for everyone is tagged")
	}
	ben := read("ben")
	if _, seen := ben[restricted]; seen {
		t.Error("a reader outside the audience is told the restricted post exists")
	}
	if len(ben[addressed].Segments) != 2 {
		t.Errorf("an addressed post is not tagged for a reader outside its segments: %+v", ben[addressed])
	}
	if got := read("owner"); !got[restricted].Restricted {
		t.Errorf("the author does not read the tag of their own restricted post: %+v", got[restricted])
	}
	if got, err := adapter.PostAudienceInfos(ctx, tenant, "ana", nil); err != nil || len(got) != 0 {
		t.Errorf("no posts: %v %v", got, err)
	}
	if _, err := adapter.PostAudienceInfos(ctx, tenant, "", ids); err == nil {
		t.Error("a read with no reader was answered")
	}
}
