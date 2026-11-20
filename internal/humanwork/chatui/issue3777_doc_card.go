package chatui

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// docEmbedGlyphPath draws the document card's type icon (a page with a folded
// corner and two text lines) so a document reads as a work object in the same
// card as a ticket or a journey (#3777).
const docEmbedGlyphPath = "M7 3h7l4 4v14H7zM14 3v4h4M9 12h6M9 16h6"

// chatcmd002WaitsOnViewer reports whether a live poll or to-do card is asking
// for the reader (#3777): an open poll the reader has not voted in, or an open
// list with an unfinished task assigned to them. A preview, a closed card, a
// reader outside the audience and a card the reader cannot act on never ask.
func chatcmd002WaitsOnViewer(m Model, view chat.Chatcmd002View, preview, closed bool) bool {
	if preview || closed || view.NotInAudience || m.Chatcmd002.Mutate == nil || strings.TrimSpace(m.CurrentUser) == "" {
		return false
	}
	switch {
	case view.Card.Poll != nil:
		return !view.Voted && len(view.MyOptions) == 0
	case view.Card.Todo != nil:
		for _, item := range view.Card.Todo.Items {
			if !item.Completed && item.AssigneeID == m.CurrentUser && view.CanTick[item.ID] {
				return true
			}
		}
	}
	return false
}

// issue3777DocCardStyles gives the document card the shared card's title and
// keeps the conversation list from crowding the timeline when a side panel is
// open on a 1280px screen (#3771): under the 1350px container query the list
// takes at most 248px, so the message column keeps about 620px beside the icon
// rail and a 340px panel. It follows the earlier container rule, so it wins.
const issue3777DocCardStyles = `.chat-workspace .chat-doc-embed .chat-embed-source{font-size:.9375rem;font-weight:700;color:var(--ink)}` +
	`@container chat (max-width:1350px){.chat-workspace[data-details-open="true"] .chat-layout{grid-template-columns:minmax(200px,min(var(--chat-rail),248px)) minmax(0,1fr) minmax(280px,340px)}}`
