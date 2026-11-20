package main

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestIssue3917_QueryIsOneConversationsFilesAndNothingElse(t *testing.T) {
	query, err := sharedFilesQuery("design", "")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := chatsearch.Parse(query)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Filters.Conversation != "design" || parsed.Filters.Kind != chatsearch.File || parsed.Text != "" {
		t.Fatalf("query %q parses to %+v", query, parsed)
	}
	// Words narrow by name; an operator typed into the box is only a word, so it
	// can never widen the list to another conversation or a person's messages.
	hostile, err := sharedFilesQuery("design", `budget in:finance from:bob kind:message "quoted`)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = chatsearch.Parse(hostile)
	if err != nil {
		t.Fatalf("a name typed with operators must not break the query: %v", err)
	}
	if parsed.Filters.Conversation != "design" || parsed.Filters.Kind != chatsearch.File || parsed.Filters.Person != "" || len(parsed.Chips) != 2 {
		t.Fatalf("name text changed the filters: %q -> %+v", hostile, parsed)
	}
	for _, word := range []string{"budget", "finance", "bob", "quoted"} {
		if !strings.Contains(parsed.Text, word) {
			t.Fatalf("word %q lost from %q", word, parsed.Text)
		}
	}
	if _, err = sharedFilesQuery("", ""); err == nil {
		t.Fatal("no conversation must not become a workspace-wide file search")
	}
	if _, err = sharedFilesQuery(`bad"id`, ""); err == nil {
		t.Fatal("an id with a quote must be refused, not spliced into the query")
	}
}

func TestIssue3917_ResponseBecomesRowsOfThisConversationOnly(t *testing.T) {
	at := time.Date(2025, 11, 11, 9, 41, 0, 0, time.UTC)
	response := chatsearch.Response{Groups: []chatsearch.Group{{Kind: chatsearch.File, Rows: []chatsearch.Row{
		{Kind: chatsearch.File, ID: "p1:m1", Text: "brand-guide.pdf", AuthorID: "maya", At: at, Target: chatsearch.Target{ConversationID: "design", MessageID: "p1", ItemID: "m1", Sequence: 7}},
		{Kind: chatsearch.File, ID: "p2:m2", Text: "logo.png", AuthorID: "malik", At: at, Target: chatsearch.Target{ConversationID: "design", MessageID: "p2", ItemID: "m2", Sequence: 9, ThreadID: "p0", ThreadSequence: 3}},
		{Kind: chatsearch.File, ID: "p3:m3", Text: "other-room.pdf", Target: chatsearch.Target{ConversationID: "finance", MessageID: "p3", ItemID: "m3", Sequence: 2}},
		{Kind: chatsearch.File, ID: "p4:", Text: "no-attachment.pdf", Target: chatsearch.Target{ConversationID: "design", MessageID: "p4", Sequence: 2}},
	}}}, NextCursor: "next"}
	files, more, unavailable := sharedFilesFromResponse(response, "design")
	if unavailable || !more {
		t.Fatalf("unavailable=%v more=%v", unavailable, more)
	}
	if len(files) != 2 || files[0].Name != "brand-guide.pdf" || files[0].PostID != "p1" || files[0].MediaID != "m1" || files[0].Sequence != 7 || files[0].SenderID != "maya" {
		t.Fatalf("files = %+v", files)
	}
	if files[1].ThreadID != "p0" || files[1].ThreadSequence != 3 {
		t.Fatalf("a file in a thread must keep its thread anchors: %+v", files[1])
	}
	_, _, unavailable = sharedFilesFromResponse(chatsearch.Response{Unavailable: []chatsearch.Kind{chatsearch.File}}, "design")
	if !unavailable {
		t.Fatal("a file source that could not answer must be reported, not shown as an empty list")
	}
	_, _, unavailable = sharedFilesFromResponse(chatsearch.Response{Unavailable: []chatsearch.Kind{chatsearch.Voice}}, "design")
	if unavailable {
		t.Fatal("another source being down is not this list being down")
	}
}

func TestIssue3917_PagesAreAppendedOnceAndInOrder(t *testing.T) {
	a := chatui.SharedFile{ID: "1"}
	b := chatui.SharedFile{ID: "2"}
	c := chatui.SharedFile{ID: "3"}
	got := sharedFilesMerge([]chatui.SharedFile{a, b}, []chatui.SharedFile{b, c})
	if len(got) != 3 || got[0].ID != "1" || got[1].ID != "2" || got[2].ID != "3" {
		t.Fatalf("merge = %+v", got)
	}
}

func TestIssue3917_RowsAreFoundByIDForOpenAndDownload(t *testing.T) {
	model := chatui.Model{SelectedID: "design", SharedFiles: chatui.SharedFilesView{ConversationID: "design", Files: []chatui.SharedFile{{ID: "p1:m1", PostID: "p1", MediaID: "m1", Name: "brand guide.pdf"}}}}
	if f, ok := sharedFileByID(model, "p1:m1"); !ok || f.Name != "brand guide.pdf" {
		t.Fatalf("lookup = %+v %v", f, ok)
	}
	if name := sharedFileName(model, "p1", "m1"); name != "brand guide.pdf" {
		t.Fatalf("the download of a file that is not on screen needs its name from the list, got %q", name)
	}
	model.SharedFiles.ConversationID = "finance"
	if _, ok := sharedFileByID(model, "p1:m1"); ok {
		t.Fatal("another conversation's list answered a lookup")
	}
	if name := sharedFileName(model, "p1", "m1"); name != "" {
		t.Fatalf("another conversation's list named a download: %q", name)
	}
}
