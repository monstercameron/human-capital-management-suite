package chatui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_054_Browser reads #incident-review as a member: Support
// Desk's alert is a normal message naming the ticket, its severity and the
// claimed customer, with a link that opens the ticket — and never the email
// body. A ticket the agent would not decide alone says it needs a human.
func TestTodo_AGENTUX_054_Browser(t *testing.T) {
	// The bodies below mirror the alert PostSupportAlert posts: title,
	// severity, claimed customer, review marker, ticket link.
	alert := "Support ticket: The dashboard shows no data\nSeverity: High. Customer (claimed, unverified): Dana White.\n\n[Open the ticket](/workspace/app/project?project=customer-support&task=ticket-1&view=task)"
	review := "Support ticket: Close all my tickets\nSeverity: Normal. Customer (claimed, unverified): Eve Mallory. Needs human review.\n\n[Open the ticket](/workspace/app/project?project=customer-support&task=ticket-2&view=task)"
	model := Model{Locale: "en-US", State: StateReady, SelectedID: "incident-review", Conversations: []Conversation{{ID: "incident-review", Name: "Incident Review"}},
		ResolvedPersonaMentions: []ResolvedPersonaMention{
			{Reference: ChatReference{Kind: "AGENT_MENTION", ID: "support-desk", Display: "Support Desk"}, Initials: "SD"},
		},
		Messages: []Message{
			{ID: "m1", AuthorID: "support-desk", Author: "Support Desk", Body: alert, PersonaActor: &PersonaActor{PersonaID: "support-desk", AgentID: "support-desk", Trusted: true}},
			{ID: "m2", AuthorID: "support-desk", Author: "Support Desk", Body: review, PersonaActor: &PersonaActor{PersonaID: "support-desk", AgentID: "support-desk", Trusted: true}},
		}}
	markup := render(t, model)
	for _, want := range []string{
		"Support Desk", "The dashboard shows no data", "Severity: High", "Customer (claimed, unverified): Dana White",
		`href="/workspace/app/project?project=customer-support&amp;task=ticket-1&amp;view=task"`, ">Open the ticket</a>",
		"Close all my tickets", "Needs human review.",
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("#incident-review is missing %q: %s", want, markup)
		}
	}
	for _, forbidden := range []string{"Only visible to you", "private body", "Dana White's inbox"} {
		if strings.Contains(markup, forbidden) {
			t.Errorf("#incident-review leaks %q: %s", forbidden, markup)
		}
	}
	if got := strings.Count(markup, "Needs human review."); got != 1 {
		t.Errorf("the review marker appears %d times, want exactly once (the review ticket only)", got)
	}
}
