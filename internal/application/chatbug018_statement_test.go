package application

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

const chatbug018Title = "Paid time off policy"

// An answer that is only a document title, or only a citation that renders as
// one, says nothing. The check refuses it wherever the answer text is judged.
func TestTodo_CHATBUG_018(t *testing.T) {
	titles := []string{chatbug018Title, "Carryover"}
	for _, test := range []struct {
		name string
		text string
		says bool
	}{
		{"the title alone", chatbug018Title, false},
		{"the title with a full stop", chatbug018Title + ".", false},
		{"the title in other case", "PAID TIME OFF POLICY", false},
		{"the title before a colon and a citation", chatbug018Title + ": [[1]]", false},
		{"a citation alone", "[[1]]", false},
		{"citations with a section", "[[1:carryover]] [[2]]", false},
		{"a link to the document", "[" + chatbug018Title + " · v1.0.0](/workspace/app/docs?document=pto)", false},
		{"the old parenthetical citation", "(" + chatbug018Title + ", version 1, Carryover section)", false},
		{"the title and its source list", chatbug018Title + "\n\nSources\n- [" + chatbug018Title + "](/workspace/app/docs?document=pto)", false},
		{"a statement", "Employees carry over up to 40 hours [[1]].", true},
		{"a statement naming the title", chatbug018Title + ": employees carry over up to 40 hours.", true},
		{"a number is a statement", chatbug018Title + ": 40", true},
	} {
		if got := personaReplyHasStatement(test.text, titles...); got != test.says {
			t.Errorf("%s: personaReplyHasStatement(%q)=%v, want %v", test.name, test.text, got, test.says)
		}
	}
}

func TestTodo_CHATBUG_018_Validator(t *testing.T) {
	for _, text := range []string{"[[1]]", "[[1]] [[2:carryover]]", " \n[[3]]\n"} {
		if err := validatePersonaChatReply(text); err == nil {
			t.Errorf("validatePersonaChatReply(%q) accepted a reply with no statement", text)
		}
		validator, admission, run, persister, _ := personaRunOutputFixture(t)
		if _, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: text, Finish: agentmodel.FinishComplete}); err == nil || persister.calls != 0 {
			t.Errorf("a reply of %q was sealed: err=%v persisted=%d", text, err, persister.calls)
		}
	}
	if err := validatePersonaChatReply("Employees carry over up to 40 hours [[1]]."); err != nil {
		t.Fatalf("a statement with a citation was refused: %v", err)
	}
}

func TestTodo_CHATBUG_018_Renderer(t *testing.T) {
	documents := []agentdocref.ResolvedDocument{{Reference: agentdocref.Reference{DocumentID: "pto"}, Version: 1, Title: chatbug018Title}}
	render := func(body string) string {
		return renderPersonaReplyWithAgentDocuments(body, "tenant-a", "room-a", PersonaReplyOutputPolicy{}, documents, nil)
	}
	for _, body := range []string{chatbug018Title, chatbug018Title + ": [[1]]", "[[1]]"} {
		if got := render(body); got != "" {
			t.Errorf("render(%q) delivered %q instead of refusing a reply with no statement", body, got)
		}
	}
	got := render("Employees carry over up to 40 hours [[1]].")
	if !strings.HasPrefix(got, "Employees carry over up to 40 hours [") || !strings.Contains(got, "Sources\n- [") {
		t.Fatalf("a real answer was not delivered with its sources: %q", got)
	}
}
