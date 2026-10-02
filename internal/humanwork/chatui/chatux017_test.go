package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	xhtml "golang.org/x/net/html"
)

// chatux017General is #general with Policy Helper as the server describes it:
// an owner given by identifier, a version, and skills in technical words.
func chatux017General(locale string) Model {
	m := chat4Fixture(locale, "answered", false)
	m.PersonaActivityReady, m.ShowDetails, m.CurrentUserName = true, true, "Alice"
	m.Members = []Member{{ID: "alice", Name: "Alice"}, {ID: "ir-001-walt-brennan", Name: "Walt Brennan"}}
	m.Callbacks.OpenPerson = func(string) {}
	persona := &m.ResolvedPersonaMentions[0]
	persona.Purpose = "Answer Ironridge policy questions with citations to current policy documents."
	persona.Owner, persona.Version = "ir-001-walt-brennan", "6"
	persona.Skills = []PersonaMentionSkill{{Name: "Search deployed tenant policy documents the requesting user and this installation may read. Returns exact document-version citations.", Tier: "T0"}, {Name: "Prepare a bounded plain-text persona reply for the current private chat. Delivery requires a fresh private-chat authorization and a sealed output admission.", Tier: "T0"}}
	persona.DataClasses = []string{"POLICY_DOCUMENT"}
	persona.CannotDo = []string{"Act beyond your current access", "Use skills outside this published version", "Change governed records", "Write to external systems"}
	persona.ReplyPlacement = PersonaReplyPrivateAudience
	return m
}

// The agent's details say five plain things, and AGENTP-019's three (version,
// skills by tier, the access it acts with), and show no identifier or technical
// skill text.
func TestTodo_CHATUX_017(t *testing.T) {
	m := chatux017General("en-US")
	markup := chatPolishMarkup(t, chatux017AgentSummary(m, m.ResolvedPersonaMentions[0]), 1440, "light")
	text := chatbug030Text(chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return chatPolishHasClass(n, "agent-summary") })[0])
	for _, want := range []string{
		"Answer Ironridge policy questions with citations to current policy documents.",
		"Owner:", "Walt Brennan",
		"What it can read", "Policy documents you can open",
		"What it will never do", "Read anything you cannot open yourself", "Do anything it was not set up to do", "Change any record",
		"Its answers are posted under your question when everyone here can open the sources; otherwise only you see them.",
		// AGENTP-019: the version, the access it acts with and its skills, by tier in words.
		"Version: 6", "Acts with your current access.", "Search deployed tenant policy documents the requesting user and this installation may read.", "Read only",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the summary does not say %q: %q", want, text)
		}
	}
	for _, forbidden := range []string{"ir-001-walt-brennan", "Returns exact document-version citations", "sealed output admission", "Use skills outside this published version", "POLICY_DOCUMENT", "T0", "Not provided"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("the summary still shows %q: %q", forbidden, text)
		}
	}
	// What it will never do is three short lines.
	never := chatPolishNodes(t, markup, func(n *xhtml.Node) bool { return n.Data == "li" && chatPolishAncestor(n, "agent-summary-never") })
	if len(never) != 3 {
		t.Fatalf("%d lines under what it will never do, want 3", len(never))
	}
	// The owner is a link to the person, by name.
	owners := chatPolishNodes(t, markup, func(n *xhtml.Node) bool {
		return n.Data == "button" && chatPolishAttr(n, "data-action") == "open-person"
	})
	if len(owners) != 1 || chatPolishAttr(owners[0], "data-id") != "ir-001-walt-brennan" || chatbug030Text(owners[0]) != "Walt Brennan" {
		t.Fatal("the person who looks after the agent is not a link to that person")
	}
	// An owner nobody can name is left out, never shown as an identifier; a team written in words is shown.
	m.Members = m.Members[:1]
	if unnamed := chatPolishMarkup(t, chatux017AgentSummary(m, m.ResolvedPersonaMentions[0]), 1440, "light"); strings.Contains(unnamed, "ir-001") || strings.Contains(unnamed, "Owner:") {
		t.Fatal("an owner with no known name is shown")
	}
	team := m.ResolvedPersonaMentions[0]
	team.Owner = "People Operations"
	if written := chatPolishMarkup(t, chatux017AgentSummary(m, team), 1440, "light"); !strings.Contains(written, "Owner: People Operations") {
		t.Fatal("a team written in words is not shown")
	}
	// The setup page is linked as "Details", for those who may open it.
	if strings.Contains(markup, "agent-summary-details") {
		t.Fatal("somebody who cannot open Agent setup is given its link")
	}
	m.IsTenantAdmin = true
	admin := chatPolishMarkup(t, chatux017AgentSummary(m, m.ResolvedPersonaMentions[0]), 1440, "light")
	links := chatPolishNodes(t, admin, func(n *xhtml.Node) bool { return n.Data == "a" && chatPolishHasClass(n, "agent-summary-details") })
	if len(links) != 1 || chatbug030Text(links[0]) != "Details" || !strings.HasPrefix(chatPolishAttr(links[0], "href"), "/workspace/app/admin/personas") {
		t.Fatal("an administrator has no Details link to the agent's page")
	}
	// The panel's 12px text; Ask stays on the header row when the row is open.
	for _, want := range []string{".agent-summary{display:grid;gap:6px;padding:6px 0 2px;font-size:.75rem", ".member-row.persona-member-row{align-items:flex-start}"} {
		if !strings.Contains(Stylesheet, want) {
			t.Fatalf("summary styles are missing %q", want)
		}
	}
	for _, locale := range []string{"de-DE", "ar"} {
		localized := chatux017General(locale)
		page := chatPolishMarkup(t, chatux017AgentSummary(localized, localized.ResolvedPersonaMentions[0]), 390, "light")
		if strings.Contains(page, "What it can read") || strings.Contains(page, "Owner:") || strings.Contains(page, "⟦") {
			t.Fatalf("%s: the summary is not translated", locale)
		}
	}
}

