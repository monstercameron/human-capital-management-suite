package chatui

// Issue 3721: the sidebar's unread count is the messages other people wrote from
// the read position on; the person's own messages never count (the server counts
// the same way). A mark on the person's own messages therefore reads "1", which
// looked like a miscount. The badge now says how it counts.
const keyIssue3721UnreadNote = "chat.issue3721.unread_note"

var issue3721Copy = map[string]map[string]string{
	"en-US": {keyIssue3721UnreadNote: "Messages from other people you have not read. Your own messages are not counted."},
	"de-DE": {keyIssue3721UnreadNote: "Ungelesene Nachrichten von anderen Personen. Ihre eigenen Nachrichten werden nicht mitgezählt."},
	"ar":    {keyIssue3721UnreadNote: "رسائل غير مقروءة من أشخاص آخرين. رسائلك لا تُحتسب."},
}

func issue3721UnreadNote(m Model) string { return laneText(m, issue3721Copy, keyIssue3721UnreadNote) }
