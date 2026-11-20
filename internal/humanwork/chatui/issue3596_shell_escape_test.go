package chatui

import "testing"

// Issue 3596: on a phone the Account menu lives in the conversation list's
// bar. Escape closes that menu (the shell's job); it must not also close the
// list, which hides the bar and strands focus on the page body.
func TestIssue3596_EscapeLeavesTheListWhileAShellMenuIsOpen(t *testing.T) {
	if got := chatEscapeStep(chatEscapeState{Sidebar: true}); got != "rail" {
		t.Fatalf("Escape with only the list open = %q, want rail", got)
	}
	if got := chatEscapeStep(chatEscapeState{Sidebar: true, ShellPopover: true}); got != "" {
		t.Fatalf("Escape with a shell menu open closed %q; the menu closes first and the list stays", got)
	}
	if got := chatEscapeStep(chatEscapeState{Sidebar: true, Thread: true, ShellPopover: true}); got != "" {
		t.Fatalf("Escape with a shell menu open closed %q, want nothing from Chat", got)
	}
}
