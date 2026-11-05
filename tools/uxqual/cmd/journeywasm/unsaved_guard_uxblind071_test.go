package main

import "testing"

// UXBLIND-071: the leave guard must cover every protected surface, not just
// the worker-id form. The appearance editor declares its dirty state and its
// message on the protected root itself; the worker-id page declares the
// message on an inner form. The resolver below is the DOM-free contract both
// shapes share.
func TestTodo_UXBLIND_071_UnsavedGuardPrompt(t *testing.T) {
	cases := []struct {
		name        string
		state       unsavedGuardState
		wantMessage string
		wantGuard   bool
	}{
		{"appearance root carries its message", unsavedGuardState{Protected: true, Dirty: true, RootMessage: "You have unsaved appearance changes. Leave without saving?"}, "You have unsaved appearance changes. Leave without saving?", true},
		{"worker-id message comes from the inner form", unsavedGuardState{Protected: true, Dirty: true, FormMessage: "Unsaved changes"}, "Unsaved changes", true},
		{"root message wins over the form message", unsavedGuardState{Protected: true, Dirty: true, RootMessage: "root", FormMessage: "form"}, "root", true},
		{"fallback message when none is declared", unsavedGuardState{Protected: true, Dirty: true}, "You have unsaved changes. Leave this page?", true},
		{"clean form leaves freely", unsavedGuardState{Protected: true, RootMessage: "root"}, "", false},
		{"unprotected surface leaves freely", unsavedGuardState{Dirty: true, RootMessage: "root"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			message, guard := tc.state.prompt()
			if guard != tc.wantGuard || message != tc.wantMessage {
				t.Fatalf("prompt() = (%q, %t), want (%q, %t)", message, guard, tc.wantMessage, tc.wantGuard)
			}
		})
	}
}
