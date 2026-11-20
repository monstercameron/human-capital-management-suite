package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

func issue3900Rows(n int) []chatsearch.Group {
	rows := make([]chatsearch.Row, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, chatsearch.Row{Kind: chatsearch.File, ID: "f" + string(rune('a'+i)), Text: "photo.png"})
	}
	return []chatsearch.Group{{Kind: chatsearch.File, Count: n, Rows: rows}}
}

// A page that has more behind it must not read as a final total.
func TestIssue3900_CountSaysLoadedSoFarWhileMoreRemain(t *testing.T) {
	cases := map[string]struct{ more, last string }{
		"en-US": {"3 results shown, more available", "3 results"},
		"de-DE": {"3 Ergebnisse angezeigt, weitere verfügbar", "3 Ergebnisse"},
		"ar":    {"", "3"},
	}
	for locale, want := range cases {
		m := chatbug077Model()
		m.Locale = locale
		view := ChatSearchView{Query: "png", Response: chatsearch.Response{NextCursor: "c1", Groups: issue3900Rows(3)}}
		m.Search, m.ChatSearch = "png", &view
		more := searchCountLabel(m)
		if want.more != "" && more != want.more {
			t.Fatalf("%s: more pages remain, count reads %q, want %q", locale, more, want.more)
		}
		view.Response.NextCursor = ""
		final := searchCountLabel(m)
		if final == more || !strings.Contains(final, m.n(3)) {
			t.Fatalf("%s: at the end the count reads %q (more-available was %q)", locale, final, more)
		}
		if want.more == "" && (more == "" || more == final) {
			t.Fatalf("%s: arabic must also tell loaded from final: %q vs %q", locale, more, final)
		}
		if want.more != "" && final != want.last {
			t.Fatalf("%s: final count %q, want %q", locale, final, want.last)
		}
	}
	// The page header carries it, once.
	m := chatbug077Model()
	view := ChatSearchView{Query: "png", Response: chatsearch.Response{NextCursor: "c1", Groups: issue3900Rows(3)}}
	m.Search, m.ChatSearch = "png", &view
	if page := renderNode(t, Build(m)); strings.Count(page, "3 results shown, more available") != 1 {
		t.Fatalf("header does not state the loaded count: %s", page)
	}
}
