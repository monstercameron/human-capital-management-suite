package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestIssue3891_ThreadActionsStayReachableAtPhoneWidth: below 768px the thread's
// Save for later and More buttons are laid out in flow and visible, so they are
// in the tab order and the accessibility tree, and they are 44px targets.
func TestIssue3891_ThreadActionsStayReachableAtPhoneWidth(t *testing.T) {
	for _, want := range []string{
		`@media(max-width:767px){.chat-workspace .thread-pane .thread-message-actions{display:flex;visibility:visible;pointer-events:auto;position:static;`,
		`.chat-workspace .thread-pane .thread-message-actions .message-action,.chat-workspace .thread-pane .thread-message-actions .thread-more{display:inline-flex;`,
		`min-width:44px;min-height:44px`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}
	// The rule must come after the rules that hide the controls so it wins.
	hide := strings.Index(Stylesheet, `.chat-workspace .thread-more{display:none}`)
	show := strings.Index(Stylesheet, `.thread-pane .thread-message-actions .thread-more{display:inline-flex`)
	if hide < 0 || show < hide {
		t.Errorf("reachable-actions rule (at %d) does not follow the narrow hide rule (at %d)", show, hide)
	}
}

// TestIssue3891_ThreadReplyActionsAreNamedInEveryLanguage: each reply carries a
// named Save for later and More button the narrow rule can then reveal.
func TestIssue3891_ThreadReplyActionsAreNamedInEveryLanguage(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3894Model(locale)
		pane := renderNode(t, threadPane(m, handlers{}))
		root, err := xhtml.Parse(strings.NewReader(pane))
		if err != nil {
			t.Fatal(err)
		}
		replies := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && chatPolishHasClass(n, "thread-message")
		})
		if len(replies) != 1 {
			t.Fatalf("%s: %d replies, want 1", locale, len(replies))
		}
		buttons := chatPolishNodesIn(replies[0], func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "button" && chatPolishAncestor(n, "thread-message-actions")
		})
		if len(buttons) < 2 {
			t.Fatalf("%s: reply holds %d action buttons, want Save for later and More", locale, len(buttons))
		}
		for _, b := range buttons {
			if chatPolishAttr(b, "type") != "button" || (chatPolishAttr(b, "aria-label") == "" && uxblind126Text(b) == "") {
				t.Errorf("%s: action button %q lacks type or accessible name", locale, chatPolishAttr(b, "class"))
			}
		}
	}
}
