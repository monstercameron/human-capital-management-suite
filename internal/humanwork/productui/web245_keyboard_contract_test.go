package productui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestTodo_WEB_245 makes keyboard operability an extension invariant. It uses
// the page-module registry rather than a page count so the next admitted page
// enters this gate automatically.
func TestTodo_WEB_245(t *testing.T) {
	for _, module := range PageModules() {
		module := module
		t.Run(string(module.Definition.ID), func(t *testing.T) {
			root := renderKeyboardContractPage(t, testView(module.Definition.ID))
			assertKeyboardDocument(t, root)
		})
	}
}

func TestTodo_WEB_245_Property(t *testing.T) {
	for _, open := range []bool{false, true} {
		attrs := navigationDrawerTriggerAria(open, "Open navigation")
		if attrs["expanded"] != strconv.FormatBool(open) || attrs["controls"] != "workspace-navigation" || attrs["haspopup"] != "dialog" {
			t.Fatalf("drawer disclosure contract for open=%t: %#v", open, attrs)
		}
	}
	for _, key := range []string{"", "Esc", "Enter", " ", "ArrowUp", "ArrowDown", "Tab", "Escapee"} {
		if drawerEscapeCloses(key) {
			t.Fatalf("non-Escape key %q dismissed a shared overlay", key)
		}
	}
	if !drawerEscapeCloses("Escape") {
		t.Fatal("Escape does not dismiss a shared overlay")
	}
}

func TestTodo_WEB_245_Accessibility(t *testing.T) {
	styles := Stylesheet()
	for _, contract := range []string{
		`:where(a[href],button,input,select,textarea,summary,[contenteditable="true"],[tabindex]:not([tabindex="-1"])):focus-visible`,
		"@media (forced-colors:active)",
		"@media (prefers-reduced-motion:reduce)",
		".skip-link:focus",
	} {
		if !strings.Contains(styles, contract) {
			t.Errorf("shared keyboard stylesheet lacks %q", contract)
		}
	}
	for _, module := range PageModules() {
		root := renderKeyboardContractPage(t, testView(module.Definition.ID))
		focusable := collectFocusable(root)
		if len(focusable) == 0 || attr(focusable[0], "href") != "#main-content" {
			t.Errorf("page %q does not put its skip link first in keyboard order", module.Definition.ID)
		}
	}
}

func TestTodo_WEB_245_Browser(t *testing.T) {
	contracts := map[string][]string{
		"global_search.go":         {`case "Escape":`, `case "ArrowDown":`, `case "ArrowUp":`, `case "Enter":`, "event.PreventDefault()"},
		"action_launcher.go":       {`drawerEscapeCloses(event.GetKey())`, `event.GetKey() == "ArrowDown"`, `event.GetKey() == "ArrowUp"`, `event.GetKey() == "Enter"`, `usePopoverFocusDismissal("action-launcher"`},
		"navigation_components.go": {`useDrawerFocusTrap("workspace-navigation", "nav-drawer-trigger"`, "OnKeyDown: onEscape"},
		"appearance_components.go": {`useDrawerFocusTrap("appearance-preview-dialog", "appearance-preview-open"`, `drawerEscapeCloses(event.GetKey())`},
		"utility_drawer.go":        {`useDrawerFocusTrap("utility-drawer-dialog", "utility-drawer-trigger"`, `drawerEscapeCloses(event.GetKey())`, `usePopoverFocusDismissal("utility-drawer", "utility-drawer-trigger"`},
		"popover_focus_wasm.go":    {`event.Get("key").String() == "Escape"`, `focusElementByID(doc, triggerID)`},
		"drawer_focus_wasm.go":     {`args[0].Get("key").String() != "Tab"`, `doc.Call("getElementById", triggerID)`, `drawerFocusableVisible(item)`},
		"../../../tools/uxqual/cmd/journeywasm/transient_popover_wasm.go": {`case transientPopoverCloseNow:`, `summary.Call("focus")`},
	}
	for path, expected := range contracts {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, contract := range expected {
			if !strings.Contains(string(body), contract) {
				t.Errorf("%s lacks browser keyboard contract %q", path, contract)
			}
		}
	}
}

func TestTodo_WEB_245_I18N(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(locale)
		for _, module := range PageModules() {
			view := ApplyLocale(testView(module.Definition.ID), locale)
			root := renderKeyboardContractPage(t, view)
			htmlNode := firstElement(root, "html")
			if attr(htmlNode, "lang") != locale.Resolved || attr(htmlNode, "dir") != string(locale.Direction) {
				t.Errorf("page %q locale %q exposes lang=%q dir=%q", module.Definition.ID, locale.Resolved, attr(htmlNode, "lang"), attr(htmlNode, "dir"))
			}
			assertKeyboardDocument(t, root)
		}
	}
}

