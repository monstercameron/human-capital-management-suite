package chatui

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

var chatbug088Author = regexp.MustCompile(`<strong[^>]*class="message-author[^"]*"[^>]*>([^<]*)</strong>`)

// chatbug088Room is #general with the Assistant's stored announcement in it and
// a second agent, Policy Helper, in the room's agent directory: the case where a
// row drawn before the directory arrives used to take another agent's name and
// a made-up picture.
func chatbug088Room(locale string) (Model, agenticon.Value, agenticon.Value) {
	assistant := agenticon.Value{Glyph: "calendar", Shape: "circle", Foreground: "--hcm-color-info", Background: "--hcm-color-info-surface"}
	policy := agenticon.Value{Glyph: "book", Shape: "hexagon", Foreground: "--hcm-color-success", Background: "--hcm-color-success-surface"}
	stored, _ := json.Marshal(AgentAnnouncementMessage{AgentName: "Agent", OwnerName: "Walt Brennan", Text: "Upcoming company holidays remaining in 2026:\n- Labor Day — Sep 7"})
	message := Message{ID: "announced", AuthorID: "hcmnext.local.persona.assistant", Author: "Hcmnext Local Persona Assistant", Body: AgentAnnouncementBodyPrefix + string(stored), SentAt: time.Date(2026, 10, 1, 20, 30, 26, 0, time.UTC)}
	model := Model{Locale: locale, State: StateReady, SelectedID: "general", CurrentTenantID: "tenant", CurrentUser: "alice",
		Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}},
		Messages:      []Message{message},
		PersonaLookup: PersonaLookupIdle,
	}
	return model, assistant, policy
}

func chatbug088Directory(model Model, assistant, policy agenticon.Value) Model {
	ref := ChatReference{Kind: "AGENT_MENTION", ID: "policy-helper", TenantID: "tenant", Display: "Policy Helper", ConversationID: "general"}
	model.ResolvedPersonaMentions = []ResolvedPersonaMention{{Reference: ref, Handle: "policy-helper", Icon: policy, Purpose: "Answers policy questions"}}
	model.PersonaPostActors = map[string]PersonaPostActor{"announced": {Display: "Assistant", Actor: PersonaActor{PersonaID: "hcmnext.local.persona.assistant", AgentID: "assistant", PersonaVersion: "2", Trusted: true, Icon: assistant, IconRevision: 3}}}
	model.PersonaLookup, model.PersonaLookupConversationID = PersonaLookupReady, "general"
	return model
}

// A message from an agent identity names the agent from the one name and icon
// source on every load order. Until the room's agent directory arrives the row
// shows a neutral mark and an empty icon slot: not "Agent" as a name, not the
// name or picture of another agent in the room, and not a picture made up from
// the author's identifier. When the directory arrives the same row is redrawn
// with the agent's own name and stored icon.
func TestTodo_CHATBUG_088(t *testing.T) {
	model, assistant, policy := chatbug088Room("en-US")
	row := func(m Model) string { return renderNode(t, message(m, handlers{}, m.Messages[0], false)) }
	authorOf := func(markup string) string {
		match := chatbug088Author.FindStringSubmatch(markup)
		if match == nil {
			t.Fatalf("the row has no author line: %s", markup)
		}
		return match[1]
	}

	// Before the directory: neutral mark, empty slot.
	before := row(model)
	if got := authorOf(before); got != "…" {
		t.Fatalf("before the directory the author line is %q, want the neutral mark", got)
	}
	for _, bad := range []string{"Assistant", "Policy Helper", "Hcmnext Local Persona Assistant", `<svg class="agent-icon"`} {
		if strings.Contains(before, bad) {
			t.Fatalf("before the directory the row shows %q: %s", bad, before)
		}
	}
	if !strings.Contains(before, "agent-icon-pending") || !strings.Contains(before, "agent-badge") {
		t.Fatalf("before the directory the row has no empty icon slot or no Agent badge: %s", before)
	}

	// Another agent arriving first (the member read, or a different room's
	// agent) does not lend its name or picture.
	other := model
	other.ResolvedPersonaMentions = chatbug088Directory(model, assistant, policy).ResolvedPersonaMentions
	if got := authorOf(row(other)); got == "Policy Helper" || strings.Contains(row(other), renderNode(t, agenticon.Node(policy))) {
		t.Fatalf("another agent's name or icon was used: author %q", got)
	}

	// After the directory: the agent's own name and stored icon, and none of the
	// other agent's.
	loaded := chatbug088Directory(model, assistant, policy)
	after := row(loaded)
	if got := authorOf(after); got != "Assistant" {
		t.Fatalf("after the directory the author line is %q, want Assistant", got)
	}
	if !strings.Contains(after, renderNode(t, agenticon.Node(assistant))) || strings.Contains(after, renderNode(t, agenticon.Node(policy))) || strings.Contains(after, "agent-icon-pending") {
		t.Fatalf("after the directory the row does not wear the Assistant's own stored icon: %s", after)
	}

	// A directory read that failed ends the wait: the generic label, still no
	// picture that belongs to anyone else.
	failed := model
	failed.PersonaLookup, failed.PersonaLookupConversationID = PersonaLookupFailed, "general"
	gone := row(failed)
	if got := authorOf(gone); got != "Agent" || !strings.Contains(gone, "agent-icon-pending") || strings.Contains(gone, `<svg class="agent-icon"`) {
		t.Fatalf("after a failed directory read: author %q, row %s", got, gone)
	}

	// The same model rendered through the whole page is redrawn when the
	// directory is set: first pass neutral, second pass named.
	if page := render(t, model); strings.Contains(page, ">Assistant<") || strings.Contains(page, ">Policy Helper<") {
		t.Fatal("the page named the agent before its directory arrived")
	}
	if page := render(t, loaded); !strings.Contains(page, ">Assistant<") {
		t.Fatal("the page did not name the agent when its directory arrived")
	}
}

// The same holds at phone and desktop widths in every language: the row never
// shows the word "Agent", or another agent, as the author before the directory
// arrives, and shows the agent's own name and icon after.
func TestTodo_CHATBUG_088_Browser(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		model, assistant, policy := chatbug088Room(locale)
		generic := agentReplyFallback(locale, "chat.agent.name", "Agent")
		before := renderAgentUXChat3Node(t, message(model, handlers{}, model.Messages[0], false), width)
		match := chatbug088Author.FindStringSubmatch(before)
		if match == nil || match[1] == generic || match[1] == "Assistant" || match[1] == "Policy Helper" {
			t.Fatalf("before the directory the author is %v", match)
		}
		if strings.Contains(before, `<svg class="agent-icon"`) || !strings.Contains(before, "agent-icon-pending") {
			t.Fatalf("before the directory the row draws an icon: %s", before)
		}
		loaded := chatbug088Directory(model, assistant, policy)
		after := renderAgentUXChat3Node(t, message(loaded, handlers{}, loaded.Messages[0], false), width)
		match = chatbug088Author.FindStringSubmatch(after)
		if match == nil || match[1] != "Assistant" {
			t.Fatalf("after the directory the author is %v", match)
		}
		if !strings.Contains(after, renderNode(t, agenticon.Node(assistant))) || strings.Contains(after, renderNode(t, agenticon.Node(policy))) {
			t.Fatalf("after the directory the icon is not the Assistant's own")
		}
	})
}
