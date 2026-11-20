package chat

import (
	"strings"
	"time"
)

// chatcmd004TimedWords are the phrases that name a moment, not a day.
var chatcmd004TimedWords = map[string]bool{"in an hour": true}

func init() {
	// Spoken forms of "in an hour" read as the same moment (#3951).
	for _, phrase := range []string{"in one hour", "in 1 hour"} {
		chatcmd004ShortWords[phrase] = "in an hour"
	}
}

// chatcmd004Timed reports whether a typed date phrase carries a time of day:
// a full timestamp, or "in an hour". A day alone does not.
func chatcmd004Timed(typed string) bool {
	typed = strings.TrimSpace(chatcmd004Digits(typed))
	if _, err := time.Parse(time.RFC3339, typed); err == nil {
		return true
	}
	typed = strings.ToLower(typed)
	if word, ok := chatcmd004ShortWords[typed]; ok {
		typed = word
	}
	return chatcmd004TimedWords[typed]
}

// chatcmd004DueChange is what the change line holds for a task's due date: the
// day alone as YYYY-MM-DD, or the instant as RFC 3339 in the author's zone when
// the task has a time of day, so the preview can show the time and its zone.
func chatcmd004DueChange(item Chatcmd002Task, now time.Time) string {
	at := item.DueAt.In(now.Location())
	if item.DueTimed {
		return at.Format(time.RFC3339)
	}
	return at.Format("2006-01-02")
}
