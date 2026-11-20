package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func issue3873Pane(t *testing.T, locale string, local localUI, draft string) *xhtml.Node {
	t.Helper()
	m := issue3894Model(locale)
	m.ThreadDrafts = map[string]string{"p1": draft}
	root, err := xhtml.Parse(strings.NewReader(renderNode(t, threadPane(m, handlers{local: local}))))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func issue3873Has(n *xhtml.Node, name string) bool {
	for _, a := range n.Attr {
		if a.Key == name {
			return true
		}
	}
	return false
}

func issue3873Toggle(t *testing.T, root *xhtml.Node) *xhtml.Node {
	t.Helper()
	found := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && n.Data == "button" && chatPolishAttr(n, "data-action") == "composer-preview-toggle"
	})
	if len(found) != 1 {
		t.Fatalf("thread composer has %d preview toggles, want one", len(found))
	}
	return found[0]
}

// TestIssue3873_PreviewToggleStaysOnWhilePreviewing: the previewed reply form is
// marked so the compact layouts keep its tool row, and the toggle is pressed,
// named and enabled in every language so the reader can return to editing.
func TestIssue3873_PreviewToggleStaysOnWhilePreviewing(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		root := issue3873Pane(t, locale, localUI{threadComposerPreview: true}, "Test **thread**")
		forms := chatPolishNodesIn(root, func(n *xhtml.Node) bool {
			return n.Type == xhtml.ElementNode && n.Data == "form" && chatPolishHasClass(n, "thread-composer")
		})
		if len(forms) != 1 || chatPolishAttr(forms[0], "data-preview") != "true" {
			t.Fatalf("%s: previewing reply form is not marked data-preview", locale)
		}
		toggle := issue3873Toggle(t, root)
		if chatPolishAttr(toggle, "aria-pressed") != "true" || chatPolishAttr(toggle, "aria-label") == "" {
			t.Errorf("%s: toggle is not a named, pressed button", locale)
		}
		if issue3873Has(toggle, "disabled") {
			t.Errorf("%s: toggle is disabled while previewing", locale)
		}
	}
}

// TestIssue3873_EmptyPreviewCanBeLeft: an emptied draft that is still previewing
// keeps an enabled toggle, so the empty preview is never a dead end.
func TestIssue3873_EmptyPreviewCanBeLeft(t *testing.T) {
	root := issue3873Pane(t, "en-US", localUI{threadComposerPreview: true}, "")
	if issue3873Has(issue3873Toggle(t, root), "disabled") {
		t.Fatal("empty preview has a disabled toggle and cannot be left")
	}
	idle := issue3873Pane(t, "en-US", localUI{}, "")
	if !issue3873Has(issue3873Toggle(t, idle), "disabled") {
		t.Fatal("an empty draft that is not previewing should still have nothing to preview")
	}
	if forms := chatPolishNodesIn(idle, func(n *xhtml.Node) bool {
		return n.Type == xhtml.ElementNode && chatPolishAttr(n, "data-preview") != ""
	}); len(forms) != 0 {
		t.Fatal("form marked as previewing when it is not")
	}
}

// TestIssue3873_CompactStylesKeepToolRow: the rule that shows the tool row for a
// preview out-ranks the compact rule that hides it when focus is elsewhere, and
// the toggle is a 44px target.
func TestIssue3873_CompactStylesKeepToolRow(t *testing.T) {
	for _, want := range []string{
		`.chat-workspace .thread-composer[data-preview="true"]:not(:focus-within) .composer-tools{display:flex}`,
		`.chat-workspace .thread-composer[data-preview="true"] .composer-preview-toggle{display:inline-flex;min-width:44px;min-height:44px}`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}
	hide := strings.Index(Stylesheet, `.thread-composer:not(:focus-within) :is(.composer-tools,`)
	show := strings.Index(Stylesheet, `.thread-composer[data-preview="true"]:not(:focus-within) .composer-tools{display:flex}`)
	if hide < 0 || show < hide {
		t.Errorf("show rule (%d) must follow the compact hide rule (%d)", show, hide)
	}
}

// TestIssue3873_PreviewLayoutDoesNotDependOnFocus: pressing the toggle moves
// focus onto it; the previewing composer must keep one layout either way, or the
// button slides from under the pointer and the press is lost.
func TestIssue3873_PreviewLayoutDoesNotDependOnFocus(t *testing.T) {
	for _, want := range []string{
		`.chat-workspace .thread-composer[data-preview="true"]{flex-direction:column;align-items:stretch;padding:0}`,
		`.chat-workspace .thread-composer[data-preview="true"] .composer-toolbar{padding:2px 6px 6px;flex:none}`,
		`.chat-workspace .thread-composer[data-preview="true"] :is(.format-tools,.emoji-control){display:none}`,
	} {
		at := strings.Index(Stylesheet, want)
		if at < 0 {
			t.Fatalf("stylesheet missing %q", want)
		}
		if compact := strings.Index(Stylesheet, `.thread-composer:not(:focus-within){flex-direction:row`); compact < 0 || at < compact {
			t.Errorf("rule %q must follow the focus-dependent compact layout", want)
		}
	}
}
