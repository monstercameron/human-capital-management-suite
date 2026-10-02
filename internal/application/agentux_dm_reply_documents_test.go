package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

// The Sources list the server writes under an answer names each cited document
// by title, section and version, and links it to the Documents hub with a
// relative address on the product's own origin: the only form the page turns
// into a link. Which reader may open a link is decided each time the answer is
// read (chat.projectAgentSources and its tests), not when the answer is stored.
func TestTodo_AGENTUX_032_Security(t *testing.T) {
	documents := []agentdocref.ResolvedDocument{
		{Reference: agentdocref.Reference{DocumentID: "pto-v4", SectionAnchor: "carry-over"}, Version: 4, Title: "Paid time off policy"},
		{Reference: agentdocref.Reference{DocumentID: "benefits-v2"}, Version: 2, Title: "Benefits guide"},
	}
	// The served composition configures no tenant origin.
	linked := renderPersonaReplyWithAgentDocuments("Employees carry over up to 40 hours.", "tenant", "conversation", PersonaReplyOutputPolicy{}, documents, nil)
	for _, want := range []string{"\n\nSources\n", "- [Paid time off policy · carry-over · v4.0.0](/workspace/app/docs?document=pto-v4#carry-over)", "- [Benefits guide · v2.0.0](/workspace/app/docs?document=benefits-v2)"} {
		if !strings.Contains(linked, want) {
			t.Fatalf("linked sources missing %q: %q", want, linked)
		}
	}
	sources := linked[strings.Index(linked, "\n\nSources\n"):]
	if strings.Contains(sources, "://") || strings.Contains(sources, "](//") || strings.Count(sources, "](/workspace/app/docs?document=") != 2 {
		t.Fatalf("a source address is not a relative address of the Documents hub: %q", sources)
	}

	// A cited document with no identifier is named and not linked.
	unnamed := renderPersonaReplyWithAgentDocuments("Employees carry over up to 40 hours.", "tenant", "conversation", PersonaReplyOutputPolicy{}, []agentdocref.ResolvedDocument{{Version: 4, Title: "Paid time off policy"}}, nil)
	if !strings.Contains(unnamed, "\n- Paid time off policy · v4.0.0") || strings.Contains(unnamed, "/workspace/app/docs") {
		t.Fatalf("a document with no identifier was linked: %q", unnamed)
	}

	// A document's title cannot write a link of its own into the list, and its
	// identifier cannot leave the hub's address.
	hostile := []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "d1&next=//evil.example/#x", SectionAnchor: "a b/../c"}, Version: 1, Title: "Handbook](https://evil.example/) [again"}}
	forged := renderPersonaReplyWithAgentDocuments("Employees carry over up to 40 hours.", "tenant", "conversation", PersonaReplyOutputPolicy{}, hostile, nil)
	line := forged[strings.Index(forged, "\n- ")+3:]
	if strings.Contains(line, "https://") || strings.Contains(line, "](//") || strings.Count(line, "](") != 1 || !strings.HasSuffix(line, "](/workspace/app/docs?document=d1%26next%3D%2F%2Fevil.example%2F%23x#a%20b%2F..%2Fc)") {
		t.Fatalf("a hostile title or identifier changed where the source leads: %q", line)
	}

	// With a tenant origin configured the address is that origin's hub and no
	// other host's.
	absolute := renderPersonaReplyWithAgentDocuments("Employees carry over up to 40 hours.", "tenant", "conversation", PersonaReplyOutputPolicy{TenantOrigin: "https://tenant.example/"}, documents[:1], nil)
	if !strings.Contains(absolute, "](https://tenant.example/workspace/app/docs?document=pto-v4#carry-over)") {
		t.Fatalf("configured tenant origin: %q", absolute)
	}
}
