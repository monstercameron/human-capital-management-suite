package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Issue 3932. The browser client's side of the private saved-message labels
// that needs no browser, so it is tested natively.

// chatsaveParseLabels turns what a person typed into labels: comma separated,
// each trimmed and folded, empty and repeated ones dropped. ok is false when a
// label is too long to keep.
func chatsaveParseLabels(input string) (labels []string, ok bool) {
	seen := map[string]bool{}
	for _, part := range strings.Split(input, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		label, valid := chat.NormalizeSavedLabel(part)
		if !valid {
			return nil, false
		}
		if key := chat.SavedLabelKey(label); !seen[key] {
			seen[key] = true
			labels = append(labels, label)
		}
	}
	return labels, true
}

// chatsaveInLabelView says whether an item is listed under the chosen label
// view: a label, the unlabelled view, or (neither chosen) everything.
func chatsaveInLabelView(item chat.SavedItem, filter string, unlabelled bool) bool {
	switch {
	case unlabelled:
		return len(item.Labels) == 0
	case filter != "":
		return chatui.SavedRowHasLabel(item.Labels, filter)
	}
	return true
}

// chatsaveLabelsView is the label part of the panel: every label in use over
// the whole list (so a filter never hides the other choices), with counts.
func chatsaveLabelsView(page chat.SavedPage, filter string, unlabelled bool, editing, manage string) chatui.SavedLabelsView {
	rows := make([][]string, 0, len(page.Items))
	for _, item := range page.Items {
		rows = append(rows, item.Labels)
	}
	options, none := chatui.SavedLabelOptions(rows)
	return chatui.SavedLabelsView{Options: options, UnlabelledCount: none, Filter: filter, OnlyUnlabelled: unlabelled, EditingItem: editing, Manage: manage}
}

// chatsaveLabelCommandOK says whether a command that changes labels carries
// what the service needs, before anything is sent.
func chatsaveLabelCommandOK(command chatsaveCommand) bool {
	switch command.Action {
	case "labels":
		return command.ConversationID != "" && command.PostID != "" && len(command.AddLabels)+len(command.RemoveLabels) > 0
	case "rename_label":
		return command.ConversationID == "" && command.PostID == "" && command.Label != "" && command.NewLabel != ""
	case "remove_label":
		return command.ConversationID == "" && command.PostID == "" && command.Label != ""
	}
	return false
}

// chatsaveApplyLabelsLocal applies a label command to a copy of the list the
// way the service will, so chips and counts change before the round trip.
func chatsaveApplyLabelsLocal(page chat.SavedPage, host string, command chatsaveCommand) chat.SavedPage {
	items := make([]chat.SavedItem, len(page.Items))
	copy(items, page.Items)
	for i, item := range items {
		switch command.Action {
		case "labels":
			if item.TenantID == host && item.ConversationID == command.ConversationID && item.PostID == command.PostID {
				if labels, err := chat.ApplySavedLabels(item.Labels, command.AddLabels, command.RemoveLabels); err == nil {
					items[i].Labels = labels
				}
			}
		case "rename_label":
			if chatui.SavedRowHasLabel(item.Labels, command.Label) {
				renamed := make([]string, 0, len(item.Labels))
				for _, label := range item.Labels {
					if strings.EqualFold(label, command.Label) {
						label = command.NewLabel
					}
					renamed = append(renamed, label)
				}
				if labels, err := chat.ApplySavedLabels(renamed, nil, nil); err == nil {
					items[i].Labels = labels
				}
			}
		case "remove_label":
			if chatui.SavedRowHasLabel(item.Labels, command.Label) {
				if labels, err := chat.ApplySavedLabels(item.Labels, nil, []string{command.Label}); err == nil {
					items[i].Labels = labels
				}
			}
		}
	}
	return chat.SavedPage{Items: items, NextCursor: page.NextCursor}
}

// chatsaveLabelInUse says whether any saved item still carries the label.
func chatsaveLabelInUse(page chat.SavedPage, label string) bool {
	for _, item := range page.Items {
		if chatui.SavedRowHasLabel(item.Labels, label) {
			return true
		}
	}
	return false
}
