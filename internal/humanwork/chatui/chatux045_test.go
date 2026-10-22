package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_045: at tablet width Saved squeezed the composer and the
// brand mark; at phone width a direct conversation was titled "P." and the
// bell and account menu were unreachable. A side panel opens over the
// conversation instead of beside it, names keep their line, and the phone
// list bar keeps the workspace controls.
func TestTodo_CHATUX_045(t *testing.T) {
	t.Run("Saved opens over the conversation at tablet widths", func(t *testing.T) {
		for _, want := range []string{
			// Saved takes no grid column: rail and conversation keep theirs.
			`.chat-workspace.chat-workspace:has(>.chatsave-panel:popover-open) .chat-layout{grid-template-columns:minmax(220px,var(--chat-rail)) minmax(0,1fr)}`,
			`.chat-workspace .chatsave-panel{inline-size:min(360px,calc(100vw - 16px))}`,
			// At the narrowest tablet widths the list steps aside as a drawer.
			`.chat-workspace[data-sidebar-open="true"]:has(>.chatsave-panel:popover-open) .chat-rail{display:flex;position:fixed`,
		} {
			if !strings.Contains(ChatUX045Styles, want) {
				t.Errorf("tablet Saved rule missing: %.80q", want)
			}
		}
		if !strings.Contains(Stylesheet, ChatUX045Styles) {
			t.Error("ChatUX045Styles is not composed into the served stylesheet")
		}
	})

	t.Run("a direct conversation keeps its whole name on one line", func(t *testing.T) {
		for _, want := range []string{
			`.agent-identity-line{flex-wrap:wrap`,
			`.agent-identity-line h1{flex:0 1 auto;min-inline-size:0;max-inline-size:100%}`,
			`.conversation-heading h1{font-size:1rem;font-weight:700;margin:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}`,
		} {
			if !strings.Contains(Stylesheet, want) {
				t.Errorf("name rule missing: %.80q", want)
			}
		}
		m := chat4Fixture("en-US", "sent", true)
		markup := renderNode(t, Build(m))
		name := m.Conversations[0].Name
		if !strings.Contains(markup, ">"+name+"<") {
			t.Errorf("the direct conversation is not titled %q in full", name)
		}
	})

	t.Run("the phone list bar leaves room for the bell and the account menu", func(t *testing.T) {
		want := `.chat-workspace[data-sidebar-open="true"] .rail-head{padding-inline-end:calc(2 * var(--hcm-control-height) + var(--hcm-space-1))}`
		if !strings.Contains(ChatUX045Styles, want) {
			t.Errorf("rail-head room rule missing: %.80q", want)
		}
	})
}
