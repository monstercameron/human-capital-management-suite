package main

import (
	"net/url"
	"strings"
)

// Issues #3948 and #3887: what closing a report dialog does, and what a dialog
// that is still loading is called. The decisions are here, apart from the page,
// so they can be tested without a browser (issue3948_discard_wasm.go acts on them).

// issue3948CloseStep decides what a request to close the dialog (Escape, Cancel,
// Close, a press on the backdrop) does. While the question "Discard this
// report?" is up, closing again keeps the report ("keep"): the safe answer is
// the one that needs no choosing. A report with a reason or details asks
// ("ask"). Anything else closes at once ("close"), which keeps an empty form
// easy to dismiss.
func issue3948CloseStep(report, dirty, asking bool) string {
	switch {
	case asking:
		return "keep"
	case report && dirty:
		return "ask"
	}
	return "close"
}

// issue3948Dirty reports whether a report holds a chosen reason or a note with
// more than spaces in it. A report that is being sent is not asked about: it is
// already on its way.
func issue3948Dirty(reason, note string, sending bool) bool {
	return !sending && (strings.TrimSpace(reason) != "" || strings.TrimSpace(note) != "")
}

// issue3887ActionOf is the action (report, remove, restore, message) a dialog
// address names, so the loading frame can be named for what it will do.
func issue3887ActionOf(href string) string {
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("action")
}
