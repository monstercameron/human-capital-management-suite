package chatui

import (
	"strings"
	"testing"
)

// Issue 3864: after a recipient is chosen or removed, focus moves to a control
// that is inside the dialog, never to the page body.
func TestIssue3864_FocusTargetAfterPickAndRemove(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		want   string
	}{
		{"direct message pick lands on the chip's Remove button", chatissue3864PickTarget(DirectMessage), ".create-dialog .chip-remove"},
		{"group pick stays in the search box", chatissue3864PickTarget(GroupChat), "#new-chat-member-search"},
		{"channel pick stays in the search box", chatissue3864PickTarget(PublicChannel), "#new-chat-member-search"},
		{"removal returns to the search box", chatissue3864RemoveTarget(), "#new-chat-member-search"},
	} {
		if tc.target != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, tc.target, tc.want)
		}
	}
}

// The target a direct-message pick names exists in what the dialog renders once
// a person is chosen, and the search box it replaces does not.
func TestIssue3864_DirectMessagePickTargetIsRendered(t *testing.T) {
	m := chatux002Model("en-US")
	local := localUI{createKind: DirectMessage, picked: []mentionCandidate{{ID: "w-1", Name: "Darius"}}}
	got := renderNode(t, createDialog(m, handlers{local: local}))
	if !strings.Contains(got, "chip-remove") || strings.Contains(got, `id="new-chat-member-search"`) {
		t.Fatalf("direct message with one person: %s", got)
	}
	if !strings.Contains(got, "create-dialog") {
		t.Fatalf("target's dialog class is gone: %s", got)
	}
	local = localUI{createKind: GroupChat, picked: []mentionCandidate{{ID: "w-1", Name: "Darius"}}}
	if got := renderNode(t, createDialog(m, handlers{local: local})); !strings.Contains(got, `id="new-chat-member-search"`) {
		t.Fatalf("group keeps the search box: %s", got)
	}
}

// The choice and the removal are announced in a polite live region.
func TestIssue3864_SelectionAndRemovalAreAnnounced(t *testing.T) {
	for _, loc := range []string{"en-US", "de-DE", "ar"} {
		m := chatux002Model(loc)
		added := chatissue3864Note(m, "added", "Darius")
		removed := chatissue3864Note(m, "removed", "Darius")
		if !strings.Contains(added, "Darius") || !strings.Contains(removed, "Darius") || added == removed || strings.Contains(added, "chat.issue3864") {
			t.Fatalf("%s: %q / %q", loc, added, removed)
		}
		got := renderNode(t, createDialog(m, handlers{local: localUI{createKind: GroupChat, pickNote: added}}))
		if !strings.Contains(got, `role="status"`) || !strings.Contains(got, `aria-live="polite"`) || !strings.Contains(got, added) {
			t.Fatalf("%s: announcement not rendered: %s", loc, got)
		}
	}
	if en := chatissue3864Note(chatux002Model("en-US"), "added", "Darius"); chatissue3864Note(chatux002Model("de-DE"), "added", "Darius") == en {
		t.Fatal("de-DE announcement is the English text")
	}
}
