package main

// The picker container carries localized action copy in data-agentdoc-remove.
// Only a button owns the removal action; matching the container swallows result clicks.
const personaDocumentPickerRemoveSelector = "button[data-agentdoc-remove]"

func personaDocumentPickerClickAction(optionID, removeTag, retryTag string) string {
	if optionID != "" {
		return "add"
	}
	if removeTag == "BUTTON" {
		return "remove"
	}
	if retryTag == "BUTTON" {
		return "retry"
	}
	return ""
}
