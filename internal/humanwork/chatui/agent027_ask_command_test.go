package chatui

import (
	"strings"
	"testing"
)

// TestTodo_AGENT_027_Composer: the "/" list offers /ask where an agent can be
// asked and nowhere else, a good line goes through as typed, and each of the
// four refusals the server makes is said in the person's language and keeps
// the draft.
func TestTodo_AGENT_027_Composer(t *testing.T) {
	channel := composerToolsModel("en-US", false)
	direct := composerToolsModel("en-US", true)
	for name, m := range map[string]Model{"a channel with an agent placed": channel, "a direct conversation with an agent": direct} {
		found := false
		for _, command := range composerCommandsFor("chat-composer").menu(m, "") {
			found = found || command.Name == "ask"
		}
		if !found {
			t.Fatalf("%s: /ask is not in the list", name)
		}
	}
	bare := channel
	bare.ResolvedPersonaMentions = nil
	for _, command := range composerCommandsFor("chat-composer").menu(bare, "") {
		if command.Name == "ask" {
			t.Fatal("/ask is offered where no agent can be asked")
		}
	}
	if text := composerCommandDescription(channel, composerCommand{Text: "cmd-ask"}); text != "Ask an agent in this conversation" {
		t.Fatalf("the list row says %q", text)
	}

	var log composerCommandLog
	for _, tc := range []struct {
		name, body, reason string
		m                  Model
	}{
		{"named agent and question", "/ask @Policy Helper how many days carry over?", "", channel},
		{"no name in an agent's own conversation", "/ask how many days carry over?", "", direct},
		{"no agent named in a channel", "/ask how many days carry over?", agent027NeedsAgent, channel},
		{"nothing asked", "/ask @Policy Helper", agent027NeedsQuestion, channel},
		{"nothing asked in a direct conversation", "/ask", agent027NeedsQuestion, direct},
	} {
		log = composerCommandLog{}
		text, consumed := composerSendCommand(log.runtime(tc.m), tc.body)
		if tc.reason == "" {
			if consumed || text != tc.body {
				t.Fatalf("%s: a good line was held back: consumed=%v text=%q", tc.name, consumed, text)
			}
			continue
		}
		if !consumed || text != "" || log.cleared != 0 || log.notice != agent027Text(tc.m, tc.reason) || log.notice == "" {
			t.Fatalf("%s: consumed=%v text=%q cleared=%d notice=%q", tc.name, consumed, text, log.cleared, log.notice)
		}
	}

	// Two agents named is refused too.
	two := channel
	second := two.ResolvedPersonaMentions[0]
	second.Reference.ID, second.Reference.Display = "comp-analyst", "Comp Analyst"
	two.ResolvedPersonaMentions = append(append([]ResolvedPersonaMention(nil), two.ResolvedPersonaMentions...), second)
	log = composerCommandLog{}
	if text, consumed := composerSendCommand(log.runtime(two), "/ask @Policy Helper @Comp Analyst compare"); !consumed || text != "" || log.notice != agent027Text(two, agent027OneAgent) {
		t.Fatalf("two agents: consumed=%v notice=%q", consumed, log.notice)
	}

	// Every reason has a sentence in each language, never a raw key.
	for _, reason := range []string{agent027NeedsAgent, agent027NeedsQuestion, agent027OneAgent, agent027NotOwnWords} {
		seen := map[string]bool{}
		for _, locale := range []string{"en-US", "de-DE", "ar"} {
			m := channel
			m.Locale = locale
			text := agent027Text(m, reason)
			if text == "" || strings.Contains(text, reason) || strings.Contains(text, "⟦") || seen[text] {
				t.Fatalf("%s in %s: %q", reason, locale, text)
			}
			seen[text] = true
		}
	}
}