// Conversation details shows that summary in the agent's row, and the agent's
// name and icon on an answer card open the same summary as a small card.
func TestTodo_CHATUX_017_Browser(t *testing.T) {
	m := chatux017General("en-US")
	page := chatPolishMarkup(t, Build(m), 1440, "light")
	rows := chatPolishNodes(t, page, func(n *xhtml.Node) bool { return n.Data == "li" && chatPolishHasClass(n, "persona-member-row") })
	if len(rows) != 1 {
		t.Fatalf("%d agent rows in Conversation details", len(rows))
	}
	row := chatbug030Text(rows[0])
	if !strings.Contains(row, "Policy documents you can open") || strings.Contains(row, "ir-001-walt-brennan") || strings.Contains(row, "sealed output admission") || !strings.Contains(row, "Version: 6") {
		t.Fatalf("the agent's row in details reads like a system record: %q", row)
	}
	if asks := chatPolishNodesIn(rows[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "persona-member-ask") }); len(asks) != 1 || asks[0].Parent != rows[0] {
		t.Fatal("Ask is not on the agent's own row")
	}

	card := chatPolishMarkup(t, html.Div(html.Props{}, personaReplyRowsForPost(m, localUI{}, "question", time.Now())...), 1440, "light")
	open := chatPolishNodes(t, card, func(n *xhtml.Node) bool { return n.Data == "button" && chatPolishHasClass(n, "agent-summary-open") })
	if len(open) != 1 || chatPolishAttr(open[0], "data-action") != "agent-profile-open" || chatPolishAttr(open[0], "data-id") != "policy-helper" || chatPolishAttr(open[0], "aria-haspopup") != "dialog" {
		t.Fatal("the agent's name and icon on the card do not open its summary")
	}
	if avatar := chatPolishNodesIn(open[0], func(n *xhtml.Node) bool { return chatPolishHasClass(n, "avatar") }); len(avatar) != 1 || !strings.Contains(chatbug030Text(open[0]), "Policy Helper") {
		t.Fatal("the control does not hold both the icon and the name")
	}
	dialog := chatPolishMarkup(t, agentProfileDialog(m, "policy-helper"), 390, "light")
	if !strings.Contains(dialog, `class="agent-summary"`) || !strings.Contains(dialog, "Policy documents you can open") || strings.Contains(dialog, "mention-profile-card") {
		t.Fatal("the small card is not the same summary")
	}
	// An agent the page cannot establish by id is not made a control.
	unknown := chatux017General("en-US")
	unknown.ResolvedPersonaMentions = nil
	plain := chatPolishMarkup(t, html.Div(html.Props{}, personaReplyRowsForPost(unknown, localUI{}, "question", time.Now())...), 1440, "light")
	if strings.Contains(plain, "agent-summary-open") {
		t.Fatal("an agent that is not known here is offered as a control")
	}
}
