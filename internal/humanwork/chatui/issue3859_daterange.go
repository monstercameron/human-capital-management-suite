package chatui

// chatsearchInputError reports whether a search error code means the person's
// own query or filters were refused, as opposed to the service being down. These
// keep the search page drawn (fields, chips) and show the matching message;
// "daterange" (ISSUE-3859) is a reversed After/Before range.
func chatsearchInputError(code string) bool {
	return code == "invalid" || code == "meaning" || code == "daterange"
}
