package chatui

import (
	"strings"
	"testing"
)

// CHAT-01: Cancel and Escape both route to the exact same place -- render.go's
// keydown handler runs `act("cancel-edit", "")` for Escape while
// m.EditingID != "" (see rootKey), and the Cancel button in the edit form
// dispatches the identical "cancel-edit" action. Both are only as good as
// Callbacks.CancelEdit being wired, which chat_wasm.go did not do until this
// fix. These tests cover chatui's own half: the action reaches the callback,
// and the Cancel control reflects whether one is wired.
func TestCancelEditActionInvokesCallback(t *testing.T) {
	called := false
	m := Model{State: StateReady, SelectedID: "room", EditingID: "post-1",
		Callbacks: Callbacks{CancelEdit: func() { called = true }}}
	// This is exactly what render.go's rootKey does for Escape while an edit
	// is open, and what the Cancel button's data-action dispatches to.
	m.act("cancel-edit", "")
	if !called {
		t.Fatal("the cancel-edit action (what Escape and Cancel both dispatch) did not invoke Callbacks.CancelEdit")
	}
}

func TestCancelEditActionIsANoOpWithoutACallback(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "room", EditingID: "post-1"}
	// Must not panic when nothing is wired (the state before this fix).
	m.act("cancel-edit", "")
}

func TestEditFormCancelButtonReflectsCallbackWiring(t *testing.T) {
	base := Model{State: StateReady, SelectedID: "room", EditingID: "post-1",
		Messages:    []Message{{ID: "post-1", AuthorID: "me", Author: "Me", Body: "draft body"}},
		CurrentUser: "me"}

	unwired := base
	markup := render(t, unwired)
	if !strings.Contains(markup, `data-action="cancel-edit"`) {
		t.Fatal("edit form is missing its Cancel control")
	}
	if !strings.Contains(markup, `data-action="cancel-edit" disabled`) {
		t.Error("Cancel must render disabled while Callbacks.CancelEdit is nil, the same as every other action button in this file")
	}

	wired := base
	wired.Callbacks = Callbacks{CancelEdit: func() {}}
	markup = render(t, wired)
	if strings.Contains(markup, `data-action="cancel-edit" disabled`) {
		t.Fatal("Cancel stayed disabled once Callbacks.CancelEdit was wired")
	}
}
