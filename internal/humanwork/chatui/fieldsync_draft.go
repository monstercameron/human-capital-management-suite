package chatui

// fieldDraftKept reports whether a sweep of the field observer (fieldsync_js.go)
// must leave a box's text alone. The person typed in the box and then moved
// focus away, so the box holds an unsaved draft, while the application's value
// (want) is the same one the box was last given (applied). Unrelated re-renders
// of the same panel must not write the saved value back over that draft; only a
// change of the application's own value, seen on its attribute, may replace it.
func fieldDraftKept(sweep, typed, hasApplied bool, applied, want string) bool {
	return sweep && typed && hasApplied && applied == want
}
