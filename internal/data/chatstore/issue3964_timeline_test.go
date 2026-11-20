package chatstore

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// TestIssue3964_TimelineRowsCarryTimeEditAndAudience_Integration pins what the
// event timeline of Conversation details reads: a search for one conversation and
// a range of days, oldest first with no words, answers each message with the time
// it was posted, whether it was edited since, and whether it was addressed to
// part of the room; a removed message, another room and a non-member never show.
func TestIssue3964_TimelineRowsCarryTimeEditAndAudience_Integration(t *testing.T) {
	s, _ := chatFixture(t)
	ctx := context.Background()
	seedConversationRow(t, s, Conversation{ID: "ops", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: "ops", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"},
		{MemberID: "bob", HomeTenantID: "tenant", Role: "member", State: "active"},
		{MemberID: "dave", HomeTenantID: "tenant", Role: "member", State: "active"},
	})
	seedConversationRow(t, s, Conversation{ID: "hr", TenantID: "tenant", Kind: "PRIVATE_CHANNEL", Name: "hr", OwnerID: "alice", Lifecycle: "ACTIVE", SettingsRevision: 1}, []Membership{
		{MemberID: "alice", HomeTenantID: "tenant", Role: "manager", State: "active"},
	})
	send := func(room, author, key string) Post {
		post, err := s.sendPostRaw(ctx, SendRequest{TenantID: "tenant", HomeTenantID: "tenant", ConversationID: room, AuthorID: author, ClientKey: key, Body: "report " + key})
		if err != nil {
			t.Fatal(err)
		}
		return post
	}
	early := send("ops", "bob", "early")
	edited := send("ops", "alice", "edited")
	narrow := send("ops", "alice", "narrow")
	gone := send("ops", "bob", "gone")
	late := send("ops", "bob", "late")
	send("hr", "alice", "elsewhere")
	day := func(offset int) time.Time { return time.Date(2025, 11, 10+offset, 10, 0, 0, 0, time.UTC) }
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		for i, id := range []string{early.ID, edited.ID, narrow.ID, gone.ID, late.ID} {
			if _, err := tx.Exec(ctx, `UPDATE chat_post SET created_at=$2 WHERE tenant_id='tenant' AND id=$1`, id, day(i)); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE chat_post SET revision=2 WHERE tenant_id='tenant' AND id=$1`, edited.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE chat_post SET tombstoned=true WHERE tenant_id='tenant' AND id=$1`, gone.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO chat_post_audience(tenant_id,conversation_id,post_id,author_id,segments,restricted,member_ids) VALUES('tenant','ops',$1,'alice','{leads}',true,'{alice,bob}')`, narrow.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	registry := chatsearch.NewRegistry()
	if err := s.RegisterChatSearch(registry, func(context.Context, chatsearch.Actor, chatsearch.Row) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	read := func(tenant, person, query string) chatsearch.Response {
		t.Helper()
		got, err := registry.Search(ctx, chatsearch.Request{Actor: chatsearch.Actor{TenantID: tenant, HomeTenantID: tenant, PersonID: person}, Query: query, Limit: 50, At: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	describe := func(got chatsearch.Response) []string {
		var out []string
		for _, g := range got.Groups {
			for _, row := range g.Rows {
				if row.Kind != chatsearch.Message {
					continue
				}
				if row.Target.ConversationID != "ops" {
					t.Fatalf("a message of %q leaked into the ops timeline: %+v", row.Target.ConversationID, row)
				}
				out = append(out, fmt.Sprint(row.Text, "|", row.AuthorID, "|", row.At.UTC().Format("01-02"), "|edited=", row.Edited, "|narrow=", row.Restricted))
			}
		}
		return out
	}
	got := describe(read("tenant", "bob", "in:ops kind:message sort:oldest after:2025-11-09 before:2025-11-15"))
	want := []string{
		"report early|bob|11-10|edited=false|narrow=false",
		"report edited|alice|11-11|edited=true|narrow=false",
		"report narrow|alice|11-12|edited=false|narrow=true",
		"report late|bob|11-14|edited=false|narrow=false",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("timeline rows =\n%v\nwant oldest first, edited and addressed-to-part marked, the removed message and other rooms left out:\n%v", got, want)
	}
	// A day range keeps its ends honest: days are exclusive on both sides.
	if inner := describe(read("tenant", "bob", "in:ops kind:message sort:oldest after:2025-11-10 before:2025-11-13")); len(inner) != 2 {
		t.Fatalf("range rows = %v, want the two between the end days", inner)
	}
	// A member the message was not addressed to never sees it, nor learns that it
	// exists: the timeline cannot carry narrower-audience content to them.
	daveRows := describe(read("tenant", "dave", "in:ops kind:message sort:oldest after:2025-11-09 before:2025-11-15"))
	if len(daveRows) != 3 {
		t.Fatalf("a member outside the message's audience read %v, want the three open messages", daveRows)
	}
	for _, row := range daveRows {
		if strings.Contains(row, "report narrow") {
			t.Fatalf("a message addressed to part of the room reached a member outside it: %v", daveRows)
		}
	}
	// A person outside the room learns nothing, and neither does another tenant.
	if out := read("tenant", "carol", "in:ops kind:message sort:oldest after:2025-11-09 before:2025-11-15"); len(out.Groups) != 0 || out.NextCursor != "" {
		t.Fatalf("a non-member was shown a timeline: %+v", out)
	}
	if foreign, err := registry.Search(ctx, chatsearch.Request{Actor: chatsearch.Actor{TenantID: "other", HomeTenantID: "other", PersonID: "bob"}, Query: "in:ops kind:message sort:oldest after:2025-11-09", Limit: 50, At: time.Now().UTC()}); err == nil && len(foreign.Groups) != 0 {
		t.Fatalf("another tenant was shown a timeline: %+v", foreign)
	}
}
