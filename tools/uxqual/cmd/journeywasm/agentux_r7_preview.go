package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"

func personaAdminApplyPreviewSelection(snapshot productui.PersonaAdminSnapshot, persona, subject, conversation string) productui.PersonaAdminSnapshot {
	snapshot.PreviewPersonaID, snapshot.PreviewSubjectID, snapshot.PreviewConversationID = persona, subject, conversation
	snapshot.PreviewValidationFields = personaAdminMissingPreviewFields(persona, subject, conversation)
	if len(snapshot.PreviewValidationFields) > 0 {
		snapshot.Preview = productui.PersonaAdminPreview{}
	}
	return snapshot
}

func personaAdminReviewOutcomeAction(action, decision string) string {
	if action == "REVIEW" && decision == "REJECT" {
		return "REJECT"
	}
	return action
}

func personaAdminShouldLoadHistory(hasSnapshot, requested bool) bool {
	return hasSnapshot && !requested
}

// Access checks and catalog refreshes return a new projection without the
// separately loaded run history. Keep history on matching agents until its
// authenticated refresh completes.
func personaAdminPreserveRunHistory(previous, next productui.PersonaAdminSnapshot) productui.PersonaAdminSnapshot {
	next.Personas = append([]productui.PersonaAdminPersona(nil), next.Personas...)
	for i := range next.Personas {
		for _, old := range previous.Personas {
			if old.ID == next.Personas[i].ID {
				next.Personas[i].RecentRuns = append([]productui.AgentControlRun(nil), old.RecentRuns...)
				next.Personas[i].RecentRunsUnavailable = old.RecentRunsUnavailable
				break
			}
		}
	}
	return next
}

func personaAdminLifecycleOutcomeAction(snapshot *productui.PersonaAdminSnapshot, personaID, action, decision string) string {
	if action == "PUBLISH" && snapshot != nil {
		for _, persona := range snapshot.Personas {
			if persona.ID == personaID && persona.Lifecycle == productui.PersonaSuspended {
				return "RESUME"
			}
		}
	}
	return personaAdminReviewOutcomeAction(action, decision)
}
