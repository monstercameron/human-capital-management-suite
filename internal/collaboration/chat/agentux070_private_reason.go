package chat

import "strings"

// An agent's answer in a channel is posted for every member unless something
// keeps it private. When it is delivered privately instead, the delivery owner
// appends one marker line naming why, so the card can say it in one plain line.
// The marker is a code, never prose: the browser turns it into words in the
// reader's language. It is stripped before the copy saved in the asker's own
// conversation with the agent is written.
const (
	// PrivateReasonAgent: the agent is set to answer privately.
	PrivateReasonAgent = "agent"
	// PrivateReasonAsked: the asker asked for privacy in the question.
	PrivateReasonAsked = "asked"
	// PrivateReasonAudience: not every member of the conversation may read every
	// source the answer cites, or that could not be established.
	PrivateReasonAudience = "audience"

	privateReasonOpen  = "<!--chat.agent.private:"
	privateReasonClose = "-->"
)

// ValidPrivateReason reports whether reason is one of the codes above.
func ValidPrivateReason(reason string) bool {
	switch reason {
	case PrivateReasonAgent, PrivateReasonAsked, PrivateReasonAudience:
		return true
	}
	return false
}

// PrivateReasonMarker is the trailing line that records why an answer is
// private. An unknown reason yields no marker.
func PrivateReasonMarker(reason string) string {
	if !ValidPrivateReason(reason) {
		return ""
	}
	return privateReasonOpen + reason + privateReasonClose
}

// SplitPrivateReason removes the last marker from body and returns the cleaned
// body and the reason it named. A body with no well-formed marker is returned
// unchanged with an empty reason.
func SplitPrivateReason(body string) (clean, reason string) {
	start := strings.LastIndex(body, privateReasonOpen)
	if start < 0 {
		return body, ""
	}
	rest := body[start+len(privateReasonOpen):]
	end := strings.Index(rest, privateReasonClose)
	if end < 0 || !ValidPrivateReason(rest[:end]) {
		return body, ""
	}
	reason = rest[:end]
	head := strings.TrimRight(body[:start], " \t\r\n")
	tail := strings.TrimLeft(rest[end+len(privateReasonClose):], " \t\r\n")
	if tail == "" {
		return head, reason
	}
	return head + "\n\n" + tail, reason
}
