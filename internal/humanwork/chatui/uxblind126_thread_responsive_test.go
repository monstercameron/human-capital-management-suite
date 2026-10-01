package chatui

import (
	"strings"
	"testing"
)

func TestTodo_UXBLIND_126(t *testing.T) {
	css := ScopedStylesheet()
	for _, want := range []string{
		`@media(max-width:760px){.thread-root-body .message-meta,.thread-message-body .message-meta{display:grid;grid-template-columns:minmax(0,1fr) auto`,
		`.thread-root-body .message-meta .thread-view-in-channel{position:static;grid-column:1 / -1`,
		`.thread-pane .message-time{white-space:nowrap}`,
		`.thread-pane .message-author{min-width:0;overflow-wrap:anywhere}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("responsive thread stylesheet missing %q", want)
		}
	}
}
