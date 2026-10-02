package chatui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// chatbug013Icons returns the stored icon a test agent owns and the shared
// fallback drawn for an agent with no stored icon.
func chatbug013Icons(t *testing.T) (stored, fallback string, value agenticon.Value) {
	t.Helper()
	value = agenticon.Generate(agenticon.Input{Name: "Policy Helper", Description: "Answer policy questions"})
	stored = renderNode(t, agenticon.Node(value))
	fallback = renderNode(t, agenticon.Node(agenticon.Value{}))
	if !value.Valid() || stored == fallback {
		t.Fatal("the fixture icon must be valid and differ from the fallback")
	}
	return stored, fallback, value
}

func TestTodo_CHATBUG_013(t *testing.T) {
	stored, fallback, value := chatbug013Icons(t)

	// The lookup finds the icon by id, then by name, from every place the model
	// carries one, and answers the zero value only when none is stored.
	m := chat4Fixture("en-US", "answered", false)
	m.ResolvedPersonaMentions[0].Icon = value
	if got := storedAgentIcon(m, []string{"policy-helper"}, ""); got != value {
		t.Fatalf("by id: %+v", got)
	}
	if got := storedAgentIcon(m, nil, " policy helper "); got != value {
		t.Fatalf("by name: %+v", got)
	}
	if got := storedAgentIcon(m, []string{"other"}, "Someone Else"); got.Valid() {
		t.Fatalf("an unknown agent was given an icon: %+v", got)
	}
	m.Members = []Member{{ID: "member-agent", Name: "Member Agent", Agent: true, Icon: value}}
	if got := storedAgentIcon(m, []string{"member-agent"}, ""); got != value {
		t.Fatalf("member: %+v", got)
	}

	// A private answer in a channel names its agent but carries no icon: the
	// card draws the stored icon, never the fallback, hidden from assistive
	// technology and at a person's avatar size.
	answer := renderPersonaPrivateAnswer(m, localUI{}, m.EphemeralMessages[0], PersonaProgressProjection{AgentName: "Policy Helper"})
	markup := renderNode(t, answer)
	if !strings.Contains(markup, stored) || strings.Contains(markup, fallback) {
		t.Fatalf("private answer does not draw the stored icon: %s", markup)
	}
	if !strings.Contains(markup, `<span aria-hidden="true" class="avatar small agent-reply-avatar">`) {
		t.Fatalf("answer icon is not a hidden avatar: %s", markup)
	}

	// A receipt that names the persona but carries no icon falls back to the
	// directory, not to the diamond.
	m.PersonaPostActors = map[string]PersonaPostActor{"answer": {Display: "Policy Helper", Actor: PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}}}
	markup = renderNode(t, renderPersonaPrivateAnswer(m, localUI{}, m.EphemeralMessages[0], PersonaProgressProjection{AgentName: "Policy Helper"}))
	if !strings.Contains(markup, stored) || strings.Contains(markup, fallback) {
		t.Fatalf("receipt without icon draws the fallback: %s", markup)
	}

	// The working row, the failure row and the author of a message do the same.
	if markup := renderNode(t, renderAgentReplyIdentity(m, "Policy Helper")); !strings.Contains(markup, stored) || strings.Contains(markup, fallback) {
		t.Fatalf("identity row: %s", markup)
	}
	author := Message{ID: "post", AuthorID: "policy-helper", Author: "Policy Helper", PersonaActor: &PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}}
	if markup := renderNode(t, integrate1MessageAvatar(m, author, "avatar")); !strings.Contains(markup, stored) || strings.Contains(markup, fallback) {
		t.Fatalf("message author: %s", markup)
	}

	// Only an agent that truly has no stored icon gets the fallback.
	none := chat4Fixture("en-US", "answered", false)
	// CHATBUG-033 changed what that is: its own icon derived from its id, never
	// the one neutral glyph every agent would share.
	if markup := renderNode(t, renderAgentReplyIdentity(none, "Policy Helper")); !strings.Contains(markup, `class="agent-icon"`) || strings.Contains(markup, fallback) {
		t.Fatalf("an agent without a stored icon lost its own fallback or was given the shared one: %s", markup)
	}
}

