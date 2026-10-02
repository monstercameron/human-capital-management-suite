package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// chatbug089LongEnvelope is the stored announcement the review server's search
// answered for "holiday": the whole envelope, with its sources, longer than a
// result row's body limit.
const chatbug089LongEnvelope = "<hcm_agent_announcement>\n" + `{"AgentName":"Agent","OwnerName":"Walt Brennan","Text":"Upcoming company holidays remaining in 2026:\n- Labor Day — Sep 7\n- Thanksgiving Day — Nov 26\n- Day after Thanksgiving — Nov 27\n- Christmas Day — Dec 25\n(2026 holiday guide)","Scheduled":false,"PostedAt":"0001-01-01T00:00:00Z","Sources":[{"Title":"2026 holiday guide","Href":"/workspace/app/docs/policies/2026-holiday-guide?version=1.0.0","Version":"1.0.0"}]}`

// TestTodo_CHATBUG_089_SearchRowOfAHeldAnnouncement is the page's own case: the
// result is a message the open conversation already holds, written by an agent
// the page trusts, and the reader's selection for that message carries the body
// as it is stored. The row was given the sentence and drew the envelope.
func TestTodo_CHATBUG_089_SearchRowOfAHeldAnnouncement(t *testing.T) {
	actor := PersonaActor{PersonaID: "assistant", AgentID: "assistant", Trusted: true}
	if !actor.valid() {
		t.Fatal("the fixture actor must be one the page trusts")
	}
	m := Model{Locale: "en-US"}
	m.PersonaPostActors = map[string]PersonaPostActor{"p1": {Actor: actor, Display: "Assistant"}}
	m.ReaderSelections = map[string]ReaderSelection{"p1": {Revision: 1, Rendering: chatrender.Rendering{Message: "p1", Revision: 1, Tone: chatrender.AsWritten, Text: chatbug089LongEnvelope}}}
	node, msg := chatsave002Body(m, Message{ID: "p1", AuthorID: "assistant", Author: "Assistant", Body: chatSearchRowBody(chatbug089LongEnvelope)})
	if msg.PersonaActor == nil || !msg.PersonaActor.valid() {
		t.Fatal("the row must be drawn as the trusted agent's message for this case to be the page's")
	}
	shown := renderNode(t, node)
	for _, raw := range []string{"hcm_agent_announcement", "AgentName", "OwnerName", "PostedAt"} {
		if strings.Contains(shown, raw) {
			t.Errorf("the row prints %q: %s", raw, shown)
		}
	}
	if !strings.Contains(shown, "Upcoming company holidays remaining in 2026") {
		t.Errorf("the row lost the sentence: %s", shown)
	}
}

// TestTodo_CHATBUG_089_SearchLongAnnouncement is the case the page showed raw:
// a whole envelope longer than the row's body limit. The row shortens long text
// around the match, so the envelope must be its sentence before that happens.
func TestTodo_CHATBUG_089_SearchLongAnnouncement(t *testing.T) {
	if len([]rune(chatbug089LongEnvelope)) <= chatsearchBodyLimit {
		t.Fatalf("the envelope must be longer than the row limit of %d to prove the case", chatsearchBodyLimit)
	}
	body := chatSearchRowBody(chatbug089LongEnvelope)
	for _, raw := range []string{"hcm_agent_announcement", "AgentName", "OwnerName", `"Sources"`, "PostedAt"} {
		if strings.Contains(body, raw) {
			t.Errorf("the row body keeps %q: %s", raw, body)
		}
	}
	if !strings.Contains(body, "Upcoming company holidays remaining in 2026") {
		t.Errorf("the row body lost the sentence: %s", body)
	}
	for _, kind := range []chatsearch.Kind{chatsearch.Message, chatsearch.Thread} {
		shown := chatbug021SearchRow(t, kind, chatbug089LongEnvelope)
		chatbug021Clean(t, "long announcement in a "+string(kind)+" row", shown)
		for _, raw := range []string{"hcm_agent_announcement", "AgentName", "PostedAt"} {
			if strings.Contains(shown, raw) {
				t.Errorf("the %s row prints %q: %s", kind, raw, shown)
			}
		}
		if flat := strings.Join(strings.Fields(shown), " "); !strings.Contains(flat, "Upcoming company") {
			t.Errorf("the %s row lost the sentence: %s", kind, shown)
		}
	}
}
