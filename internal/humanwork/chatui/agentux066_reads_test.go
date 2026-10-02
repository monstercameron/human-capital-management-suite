package chatui

import (
	"strings"
	"testing"
)

func agentUX066Model(locale string) Model {
	m := chat4Fixture(locale, "sent", false)
	m.ShowDetails = true
	m.AmbientReads = []AmbientRead{{Agent: "task-catcher"}, {Agent: "reminder"}}
	m.Callbacks.SetAmbientOptOut = func(bool) {}
	return m
}

// Every member is told who reads messages here: the header and the details say
// "Task Catcher and Reminder read messages here", the details carry the
// member's own switch "Don't act on my messages", and an agent whose budget is
// spent is shown as paused with the reason.
func TestTodo_AGENTUX_066_Browser(t *testing.T) {
	chat4Matrix(t, func(t *testing.T, locale string, width int) {
		m := agentUX066Model(locale)
		line := ambientReadsLine(m)
		if line == "" || !strings.Contains(line, "Task Catcher") || !strings.Contains(line, "Reminder") {
			t.Fatalf("the sentence is %q", line)
		}
		header := renderAgentUXChat3Node(t, timeline(m, handlers{}), width)
		chat4Require(t, header, `class="topic-reads"`, line)
		page := render(t, m)
		chat4Require(t, page, `class="ambient-reads-line"`, strings.ReplaceAll(laneText(m, agentux066Copy, keyReadsOptOut), "'", "&#39;"), `role="switch"`, `aria-checked="false"`, `data-action="ambient-optout"`)
		if locale == "en-US" && !strings.Contains(line, "Task Catcher and Reminder read messages here") {
			t.Fatalf("the English sentence is %q", line)
		}

		// The member's own choice is shown as on, and a spent budget is shown.
		m.AmbientOptOut = true
		m.AmbientReads[0].Paused = true
		page = render(t, m)
		chat4Require(t, page, `aria-checked="true"`, `class="ambient-reads-paused"`, laneTextf(m, agentux066Copy, keyReadsPaused, map[string]string{"name": "Task Catcher"}))
		if locale == "en-US" && !strings.Contains(page, "Task Catcher: paused, daily limit reached") {
			t.Fatal("the paused state is not said in words")
		}
	})
}

// With nothing granted, nothing is said: no header text, no section. In a
// direct conversation between two people nothing is said either, whatever the
// model holds. Without a callback the switch is disabled rather than a no-op.
func TestTodo_AGENTUX_066(t *testing.T) {
	m := agentUX066Model("en-US")
	m.AmbientReads = nil
	if page := render(t, m); strings.Contains(page, "topic-reads") || strings.Contains(page, "ambient-reads") {
		t.Fatalf("nothing is granted but the page says something: %s", page)
	}
	direct := agentUX066Model("en-US")
	direct.Conversations[0] = Conversation{ID: "general", Name: "Priya Shah", Kind: DirectMessage, Joined: true}
	if page := render(t, direct); strings.Contains(page, "topic-reads") || strings.Contains(page, "ambient-reads") {
		t.Fatalf("a direct conversation between two people says an agent reads it: %s", page)
	}
	m = agentUX066Model("en-US")
	m.Callbacks.SetAmbientOptOut = nil
	page := render(t, m)
	at := strings.Index(page, "ambient-reads-optout")
	if at < 0 || !strings.Contains(page[at:min(len(page), at+400)], "disabled") {
		t.Fatalf("the switch is live with no callback: %s", page)
	}
	// The switch sends the opposite of what is shown.
	var sent []bool
	m = agentUX066Model("en-US")
	m.Callbacks.SetAmbientOptOut = func(v bool) { sent = append(sent, v) }
	if !ambientReadsClick(m, "ambient-optout") || ambientReadsClick(m, "other") {
		t.Fatal("the click was not recognised")
	}
	m.AmbientOptOut = true
	ambientReadsClick(m, "ambient-optout")
	if len(sent) != 2 || !sent[0] || sent[1] {
		t.Fatalf("the switch sent %v", sent)
	}
	// One agent is "reads", and the names are never another agent's.
	m = agentUX066Model("en-US")
	m.AmbientReads = m.AmbientReads[:1]
	if got := ambientReadsLine(m); got != "Task Catcher reads messages here" {
		t.Fatalf("one agent: %q", got)
	}
}
