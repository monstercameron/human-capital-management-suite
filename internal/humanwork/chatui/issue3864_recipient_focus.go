package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Issue 3864: choosing or removing a recipient removes the control that had
// focus, so focus moves on purpose and the change is announced.

// chatissue3864PickTarget is the control that takes focus after a person is
// chosen. A direct message holds one person, so its search box goes away and the
// chip's Remove button (inside the dialog, safe to press Enter on) takes focus;
// other kinds keep the search box for the next person.
func chatissue3864PickTarget(kind ConversationKind) string {
	if kind == DirectMessage {
		return ".create-dialog .chip-remove"
	}
	return "#new-chat-member-search"
}

// chatissue3864RemoveTarget is where focus goes after a Remove button is used:
// the search box, which is back once the chip is gone.
func chatissue3864RemoveTarget() string { return "#new-chat-member-search" }

var chatissue3864Copy = map[string]map[string]string{
	"en-US": {"added": "{name} added.", "removed": "{name} removed."},
	"de-DE": {"added": "{name} hinzugefügt.", "removed": "{name} entfernt."},
	"ar":    {"added": "تمت إضافة {name}.", "removed": "تمت إزالة {name}."},
}

// chatissue3864Note is the sentence announced for a chosen ("added") or removed
// ("removed") person.
func chatissue3864Note(m Model, key, name string) string {
	return laneTextf(m, chatissue3864Copy, key, map[string]string{"name": name})
}

// chatissue3864Announcer is the polite live region that speaks the note.
func chatissue3864Announcer(note string) ui.Node {
	return html.P(html.Props{ID: "new-chat-pick-status", Class: "sr-only", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}, Text: note})
}
