package main

import "strings"

type personaAdminSelectionOption struct {
	Value string
	Label string
}

func personaAdminResolveSelection(typed, selected string, options []personaAdminSelectionOption) (string, string, bool) {
	typed = strings.TrimSpace(typed)
	selected = strings.TrimSpace(selected)
	for _, option := range options {
		if selected != "" && option.Value == selected && (typed == "" || strings.EqualFold(strings.TrimSpace(option.Label), typed)) {
			return option.Value, option.Label, true
		}
	}
	for _, option := range options {
		if strings.EqualFold(strings.TrimSpace(option.Label), typed) {
			return option.Value, option.Label, true
		}
	}
	if typed != "" {
		for _, option := range options {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(option.Label)), strings.ToLower(typed)) {
				return option.Value, option.Label, true
			}
		}
	}
	return "", typed, false
}

func personaAdminMissingPreviewFields(persona, subject, conversation string) []string {
	missing := make([]string, 0, 3)
	if strings.TrimSpace(persona) == "" {
		missing = append(missing, "persona")
	}
	if strings.TrimSpace(subject) == "" {
		missing = append(missing, "subject")
	}
	if strings.TrimSpace(conversation) == "" {
		missing = append(missing, "conversation")
	}
	return missing
}
