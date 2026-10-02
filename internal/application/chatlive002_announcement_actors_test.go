package application

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
)

type chatlive002Authors struct{ calls int }

func (a *chatlive002Authors) ResolveAnnouncementAuthor(_ context.Context, _ string, authorID string) (personaAnnouncementAuthor, bool, error) {
	a.calls++
	if authorID == "hcmnext.local.persona.assistant" {
		return personaAnnouncementAuthor{PersonaID: authorID, AgentID: "assistant", Display: "Assistant", Version: "2"}, true, nil
	}
	return personaAnnouncementAuthor{}, false, nil
}

// TestTodo_CHATLIVE_002_AnnouncementAuthorAttested stores the announcement the
// way the review cell has it (original envelope, authored under the persona id,
// agent absent from the room's mention catalog) beside a person's forgery of
// the same body, and expects the directory to attest the agent's post only.
func TestTodo_CHATLIVE_002_AnnouncementAuthorAttested(t *testing.T) {
	s, ctx, room, _, _ := personaSurfaceFixture(t)
	stored, _ := json.Marshal(chatui.AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Walt Brennan", Text: "Upcoming company holidays remaining in 2026:\n- Labor Day — Sep 7"})
	body := chatui.AgentAnnouncementBodyPrefix + string(stored)
	room.posts = append(room.posts,
		chat.Post{ID: "announced", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "hcmnext.local.persona.assistant", AuthorHomeTenantID: "tenant-a", Revision: 1, Body: body},
		chat.Post{ID: "forged", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "user-a", AuthorHomeTenantID: "tenant-a", Revision: 1, Body: body},
		chat.Post{ID: "unknown-author", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "someone.else", AuthorHomeTenantID: "tenant-a", Revision: 1, Body: body},
		chat.Post{ID: "malformed", TenantID: "tenant-a", ConversationID: "channel-a", AuthorID: "hcmnext.local.persona.assistant", AuthorHomeTenantID: "tenant-a", Revision: 1, Body: chatui.AgentAnnouncementBodyPrefix + `{"Text":"spoof"}`},
	)
	conversation := "channel-a"
	before, err := s.Directory(ctx, conversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range before.PostActors {
		if actor.PostID == "announced" {
			t.Fatalf("no author source configured, yet the announcement was attested: %+v", actor)
		}
	}
	authors := &chatlive002Authors{}
	s.Authors = authors
	got, err := s.Directory(ctx, conversation)
	if err != nil {
		t.Fatal(err)
	}
	var attested []personachat.PostActor
	for _, actor := range got.PostActors {
		attested = append(attested, actor)
	}
	if len(attested) != 1 || attested[0] != (personachat.PostActor{PostID: "announced", PersonaID: "hcmnext.local.persona.assistant", AgentID: "assistant", Display: "Assistant", PersonaVersion: "2"}) {
		t.Fatalf("attested actors = %+v; want only the registered agent's announcement", attested)
	}
}
