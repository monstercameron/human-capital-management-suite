package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

// Answer sources cite the resolved version's sealed label, not the
// sequence fallback (AGENTUX-065).
func TestAgentUX065_AnswerSourcesShowSealedLabel(t *testing.T) {
	documents := []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "pto", SectionAnchor: "carryover"}, Title: "Paid time off policy", Version: 3, VersionLabel: "1.4.2"}}
	got := renderPersonaReplyWithAgentDocuments("40 hours carry over.", "tenant", "room", PersonaReplyOutputPolicy{}, documents, nil)
	if !strings.Contains(got, "[Paid time off policy · carryover · v1.4.2](/workspace/app/docs?document=pto#carryover)") {
		t.Fatalf("sealed label citation missing: %q", got)
	}
	if strings.Contains(got, "v3.0.0") {
		t.Fatalf("sequence fallback leaked beside the sealed label: %q", got)
	}
	unlabelled := []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "pto"}, Title: "Paid time off policy", Version: 3}}
	got = renderPersonaReplyWithAgentDocuments("40 hours carry over.", "tenant", "room", PersonaReplyOutputPolicy{}, unlabelled, nil)
	if !strings.Contains(got, "[Paid time off policy · v3.0.0](/workspace/app/docs?document=pto)") {
		t.Fatalf("legacy sequence display missing: %q", got)
	}
}
