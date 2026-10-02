package main

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
)

// Picker rows remember the searched version for switching back to pinned.
func agentAnnouncementDocumentReference(id, label, mode, anchor string, remembered uint64) agentdocref.Reference {
	versionMode := agentdocref.VersionMode(mode)
	if versionMode == agentdocref.ModeLatestPublished {
		remembered = 0
	}
	return agentdocref.Reference{DocumentID: id, Label: label, VersionMode: versionMode, PinnedVersion: remembered, SectionAnchor: anchor}
}

// Return the first field that needs attention, in the form's reading order.
func agentAnnouncementInvalidField(input agentcontrols.AnnouncementDraft) string {
	if strings.TrimSpace(input.PersonaID) == "" {
		return "agent"
	}
	if strings.TrimSpace(input.ConversationID) == "" || strings.TrimSpace(input.InstallationID) == "" {
		return "conversation"
	}
	if strings.TrimSpace(input.Instruction) == "" || len([]rune(input.Instruction)) > 1000 {
		return "instruction"
	}
	if len(input.Documents) < 1 || len(input.Documents) > 5 || agentdocref.Validate(input.Documents, 5) != nil {
		return "documents"
	}
	switch input.Cadence {
	case "NOW", "ONCE", "DAILY":
		if len(input.Weekdays) != 0 || input.MonthDay != 0 {
			return "when"
		}
	case "WEEKLY":
		if len(input.Weekdays) == 0 || len(input.Weekdays) > 7 || input.MonthDay != 0 {
			return "weekdays"
		}
		seen := map[int]bool{}
		for _, day := range input.Weekdays {
			if day < 0 || day > 6 || seen[day] {
				return "weekdays"
			}
			seen[day] = true
		}
	case "MONTHLY":
		if input.MonthDay < 1 || input.MonthDay > 31 || len(input.Weekdays) != 0 {
			return "month-day"
		}
	default:
		return "when"
	}
	if input.Cadence != "NOW" && input.Cadence != "ONCE" {
		if _, err := time.Parse("15:04", input.Time); err != nil {
			return "time"
		}
	}
	if _, err := time.LoadLocation(input.Zone); err != nil {
		return "zone"
	}
	return ""
}

func agentAnnouncementPrimaryAction(cadence string, editing bool) (action, copyKey string) {
	if cadence == "NOW" || cadence == "ONCE" {
		if editing {
			return "save", "save_changes"
		}
		return "post", "post_now"
	}
	return "save", "save"
}
