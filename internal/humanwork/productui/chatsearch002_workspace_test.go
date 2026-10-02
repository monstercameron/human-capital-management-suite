package productui

import (
	"testing"
)

// TestTodo_CHATSEARCH_002_WorkspaceSearch: the workspace search box puts its
// query to Chat as well. Chat's rows are listed after the local ones for the
// same query, an answer for a query the reader has since changed is dropped,
// and Chat is asked only for a reader whose workspace shows them Chat.
func TestTodo_CHATSEARCH_002_WorkspaceSearch(t *testing.T) {
	local := []GlobalSearchItem{{ID: "page:people", Kind: "page", Label: "People"}}
	chat := []GlobalSearchItem{{ID: "chat:message:p1", Kind: "chat", KindLabel: "Message", Label: "The holiday calendar", Description: "#general", Href: "/workspace/app/chat#channel=general&at=12&message=p1"}}

	controller := newGlobalSearchController("")
	generation := controller.Edit("holiday")
	if !controller.Resolve(generation, "holiday", local) {
		t.Fatal("the local answer was refused")
	}
	version, accepted := controller.ResolveRemote(generation, "holiday", chat)
	if !accepted || version == 0 {
		t.Fatalf("Chat's answer was refused: %d %v", version, accepted)
	}
	results, query, current := controller.Results()
	if !current || query != "holiday" || len(results) != 2 || results[0].ID != "page:people" || results[1].ID != "chat:message:p1" {
		t.Fatalf("results=%+v query=%q current=%v", results, query, current)
	}
	// A second answer for the same query replaces the first and is a new version.
	if next, accepted := controller.ResolveRemote(generation, "holiday", nil); !accepted || next == version {
		t.Fatalf("a second answer: %d %v", next, accepted)
	}
	if results, _, _ = controller.Results(); len(results) != 1 {
		t.Fatalf("an empty answer from Chat left its rows: %+v", results)
	}
	controller.ResolveRemote(generation, "holiday", chat)

	// The reader types on: the rows on screen stay together until the new query
	// is answered, and Chat's late answer for the old query is dropped.
	next := controller.Edit("holidays")
	if results, _, current = controller.Results(); current || len(results) != 2 {
		t.Fatalf("while typing: %+v current=%v", results, current)
	}
	if _, accepted := controller.ResolveRemote(generation, "holiday", chat); accepted {
		t.Fatal("Chat's answer for a superseded query was accepted")
	}
	if _, accepted := controller.ResolveRemote(next, "holiday", chat); accepted {
		t.Fatal("Chat's answer for other words than the box holds was accepted")
	}
	controller.Resolve(next, "holidays", nil)
	if results, _, current = controller.Results(); !current || len(results) != 0 {
		t.Fatalf("the old query's Chat rows are listed under the new query: %+v", results)
	}
	// Choosing a destination empties everything.
	controller.ResolveRemote(next, "holidays", chat)
	controller.Clear()
	if results, _, _ = controller.Results(); len(results) != 0 {
		t.Fatalf("after a choice: %+v", results)
	}

	// Chat is asked only where the reader's workspace shows them Chat.
	asked := 0
	view := testView(PageHome)
	view.SearchChat = func(string, func([]GlobalSearchItem)) { asked++ }
	props := globalSearchProps(view)
	if visible := PageVisible(PageChat, view.Roles); (props.Remote != nil) != visible {
		t.Fatalf("Chat visible=%v but Remote set=%v", visible, props.Remote != nil)
	}
	view.Roles = []string{"no-such-role"}
	if PageVisible(PageChat, view.Roles) {
		t.Skip("every role sees Chat in this registry")
	}
	if props = globalSearchProps(view); props.Remote != nil {
		t.Fatal("Chat is asked for a reader who is not shown Chat")
	}
	if asked != 0 {
		t.Fatal("building the search asked Chat")
	}
	if globalSearchRemoteMinimum < 2 {
		t.Fatal("one letter is put to the service")
	}
}
