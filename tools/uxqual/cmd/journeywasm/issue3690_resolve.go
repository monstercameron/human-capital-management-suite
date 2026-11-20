package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatsearchControlBase is the query the Refine controls are applied to. The
// controls show a name typed without quotes (from:@Julian Miller) as the one
// person it names, so the query is read the same way first; otherwise the
// second word of the name would stay behind as a search word when the person
// control replaces the filter (ISSUE-3690). A query that cannot be resolved is
// used as typed.
func chatsearchControlBase(model chatui.Model, typed string) string {
	if resolved, err := chatui.ResolveChatSearchQuery(model, typed); err == nil {
		return resolved
	}
	return typed
}
