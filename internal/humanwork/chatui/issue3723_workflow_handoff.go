package chatui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3723: start a governed promotion from a conversation. The message menu
// and the composer's /promote command hand the person to the existing,
// governed promotion picker (People, eligible employees) with the conversation
// and the message as opaque ids. Chat decides nothing there: what the person
// may start, for whom and with what is resolved by the workflow itself, and a
// private conversation is never authorization.

// MessageWorkflowState is the handoff's client state. Enabled is set by the
// page only when the viewer's workspace reaches the promotion flow; Start takes
// the viewer there. Transient client state, never message content.
type MessageWorkflowState struct {
	Enabled bool
	// Start opens the flow for a conversation, and for one message of it when
	// messageID is not empty.
	Start func(conversationID, messageID string)
}

const (
	keyWorkflowHandoffMenu    = "issue3723.menu"
	keyWorkflowHandoffCommand = "issue3723.command"
)

var issue3723Copy = map[string]map[string]string{
	"en-US": {
		keyWorkflowHandoffMenu:    "Start a promotion from this message",
		keyWorkflowHandoffCommand: "Start a promotion request from this conversation",
	},
	"de-DE": {
		keyWorkflowHandoffMenu:    "Beförderung aus dieser Nachricht starten",
		keyWorkflowHandoffCommand: "Eine Beförderung aus dieser Unterhaltung heraus starten",
	},
	"ar": {
		keyWorkflowHandoffMenu:    "بدء ترقية من هذه الرسالة",
		keyWorkflowHandoffCommand: "بدء طلب ترقية من هذه المحادثة",
	},
}

// WorkflowHandoffText is the copy of the promotion handoff in the viewer's language.
func WorkflowHandoffText(locale, key string) string {
	return chatbug039Text(key, issue3723Copy[chatEmojiLocale(locale)][key], issue3723Copy["en-US"][key])
}

// workflowHandoffIDLimit bounds an id carried in an address.
const workflowHandoffIDLimit = 200

// WorkflowHandoffHref is the address of the governed promotion picker with the
// conversation (and message) it was started from. Only opaque ids travel; the
// message text never does. It is empty when there is no usable conversation id.
func WorkflowHandoffHref(locale, conversationID, messageID string) string {
	conversationID, messageID = strings.TrimSpace(conversationID), strings.TrimSpace(messageID)
	if conversationID == "" || len(conversationID) > workflowHandoffIDLimit || len(messageID) > workflowHandoffIDLimit {
		return ""
	}
	query := url.Values{"eligible": {"1"}, "from_conversation": {conversationID}}
	if messageID != "" {
		query.Set("from_message", messageID)
	}
	if resolved := chatEmojiLocale(locale); resolved != "en-US" {
		query.Set("locale", resolved)
	}
	return "/workspace/app/people?" + query.Encode()
}

// workflowHandoffAvailable says whether this conversation may offer the
// handoff: the page reaches the flow, the viewer is signed in, the room is
// served by this company and it is not a conversation with an agent.
func workflowHandoffAvailable(m Model) bool {
	if !m.MessageWorkflow.Enabled || m.MessageWorkflow.Start == nil || m.CurrentUser == "" || m.SelectedID == "" {
		return false
	}
	c := m.selected()
	if c.Agent {
		return false
	}
	return c.HostTenantID == "" || c.HostTenantID == m.CurrentTenantID
}

// workflowHandoffMenuItem is the Start a promotion row of a message's menu. A
// removed message has nothing to start from.
func workflowHandoffMenuItem(m Model, msg Message) []ui.Node {
	if !workflowHandoffAvailable(m) || msg.ID == "" || msg.Body == chat.RemovedByAdministrator {
		return nil
	}
	label := WorkflowHandoffText(m.Locale, keyWorkflowHandoffMenu)
	return []ui.Node{chatux022Item("menu-item chatworkflow-menu-item", "workflow-start", msg.ID, "people", label, false)}
}

// workflowHandoffOpen runs when the Start a promotion row is chosen. The menu
// is closed by the dispatcher after every action.
func (m Model) workflowHandoffOpen(postID string) {
	if !workflowHandoffAvailable(m) || postID == "" {
		return
	}
	m.MessageWorkflow.Start(m.SelectedID, postID)
}

// workflowHandoffRun is what /promote does: it consumes the line and hands off
// with the conversation only.
func workflowHandoffRun(rt composerCommandRuntime, _ string) {
	if !workflowHandoffAvailable(rt.Model) {
		return
	}
	if rt.ClearDraft != nil {
		rt.ClearDraft()
	}
	rt.Model.MessageWorkflow.Start(rt.Model.SelectedID, "")
}

// workflowHandoffCommandText is the one line /promote shows in the list.
func workflowHandoffCommandText(locale string) string {
	return WorkflowHandoffText(locale, keyWorkflowHandoffCommand)
}
