package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatsearchDelay is how long typing pauses before a search goes out, so a
// word typed letter by letter is one request and not one per keystroke.
const chatsearchDelay = 250

// chatsearchFlow is the search's own state, kept apart from the browser so
// every transition can be tested without one. A search is identified by what
// is sent to the service (the words with channel and person names resolved),
// so the same words asked twice, or asked again when the directory finishes
// loading, are the same search.
type chatsearchFlow struct {
	// Query is the words in the box that the results belong to; empty means no
	// search is active.
	Query string
	// Key is the request Query resolves to.
	Key string
	// Generation grows with every new search and every clear; an answer that
	// carries an older one is dropped.
	Generation uint64
	// Pending is true from the moment a search starts until it answers.
	Pending bool
	// Settled is true once Key's outcome, results or the one failure line, is
	// on screen.
	Settled bool
	// Opened is true while a result's conversation is on screen.
	Opened bool
}

// begin starts a search for query, resolving to key. send is false when that
// same search is already running or already answered while its words are still
// in the box: nothing is asked again. retry forces a new request.
func (f *chatsearchFlow) begin(query, key string, active, retry bool) (generation uint64, send bool) {
	if !retry && active && f.Query == query && f.Key == key && (f.Pending || f.Settled) {
		f.Opened = false
		return f.Generation, false
	}
	f.Generation++
	f.Query, f.Key, f.Pending, f.Settled, f.Opened = query, key, true, false, false
	return f.Generation, true
}

// complete records the end of search generation, answered or failed. It is
// false for a completion that was overtaken by a newer search or by clearing
// the box; such a completion, a cancellation arriving late included, changes
// nothing. A failed search ends its pending state but is not held as an answer,
// so asking again sends again.
func (f *chatsearchFlow) complete(generation uint64, failed bool) bool {
	if generation != f.Generation || f.Query == "" {
		return false
	}
	f.Pending, f.Settled = false, !failed
	return true
}

// current reports that generation is still the search the box is waiting for.
func (f *chatsearchFlow) current(generation uint64) bool {
	return generation == f.Generation && f.Query != ""
}

// chatsearchComplete applies the end of search generation: the flow stops
// pending, the outcome is held for a repeat of the same search, and it is put
// on the model, results or the one failure line, provided the box still holds
// its words. It is false when the completion was overtaken.
func chatsearchComplete(f *chatsearchFlow, held *chatui.ChatSearchView, m *chatui.Model, generation uint64, outcome chatui.ChatSearchView) bool {
	if !f.complete(generation, outcome.Error != "") {
		return false
	}
	*held = outcome
	return chatsearchModelShow(m, outcome)
}

// clear ends the search: the box is empty, the conversation shows again and
// any answer still on its way is dropped.
func (f *chatsearchFlow) clear() {
	*f = chatsearchFlow{Generation: f.Generation + 1}
}

// open records that a result's conversation is on screen.
func (f *chatsearchFlow) open() bool {
	if f.Query == "" {
		return false
	}
	f.Opened = true
	return true
}

// back returns from a result's conversation to the results. It is false when
// there is nothing to return to.
func (f *chatsearchFlow) back() bool {
	if f.Query == "" || !f.Opened {
		return false
	}
	f.Opened = false
	return true
}

// chatsearchModelBegin puts a search on the model at once: the words stay in
// the box, the results area appears and says it is searching, and the
// conversation behind it is untouched.
func chatsearchModelBegin(m *chatui.Model, view chatui.ChatSearchView) {
	m.Search, m.SearchLoading, m.SearchError = view.Query, false, ""
	m.ChatSearch, m.SearchOpened = &view, false
	m.FocusMessageID = ""
}

// chatsearchModelShow draws a search outcome, provided the box still holds the
// words it answers; a box that was cleared meanwhile stays cleared.
func chatsearchModelShow(m *chatui.Model, view chatui.ChatSearchView) bool {
	if m.Search != view.Query {
		return false
	}
	m.ChatSearch = &view
	return true
}

// chatsearchModelOpened shows the conversation of an opened result while the
// query stays in the box and the results stay behind the "Return to results"
// bar.
func chatsearchModelOpened(m *chatui.Model, view chatui.ChatSearchView) {
	m.Search, m.ChatSearch, m.SearchOpened = view.Query, &view, true
}

// chatsearchModelOpenFilter shows where a filter result is managed: the
// Conversation details of its channel, with the filter settings open under
// Manage channel. A filter result used to open nothing at all.
func chatsearchModelOpenFilter(m *chatui.Model) {
	m.ShowDetails, m.ShowFilterSettings = true, true
	m.ShowPerson, m.ShowThread, m.ThreadParentID = false, false, ""
}

// chatsearchModelBack shows the results again in place of the conversation.
func chatsearchModelBack(m *chatui.Model) {
	m.SearchOpened = false
}

// chatsearchModelClear empties the search: the conversation that was open
// shows again, with no results area and no bar.
func chatsearchModelClear(m *chatui.Model) {
	m.Search, m.SearchLoading, m.SearchError = "", false, ""
	m.ChatSearch, m.SearchOpened = nil, false
}

// chatsearchOpenNavigation is the browser-history entry for opening a result:
// the conversation the result is in, so the address names the conversation on
// screen and not the one the search began from.
func chatsearchOpenNavigation(model chatui.Model, target chatsearch.Target) chatNavigationState {
	navigation := model
	if target.ConversationID != "" {
		navigation.SelectedID = target.ConversationID
	}
	navigation.Search = ""
	navigation.ShowDetails, navigation.ShowPerson = false, false
	navigation.ShowThread, navigation.ThreadParentID = false, ""
	navigation.FocusMessageID = target.MessageID
	if target.ThreadID != "" && target.Sequence > 0 {
		navigation.ShowThread, navigation.ThreadParentID = true, target.ThreadID
	}
	state := chatNavigationStateFromModel(navigation)
	if target.MessageID != "" && target.Sequence > 0 && !state.ShowThread {
		state.FocusSequence = target.Sequence
	}
	return state
}
