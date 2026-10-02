package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
)

// A search that returned two documents and an answer drawn from one of them
// cites one: the Sources under the delivered answer list exactly the document
// the sealed answer cites, and the other search hit is not named anywhere. The
// search output and the sealed citations are fixtures; no model is involved.
func TestTodo_AGENTUX_060_Integration(t *testing.T) {
	search := []byte(`{"Hits":[{"DocumentID":"doc-pto","VersionID":"docv-1","Title":"Paid time off policy"},{"DocumentID":"doc-holidays","VersionID":"docv-4","Title":"2026 holiday guide"}],"Total":2}`)
	searched := personaRunSearchedDocuments(search)
	if len(searched) != 2 {
		t.Fatalf("the search returned %+v", searched)
	}
	// The sealed answer cites the first hit only.
	sealed := []agentsecurity.Citation{{SourceID: "document:doc-pto/version:docv-1"}}
	cited := personaRunCitedDocuments(searched, sealed)
	if len(cited) != 1 || cited[0].Title != "Paid time off policy" {
		t.Fatalf("cited documents = %+v, want the one the answer was drawn from", cited)
	}
	delivered := renderPersonaReplyWithAgentDocuments("Employees may carry over up to 40 hours [[1]].", "tenant-a", "general", PersonaReplyOutputPolicy{}, cited, nil)
	cut := strings.LastIndex(delivered, "\nSources\n")
	if cut < 0 {
		t.Fatalf("the delivered answer has no Sources: %q", delivered)
	}
	lines := 0
	for _, line := range strings.Split(delivered[cut+len("\nSources\n"):], "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			lines++
		}
	}
	if lines != 1 || !strings.Contains(delivered[cut:], "Paid time off policy") {
		t.Fatalf("Sources lists %d documents: %q", lines, delivered[cut:])
	}
	if strings.Contains(delivered, "2026 holiday guide") || strings.Contains(delivered, "doc-holidays") {
		t.Fatalf("a document the answer did not use is named: %q", delivered)
	}
	// Citing both lists both, in the order the search returned them.
	both := personaRunCitedDocuments(searched, append(sealed, agentsecurity.Citation{SourceID: "document:doc-holidays/version:docv-4"}))
	if len(both) != 2 || both[0].Title != "Paid time off policy" || both[1].Title != "2026 holiday guide" {
		t.Fatalf("an answer drawn from both documents cites %+v", both)
	}
}
