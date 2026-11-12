package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// chatux3708ListingAfterLeave is the Browse listing with a channel the reader
// has just left put back into it as a Join row (issue 3708). The listing holds
// only the channels the reader is not in, so without this a channel that left
// the rail vanished from Browse until the listing was read again. A listing
// never read this session stays nil: the read that follows decides what it holds.
func chatux3708ListingAfterLeave(listing []chatui.Conversation, left chatui.Conversation) []chatui.Conversation {
	if listing == nil || left.ID == "" {
		return listing
	}
	for _, c := range listing {
		if c.ID == left.ID {
			return listing
		}
	}
	left.Joined = false
	return append(append(make([]chatui.Conversation, 0, len(listing)+1), listing...), left)
}

// chatux3708RestoreBrowse records a channel the reader has just left in the
// Browse listing, and in the rows Browse shows when it is open for the query
// typed there.
func chatux3708RestoreBrowse(left chatui.Conversation) {
	if left.ID == "" {
		return
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		chatBrowser.browseAll = chatux3708ListingAfterLeave(chatBrowser.browseAll, left)
		if model.ShowBrowse && chatBrowser.browseAll != nil {
			model.Browse = filterChatBrowse(chatBrowser.browseAll, model.BrowseQuery)
		}
	})
}
