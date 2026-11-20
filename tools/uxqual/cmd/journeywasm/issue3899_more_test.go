package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func issue3899First() chatui.ChatSearchView {
	return chatui.ChatSearchView{Query: "png", Response: chatsearch.Response{NextCursor: "c1", Groups: []chatsearch.Group{
		{Kind: chatsearch.File, Count: 2, Rows: []chatsearch.Row{r1Row(chatsearch.File, "f1"), r1Row(chatsearch.File, "f2")}},
	}}}
}

// Asking for the next page keeps the results already read on screen.
func TestIssue3899_NextPageKeepsResultsWhileLoading(t *testing.T) {
	held := chatsearchBeginView(issue3899First(), "png", "c1")
	if held.Loading || !held.LoadingMore || len(held.Response.Groups) != 1 || len(held.Response.Groups[0].Rows) != 2 {
		t.Fatalf("results must stay while the next page loads: %+v", held)
	}
	// A first page, other words and a failed earlier answer show progress alone.
	if v := chatsearchBeginView(issue3899First(), "png", ""); !v.Loading || len(v.Response.Groups) != 0 {
		t.Fatalf("first page: %+v", v)
	}
	if v := chatsearchBeginView(issue3899First(), "other", "c1"); !v.Loading || len(v.Response.Groups) != 0 {
		t.Fatalf("other words: %+v", v)
	}
}

// The rows a page added are named, in the order the list shows them, so the
// first can be marked and focused.
func TestIssue3899_AppendedPageNamesItsNewResults(t *testing.T) {
	first := issue3899First()
	next := chatui.ChatSearchView{Query: "png", Response: chatsearch.Response{Groups: []chatsearch.Group{
		{Kind: chatsearch.File, Count: 2, Rows: []chatsearch.Row{r1Row(chatsearch.File, "f2"), r1Row(chatsearch.File, "f3")}},
	}}}
	got := chatsearchAppendPage(chatsearchBeginView(first, "png", "c1"), "c1", next)
	if len(got.Fresh) != 1 || got.Fresh[0] != chatui.Issue3899RowKey(r1Row(chatsearch.File, "f3")) || got.LoadingMore {
		t.Fatalf("fresh %v loadingMore %v", got.Fresh, got.LoadingMore)
	}
	if again := chatsearchAppendPage(first, "", next); len(again.Fresh) != 0 {
		t.Fatalf("a first page marks nothing: %v", again.Fresh)
	}
}
