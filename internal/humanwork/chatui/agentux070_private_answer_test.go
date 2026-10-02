package chatui

import (
	"strings"
	"testing"
)

// AGENTUX-070: the composer's "Private answer" switch is offered only while the
// draft asks an agent, in a channel where agents answer to the channel by
// default; it says in one line who will see the answer; and what it adds to the
// question is the sentence the server reads.
func TestTodo_AGENTUX_070_PrivateAnswerToggle(t *testing.T) {
	model := chat4Fixture("en-US", "answered", false)
	agent := model.ResolvedPersonaMentions[0].Reference
	person := ChatReference{Kind: "PERSON_MENTION", ID: "bob", Display: "Bob"}

	if !agentux070Applies(model, []ChatReference{agent}) || !agentux070Applies(model, []ChatReference{person, agent}) {
		t.Fatal("the switch is not offered for a draft that asks an agent in a public channel")
	}
	for name, tc := range map[string]struct {
		change func(*Model)
		refs   []ChatReference
	}{
		"no mention":              {func(*Model) {}, nil},
		"only a person":           {func(*Model) {}, []ChatReference{person}},
		"an agent set to private": {func(m *Model) { m.ResolvedPersonaMentions[0].ReplyPlacement = PersonaReplyPrivateAlways }, []ChatReference{agent}},
		"an agent not in the room": {func(m *Model) {
			m.ResolvedPersonaMentions[0].Reference.ID = "other"
		}, []ChatReference{agent}},
		"a private channel": {func(m *Model) { m.Conversations[0].Kind = PrivateChannel }, []ChatReference{agent}},
	} {
		m := chat4Fixture("en-US", "answered", false)
		tc.change(&m)
		if agentux070Applies(m, tc.refs) || agentux070Row(m, localUI{}, tc.refs) != nil {
			t.Errorf("%s: the switch is offered", name)
		}
	}
	direct := chat4Fixture("en-US", "answered", true)
	if agentux070Applies(direct, []ChatReference{{Kind: "AGENT_MENTION", ID: "policy-helper"}}) {
		t.Error("a direct conversation with an agent is private by nature and has nothing to choose")
	}

	// The line says who will see the answer, before it is sent.
	off := renderNode(t, agentux070Row(model, localUI{}, []ChatReference{agent}))
	for _, want := range []string{"Private answer", "Everyone in", "general", "will see the answer.", `aria-checked="false"`, `data-action="agent-private-answer"`} {
		if !strings.Contains(off, want) {
			t.Errorf("the switch off lacks %q: %s", want, off)
		}
	}
	on := renderNode(t, agentux070Row(model, localUI{privateAnswer: true}, []ChatReference{agent}))
	if !strings.Contains(on, "Only you will see the answer.") || !strings.Contains(on, `aria-checked="true"`) || strings.Contains(on, "Everyone in") {
		t.Errorf("the switch on: %s", on)
	}
	for _, locale := range []string{"de-DE", "ar"} {
		m := chat4Fixture(locale, "answered", false)
		markup := renderNode(t, agentux070Row(m, localUI{}, []ChatReference{agent}))
		if strings.Contains(markup, "agentux070") || strings.Contains(markup, "Private answer") || strings.Contains(markup, "will see the answer") {
			t.Errorf("%s: English or a raw key on the page: %s", locale, markup)
		}
	}

	// What the switch adds to the question.
	if got := agentux070Body("@Policy Helper how many hours carry over?", true); got != "@Policy Helper how many hours carry over? Keep this private." {
		t.Errorf("body = %q", got)
	}
	if got := agentux070Body("@Policy Helper how many hours carry over?", false); got != "@Policy Helper how many hours carry over?" {
		t.Errorf("an off switch changed the question: %q", got)
	}
	if got := agentux070Body("Keep this private: how many hours?", true); got != "Keep this private: how many hours?" {
		t.Errorf("the asker's own words were repeated: %q", got)
	}
}
