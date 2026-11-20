package main

import (
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3797. The page counts messages as seen only while it is visible and
// has the focus. chatAttention is that state; the browser half
// (issue3797_attention_wasm.go) keeps it current from the page's own events.
var chatAttention struct {
	sync.Mutex
	hidden, blurred bool
}

// setChatAttention records whether the page is hidden and whether it lacks the
// focus, and says whether reading was allowed before and is allowed now.
func setChatAttention(hidden, blurred bool) (was, now bool) {
	chatAttention.Lock()
	defer chatAttention.Unlock()
	was = chatui.ChatReadsAllowed(!chatAttention.hidden, !chatAttention.blurred)
	chatAttention.hidden, chatAttention.blurred = hidden, blurred
	return was, chatui.ChatReadsAllowed(!hidden, !blurred)
}

// chatPageAway is true while the person is not looking at the page.
func chatPageAway() bool {
	chatAttention.Lock()
	defer chatAttention.Unlock()
	return !chatui.ChatReadsAllowed(!chatAttention.hidden, !chatAttention.blurred)
}

// chatFirstUnseen is the message the "New" line goes above when messages
// arrive while the page is away: the one already marked stays; otherwise the
// first message somebody else wrote, so the person's own never starts it.
func chatFirstUnseen(current, viewer string, posts []chatUnseenPost) string {
	if current != "" {
		return current
	}
	for _, post := range posts {
		if post.ID != "" && post.AuthorID != viewer {
			return post.ID
		}
	}
	return ""
}

type chatUnseenPost struct{ ID, AuthorID string }

// chatTabBadge is the count text in front of the tab title.
func chatTabBadge(model chatui.Model) string {
	unread, mentions := chatui.ChatAttentionCounts(model)
	return chatui.ChatAttentionTitlePrefix(model.Locale, unread, mentions)
}

// chatBadgedTabTitle puts the badge in front of the title.
func chatBadgedTabTitle(badge, title string) string { return badge + title }
