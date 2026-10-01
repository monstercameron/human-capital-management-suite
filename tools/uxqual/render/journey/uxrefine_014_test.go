package journey

import (
	"strings"
	"testing"
)

// TestTodo_UXBLIND_014_Browser checks the generated accessible markup used by
// the journey browser contract. Real Codex visual proof at desktop, 390px,
// and 320px remains pending against the running WASM client.
func TestTodo_UXBLIND_014_Browser(t *testing.T) {
	view := &NotesView{
		Notes: []NoteEntry{{ID: "n1", Author: "Thomas Baker", Initials: "TB", Stage: "Finance approval", At: "19 Sep 2026, 07:00 UTC", ISO: "2026-09-19T07:00:00Z", Body: "Budget confirmed."}},
		Composer: &NoteComposer{
			Field:    Field{ID: "note-body", Name: "note_body", Kind: fieldKindTextarea, Label: "Add a note", Help: "Everyone who can open this promotion can read notes."},
			MaxRunes: 2000,
			Action:   "#/journeys/x",
			OnSubmit: func(map[string]string) {},
		},
	}
	out := mustRenderNode(t, notesSectionLocale(live{locale: "en-US", values: map[string]string{"note-body": "Budget confirmed."}}, view))
	for _, want := range []string{
		`<section aria-labelledby="notes-heading"`,
		`<article aria-label="Note from Thomas Baker"`,
		`<time class="jn-note-at" datetime="2026-09-19T07:00:00Z">`,
		`Stage: Finance approval`,
		`<textarea`, `id="note-body"`, `aria-describedby="note-body-help"`, `id="note-body-count"`,
		`role="status"`, `aria-live="polite"`, `>Add note</button>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("browser contract markup missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `onclick="`) {
		t.Fatal("note markup exposed an inline onclick instead of the client event binding")
	}

	history := mustRenderNode(t, timelineSectionLocale("en-US", []TimelineEvent{
		{At: "19 Sep 2026, 07:01 UTC", Actor: "Ana Flores", Title: "Promotion requested"},
		{At: "19 Sep 2026, 07:02 UTC", Actor: "System", Title: "Proposal checks completed"},
	}))
	for _, want := range []string{"by Ana Flores", "by System", "Promotion requested", "Proposal checks completed"} {
		if !strings.Contains(history, want) {
			t.Errorf("history markup missing actor or title %q: %s", want, history)
		}
	}
}
