package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"reflect"
)

// Previewing saved text preserves its definition and revision. Edits must be
// saved first, so posting cannot silently publish a different definition.
func agentAnnouncementPreviewMatchesSaved(draft agentcontrols.AnnouncementDraft, rows []productui.AgentAnnouncementRow) bool {
	for _, row := range rows {
		if row.ID != draft.ID || row.Revision != draft.ExpectedRevision {
			continue
		}
		saved := row.Editor
		return saved.InstallationID == draft.InstallationID && saved.PersonaID == draft.PersonaID && saved.ConversationID == draft.ConversationID && saved.Instruction == draft.Instruction && saved.Zone == draft.Zone && reflect.DeepEqual(saved.Documents, draft.Documents)
	}
	return false
}
