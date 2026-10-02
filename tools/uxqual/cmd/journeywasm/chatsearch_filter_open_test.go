package main

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATMOD_003_SearchResultOpens: a filter found by search opens the
// place it is managed, Conversation details with the filter settings, instead
// of nothing. The client used to call the settings view and drop what it
// returned.
func TestTodo_CHATMOD_003_SearchResultOpens(t *testing.T) {
	m := chatui.Model{SelectedID: "room", ShowThread: true, ThreadParentID: "post", ShowPerson: true, Search: "client names"}
	chatsearchModelOpenFilter(&m)
	if !m.ShowDetails || !m.ShowFilterSettings {
		t.Fatalf("a filter result does not open the details with the filter settings: %+v", m)
	}
	if m.ShowThread || m.ThreadParentID != "" || m.ShowPerson {
		t.Fatal("a thread or a person's card stays over the details the result opens")
	}
	if m.SelectedID != "room" || m.Search != "client names" {
		t.Fatal("opening a filter result changed the conversation or dropped the query")
	}
	// The click handler uses it, and no longer discards the settings view.
	source, err := os.ReadFile("chatsearch_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "chatBrowser.mutate(chatsearchModelOpenFilter)") {
		t.Error("the open handler does not show the details for a filter result")
	}
	if strings.Contains(string(source), "\t\t\tmodel.FilterSettings()\n") {
		t.Error("the open handler still calls the settings view and drops its result")
	}
}
