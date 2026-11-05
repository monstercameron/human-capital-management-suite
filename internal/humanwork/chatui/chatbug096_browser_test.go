package chatui_test

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func chatbug096BrowserRender(t *testing.T, locale string, view chatui.ChatSearchView) string {
	t.Helper()
	markup, err := ui.RenderToString(chatui.RenderChatSearch(locale, view))
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(markup)
}

// TestTodo_CHATBUG_096_Browser: a search whose sources answered all but one
// draws the rows that arrived and names only the source that truly failed, in
// one sentence with one Retry beside it, in every language.
func TestTodo_CHATBUG_096_Browser(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	words := map[string]struct{ sentence, retry string }{
		"en-US": {"Some content sources are unavailable: Saved item. Try again later.", "Try again"},
		"de-DE": {"Einige Inhaltsquellen sind nicht verfügbar: Gespeicherter Inhalt. Später erneut versuchen.", "Erneut versuchen"},
		"ar":    {"بعض مصادر المحتوى غير متاحة: عنصر محفوظ. حاول لاحقاً.", "حاول مرة أخرى"},
	}
	for locale, want := range words {
		view := chatui.ChatSearchView{Query: "holiday", Response: chatsearch.Response{
			Kinds: []chatsearch.Kind{chatsearch.Message, chatsearch.Saved},
			Groups: []chatsearch.Group{{Kind: chatsearch.Message, Count: 2, Rows: []chatsearch.Row{
				{Kind: chatsearch.Message, ID: "m1", AuthorID: "alice", At: at, Text: "The holiday schedule is posted", Target: chatsearch.Target{ConversationID: "people", MessageID: "m1", Sequence: 4}},
				{Kind: chatsearch.Message, ID: "m2", AuthorID: "alice", At: at, Text: "Holiday party on Friday", Target: chatsearch.Target{ConversationID: "general", MessageID: "m2", Sequence: 9}},
			}}},
			Unavailable: []chatsearch.Kind{chatsearch.Saved},
		}}
		got := chatbug096BrowserRender(t, locale, view)
		if !strings.Contains(got, `<mark class="search-hit">holiday</mark> schedule`) || !strings.Contains(got, `<mark class="search-hit">Holiday</mark> party`) {
			t.Errorf("%s: the rows that arrived are not drawn: %s", locale, got)
		}
		if n := strings.Count(got, want.sentence); n != 1 {
			t.Errorf("%s: the failed source is named %d times, want the one sentence %q", locale, n, want.sentence)
		}
		for _, other := range []string{"Agent answer", "Thread reply", "Pinned messages"} {
			if strings.Contains(got, other) {
				t.Errorf("%s: a source that answered is named unavailable: %q", locale, other)
			}
		}
		// The one sentence carries one Retry: a native button in the reader's
		// words, beside the sentence, inside the results' reading direction.
		if n := strings.Count(got, `data-chatsearch-action="retry"`); n != 1 {
			t.Errorf("%s: the partial failure carries %d retry controls, want one", locale, n)
		}
		if !strings.Contains(got, ">"+want.retry+"<") {
			t.Errorf("%s: the partial failure has no %q control: %s", locale, want.retry, got)
		}
		if at := strings.Index(got, `data-chatsearch-action="retry"`); at < 0 || strings.Index(got, want.sentence) > at {
			t.Errorf("%s: the retry is not beside the sentence", locale)
		} else if open := strings.LastIndex(got[:at], "<button"); open < 0 || strings.Contains(got[open:at], ">") {
			t.Errorf("%s: the retry is not a native button: %s", locale, got)
		} else if end := strings.Index(got[open:], ">"); end < 0 || !strings.Contains(got[open:open+end], `type="button"`) {
			t.Errorf("%s: the retry is not a native button: %s", locale, got)
		}
		dir := `dir="ltr"`
		if strings.HasPrefix(locale, "ar") {
			dir = `dir="rtl"`
		}
		if retry := strings.Index(got, `data-chatsearch-action="retry"`); retry < 0 || !strings.Contains(got[:retry], dir) {
			t.Errorf("%s: the retry is outside the %s results", locale, dir)
		}
	}
}
