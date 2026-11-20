package chatui

import "github.com/monstercameron/GoWebComponents/v5/html"

const keyIssue3988ThreadTarget = "chat.issue3988.thread_target"

var issue3988Copy = map[string]map[string]string{
	"en-US": {keyIssue3988ThreadTarget: "Search result, reply by {author}, {time}"},
	"de-DE": {keyIssue3988ThreadTarget: "Suchergebnis, Antwort von {author}, {time}"},
	"ar":    {keyIssue3988ThreadTarget: "نتيجة البحث، رد من {author}، {time}"},
}

// issue3988ThreadReplyProps decorates one thread reply's row. The reply a search
// result opened is marked like a timeline hit, can take keyboard focus (the
// browser moves it there once the thread is on screen) and is named as the
// search result, so a screen reader announces the destination. Every other
// reply is left as it was.
func issue3988ThreadReplyProps(m Model, msg Message, props html.Props) html.Props {
	if msg.ID == "" || msg.ID != m.FocusMessageID {
		return props
	}
	props.Class += " search-target"
	props.Role = "article"
	props.Aria = map[string]string{"label": laneTextf(m, issue3988Copy, keyIssue3988ThreadTarget, map[string]string{"author": msg.Author, "time": msg.TimeLabel})}
	props.Raw = map[string]any{"tabindex": "0"}
	return props
}
