package chatui

import (
	"strings"
	"testing"
)

// TestIssue3609_GateOverlayRulesOutsideWorkspaceScope pins the Gate dialog's
// rules outside the @scope (.chat-workspace) block. The client appends the
// overlay to document.body, where a scoped rule never matches, so the dialog
// was laid out static below the locked viewport.
func TestIssue3609_GateOverlayRulesOutsideWorkspaceScope(t *testing.T) {
	sheet := ScopedStylesheet()
	const overlay = ".chatgate-overlay{position:fixed;inset:0;z-index:1000;"
	if !strings.Contains(sheet, overlay) {
		t.Fatalf("the served stylesheet has no fixed Gate overlay rule")
	}
	scopeEnd := strings.Index(sheet, "}"+ChatUX013Global)
	if scopeEnd < 0 {
		t.Fatalf("the workspace scope block does not end where the global rules begin")
	}
	// The scoped copy inside the workspace block does not reach the body-level
	// dialog; a copy outside the block must.
	if !strings.Contains(sheet[scopeEnd:], overlay) {
		t.Fatalf("no Gate overlay rule sits outside the workspace scope, which ends at %d; the body-level dialog gets no fixed position", scopeEnd)
	}
	if !strings.Contains(sheet[scopeEnd:], ".chatgate{") {
		t.Fatalf("the Gate dialog panel rules are not outside the workspace scope")
	}
}