func TestTodo_WEB_245_Performance(t *testing.T) {
	if allocations := testing.AllocsPerRun(1_000, func() { _ = drawerEscapeCloses("Escape") }); allocations != 0 {
		t.Fatalf("shared Escape predicate allocations = %.0f, want 0", allocations)
	}
	if allocations := testing.AllocsPerRun(1_000, func() { _ = navigationDrawerDialogAttrs(false) }); allocations != 0 {
		t.Fatalf("closed drawer keyboard contract allocations = %.0f, want 0", allocations)
	}
}

func TestTodo_WEB_245_Regression(t *testing.T) {
	for _, module := range PageModules() {
		root := renderKeyboardContractPage(t, testView(module.Definition.ID))
		walkElements(root, func(node *xhtml.Node) {
			if node.Data == "button" {
				switch attr(node, "type") {
				case "button", "submit", "reset":
				default:
					t.Errorf("page %q has a button without an explicit safe type: %s", module.Definition.ID, keyboardNodeSummary(node))
				}
			}
			if node.Data == "a" && attr(node, "href") == "" {
				t.Errorf("page %q has an anchor without a keyboard destination: %s", module.Definition.ID, keyboardNodeSummary(node))
			}
		})
	}
}

func renderKeyboardContractPage(t *testing.T, view View) *xhtml.Node {
	t.Helper()
	document, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(document))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func assertKeyboardDocument(t *testing.T, root *xhtml.Node) {
	t.Helper()
	ids := map[string]*xhtml.Node{}
	walkElements(root, func(node *xhtml.Node) {
		if id := attr(node, "id"); id != "" {
			ids[id] = node
		}
	})
	walkElements(root, func(node *xhtml.Node) {
		if tabindex := attr(node, "tabindex"); tabindex != "" {
			value, err := strconv.Atoi(tabindex)
			if err != nil || value > 0 {
				t.Errorf("%s has invalid keyboard order tabindex=%q", keyboardNodeSummary(node), tabindex)
			}
		}
		if node.Data == "details" {
			first := firstElementChild(node)
			if first == nil || first.Data != "summary" {
				t.Errorf("%s does not begin with a native keyboard disclosure summary", keyboardNodeSummary(node))
			}
		}
		if role := attr(node, "role"); !isNativeKeyboardControl(node) && (role == "button" || role == "link" || role == "checkbox" || role == "radio" || role == "switch") && attr(node, "tabindex") != "0" {
			t.Errorf("%s uses an interactive role without native keyboard behavior or tabindex=0", keyboardNodeSummary(node))
		}
		for _, name := range []string{"aria-controls", "aria-activedescendant"} {
			for _, id := range strings.Fields(attr(node, name)) {
				if ids[id] == nil {
					t.Errorf("%s %s references missing id %q", keyboardNodeSummary(node), name, id)
				}
			}
		}
		if attr(node, "role") == "tree" && keyboardAccessibleName(root, node) == "" {
			t.Errorf("%s has no accessible tree name", keyboardNodeSummary(node))
		}
		if attr(node, "role") == "treeitem" {
			if _, err := strconv.Atoi(attr(node, "aria-level")); err != nil {
				t.Errorf("%s has no valid aria-level", keyboardNodeSummary(node))
			}
			if len(collectFocusable(node)) == 0 && attr(node, "tabindex") != "0" {
				t.Errorf("%s exposes no keyboard-reachable action", keyboardNodeSummary(node))
			}
		}
		if attr(node, "tabindex") == "0" && !isNativeKeyboardControl(node) && keyboardAccessibleName(root, node) == "" {
			t.Errorf("%s is keyboard focusable but unnamed", keyboardNodeSummary(node))
		}
	})
}

func firstElementChild(node *xhtml.Node) *xhtml.Node {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode {
			return child
		}
	}
	return nil
}

func isNativeKeyboardControl(node *xhtml.Node) bool {
	switch node.Data {
	case "a", "button", "input", "select", "textarea", "summary":
		return true
	default:
		return false
	}
}

func keyboardAccessibleName(root, node *xhtml.Node) string {
	if label := strings.TrimSpace(attr(node, "aria-label")); label != "" {
		return label
	}
	for _, id := range strings.Fields(attr(node, "aria-labelledby")) {
		if target := findElementByID(root, id); target != nil {
			if label := strings.TrimSpace(nodeText(target)); label != "" {
				return label
			}
		}
	}
	return strings.TrimSpace(nodeText(node))
}

func keyboardNodeSummary(node *xhtml.Node) string {
	return fmt.Sprintf("<%s id=%q role=%q>", node.Data, attr(node, "id"), attr(node, "role"))
}
