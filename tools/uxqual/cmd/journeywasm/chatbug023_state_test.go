package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATBUG-023 and CHATBUG-022 (client): every transition of the search, with no
// browser. One request per search, a clear that cannot be undone by a late
// answer, a return that survives until the box is cleared, and an address that
// names the conversation on screen.
func TestTodo_CHATBUG_023(t *testing.T) {
	var f chatsearchFlow

	// Typing "holiday" and then asking again for the same words (the directory
	// finishing its load re-runs the active query) is one request.
	g1, send := f.begin("holiday", "holiday\x00", false, false)
	if !send {
		t.Fatal("the first search was not sent")
	}
	if _, again := f.begin("holiday", "holiday\x00", true, false); again {
		t.Fatal("the same search, still running, was sent twice")
	}
	if !f.complete(g1, false) {
		t.Fatal("the answer to the current search was dropped")
	}
	if _, again := f.begin("holiday", "holiday\x00", true, false); again {
		t.Fatal("the same search, already answered, was sent again")
	}
	if _, retry := f.begin("holiday", "holiday\x00", true, true); !retry {
		t.Fatal("Retry did not ask again")
	}
	// Names resolved by the directory change the request: that is a new search.
	g2, send := f.begin("holiday in:#general", "holiday in:g1\x00", true, false)
	if !send || g2 == g1 {
		t.Fatal("a different request was not sent")
	}
	// A box that no longer holds the words (cleared, or another room opened)
	// does not suppress asking again.
	f.complete(g2, false)
	if _, send = f.begin("holiday in:#general", "holiday in:g1\x00", false, false); !send {
		t.Fatal("a search whose words left the box was served from a stale answer")
	}

	// A late answer cannot bring back a cleared search.
	late, _ := f.begin("survey", "survey\x00", true, false)
	f.clear()
	if f.complete(late, false) {
		t.Fatal("an answer arriving after the box was cleared was accepted")
	}
	if f.Query != "" || f.Opened || f.Pending || f.Settled {
		t.Fatalf("clear left state behind: %+v", f)
	}

	// Open a result, return to the results, open again, clear.
	g, _ := f.begin("holiday", "holiday\x00", false, false)
	f.complete(g, false)
	if f.back() {
		t.Fatal("returned from a conversation that was never opened")
	}
	if !f.open() || !f.Opened {
		t.Fatal("opening a result was not recorded")
	}
	if !f.back() || f.Opened {
		t.Fatal("Return to results did not return")
	}
	f.open()
	f.clear()
	if f.back() || f.open() {
		t.Fatal("the return bar outlives the search")
	}

	// The model: the words stay in the box and the area says it is searching.
	m := chatui.Model{SelectedID: "general", Search: "", SearchError: "stale"}
	chatsearchModelBegin(&m, chatui.ChatSearchView{Query: "holiday", Loading: true})
	if m.Search != "holiday" || m.ChatSearch == nil || !m.ChatSearch.Loading || m.SearchOpened || m.SearchError != "" || m.SelectedID != "general" {
		t.Fatalf("begin: %+v", m)
	}
	answer := chatui.ChatSearchView{Query: "holiday", Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Message, ID: "m"}}}}}}
	if !chatsearchModelShow(&m, answer) || m.ChatSearch.Loading || len(m.ChatSearch.Response.Groups) != 1 {
		t.Fatalf("show: %+v", m.ChatSearch)
	}
	// Opened: the query stays, the bar can lead back.
	chatsearchModelOpened(&m, answer)
	if m.Search != "holiday" || !m.SearchOpened || m.ChatSearch == nil {
		t.Fatalf("opened: %+v", m)
	}
	chatsearchModelBack(&m)
	if m.SearchOpened || m.Search != "holiday" || m.ChatSearch == nil {
		t.Fatalf("back: %+v", m)
	}
	chatsearchModelOpened(&m, answer)
	// Clearing returns to the conversation that was open and removes the bar.
	chatsearchModelClear(&m)
	if m.Search != "" || m.ChatSearch != nil || m.SearchOpened || m.SelectedID != "general" {
		t.Fatalf("clear: %+v", m)
	}
	// An answer for words that left the box is not drawn.
	if chatsearchModelShow(&m, answer) || m.ChatSearch != nil {
		t.Fatal("a late answer reopened a cleared search")
	}

	// The address: opening a result in another conversation records that
	// conversation, not the one the search began from.
	model := chatui.Model{CurrentTenantID: "tenant", CurrentUser: "walt", SelectedID: "general", ShowDetails: true}
	state := chatsearchOpenNavigation(model, chatsearch.Target{ConversationID: "policy-helper-dm", MessageID: "m9", Sequence: 12})
	if state.ConversationID != "policy-helper-dm" || state.FocusMessageID != "m9" || state.FocusSequence != 12 || state.ShowDetails || !validChatNavigationState(state) {
		t.Fatalf("message navigation: %+v", state)
	}
	state = chatsearchOpenNavigation(model, chatsearch.Target{ConversationID: "incident", MessageID: "r2", Sequence: 7, ThreadID: "p1", ThreadSequence: 3})
	if state.ConversationID != "incident" || !state.ShowThread || state.ThreadParentID != "p1" || !validChatNavigationState(state) {
		t.Fatalf("thread navigation: %+v", state)
	}
	state = chatsearchOpenNavigation(model, chatsearch.Target{ConversationID: "random"})
	if state.ConversationID != "random" || state.FocusMessageID != "" {
		t.Fatalf("conversation navigation: %+v", state)
	}
	if got := chatChannelFragmentValue([]chatui.Conversation{{ID: "g1", Name: "general", Kind: chatui.PublicChannel}, {ID: "dm", Name: "Policy Helper", Kind: chatui.DirectMessage}}, state.ConversationID); got != "random" {
		t.Fatalf("an unlisted room keeps its id in the address: %q", got)
	}
}

