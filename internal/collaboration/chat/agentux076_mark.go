package chat

import "strings"

// AGENTUX-076: a general-purpose agent may answer from general knowledge when no
// document covers a question. Such an answer carries a typed mark, the way a
// private answer carries its reason: one trailing line that is a code, never
// prose. The card turns it into "Not from your documents" in the reader's
// language, and the copy saved in the asker's conversation keeps a plain line.
const (
	ungroundedMarker = "<!--chat.agent.source:none-->"
	// UngroundedNote is the plain line the saved copy of such an answer carries,
	// where there is no card to show the mark.
	UngroundedNote = "Not from your documents."
)

// UngroundedMark is the trailing line that marks an answer as not from the
// documents.
func UngroundedMark() string { return ungroundedMarker }

// SplitUngrounded removes the mark from body and says whether it was there.
func SplitUngrounded(body string) (clean string, ungrounded bool) {
	start := strings.LastIndex(body, ungroundedMarker)
	if start < 0 {
		return body, false
	}
	head := strings.TrimRight(body[:start], " \t\r\n")
	tail := strings.TrimLeft(body[start+len(ungroundedMarker):], " \t\r\n")
	if tail == "" {
		return head, true
	}
	return head + "\n\n" + tail, true
}
