package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
)

// Issue3899BoundaryID names the marker drawn above the first result a page of
// "Show next results" added. The browser client moves keyboard focus to the
// result after it, so a person continues at the seam instead of at the top.
const Issue3899BoundaryID = "chatsearch-boundary"

// Issue3899RowKey is how a result is told apart across pages.
func Issue3899RowKey(row chatsearch.Row) string { return string(row.Kind) + "\x00" + row.ID }

func issue3899Fresh(v ChatSearchView, row chatsearch.Row) bool {
	if len(v.Fresh) == 0 {
		return false
	}
	key := Issue3899RowKey(row)
	for _, fresh := range v.Fresh {
		if fresh == key {
			return true
		}
	}
	return false
}

// issue3899Boundary is the seam between the results already read and the ones
// just added. It says how many came in and how many are shown now, and when the
// last page is in it says that this is every result, which is what a person
// who just lost the "Show next results" control needs to hear.
func issue3899Boundary(locale string, v ChatSearchView) ui.Node {
	total, _ := chatSearchCount(&v)
	key := "added"
	if v.Response.NextCursor == "" {
		key = "addedEnd"
	}
	text := chatsearchText(locale, key)
	text = strings.ReplaceAll(text, "{n}", issue3899Count(locale, len(v.Fresh)))
	text = strings.ReplaceAll(text, "{total}", issue3899Count(locale, total))
	return html.Div(html.Props{ID: Issue3899BoundaryID, Class: "sr-only chatsearch-boundary", TabIndex: -1, Role: "status", Aria: map[string]string{"live": "polite"}, Text: text})
}

func issue3899Count(locale string, n int) string {
	return Model{Locale: locale}.n(n)
}
