package main

// shouldBlockUnsavedNavigation centralizes the guard decision so the browser
// enhancer and its cancellation contract can be tested without a DOM.
func shouldBlockUnsavedNavigation(unsaved bool, target, current string) bool {
	return unsaved && target != "" && target != current
}
