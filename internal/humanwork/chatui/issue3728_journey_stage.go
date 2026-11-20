package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3728: the line a conversation shows when a workflow request that was
// shared into it is decided. The post's body names the outcome and the person
// who decided; the sentence is written here, from names the page already holds,
// in the reader's language. It holds no reason and no request detail. Whether
// this reader may open the request was decided by the server when it sent the
// post: a reader who may not has the typed journey reference stripped, and then
// the line names no decider and no outcome and offers no link.

const (
	keyIssue3728Approved = "chat.issue3728.approved"
	keyIssue3728Rejected = "chat.issue3728.rejected"
	keyIssue3728Blocked  = "chat.issue3728.blocked"
	keyIssue3728Updated  = "chat.issue3728.updated"
	keyIssue3728Link     = "chat.issue3728.link"
	keyIssue3728Someone  = "chat.issue3728.someone"
)

var issue3728Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3728Approved: "{actor} approved a workflow request.",
		keyIssue3728Rejected: "{actor} rejected a workflow request.",
		keyIssue3728Blocked:  "A workflow request approved by {actor} is now blocked.",
		keyIssue3728Updated:  "A workflow request shared here was updated.",
		keyIssue3728Link:     "View request",
		keyIssue3728Someone:  "Someone",
	},
	"de-DE": {
		keyIssue3728Approved: "{actor} hat eine Workflow-Anfrage genehmigt.",
		keyIssue3728Rejected: "{actor} hat eine Workflow-Anfrage abgelehnt.",
		keyIssue3728Blocked:  "Eine von {actor} genehmigte Workflow-Anfrage ist jetzt blockiert.",
		keyIssue3728Updated:  "Eine hier geteilte Workflow-Anfrage wurde aktualisiert.",
		keyIssue3728Link:     "Anfrage ansehen",
		keyIssue3728Someone:  "Jemand",
	},
	"ar": {
		keyIssue3728Approved: "وافق {actor} على طلب سير عمل.",
		keyIssue3728Rejected: "رفض {actor} طلب سير عمل.",
		keyIssue3728Blocked:  "طلب سير العمل الذي وافق عليه {actor} متوقف الآن.",
		keyIssue3728Updated:  "تم تحديث طلب سير عمل تمت مشاركته هنا.",
		keyIssue3728Link:     "عرض الطلب",
		keyIssue3728Someone:  "شخص ما",
	},
}

// issue3728OpenRequest is the journey the line's typed reference names, when
// the server left it for this reader.
func issue3728OpenRequest(msg Message) (string, bool) {
	for _, ref := range msg.WorkReferences {
		if id, ok := JourneyChatReference(ref); ok {
			return id, true
		}
	}
	return "", false
}

// issue3728LineText is the sentence a decision line stands for, or false for
// any other message.
func issue3728LineText(m Model, msg Message) (string, bool) {
	line, ok := chat.ParseJourneyStage(msg.Body)
	if !ok {
		return "", false
	}
	if _, open := issue3728OpenRequest(msg); !open {
		return laneText(m, issue3728Copy, keyIssue3728Updated), true
	}
	actor := chatKnownName(m, line.Decider)
	if actor == "" {
		actor = laneText(m, issue3728Copy, keyIssue3728Someone)
	}
	key := keyIssue3728Approved
	switch line.Outcome {
	case chat.JourneyStageRejected:
		key = keyIssue3728Rejected
	case chat.JourneyStageBlocked:
		key = keyIssue3728Blocked
	}
	return laneTextf(m, issue3728Copy, key, map[string]string{"actor": actor}), true
}

// issue3728LineLink is the link to the request, for a reader who may open it.
// The address is built from the validated id, never from the post's text.
func issue3728LineLink(m Model, msg Message) (ui.Node, bool) {
	if _, ok := chat.ParseJourneyStage(msg.Body); !ok {
		return nil, false
	}
	id, open := issue3728OpenRequest(msg)
	if !open {
		return nil, false
	}
	href := JourneyReferenceURL(id)
	if href == "" {
		return nil, false
	}
	label := strings.TrimSpace(laneText(m, issue3728Copy, keyIssue3728Link))
	return html.A(html.Props{Class: "chatux021-line-link", Href: href, Data: map[string]string{"action": "open-journey-request"}}, ui.Text(label)), true
}

// Issue3728Styles makes the link a 44px target that wraps at 320px and follows
// the reading direction.
const Issue3728Styles = `
.chatux021-line-link{display:inline-flex;align-items:center;justify-content:center;min-block-size:44px;min-inline-size:44px;padding-inline:8px;color:var(--chat-accent-text,var(--accent));text-decoration:underline}
`
