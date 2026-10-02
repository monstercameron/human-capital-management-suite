package chatui

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The answer card says each thing once: the document's title is not a label in
// front of the answer, the question is quoted only on the copy saved in the
// agent's own conversation, the composer shows at most one hint, and the
// working card is the answered card's frame with a counter and Stop.
func TestTodo_AGENTUX_060(t *testing.T) {
	sources := "\n\nSources\n- [Paid time off policy · Carryover · v1.0.0](/workspace/app/docs?document=pto&version=1#carryover) <!--chat.agent.source.readable:true-->"
	for _, tc := range []struct{ name, body, want, gone string }{
		{"a label with a colon", "Paid time off policy: employees may carry over up to 40 hours.", "Employees may carry over up to 40 hours.", "policy: employees"},
		{"a label with a dash", "Paid time off policy — Employees may carry over up to 40 hours.", "Employees may carry over up to 40 hours.", "policy — Employees"},
		{"the title as the subject of the sentence", "Paid time off policy allows 40 hours to carry over.", "allows 40 hours to carry over.", "\x00"},
		{"no label", "Employees may carry over up to 40 hours.", "Employees may carry over up to 40 hours.", "\x00"},
	} {
		envelope := parseAgentReplyEnvelope(tc.body + sources)
		if !strings.Contains(envelope.Body, tc.want) || strings.Contains(envelope.Body, tc.gone) {
			t.Fatalf("%s: the answer reads %q", tc.name, envelope.Body)
		}
		if tc.gone != "\x00" && (strings.HasPrefix(envelope.Body, "[Paid time off policy]") || strings.HasPrefix(envelope.Body, "Paid time off policy")) {
			t.Fatalf("%s: the answer still starts with the document's title: %q", tc.name, envelope.Body)
		}
		if len(envelope.Sources) != 1 {
			t.Fatalf("%s: sources = %+v", tc.name, envelope.Sources)
		}
	}
	// The title as the sentence's subject stays, linked or not.
	if kept := parseAgentReplyEnvelope("Paid time off policy allows 40 hours to carry over." + sources).Body; !strings.Contains(kept, "Paid time off policy") {
		t.Fatalf("a title that is part of the sentence was removed: %q", kept)
	}
	// A title the answer does not cite is not treated as a label.
	if other := parseAgentReplyEnvelope("Holiday guide: the office closes on Labor Day." + sources).Body; !strings.HasPrefix(other, "Holiday guide:") {
		t.Fatalf("text that is not a cited title was removed: %q", other)
	}

	// The quoted question: on the saved copy in the agent's conversation, not in the channel.
	now := time.Now()
	question, _ := json.Marshal(agentQuestionContext{Label: "#general", Text: "how many PTO hours carry over?", At: now})
	withContext := "Up to 40 hours." + sources + "\n\n[chat-agent-question-context:" + base64.RawURLEncoding.EncodeToString(question) + "](/chat/share/signed)"
	channel := chat4Fixture("en-US", "answered", false)
	channel.PersonaActivityReady = true
	channel.EphemeralMessages[0].Body = withContext
	if card := chatbug047Rows(t, channel); strings.Contains(card, "agent-question-context") || strings.Contains(card, "You asked in") || strings.Contains(card, "View in") {
		t.Fatal("an answer in the channel quotes the question it sits under")
	}
	direct := chat4Fixture("en-US", "answered", true)
	direct.Messages[1].Body = withContext
	if page := render(t, direct); !strings.Contains(page, "agent-question-context") || !strings.Contains(page, "You asked in") || !strings.Contains(page, "how many PTO hours carry over?") {
		t.Fatal("the saved copy in the agent's conversation lost its quoted question")
	}

	// Composer hints: one at a time, and none while a list is open or nothing is typed.
	hint := func(m Model, h handlers) string {
		markup, err := ui.RenderToString(chatComposerHint(m, h))
		if err != nil {
			t.Fatal(err)
		}
		return markup
	}
	m := chat4Fixture("en-US", "sent", false)
	m.Draft = "@pol how many hours?"
	typed := hint(m, handlers{})
	if !strings.Contains(typed, "Did you mean @Policy Helper?") && !strings.Contains(typed, "composer-unresolved-mention") {
		t.Fatalf("a name typed without a mention is not noticed: %s", typed)
	}
	if strings.Contains(typed, "composer-agent-reply-hint") {
		t.Fatalf("two hints are shown at once: %s", typed)
	}
	if open := hint(m, handlers{mentionView: mentionState{Open: true, Target: "chat-composer"}}); strings.Contains(open, "composer-unresolved-mention") || strings.Contains(open, "composer-agent-reply-hint") {
		t.Fatalf("a hint is shown while the mention list is open: %s", open)
	}
	m.Draft = ""
	if empty := hint(m, handlers{mentionReplyHint: "Everyone will see your question.", composerAgentName: "Policy Helper"}); strings.Contains(empty, "composer-agent-reply-hint") || strings.Contains(empty, "composer-unresolved-mention") {
		t.Fatalf("a hint is shown under an empty composer: %s", empty)
	}
	m.Draft = "how many hours?"
	if chosen := hint(m, handlers{mentionReplyHint: "Everyone will see your question.", composerAgentName: "Policy Helper"}); !strings.Contains(chosen, "composer-agent-reply-hint") || strings.Contains(chosen, "composer-unresolved-mention") {
		t.Fatalf("a question to a chosen agent does not show exactly the reply hint: %s", chosen)
	}

	// The hint's room does not come and go with the hint: wherever a hint can
	// appear the slot is kept while it is empty, so no keystroke changes the
	// composer's height. Where no hint can appear the slot keeps no room.
	reserved := func(markup string) bool {
		return strings.Contains(markup, `class="composer-hint-slot composer-hint-reserved"`)
	}
	channel = chat4Fixture("en-US", "sent", false)
	chip := handlers{mentionReplyHint: "Everyone will see your question.", composerAgentName: "Policy Helper"}
	for _, draft := range []string{"", "h", "how many hours?"} {
		channel.Draft = draft
		// An agent is named in the draft: before the first keystroke, at it, and after.
		if markup := hint(channel, chip); !reserved(markup) {
			t.Fatalf("draft %q with an agent named: the hint's room is not kept: %s", draft, markup)
		}
		// No agent named yet, in a channel that has one: "Did you mean" may come.
		if markup := hint(channel, handlers{}); !reserved(markup) {
			t.Fatalf("draft %q in a channel with an agent: the hint's room is not kept: %s", draft, markup)
		}
		// A list is open over the composer: the room stays, the hint does not show.
		if markup := hint(channel, handlers{mentionReplyHint: chip.mentionReplyHint, composerAgentName: chip.composerAgentName, mentionView: mentionState{Open: true, Target: "chat-composer"}}); !reserved(markup) || strings.Contains(markup, "composer-agent-reply-hint") {
			t.Fatalf("draft %q with the mention list open: %s", draft, markup)
		}
	}
	plain := chat4Fixture("en-US", "sent", false)
	plain.ResolvedPersonaMentions = nil
	for _, draft := range []string{"", "hello"} {
		plain.Draft = draft
		if markup := hint(plain, handlers{}); markup != `<div class="composer-hint-slot"></div>` {
			t.Fatalf("a conversation with no agent, draft %q: %s", draft, markup)
		}
	}
	own := chat4Fixture("en-US", "sent", true)
	own.Draft = "how many hours?"
	if markup := hint(own, handlers{}); reserved(markup) {
		t.Fatalf("the person's own conversation with an agent keeps room for a hint it never shows: %s", markup)
	}
	// On a phone an empty slot is folded away. The kept room wins over that rule
	// whenever the composer is in use (it has the caret, or holds text), which is
	// every moment a keystroke can happen; the resting one-row composer of a
	// phone, empty and without the caret, stays one row.
	keep := ".chat-workspace .chat-composer:focus-within .composer-hint-slot.composer-hint-reserved:empty,.chat-workspace .chat-composer:has(.composer-input:not(:placeholder-shown)) .composer-hint-slot.composer-hint-reserved:empty{display:block}"
	fold := ".chat-workspace .chat-composer .composer-hint-slot:empty{display:none}"
	if !strings.Contains(Stylesheet, keep) || !strings.Contains(Stylesheet, fold) || !strings.Contains(Stylesheet, ".composer-hint-slot{position:relative;flex:none;height:44px;min-height:44px") {
		t.Fatal("the stylesheet does not keep the reserved hint slot's room while the composer is in use")
	}
	// Each selector of the keeping rule outranks the folding rule, whatever
	// their order in the stylesheet: it names one more class and one more state.
	for _, selector := range strings.Split(strings.TrimSuffix(keep, "{display:block}"), ",") {
		if strings.Count(selector, ".")+strings.Count(selector, ":") <= strings.Count(strings.TrimSuffix(fold, "{display:none}"), ".")+strings.Count(strings.TrimSuffix(fold, "{display:none}"), ":") {
			t.Fatalf("%q does not outrank the rule that folds the slot away", selector)
		}
	}

	// The working card: the answered card's frame, a counter after five seconds, and Stop.
	if !strings.Contains(Stylesheet, ".persona-progress-status.agent-reply-row,.chat-ephemeral.agent-reply-row{") {
		t.Fatal("the working and answered cards no longer share one frame rule")
	}
	early := chatbug047Rows(t, chat4Fixture("en-US", "working2", false))
	if strings.Contains(early, "agent-reply-counter") || strings.Contains(early, "agent-progress-cancel") {
		t.Fatal("a run two seconds old already shows a counter or Stop")
	}
	late := chatbug047Rows(t, chat4Fixture("en-US", "working15", false))
	if !strings.Contains(late, "0:15") || !strings.Contains(late, ">Stop</button>") {
		t.Fatalf("a run fifteen seconds old shows no elapsed time or no Stop: %s", late)
	}
}

// Retry replaces the failed card in place: the working state stands where the
// failure stood, under the question, and there is one card throughout.
func TestTodo_AGENTUX_060_Browser(t *testing.T) {
	m := chat4Fixture("en-US", "failed", false)
	m.PersonaActivityReady = true
	before := chatbug047Rows(t, m)
	if !strings.Contains(before, `data-agent-reply-state="failed"`) || !strings.Contains(before, ">Ask again<") {
		t.Fatal("the failed card has no Ask again")
	}
	m.AgentRetries = map[string]AgentRetryState{"question": {Asking: true, Since: time.Now()}}
	during := chatbug047Rows(t, m)
	if strings.Count(during, `data-agent-reply-state=`) != 1 || !strings.Contains(during, `data-agent-reply-state="working"`) || strings.Contains(during, ">Ask again<") {
		t.Fatalf("Ask again did not put the working state in the failed card's place: %s", during)
	}
	rows := personaReplyRowsForPost(m, localUI{}, "question", time.Now())
	if len(rows) != 1 {
		t.Fatalf("%d cards under the question while it is asked again", len(rows))
	}
}
