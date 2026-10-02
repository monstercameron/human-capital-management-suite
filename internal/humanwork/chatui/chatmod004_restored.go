package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATMOD-004: "Restore puts it back and says so". A restore reached readers
// as an ordinary edit: the line "Removed by an administrator" turned back into
// the message with nothing to say why. The page now remembers, for as long as
// it is open, which removed messages came back, and writes one line under each
// of them. The line is a status, so a screen reader announces it once when it
// appears. A person who opens the conversation later sees the message as it
// always was; the author is told by a notice of their own either way.

// WithRestored returns the state with one more message marked as restored
// while this page was open. The map is copied: the state is shared by value.
func (s ModerationState) WithRestored(id string) ModerationState {
	if id == "" || s.Restored[id] {
		return s
	}
	next := make(map[string]bool, len(s.Restored)+1)
	for key := range s.Restored {
		next[key] = true
	}
	next[id] = true
	s.Restored = next
	return s
}

// WithoutRestored forgets that a message was restored: it was removed again,
// and the line under it would contradict what is on screen.
func (s ModerationState) WithoutRestored(id string) ModerationState {
	if !s.Restored[id] {
		return s
	}
	next := make(map[string]bool, len(s.Restored))
	for key := range s.Restored {
		if key != id {
			next[key] = true
		}
	}
	s.Restored = next
	return s
}

// chatmod004RestoredLine is the line under a message that was restored.
func chatmod004RestoredLine(locale string) ui.Node {
	return html.P(html.Props{Class: "chatremove-restored", Role: "status", Text: chatremoveText(locale, "restored_notice")})
}

// chatmod004WithRestoredLine puts the line at the end of a message row.
func chatmod004WithRestoredLine(m Model, row ui.Node) ui.Node {
	if row == nil {
		return row
	}
	row.Children = append(row.Children, chatmod004RestoredLine(m.Locale))
	return row
}

// chatmod004RestoredStyles: a quiet line under the text, in the text's column.
const chatmod004RestoredStyles = `
.chat-workspace .message>.chatremove-restored{grid-column:2;margin:4px 0 0;color:var(--hcm-color-text-muted);font-size:.8125rem}
`
