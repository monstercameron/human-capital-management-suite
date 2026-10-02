package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// chatsearchSettle puts the searched words on the model once a search has
// answered, whether it found matches or the service was unavailable. Either way
// the results area is what shows the outcome: it is where the quiet
// "Search is not available right now." line is drawn, so nothing is ever
// printed above the channel header. Clearing the box empties Search again and
// the conversation returns exactly as it was.
func chatsearchSettle(m *chatui.Model, query string) {
	m.Search, m.SearchLoading, m.SearchError = query, false, ""
}