func TestTodo_CHATBUG_013_Browser(t *testing.T) {
	stored, fallback, value := chatbug013Icons(t)
	for _, state := range []string{"answered", "sent", "failed"} {
		m := chat4Fixture("en-US", state, false)
		m.ResolvedPersonaMentions[0].Icon = value
		m.ShowDetails = true
		m.Messages = append(m.Messages, Message{ID: "agent-post", AuthorID: "policy-helper", Author: "Policy Helper", Body: "Carry over up to 40 hours.", TimeLabel: "9:32", SentAt: time.Now(),
			PersonaActor: &PersonaActor{PersonaID: "policy-helper", AgentID: "policy-helper", Trusted: true}})
		markup := render(t, m)
		if strings.Contains(markup, fallback) {
			t.Fatalf("%s: the shared fallback icon is drawn for an agent with a stored icon", state)
		}
		// Details, the answer or progress row and the message author all draw it.
		want := 2
		if state == "answered" {
			want = 3
		}
		if got := strings.Count(markup, stored); got < want {
			t.Fatalf("%s: stored icon drawn %d times, want at least %d", state, got, want)
		}
	}
}

// chatbug016SideZ and chatbug016ComposerZ read the phone-width stacking of the
// side panel and of the message box out of the stylesheet itself.
func chatbug016Z(t *testing.T, sheet, selector string) int {
	t.Helper()
	pattern := regexp.MustCompile(regexp.QuoteMeta(selector) + `\{[^}]*z-index:(\d+)`)
	matches := pattern.FindAllStringSubmatch(sheet, -1)
	if len(matches) == 0 {
		t.Fatalf("no z-index rule for %s", selector)
	}
	last, _ := strconv.Atoi(matches[len(matches)-1][1])
	return last
}

func TestTodo_CHATBUG_016(t *testing.T) {
	// The panel is the container-query rule for narrow workspaces; the composer
	// is sticky at phone width. The panel must be stacked above it, or the
	// message box and Send button sit on top of the panel's lower part.
	start := strings.Index(Stylesheet, `@container chat (max-width:760px){`)
	if start < 0 {
		t.Fatal("narrow-width rule for the side panel is missing")
	}
	narrow := Stylesheet[start : start+700]
	side := chatbug016Z(t, narrow, ".chat-side")
	composer := chatbug016Z(t, AgentUXChat3Styles, ".chat-composer")
	if side <= composer {
		t.Fatalf("side panel z-index %d is not above the composer's %d", side, composer)
	}
	// Still below the drawers and the thread pane, which cover everything.
	if side >= 1199 || chatbug016Z(t, AgentUXChat3Styles, ".chat-side.thread-pane") <= composer {
		t.Fatalf("panel stacking out of order: side %d", side)
	}
	if !strings.Contains(narrow, ".chat-side{position:absolute;inset:0 0 0 auto;width:min(380px,100%)") {
		t.Fatal("the panel no longer fills the workspace")
	}
}

func TestTodo_CHATBUG_016_Browser(t *testing.T) {
	// Every panel that opens beside the conversation has its own close button,
	// and the composer is part of the conversation, not of the panel.
	m := chat4Fixture("en-US", "sent", false)
	m.ShowDetails = true
	markup := render(t, m)
	for _, want := range []string{`data-action="close-details"`, `class="chat-side chat-details"`, `chat-composer`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("details open: missing %q", want)
		}
	}
	if strings.Index(markup, "chat-composer") > strings.Index(markup, `class="chat-side chat-details"`) {
		t.Fatal("the composer must precede the panel in the tree so that stacking, not order, decides which is on top")
	}
	closed := chat4Fixture("en-US", "sent", false)
	if got := render(t, closed); strings.Contains(got, `data-action="close-details"`) || !strings.Contains(got, "chat-composer") {
		t.Fatal("with the panel closed the composer must remain and the close button must not")
	}
	thread := chat4Fixture("en-US", "sent", false)
	thread.ThreadParentID, thread.ShowThread = "question", true
	if got := render(t, thread); !strings.Contains(got, "thread-pane") || !strings.Contains(got, `data-action="close-thread"`) {
		t.Fatal("the thread panel has no close button")
	}
}
