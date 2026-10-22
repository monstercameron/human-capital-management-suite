package chatui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_019 pins the Chat mention and badge findings of the
// round-1 review. F7 keeps the Agents group above the fold by rendering it
// first; F8 gives the mention empty state a reason and a next step (the
// no-agents-installed case stays people-only per the later, test-pinned
// CHATBUG-028 decision); F40 isolates a Latin handle inside Arabic
// attribution copy.
func TestTodo_AGENTUX_019(t *testing.T) {
	t.Run("F7 the Agents group renders before the people", func(t *testing.T) {
		for _, query := range []string{"", "pol"} {
			m := agentUXMentionModel(PersonaLookupReady)
			m.Members = []Member{{ID: "walt", Name: "Walt Brennan"}, {ID: "polly", Name: "Polly Adams"}}
			markup := renderNode(t, mentionMenu(m, mentionState{Target: "chat-composer", Query: query, Open: true, ShowAllPeople: true}, "chat-composer"))
			agents, people := strings.Index(markup, `class="mention-heading agents"`), strings.Index(markup, `class="mention-heading people"`)
			if agents < 0 || (people >= 0 && agents > people) {
				t.Fatalf("query %q: Agents group is not first: agents %d people %d", query, agents, people)
			}
		}
	})

	t.Run("F8 the mention empty state gives a reason and a next step", func(t *testing.T) {
		m := agentUXMentionModel(PersonaLookupIdle)
		m.Members = []Member{{ID: "walt", Name: "Walt Brennan"}}
		m.ResolvedPersonaMentions = nil
		markup := renderNode(t, mentionMenu(m, mentionState{Target: "chat-composer", Query: "zzz", Open: true}, "chat-composer"))
		if !strings.Contains(markup, "No one matches") || !strings.Contains(markup, "Try a different spelling.") {
			t.Fatalf("empty state has no reason or next step: %s", markup)
		}
		failed := agentUXMentionModel(PersonaLookupFailed)
		failed.ResolvedPersonaMentions = nil
		failedMarkup := renderNode(t, mentionMenu(failed, mentionState{Target: "chat-composer", Query: "", Open: true}, "chat-composer"))
		if !strings.Contains(failedMarkup, "The agent list could not be loaded.") || !strings.Contains(failedMarkup, `data-action="persona-mention-retry"`) {
			t.Fatalf("failed agents state has no reason or retry: %s", failedMarkup)
		}
	})

	t.Run("F40 Arabic attribution isolates the handle", func(t *testing.T) {
		actor := &PersonaActor{PersonaID: "persona-1", AgentID: "agent-1", InvokerHandle: "dana-ruiz", Trusted: true}
		arabic := renderNode(t, PersonaBadgeLocalized("ar", actor))
		if !strings.Contains(arabic, `<bdi dir="ltr">@dana-ruiz</bdi>`) {
			t.Fatalf("Arabic handle is not isolated: %s", arabic)
		}
		if !strings.Contains(arabic, `aria-label="وكيل; يعمل نيابة عن @dana-ruiz"`) {
			t.Fatalf("accessible name lost the sentence: %s", arabic)
		}
		english := renderNode(t, PersonaBadgeLocalized("en-US", actor))
		if !strings.Contains(english, "acting for @dana-ruiz") || strings.Contains(english, "<bdi") {
			t.Fatalf("English attribution changed shape: %s", english)
		}
		german := renderNode(t, PersonaBadgeLocalized("de-DE", actor))
		if !strings.Contains(german, "handelt für @dana-ruiz") || strings.Contains(german, "<bdi") {
			t.Fatalf("German attribution changed shape: %s", german)
		}
	})
}
