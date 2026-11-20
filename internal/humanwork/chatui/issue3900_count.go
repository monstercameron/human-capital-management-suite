package chatui

import "strings"

// issue3900CountLabel is the count above the search results. With another page
// available it reads as the number loaded so far ("20 results shown, more
// available"), so a person never takes a first page for the whole answer; once
// the last page is in it is a plain total.
func issue3900CountLabel(m Model, n int, more bool) string {
	key := "count"
	switch {
	case more && n == 1:
		key = "countMoreOne"
	case more:
		key = "countMore"
	case n == 1:
		key = "countOne"
	}
	return strings.ReplaceAll(chatsearchText(m.Locale, key), "{n}", m.n(n))
}
