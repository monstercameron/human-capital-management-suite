package chatui

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// A to-do's deadline is shown as a day alone when the author wrote a day, and
// with its time of day and the zone's offset when the author wrote a time
// ("in an hour", a full timestamp), in the viewer's zone (#3951). The preview,
// its list of changes and the posted card read it the same way.

// chatcmd002DueText is the deadline of a task as a reader sees it.
func chatcmd002DueText(m Model, task chat.Chatcmd002Task) string {
	if task.DueAt == nil {
		return ""
	}
	if task.DueTimed {
		return chatcmd002Stamp(m, *task.DueAt)
	}
	return task.DueAt.In(chatcmd003Location(m)).Format("2006-01-02")
}

// chatcmd003ChangeDue is the date of a due-date change line: the day, or, for
// a timed deadline (the change then holds an RFC 3339 instant), the time with
// its zone.
func chatcmd003ChangeDue(m Model, after string) string {
	if at, err := time.Parse(time.RFC3339, after); err == nil {
		return chatcmd002Stamp(m, at)
	}
	return after
}
