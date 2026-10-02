package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_AGENTUX_030(t *testing.T) {
	conversation := Conversation{ID: "673214ec-4402", Name: "Policy Helper", AgentID: "policy-helper", Agent: true, Kind: DirectMessage}
	model := Model{State: StateReady, Locale: "en-US", CurrentUser: "ir-001-walt-brennan", SelectedID: conversation.ID,
		Conversations: []Conversation{conversation},
		Sections:      []SidebarSection{{ID: "direct", Chats: []Conversation{conversation}}},
		Callbacks:     Callbacks{SelectConversation: func(string) {}},
	}
	markup := render(t, model)
	for _, want := range []string{"Policy Helper", "agent-badge", `placeholder="Ask Policy Helper"`, "agent-dm-avatar", ">Agent<"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("agent direct presentation missing %q: %s", want, markup)
		}
	}
	model.Search, model.SearchChannels = "policy", []Conversation{conversation}
	searchMarkup := render(t, model)
	if !strings.Contains(searchMarkup, "Policy Helper") || !strings.Contains(searchMarkup, "agent-badge") {
		t.Fatalf("agent direct search result=%s", searchMarkup)
	}
}

func TestTodo_AGENTUX_031(t *testing.T) {
	model := Model{Locale: "en-US", PersonaPostActors: map[string]PersonaPostActor{
		"durable-copy": {Display: "Policy Helper", Actor: PersonaActor{PersonaID: "policy", AgentID: "policy-helper", Trusted: true}},
	}}
	durable := personaTrustedMessage(model, Message{ID: "durable-copy", AuthorID: "ir-001-walt-brennan", Author: "Walt Brennan", Body: "Answer"})
	if durable.Author != "Policy Helper" || durable.PersonaActor == nil || !durable.PersonaActor.valid() {
		t.Fatalf("durable answer did not use its receipt-backed agent attribution: %+v", durable)
	}
	markup, err := ui.RenderToString(html.Div(html.Props{}, message(model, handlers{}, durable, false)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Policy Helper", "agent-badge", "agent-dm-avatar"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("durable agent attribution missing %q: %s", want, markup)
		}
	}
	if strings.Contains(markup, "message own") {
		t.Fatalf("durable agent answer retained the invoker's visual ownership: %s", markup)
	}
	legacy := parseAgentReplyEnvelope("Answer\n\nOpen the source conversation: /chat/share/signed-source")
	if legacy.Body != "Answer" || legacy.Backlink != "/chat/share/signed-source" {
		t.Fatalf("legacy durable copy exposed its raw locator: %+v", legacy)
	}
}

func TestTodo_AGENTUX_032(t *testing.T) {
	for _, tc := range []struct {
		name, locale, body string
		links              int
	}{
		{name: "one source", locale: "en-US", body: "Answer\n\nSources\n- [Paid time off policy (version 4)](/workspace/app/docs?document=pto-v4)", links: 1},
		{name: "two sources", locale: "en-US", body: "Answer\n\nSources\n- [Paid time off policy (version 4)](/workspace/app/docs?document=pto-v4)\n- [Benefits guide (version 2)](/workspace/app/docs?document=benefits-v2)", links: 2},
		{name: "revoked reader", locale: "en-US", body: "Answer\n\nSources\n- Paid time off policy (version 4)", links: 0},
		{name: "rtl", locale: "ar", body: "إجابة\n\nSources\n- [Paid time off policy (version 4)](/workspace/app/docs?document=pto-v4)", links: 1},
		// An address on another host is never a link, whatever its path says.
		{name: "another host", locale: "en-US", body: "Answer\n\nSources\n- [Paid time off policy (version 4)](https://tenant.example/workspace/app/docs?document=pto-v4)", links: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := Model{Locale: tc.locale}
			message := EphemeralMessage{ID: "answer", ThreadID: "question", Body: tc.body, OnlyVisibleToYou: true, CreatedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
			markup, err := ui.RenderToString(html.Div(html.Props{}, renderPersonaPrivateAnswer(model, localUI{}, message, PersonaProgressProjection{AgentName: "Policy Helper"})))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, "agent-reply-sources") || strings.Count(markup, "agent-reply-source-link") != tc.links || !strings.Contains(markup, "Paid time off policy") {
				t.Fatalf("sources projection=%s", markup)
			}
			if tc.locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
				t.Fatalf("RTL source list lost direction: %s", markup)
			}
		})
	}
}
