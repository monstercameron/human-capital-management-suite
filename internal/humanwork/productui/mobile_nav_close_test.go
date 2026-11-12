package productui

import (
	"os"
	"strings"
	"testing"
)

// TestIssue_3606_BackdropClosesWithDrawer: closing the mobile drawer must hide
// its backdrop. The open path sets an inline display:block, which overrides the
// hidden attribute the close path sets, so the full-screen layer stayed over
// the page. The close branch has to set display:none inline as well.
// bindMobileNavigationDrawer is js/wasm-only, so this pins its source.
func TestIssue_3606_BackdropClosesWithDrawer(t *testing.T) {
	body, err := os.ReadFile("popover_focus_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	closeStart := strings.Index(src, "} else {\n\t\t\troot.Call(\"removeAttribute\", \"data-hcm-mobile-open\")")
	if closeStart < 0 {
		t.Fatal("the drawer close branch moved; update this contract")
	}
	closeBranch := src[closeStart:]
	if end := strings.Index(closeBranch, "trigger.Set(\"aria-expanded\", \"false\")"); end > 0 {
		closeBranch = closeBranch[:end]
	}
	if !strings.Contains(closeBranch, `backdrop.Set("hidden", true)`) || !strings.Contains(closeBranch, `backdrop.Get("style").Set("display", "none")`) {
		t.Errorf("the drawer close branch hides the backdrop attribute but not its inline display, so the layer stays over the page")
	}
}

// TestIssue_3564_DrawerTabEntersDrawer: while the mobile drawer is open, a Tab
// pressed from the trigger (outside the drawer) must be pulled into the drawer
// instead of reaching the page behind it. The focus trap listens on the drawer
// element only, so the document-level key handler owns that case.
func TestIssue_3564_DrawerTabEntersDrawer(t *testing.T) {
	body, err := os.ReadFile("popover_focus_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	keyStart := strings.Index(src, "key := js.FuncOf(")
	if keyStart < 0 {
		t.Fatal("the drawer key handler moved; update this contract")
	}
	handler := src[keyStart:]
	if end := strings.Index(handler, "resize := js.FuncOf("); end > 0 {
		handler = handler[:end]
	}
	if !strings.Contains(handler, `case "Tab":`) || !strings.Contains(handler, `root.Call("contains", active)`) || !strings.Contains(handler, `items[0].Call("focus")`) {
		t.Errorf("a Tab from the trigger is not pulled into the open drawer:\n%s", handler)
	}
}
