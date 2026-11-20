package chatui

import "strings"

// chatsearchUnknownPerson reports a from: filter that names no one the page
// knows: a first name alone is not a name (a word that is not part of one is a
// search word), so the empty result says to type the full name (ISSUE-3691).
func chatsearchUnknownPerson(v ChatSearchView, person string) bool {
	return v.Context != nil && person != "" && chatsearchPersonName(v, person) == person
}

// chatsearchDisplayQuery is the query as the page should read it back: names
// typed without quotes become the one person or channel they name, so the chips
// and fields show "Julian Miller" and not "@Julian" with "Miller" left over as a
// search word (ISSUE-3691). Without the page's own model, or when the words
// cannot be resolved, the query is shown as typed.
func chatsearchDisplayQuery(v ChatSearchView) string {
	if v.Context == nil {
		return v.Query
	}
	resolved, err := ResolveChatSearchQuery(*v.Context, v.Query)
	if err != nil || strings.TrimSpace(resolved) == "" {
		return v.Query
	}
	return resolved
}

// chatsearchPersonName is the display name of a person id the directory knows,
// or the value unchanged.
func chatsearchPersonName(v ChatSearchView, id string) string {
	if v.Context == nil {
		return id
	}
	for _, p := range mentionIndex(*v.Context) {
		if p.id == id {
			return p.name
		}
	}
	return id
}