// CHATBUG-022 (client): typing starts a debounced search, Enter starts a second
// for the same words, the first is cancelled, and its cancellation arrives after
// the second's success. The second's results must be shown, "Searching" must
// end, and nothing the cancelled one does may undo that.
func TestTodo_CHATBUG_022(t *testing.T) {
	const query, key = "survey", "survey\x00"
	var f chatsearchFlow
	var held chatui.ChatSearchView
	m := chatui.Model{SelectedID: "general"}
	results := chatui.ChatSearchView{Query: query, Response: chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 1, Rows: []chatsearch.Row{{Kind: chatsearch.Message, ID: "m1", Text: "survey closes tonight"}}}}}}
	begin := func(retry bool) (uint64, bool) {
		g, send := f.begin(query, key, strings.TrimSpace(m.Search) == query, retry)
		if send {
			held = chatui.ChatSearchView{Query: query, Loading: true}
		}
		chatsearchModelBegin(&m, held)
		return g, send
	}

	first, send := begin(false)
	if !send || !m.ChatSearch.Loading {
		t.Fatalf("the typed search did not start: %+v", m.ChatSearch)
	}
	// Enter asks for the same words again while the first is still running.
	if _, again := begin(false); again {
		t.Fatal("Enter sent a second request for the same words")
	}
	// Even when a second is forced (Retry), the first is overtaken, not the second.
	second, send := begin(true)
	if !send || second == first {
		t.Fatal("the forced second search did not start")
	}
	if !chatsearchComplete(&f, &held, &m, second, results) {
		t.Fatal("the success of the current search was dropped")
	}
	if m.ChatSearch.Loading || len(m.ChatSearch.Response.Groups) != 1 || f.Pending || !f.Settled {
		t.Fatalf("Searching did not end with the results shown: %+v pending=%v settled=%v", m.ChatSearch, f.Pending, f.Settled)
	}
	// The cancelled first request now reports its cancellation.
	cancelled := chatui.ChatSearchView{Query: query, Error: "error"}
	if chatsearchComplete(&f, &held, &m, first, cancelled) {
		t.Fatal("the cancellation of the overtaken request was applied")
	}
	if m.ChatSearch.Loading || m.ChatSearch.Error != "" || len(m.ChatSearch.Response.Groups) != 1 || len(held.Response.Groups) != 1 || !f.Settled {
		t.Fatalf("the late cancellation undid the results: %+v", m.ChatSearch)
	}

	// The same query submitted again with the answer held shows it at once and
	// asks for nothing.
	again, send := begin(false)
	if send || again != second {
		t.Fatal("a held answer was asked for again")
	}
	if m.ChatSearch.Loading || len(m.ChatSearch.Response.Groups) != 1 || m.SearchOpened {
		t.Fatalf("the held results were not shown at once: %+v", m.ChatSearch)
	}

	// A failed request ends "Searching" with the one failure line, and is not
	// held: asking again sends again.
	chatsearchModelClear(&m)
	f.clear()
	held = chatui.ChatSearchView{}
	failing, send := begin(false)
	if !send {
		t.Fatal("the search after a clear did not start")
	}
	failed := chatui.ChatSearchView{Query: query, Error: "error"}
	if !chatsearchComplete(&f, &held, &m, failing, failed) {
		t.Fatal("the failure of the current search was dropped")
	}
	if m.ChatSearch.Loading || m.ChatSearch.Error != "error" || f.Pending {
		t.Fatalf("Searching did not end on failure: %+v", m.ChatSearch)
	}
	if _, retry := begin(false); !retry {
		t.Fatal("a failed search was held as if it had been answered")
	}
	// A timeout is the current search's own failure, not an overtaking.
	if !f.current(f.Generation) || f.current(f.Generation-1) {
		t.Fatal("current() mixes up searches")
	}
}
