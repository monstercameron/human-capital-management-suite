package main

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATMOD_005_Search: Enter in the Moderation page's search box reads
// the page again with the words, so the server searches the queue and its
// history; the tab, the language and the time zone of the page stay.
func TestTodo_CHATMOD_005_Search(t *testing.T) {
	page := chatui.ModerationPageHref + "?locale=de-DE&tab=resolved&tz=120"
	got, err := url.Parse(chatmod005SearchHref(page, "  client names "))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != chatui.ModerationPageHref || got.Query().Get("query") != "client names" || got.Query().Get("tab") != "resolved" || got.Query().Get("locale") != "de-DE" || got.Query().Get("tz") != "120" {
		t.Fatalf("the search address lost the words or the page it was asked on: %s", got)
	}
	// Searching again replaces the words; it does not add a second query.
	again, _ := url.Parse(chatmod005SearchHref(got.String(), "harassment"))
	if values := again.Query()["query"]; len(values) != 1 || values[0] != "harassment" {
		t.Fatalf("a second search carries %v", values)
	}
	// An emptied box is the page without a search.
	cleared, _ := url.Parse(chatmod005SearchHref(again.String(), "   "))
	if cleared.Query().Has("query") || cleared.Query().Get("tab") != "resolved" {
		t.Fatalf("clearing the box left %s", cleared)
	}
	// It is a page address: the dialog check must not take it for a dialog.
	if chatmodIsDialog(got.String()) || !chatmodIsDialog(chatui.ModerationPageHref+"?action=report&conversation=room&post=p") {
		t.Fatal("a search address reads as a dialog, or a dialog's as a page")
	}

	// The form's submit is wired to it, and no longer a no-op.
	source, err := os.ReadFile("chatremove_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "chatmod005SearchHref(b.pageHref,") {
		t.Error("Enter in the search box does not ask the server")
	}
}
