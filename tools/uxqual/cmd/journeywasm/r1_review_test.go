package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// The filter form adds a channel, person, date or kind token to the typed query
// and removes the token it replaces. It used to remove the plain WORDS "in",
// "from", "on", "before", "after", "kind" and "lang" as well, because it split
// every token at a colon without asking whether there was one.
func TestR1_ReviewControlQueryKeepsOrdinaryWords(t *testing.T) {
	got, err := chatsearchControlQuery("meeting in room from friday", map[string]string{"channel": "General", "person": "Ana Lee", "on": "2026-10-01"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `meeting in room from friday in:"General" from:"Ana Lee" on:"2026-10-01"`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// The control still replaces a filter that was already typed, in any case.
	got, err = chatsearchControlQuery(`budget IN:#old from:@x`, map[string]string{"channel": "new"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := `budget from:@x in:"new"`; got != want {
		t.Fatalf("replace: got %q want %q", got, want)
	}
}

func r1Row(kind chatsearch.Kind, id string) chatsearch.Row {
	return chatsearch.Row{Kind: kind, ID: id, TenantID: "t"}
}

// "Show next results" adds the next page after the results on screen; it used to
// replace them with that page alone.
func TestR1_ReviewNextPageIsAppended(t *testing.T) {
	first := chatui.ChatSearchView{Query: "budget", Response: chatsearch.Response{NextCursor: "c1", Groups: []chatsearch.Group{
		{Kind: chatsearch.Message, Count: 2, Rows: []chatsearch.Row{r1Row(chatsearch.Message, "m1"), r1Row(chatsearch.Message, "m2")}},
	}}}
	next := chatui.ChatSearchView{Query: "budget", Response: chatsearch.Response{NextCursor: "", Groups: []chatsearch.Group{
		{Kind: chatsearch.Message, Count: 2, Rows: []chatsearch.Row{r1Row(chatsearch.Message, "m2"), r1Row(chatsearch.Message, "m3")}},
		{Kind: chatsearch.File, Count: 1, Rows: []chatsearch.Row{r1Row(chatsearch.File, "f1")}},
	}}}
	got := chatsearchAppendPage(first, "c1", next)
	if got.Response.NextCursor != "" || len(got.Response.Groups) != 2 {
		t.Fatalf("merged %+v", got.Response)
	}
	messages := got.Response.Groups[0]
	if messages.Kind != chatsearch.Message || messages.Count != 3 || len(messages.Rows) != 3 || messages.Rows[2].ID != "m3" {
		t.Fatalf("messages %+v", messages)
	}
	if got.Response.Groups[1].Kind != chatsearch.File || got.Response.Groups[1].Count != 1 {
		t.Fatalf("files %+v", got.Response.Groups[1])
	}
	if len(first.Response.Groups[0].Rows) != 2 {
		t.Fatal("the held first page was changed")
	}
	// A first page, a failed page and other words are not merged.
	if again := chatsearchAppendPage(first, "", next); len(again.Response.Groups[0].Rows) != 2 {
		t.Fatalf("first page merged: %+v", again.Response)
	}
	failed := next
	failed.Error = "error"
	if again := chatsearchAppendPage(first, "c1", failed); again.Error != "error" {
		t.Fatalf("failure lost: %+v", again)
	}
	other := next
	other.Query = "other"
	if again := chatsearchAppendPage(first, "c1", other); len(again.Response.Groups[0].Rows) != 2 {
		t.Fatalf("other words merged: %+v", again.Response)
	}
}
