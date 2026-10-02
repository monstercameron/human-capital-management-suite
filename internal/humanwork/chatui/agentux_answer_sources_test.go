package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

func TestAgentUXQuality_SourceTree(t *testing.T) {
	if body := parseAgentReplyEnvelope("The agent uses version 7.").Body; strings.Contains(body, "version 7") || !strings.Contains(body, "v7.0.0") {
		t.Fatalf("uncited legacy version not rendered semantically: %s", body)
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		body := "Carryover (Paid time off policy, version 1, Carryover section).\n\nSources\n- [Paid time off policy · Carryover · v1.0.0](/workspace/app/docs?document=pto&version=one#carryover) <!--chat.agent.source.readable:true-->\n- Private guide <!--chat.agent.source.readable:false-->"
		envelope := parseAgentReplyEnvelope(body)
		if strings.Contains(envelope.Body, "version 1") || !strings.Contains(envelope.Body, "](/workspace/app/docs?document=pto&version=one#carryover)") {
			t.Fatalf("inline citation: %s", envelope.Body)
		}
		markup := renderNode(t, html.Div(html.Props{}, renderAgentReplySources(Model{Locale: locale}, envelope)...))
		for _, expected := range []string{"icon-document", "href=", "v1.0.0", agentAnswerSourceUnavailable(locale)} {
			if !strings.Contains(markup, expected) {
				t.Fatalf("%s missing %q: %s", locale, expected, markup)
			}
		}
	}
}

func TestAgentUXQuality_SourceTree_Security(t *testing.T) {
	envelope := parseAgentReplyEnvelope("Answer\n\nSources\n- [Private](/workspace/app/docs?document=secret) <!--chat.agent.source.readable:false-->")
	if envelope.Sources[0].Href != "" || envelope.Sources[0].Readable {
		t.Fatalf("denied source linked: %+v", envelope)
	}
	markup := renderNode(t, html.Div(html.Props{}, renderAgentReplySources(Model{Locale: "en-US"}, envelope)...))
	if strings.Contains(markup, "href=") {
		t.Fatalf("unreadable source rendered as link: %s", markup)
	}
}

func TestAgentUXQuality_ReactionTree(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, emoji := range []string{"👀", "✅", "🏖️"} {
			markup := renderNode(t, reactionRow(Model{Locale: locale}, Message{ID: "asking-message", Chips: []ReactionChip{{Emoji: emoji, Count: 1}}}))
			if !strings.Contains(markup, emoji) || !strings.Contains(markup, `data-id="asking-message"`) || !strings.Contains(markup, "aria-label=") || !strings.Contains(markup, "reaction-count") {
				t.Fatalf("reaction not tied to asking message: %s", markup)
			}
		}
	}
}
