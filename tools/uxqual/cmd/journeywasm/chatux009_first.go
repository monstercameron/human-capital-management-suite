package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// chatux009FirstLoad reports whether this read is the page's first: nothing has
// been shown for any conversation yet, so the messages can arrive after the page
// does. A page that has shown messages, failed, or settled on an empty
// conversation reads its timeline in the same step as its list, as before.
func chatux009FirstLoad(previous chatui.Model) bool {
	return len(previous.Messages) == 0 && (previous.State == "" || previous.State == chatui.StateLoading)
}

// chatux009DeferTimeline reports whether a first load leaves the open
// conversation's messages to be read after the page is on screen: only for a
// conversation the reader belongs to, with no search open, no stream already
// delivering it, and not a preview of one they have not joined.
func chatux009DeferTimeline(firstLoad bool, model chatui.Model, selectedListed, streaming bool) bool {
	return firstLoad && model.SelectedID != "" && selectedListed && model.Search == "" && !streaming && model.PreviewConversation == nil
}
