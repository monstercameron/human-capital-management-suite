package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"

func agentUXR7ImportedBy(snapshot *productui.PersonaAdminSnapshot, subject string) string {
	if snapshot == nil {
		return ""
	}
	for _, person := range snapshot.SubjectOptions {
		if person.ID == subject {
			return person.Label
		}
	}
	for _, persona := range snapshot.Personas {
		if persona.Owner == subject && persona.OwnerName != "" {
			return persona.OwnerName
		}
		if persona.Steward == subject && persona.StewardName != "" {
			return persona.StewardName
		}
	}
	return ""
}

func agentUXR7PreserveDraftMetadata(previous, next productui.AgentPortableReviewDraft) productui.AgentPortableReviewDraft {
	if previous.ID != next.ID {
		return next
	}
	if next.Name == "" {
		next.Name = previous.Name
	}
	if next.ImportedAt == "" {
		next.ImportedAt = previous.ImportedAt
	}
	if next.ImportedBy == "" {
		next.ImportedBy = previous.ImportedBy
	}
	return next
}
