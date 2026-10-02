package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// One request per submitted query: the same words asked again while the first
// is on its way, or after it answered, are not sent again, whatever the number
// of renders that ask; a retry and a changed query are.
func TestTodo_CHATBUG_022_OneRequestPerQuery(t *testing.T) {
	var flow chatsearchFlow
	sent := 0
	ask := func(query string, active, retry bool) uint64 {
		generation, send := flow.begin(query, query, active, retry)
		if send {
			sent++
		}
		return generation
	}
	first := ask("holiday", false, false)
	for i := 0; i < 5; i++ {
		ask("holiday", true, false)
	}
	if sent != 1 {
		t.Fatalf("a re-render asked again while the search was waiting: %d requests", sent)
	}
	if !flow.complete(first, false) {
		t.Fatal("the answer to the only request was refused")
	}
	for i := 0; i < 5; i++ {
		ask("holiday", true, false)
	}
	if sent != 1 {
		t.Fatalf("a re-render asked again after the answer: %d requests", sent)
	}
	ask("holiday pay", true, false)
	if sent != 2 {
		t.Fatalf("a changed query was not sent: %d requests", sent)
	}
}

// A failure is not an answer: asking again sends again, and the retry button is
// that asking. A late answer to an older query changes nothing.
func TestTodo_CHATBUG_022_FailureRetriesAndLateAnswersAreDropped(t *testing.T) {
	var flow chatsearchFlow
	older, send := flow.begin("holiday", "holiday", false, false)
	if !send {
		t.Fatal("the first search was not sent")
	}
	newer, send := flow.begin("holiday pay", "holiday pay", true, false)
	if !send || newer == older {
		t.Fatalf("the second search was not a new request: %d %t", newer, send)
	}
	if flow.complete(older, false) || flow.current(older) {
		t.Fatal("the answer to the older query was accepted after a newer search began")
	}

	view := chatui.ChatSearchView{Query: "holiday pay"}
	model := chatui.Model{Search: "holiday pay"}
	failure := chatui.ChatSearchView{Query: "holiday pay", Error: "error"}
	if !chatsearchComplete(&flow, &view, &model, newer, failure) || model.ChatSearch == nil || model.ChatSearch.Error != "error" {
		t.Fatalf("the failure was not put on screen: %+v", model.ChatSearch)
	}
	if _, send := flow.begin("holiday pay", "holiday pay", true, false); send {
		// A failed search is not held as an answer, so asking again sends again.
	} else {
		t.Fatal("asking again after a failure did not send")
	}
	retry, send := flow.begin("holiday pay", "holiday pay", true, true)
	if !send {
		t.Fatal("Try again did not send")
	}

	// The box is cleared while the retry is on its way: its answer is dropped
	// and does not bring the results back.
	flow.clear()
	model = chatui.Model{Search: ""}
	if chatsearchComplete(&flow, &view, &model, retry, chatui.ChatSearchView{Query: "holiday pay"}) || model.ChatSearch != nil {
		t.Fatal("an answer arrived after the box was cleared and was drawn")
	}
}
