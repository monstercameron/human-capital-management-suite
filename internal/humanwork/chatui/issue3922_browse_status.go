package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Browse channels announces its filter result (#3922). The dialog keeps one
// polite status region in the page. Once the person stops typing the host sets
// Model.BrowseAnnounce and the region says how many channels match, or that
// none do; while they type it is empty, so no keystroke is announced and focus
// never leaves the filter.

var issue3922Copy = map[string][3]string{
	"results": {"Results: {count}", "Ergebnisse: {count}", "النتائج: {count}"},
}

func issue3922Text(m Model, key string) string {
	copy := issue3922Copy[key]
	return chatbug039Text(key, copy[chatbug039LocaleIndex(m.Locale)], copy[0])
}

// issue3922BrowseStatus is the announcement for the channels now listed.
func issue3922BrowseStatus(m Model, shown int) string {
	if shown == 0 {
		if q := strings.TrimSpace(m.BrowseQuery); q != "" {
			return m.tf(KeyBrowseNoMatch, map[string]string{"query": q})
		}
		return m.t(KeyBrowseEmpty)
	}
	return chatcmd002Fill(issue3922Text(m, "results"), "count", chatPlural(m.Locale, "channels", shown))
}

// issue3922BrowseStatusNode is the status region; empty until the query settles.
func issue3922BrowseStatusNode(m Model, shown int) ui.Node {
	text := ""
	if m.BrowseAnnounce {
		text = issue3922BrowseStatus(m, shown)
	}
	return html.Div(html.Props{Class: "sr-only browse-status", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}, Text: text})
}
