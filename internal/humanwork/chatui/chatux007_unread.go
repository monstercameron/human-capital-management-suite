package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-007: what the conversation list says about unread and mentioned
// conversations.
//
// The data is already on every row: Conversation.Unread is the unread count and
// Conversation.Mentions the count of those that mention the viewer; the client
// fills both for every conversation from the recipient counts, not only for the
// open one. railRow draws a mention count when there is one and the unread count
// otherwise. The look is in chatux007_styles.go: an unread row is bold with a
// plain neutral count, a mention count is in the accent colour, and the selected
// row is the only filled row.

// The two jump directions. A conversation is unread-and-out-of-view above or
// below the part of the list that is on screen.
const (
	chatux007Up   = "up"
	chatux007Down = "down"
)

// chatux007Span is where one conversation row is on screen, top and bottom in
// the same coordinates as the list's visible part.
type chatux007Span struct{ Top, Bottom float64 }

// chatux007Hidden reports whether less than half of a row is on screen, and on
// which side. A row that is only clipped by a few pixels still shows its name,
// so it does not count as out of view.
func chatux007Hidden(viewTop, viewBottom float64, row chatux007Span) (above, below bool) {
	height := row.Bottom - row.Top
	if height <= 0 {
		return false, false
	}
	visible := min(row.Bottom, viewBottom) - max(row.Top, viewTop)
	if visible*2 >= height {
		return false, false
	}
	return row.Bottom <= viewTop+height/2, row.Top >= viewBottom-height/2
}

// chatux007OutOfView says whether any unread row is out of view above and
// below. Rows are the unread ones only, in list order.
func chatux007OutOfView(viewTop, viewBottom float64, rows []chatux007Span) (above, below bool) {
	for _, row := range rows {
		a, b := chatux007Hidden(viewTop, viewBottom, row)
		above, below = above || a, below || b
	}
	return above, below
}

// chatux007Target is the unread row a jump in direction goes to: the nearest
// one out of view on that side, or -1 when there is none.
func chatux007Target(direction string, viewTop, viewBottom float64, rows []chatux007Span) int {
	target := -1
	for i, row := range rows {
		above, below := chatux007Hidden(viewTop, viewBottom, row)
		switch {
		case direction == chatux007Down && below && target < 0:
			target = i
		case direction == chatux007Up && above:
			target = i
		}
	}
	return target
}

// chatux007Jump is one Jump to unread control. It is drawn closed and pinned to
// the top or the bottom edge of the list's scroll area; the client opens it
// only while an unread conversation is out of view on that side. It is always in
// the tree so that its siblings keep their places as unread counts come and go.
func chatux007Jump(m Model, direction string) ui.Node {
	label := chatux002Text(m, keyChatux007Jump)
	glyph := "arrow-down"
	if direction == chatux007Up {
		glyph = "arrow-up"
	}
	return html.Div(html.Props{Class: "chatux007-jump-slot chatux007-jump-" + direction},
		html.Button(html.Props{Class: "chatux007-jump", Type: "button", Hidden: true, Disabled: false,
			Data: map[string]string{"action": "jump-unread", "id": direction, "jump": direction},
			Aria: map[string]string{"label": label}},
			icon(glyph), html.Span(html.Props{Text: label})))
}
