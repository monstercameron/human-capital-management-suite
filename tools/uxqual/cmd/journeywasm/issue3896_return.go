package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatsearchOrigin is where a person was in the results when they opened one
// (#3896): which result, and how far the results had been scrolled.
type chatsearchOrigin struct {
	// Target and Kind are the open control's data-target and data-kind.
	Target, Kind string
	Scroll       float64
}

// chatsearchReturnPlan says where "Return to results" lands. When the opened
// result is still among the results held, focus goes to its open control and
// the list is put back where it was. When it is not (the held results changed
// or it is no longer there) the results are shown from the top and focus goes
// to the labelled summary, never to the page.
func chatsearchReturnPlan(origin chatsearchOrigin, view chatui.ChatSearchView) (selector string, scroll float64) {
	if origin.Target != "" && chatsearchHolds(view, origin) {
		return `.chatsearch-open[data-target="` + origin.Target + `"][data-kind="` + origin.Kind + `"]`, origin.Scroll
	}
	return "#" + chatui.Issue3857SummaryID, 0
}

func chatsearchHolds(view chatui.ChatSearchView, origin chatsearchOrigin) bool {
	want := origin.Target + "\x00" + origin.Kind
	for _, group := range view.Response.Groups {
		for _, row := range group.Rows {
			if chatui.Issue3896RowToken(row) == want {
				return true
			}
		}
	}
	return false
}
