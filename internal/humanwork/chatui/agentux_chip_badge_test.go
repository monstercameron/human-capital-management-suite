package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

// The badge is accessible context, not hover-only visible copy. Keeping the
// chip's visible label invariant prevents both layout growth and a misleading
// text change on hover or keyboard focus.
func TestMentionChipCollapsedBadgeCannotWrap(t *testing.T) {
	reference := ChatReference{Kind: "AGENT_MENTION", ID: "policy", Display: "Policy Helper"}
	model := Model{renderReferences: []ChatReference{reference}, ResolvedPersonaMentions: []ResolvedPersonaMention{{Reference: reference}}}
	markup := renderNode(t, html.Span(html.Props{}, mentionReferenceBody(model, "@Policy Helper")...))
	if !strings.Contains(markup, `class="sr-only mention-chip-agent-badge"`) || !strings.Contains(markup, `>@Policy Helper<span`) {
		t.Fatalf("mention chip changed its visible label: %s", markup)
	}
	for _, forbidden := range []string{"max-width:0", ".mention-chip:hover .mention-chip-agent-badge", ".mention-chip:focus .mention-chip-agent-badge"} {
		if strings.Contains(AgentUXChat2Styles, forbidden) {
			t.Fatalf("mention chip retained hover-only badge behavior %q", forbidden)
		}
	}
}
