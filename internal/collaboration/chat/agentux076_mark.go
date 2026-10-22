package chat

import "strings"

// AGENTUX-076: a general-purpose agent may answer from general knowledge when no
// document covers a question. Such an answer carries a typed mark, the way a
// private answer carries its reason: one trailing line that is a code, never
// prose. The card turns it into "Not from your documents" in the reader's
// language (en, de-DE and ar copy in chatui), and so does every body the client
// draws that still carries the mark: the post in a channel, the copy saved in
// the asker's own conversation and a shared answer. The server writes no English
// sentence of its own into a message a person reads.
const ungroundedMarker = "<!--chat.agent.source:none-->"

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
