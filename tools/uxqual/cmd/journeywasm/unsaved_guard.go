package main

// shouldBlockUnsavedNavigation centralizes the guard decision so the browser
// enhancer and its cancellation contract can be tested without a DOM.
func shouldBlockUnsavedNavigation(unsaved bool, target, current string) bool {
	return unsaved && target != "" && target != current
}

// unsavedGuardFallbackMessage is the leave question when a protected surface
// declares its dirty state but no message of its own.
const unsavedGuardFallbackMessage = "You have unsaved changes. Leave this page?"

// unsavedGuardState is the DOM-declared dirty state of one protected
// surface: the appearance editor carries its message on the protected root
// itself, while the worker-id page carries it on an inner form.
type unsavedGuardState struct {
	Protected, Dirty         bool
	RootMessage, FormMessage string
}

// prompt resolves the leave-confirmation message. ok is false when the
// surface needs no guard and navigation may proceed without asking.
func (s unsavedGuardState) prompt() (message string, ok bool) {
	if !s.Protected || !s.Dirty {
		return "", false
	}
	if s.RootMessage != "" {
		return s.RootMessage, true
	}
	if s.FormMessage != "" {
		return s.FormMessage, true
	}
	return unsavedGuardFallbackMessage, true
}
