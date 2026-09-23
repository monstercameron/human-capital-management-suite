package journey

import (
	"strings"
	"testing"
)

// Native rendering retains a usable disclosure before WASM mounts. The live
// overlay is tested separately with keyboard interaction in the Codex browser.
func TestTodo_NAAS_001_ReviewAccessibility(t *testing.T) {
	css := Stylesheet()
	if rule := cssRule(t, css, ".jn-confirm-actionbar>.jn-confirm-cancel"); !strings.Contains(rule, "width:auto") || !strings.Contains(rule, "flex:0 0 auto") {
		t.Fatalf("text Cancel inherited the small square header-close sizing: %s", rule)
	}
	if rule := cssRule(t, css, ".jn-confirm[open]::details-content"); !strings.Contains(rule, "display:contents") || !strings.Contains(rule, "content-visibility:visible") {
		t.Fatalf("the open native disclosure can hide modal text from accessibility: %s", rule)
	}
	if rule := cssRule(t, css, ".jn-confirm-actionbar"); !strings.Contains(rule, "flex-wrap:wrap") {
		t.Fatalf("review buttons cannot wrap at narrow widths: %s", rule)
	}
	if rule := cssRule(t, css, ".jn-confirm-actionbar>.jn-btn"); !strings.Contains(rule, "flex-shrink:0") || !strings.Contains(rule, "white-space:normal") {
		t.Fatalf("long localized actions can squeeze Cancel or overflow: %s", rule)
	}
}
