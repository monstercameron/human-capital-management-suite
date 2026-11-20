//go:build !(js && wasm)

package main

import "testing"

// Issue 3910: a note typed in the Saved panel is kept when the filter changes.
func TestIssue3910_NoteDraftSurvivesFilterChange(t *testing.T) {
	var drafts chatsaveNoteDrafts
	const field = "chatsave-note-field-p1"
	drafts.remember(field, "call Dana about benefits", "")
	if got, ok := drafts.recall(field); !ok || got != "call Dana about benefits" {
		t.Fatalf("typed note was lost: %q %v", got, ok)
	}
	// The field vanishes with the tab and the editor reopens: the draft is still there.
	drafts.remember(field, "", "old note")
	if got, ok := drafts.recall(field); !ok || got != "" {
		t.Fatalf("clearing the field by hand must be remembered as empty: %q %v", got, ok)
	}
}

func TestIssue3910_NoteDraftEqualToSavedNoteIsNotADraft(t *testing.T) {
	var drafts chatsaveNoteDrafts
	drafts.remember("chatsave-note-field-p1", "same", "same")
	if _, ok := drafts.recall("chatsave-note-field-p1"); ok {
		t.Fatal("an unchanged note is not a draft")
	}
}

func TestIssue3910_SaveAndCancelDiscardTheDraft(t *testing.T) {
	var drafts chatsaveNoteDrafts
	drafts.remember("chatsave-note-field-p1", "x", "")
	drafts.remember("chatsave-note-field-p2", "y", "")
	drafts.discard("chatsave-note-field-p1")
	if _, ok := drafts.recall("chatsave-note-field-p1"); ok {
		t.Fatal("cancel or save must drop the draft")
	}
	if _, ok := drafts.recall("chatsave-note-field-p2"); !ok {
		t.Fatal("another item's draft must stay")
	}
	drafts.reset()
	if _, ok := drafts.recall("chatsave-note-field-p2"); ok {
		t.Fatal("closing the panel drops drafts")
	}
}

// Issue 3878: the empty-state link moves focus to the tab it selects.
func TestIssue3878_EmptyLinkFocusesSelectedTab(t *testing.T) {
	for _, tab := range []string{"todo", "done", "all"} {
		if got := chatsaveTabFocusID(tab); got != "chatsave-tab-"+tab {
			t.Errorf("%s: %q", tab, got)
		}
	}
}

func TestIssue3910_RestoredDraftIsMarkedTypedOnlyWhenItDiffersFromTheSavedNote(t *testing.T) {
	if !chatsaveRestoreNeedsKeep("my draft", "") {
		t.Fatal("a restored draft that differs from the saved note must be kept against the field sweep")
	}
	if !chatsaveRestoreNeedsKeep("", "saved note") {
		t.Fatal("a draft cleared by the person must be kept against the field sweep")
	}
	if chatsaveRestoreNeedsKeep("same", "same") {
		t.Fatal("text equal to the saved note needs no protection")
	}
}
