//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3932. The Saved panel's private-label controls. The state lives on the
// panel's own object (label, unlabelled, editingLabels, labelManage); nothing
// about labels is kept in the page's storage.

// labelClick handles the label actions of a press and says whether it did.
func (b *chatsaveBrowser) labelClick(action string, button js.Value, host, conversationID, postID string) bool {
	name := ""
	if attr := button.Call("getAttribute", "data-saved-label"); attr.Type() == js.TypeString {
		name = attr.String()
	}
	switch action {
	case "label-all":
		b.label, b.unlabelled, b.labelManage = "", false, ""
		b.render()
	case "label-filter":
		if strings.EqualFold(b.label, name) {
			b.label = ""
		} else {
			b.label = name
		}
		b.unlabelled, b.labelManage = false, ""
		b.render()
	case "label-unlabelled":
		b.unlabelled, b.label, b.labelManage = !b.unlabelled, "", ""
		b.render()
	case "label-edit":
		b.editingLabels, b.editingNote, b.reminderMenu, b.pickingDate = chatui.SavedItemKey(host, conversationID, postID), "", "", false
		b.focusID = "chatsave-labels-field-" + postID
		b.render()
	case "label-cancel":
		b.editingLabels = ""
		b.focusID = "chatsave-labeladd-" + postID
		b.render()
	case "label-add":
		b.focusID = "chatsave-labels-field-" + postID
		b.labelCommand(host, chatsaveCommand{Action: "labels", ConversationID: conversationID, PostID: postID, AddLabels: []string{name}})
	case "label-drop":
		// The removed chip held focus; the message's Add label button is the next stop, else the tabs.
		b.focusID = "chatsave-labeladd-" + postID
		if !js.Global().Get("document").Call("getElementById", b.focusID).Truthy() {
			b.focusID = "chatsave-tab-" + b.tab
		}
		b.labelCommand(host, chatsaveCommand{Action: "labels", ConversationID: conversationID, PostID: postID, RemoveLabels: []string{name}})
	case "label-rename":
		b.labelManage = "rename"
		b.focusID = "chatsave-label-rename-field"
		b.render()
	case "label-remove":
		b.labelManage = "remove"
		b.focusID = "chatsave-label-manage-cancel"
		b.render()
	case "label-manage-cancel":
		b.cancelLabelManage()
	case "label-remove-confirm":
		b.label, b.labelManage = "", ""
		b.focusID = "chatsave-labelf-all"
		b.labelCommand("", chatsaveCommand{Action: "remove_label", Label: name})
	default:
		return false
	}
	return true
}

// labelCommand sends one label change, showing its result at once.
func (b *chatsaveBrowser) labelCommand(host string, command chatsaveCommand) {
	if host == "" {
		host = b.cfg.Tenant
	}
	if b.commandFailed {
		b.errorCode = ""
	}
	b.commandFailed = false
	b.applyLocal(host, command)
	b.command(host, command)
}

// cancelLabelManage closes the rename or remove step and returns focus to the
// control that opened it.
func (b *chatsaveBrowser) cancelLabelManage() {
	b.focusID = "chatsave-label-remove"
	if b.labelManage == "rename" {
		b.focusID = "chatsave-label-rename"
	}
	b.labelManage = ""
	b.render()
}

// cancelLabelEdit closes an item's label editor without saving.
func (b *chatsaveBrowser) cancelLabelEdit() {
	post := chatsavePostOf(b.editingLabels)
	b.editingLabels = ""
	if post != "" {
		b.focusID = "chatsave-labeladd-" + post
	}
	b.render()
}

// labelSubmit handles the label editor and the rename form; it reports whether
// the form was one of them.
func (b *chatsaveBrowser) labelSubmit(kind string, form js.Value, host, conversationID, postID string) bool {
	value := form.Call("querySelector", "input").Get("value").String()
	switch kind {
	case "labels":
		labels, ok := chatsaveParseLabels(value)
		if !ok {
			// A label that is too long is reported as a label problem, not as a bad note or reminder time.
			b.errorCode, b.commandFailed, b.failedAction = "label_invalid", true, "labels"
			b.render()
			return true
		}
		b.editingLabels = ""
		b.focusID = "chatsave-labeladd-" + postID
		if len(labels) == 0 {
			b.render()
			return true
		}
		b.labelCommand(host, chatsaveCommand{Action: "labels", ConversationID: conversationID, PostID: postID, AddLabels: labels})
		return true
	case "label-rename":
		to, ok := chat.NormalizeSavedLabel(value)
		from := form.Call("getAttribute", "data-saved-label").String()
		if !ok {
			b.errorCode, b.commandFailed, b.failedAction = "label_invalid", true, "rename_label"
			b.render()
			return true
		}
		b.labelManage = ""
		b.focusID = "chatsave-label-rename"
		if to == from {
			b.render()
			return true
		}
		b.label = to
		b.labelCommand("", chatsaveCommand{Action: "rename_label", Label: from, NewLabel: to})
		return true
	}
	return false
}
