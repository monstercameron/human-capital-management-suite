package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

// A reply that is only a title is replaced by one sentence of the server's own
// that says how many documents the agent can read and names them; a run with no
// searched document has nothing to say.
func TestTodo_CHATBUG_049_Composed(t *testing.T) {
	pto := personaQualitySearchedDocument{DocumentID: "pto", Title: "Paid time off policy", SectionTitle: "Carryover"}
	guide := personaQualitySearchedDocument{DocumentID: "guide", Title: "2026 holiday guide"}
	for _, test := range []struct {
		name     string
		searched []personaQualitySearchedDocument
		want     string
	}{
		{"nothing searched", nil, ""},
		{"one document", []personaQualitySearchedDocument{pto}, "I can read one document here: Paid time off policy."},
		{"one document found in two sections", []personaQualitySearchedDocument{pto, {DocumentID: "pto", Title: "Paid time off policy", SectionTitle: "Accrual"}}, "I can read one document here: Paid time off policy."},
		{"two documents", []personaQualitySearchedDocument{pto, guide}, "I can read 2 documents here: Paid time off policy, 2026 holiday guide."},
		{"a title that would read as a link", []personaQualitySearchedDocument{{DocumentID: "x", Title: "Policy [draft]"}}, ""},
	} {
		got := personaComposedListReply(test.searched)
		if got != test.want {
			t.Errorf("%s: %q, want %q", test.name, got, test.want)
		}
		if got == "" {
			continue
		}
		// The sentence passes every check the model's own reply is held to.
		if !personaReplyHasStatement(got, personaReplyStatementTitles(test.searched)...) || validatePersonaChatReply(got) != nil {
			t.Errorf("%s: the composed sentence is refused by the reply checks: %q", test.name, got)
		}
	}
	// A long list is cut, never padded.
	var many []personaQualitySearchedDocument
	for index := range 14 {
		many = append(many, personaQualitySearchedDocument{DocumentID: string(rune('a' + index)), Title: "Document " + string(rune('A'+index))})
	}
	if got := personaComposedListReply(many); !strings.HasPrefix(got, "I can read 10 documents here: Document A, ") || strings.Contains(got, "Document K") {
		t.Errorf("a long list reads %q", got)
	}

	// Delivered, the sentence still names the document and cites it in Sources.
	documents := []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "pto"}, Version: 1, Title: "Paid time off policy"}}
	delivered := renderPersonaReplyWithAgentDocuments("I can read one document here: Paid time off policy.", "tenant-a", "room-a", PersonaReplyOutputPolicy{}, documents, nil)
	if !strings.Contains(delivered, "I can read one document here:") || !strings.Contains(delivered, "Sources\n- [") {
		t.Fatalf("the composed sentence was not delivered with its source: %q", delivered)
	}
}
