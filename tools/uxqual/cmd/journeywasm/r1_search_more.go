package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatsearch"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatsearchAppendPage puts the page that "Show next results" asked for after the
// results already on screen. The page used to replace them, so the first twenty
// matches vanished with no way back to them. A first page (no cursor), a failed
// page and an answer to different words stand as they are.
func chatsearchAppendPage(previous chatui.ChatSearchView, cursor string, outcome chatui.ChatSearchView) chatui.ChatSearchView {
	if cursor == "" || outcome.Error != "" || previous.Query != outcome.Query {
		return outcome
	}
	merged := outcome
	merged.Response.Groups = nil
	listed := map[string]bool{}
	for _, group := range previous.Response.Groups {
		group.Rows = append([]chatsearch.Row(nil), group.Rows...)
		for _, row := range group.Rows {
			listed[string(row.Kind)+"\x00"+row.ID] = true
		}
		merged.Response.Groups = append(merged.Response.Groups, group)
	}
	for _, group := range outcome.Response.Groups {
		at := -1
		for i, held := range merged.Response.Groups {
			if held.Kind == group.Kind {
				at = i
				break
			}
		}
		if at < 0 {
			merged.Response.Groups = append(merged.Response.Groups, chatsearch.Group{Kind: group.Kind, Rows: []chatsearch.Row{}})
			at = len(merged.Response.Groups) - 1
		}
		for _, row := range group.Rows {
			key := string(row.Kind) + "\x00" + row.ID
			if listed[key] {
				continue
			}
			listed[key] = true
			merged.Response.Groups[at].Rows = append(merged.Response.Groups[at].Rows, row)
		}
		merged.Response.Groups[at].Count = len(merged.Response.Groups[at].Rows)
	}
	// A source that failed on any page is still unavailable for the whole list.
	known := map[chatsearch.Kind]bool{}
	for _, kind := range merged.Response.Unavailable {
		known[kind] = true
	}
	for _, kind := range previous.Response.Unavailable {
		if !known[kind] {
			merged.Response.Unavailable = append(merged.Response.Unavailable, kind)
		}
	}
	return merged
}
