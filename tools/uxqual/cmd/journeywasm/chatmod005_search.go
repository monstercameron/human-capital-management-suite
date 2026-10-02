package main

import (
	"net/url"
	"strings"
)

// CHATMOD-005: the queue and its history are searchable. Typing in the
// Moderation page's search box hides the rows on the page that do not match;
// that never reached an item that is not on the page (the Open tab holds the
// open items only, and a tab holds at most what one read returns). Pressing
// Enter asks the server, which searches every item the person may open, open or
// resolved, and the page is read again with the answer.

// chatmodIsDialog reports whether an address of the moderation page names a
// dialog (remove, report, restore, message the author) and not the page.
func chatmodIsDialog(href string) bool {
	u, err := url.Parse(href)
	return err == nil && u.Query().Get("action") != ""
}

// chatmod005SearchHref is the address of the Moderation page showing what
// matches query: the page that is showing, with its tab and language, and the
// words. An empty query is the page without a search.
func chatmod005SearchHref(pageHref, query string) string {
	u, err := url.Parse(pageHref)
	if err != nil || u.Path == "" {
		return pageHref
	}
	q := u.Query()
	if query = strings.TrimSpace(query); query == "" {
		q.Del("query")
	} else {
		q.Set("query", query)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
