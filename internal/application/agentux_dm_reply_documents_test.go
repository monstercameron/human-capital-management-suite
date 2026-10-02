package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

func TestTodo_AGENTUX_032_Security(t *testing.T) {
	documents := []agentdocref.ResolvedDocument{
		{Reference: agentdocref.Reference{DocumentID: "pto-v4", SectionAnchor: "carry-over"}, Version: 4, Title: "Paid time off policy"},
		{Reference: agentdocref.Reference{DocumentID: "benefits-v2"}, Version: 2, Title: "Benefits guide"},
	}
	linked := renderPersonaReplyWithAgentDocuments("Answer", "tenant", "conversation", PersonaReplyOutputPolicy{TenantOrigin: "https://tenant.example"}, documents, nil)
	for _, want := range []string{"Sources\n", "[Paid time off policy (version 4, section carry-over)](https://tenant.example/workspace/app/docs?document=pto-v4#carry-over)", "[Benefits guide (version 2)](https://tenant.example/workspace/app/docs?document=benefits-v2)"} {
		if !strings.Contains(linked, want) {
			t.Fatalf("linked sources missing %q: %q", want, linked)
		}
	}
	revoked := renderPersonaReplyWithAgentDocuments("Answer", "tenant", "conversation", PersonaReplyOutputPolicy{}, documents[:1], nil)
	if !strings.Contains(revoked, "- Paid time off policy (version 4, section carry-over)") || strings.Contains(revoked, "/workspace/app/docs") {
		t.Fatalf("revoked reader received a document link: %q", revoked)
	}
}
