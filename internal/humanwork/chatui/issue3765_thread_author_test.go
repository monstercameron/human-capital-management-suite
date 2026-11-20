package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestIssue3765_ThreadRootAuthorIsNotTruncated: the thread panel's root
// message shows the author's whole name. The timeline's rule that cuts a long
// author with an ellipsis (.message-author) must not apply inside the thread
// pane, where the long timestamp and "View in channel" share the row; the name
// wraps and the timestamp reflows instead.
func TestIssue3765_ThreadRootAuthorIsNotTruncated(t *testing.T) {
	css := ScopedStylesheet()
	if !strings.Contains(css, ".thread-pane .message-author") {
		t.Fatal("no thread-pane author rule")
	}
	sheet := Stylesheet
	for property, want := range map[string]string{
		"white-space":   "normal",
		"text-overflow": "clip",
		"overflow":      "visible",
	} {
		if got := chatbugCascadeValue(sheet, ".chat-workspace .thread-pane .message-author", property); got != want {
			t.Errorf("thread author %s = %q, want %q (no ellipsis)", property, got, want)
		}
	}
	if got := chatbugCascadeValue(sheet, ".chat-workspace .thread-pane .message-meta", "flex-wrap"); got != "wrap" {
		t.Errorf("thread meta flex-wrap = %q, want wrap so the timestamp reflows under the name", got)
	}
	if got := chatbugCascadeValue(sheet, ".chat-workspace .thread-pane .message-time", "white-space"); got == "nowrap" || got == "" {
		// The time may keep one line only when it can drop below the name.
		if chatbugCascadeValue(sheet, ".chat-workspace .thread-pane .message-meta", "flex-wrap") != "wrap" {
			t.Errorf("thread timestamp cannot reflow")
		}
	}
	// Logical properties only: the same rule holds in right-to-left locales.
	if physical := issue3765Styles; strings.Contains(physical, "-left") || strings.Contains(physical, "-right") {
		t.Errorf("thread author rules use a physical side: %s", physical)
	}
}

// TestIssue3765_ThreadRootRendersFullAuthor: the rendered root names its author
// in full, in a button that carries no inline clipping, in every language.
func TestIssue3765_ThreadRootRendersFullAuthor(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3894Model(locale)
		root, err := xhtml.Parse(strings.NewReader(renderNode(t, threadPane(m, handlers{}))))
		if err != nil {
			t.Fatal(err)
		}
		authors := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && chatPolishHasClass(n, "message-author") && chatPolishAncestor(n, "thread-root")
		})
		if len(authors) != 1 {
			t.Fatalf("%s: %d thread root authors, want one", locale, len(authors))
		}
		if got := strings.TrimSpace(uxblind126Text(authors[0])); got == "" || strings.Contains(got, "…") || strings.Contains(got, "...") {
			t.Errorf("%s: root author text = %q", locale, got)
		}
	}
}
