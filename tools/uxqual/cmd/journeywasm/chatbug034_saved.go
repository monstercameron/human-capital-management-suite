package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatsaveIsSaved reports whether the list holds this person's save of one
// message. The hover bar's bookmark decides between saving and unsaving from
// this, not from the button's own pressed state: the button is redrawn with
// every hover and render, and a press on a message the list already holds must
// remove it.
func chatsaveIsSaved(page chat.SavedPage, cfg journeyclient.Config, host, conversationID, postID string) bool {
	for _, item := range page.Items {
		if item.HomeTenantID == cfg.Tenant && item.PersonID == cfg.Subject && item.PostID == postID && item.ConversationID == conversationID && item.TenantID == host {
			return true
		}
	}
	return false
}

// chatsaveHoverCommand is the command a press on the hover bar's bookmark sends:
// "remove" for a message the list holds, "save" otherwise.
func chatsaveHoverCommand(page chat.SavedPage, cfg journeyclient.Config, host, conversationID, postID string) chatsaveCommand {
	action := "save"
	if chatsaveIsSaved(page, cfg, host, conversationID, postID) {
		action = "remove"
	}
	return chatsaveCommand{Action: action, ConversationID: conversationID, PostID: postID}
}

// chatsaveBodies is the text of every saved message the list can show, so the
// documents they refer to are read and the panel shows their titles.
func chatsaveBodies(page chat.SavedPage) []string {
	out := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		if item.Availability == "readable" && item.Post != nil && item.Post.Body != "" {
			out = append(out, item.Post.Body)
		}
	}
	return out
}

// chatsaveActionFailure splits a failed state into the list failing to load and
// one change being refused. A refused change keeps the list on screen and says
// which action failed; only a list that could not be read says so.
func chatsaveActionFailure(code string, commandFailed bool) (listError, actionError string) {
	if code == "" {
		return "", ""
	}
	if commandFailed && code != "saved_limit" && code != "invalid_argument" {
		return "", code
	}
	return code, ""
}
