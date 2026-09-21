package journey

import (
	"regexp"
	"strings"
	"testing"
)

func notesFixture() *NotesView {
	return &NotesView{
		Notes: []NoteEntry{
			{ID: "n1", Author: "Thomas Baker", Initials: "TB", Stage: "Finance approval", At: "19 Sep 2026, 07:00 UTC",
				ISO: "2026-09-19T07:00:00Z", Body: "Budget confirmed.\nSee the Q4 plan."},
			{ID: "n2", Author: "You", Initials: "Y", Own: true, Body: "Thanks <b>both</b>"},
		},
		Composer: &NoteComposer{
			Field: Field{ID: "note-body", Name: "note_body", Kind: fieldKindTextarea, Label: "Add a note",
				Help: "Everyone who can open this promotion can read notes."},
			MaxRunes: 20,
			Action:   "#/journeys/x",
		},
	}
}

func TestNotesSectionRendersAttributedAccessibleNotes(t *testing.T) {
	out := mustRenderNode(t, notesSectionLocale(live{locale: "en-US"}, notesFixture()))
	for _, want := range []string{
		`id="notes-heading"`, `class="jn-notes-count"`, `<ol class="jn-notes"`,
		`<article aria-label="Note from Thomas Baker" class="jn-note-card"`,
		`<time class="jn-note-at" datetime="2026-09-19T07:00:00Z">19 Sep 2026, 07:00 UTC</time>`,
		`Stage: Finance approval`, `data-own="true"`,
		`class="jn-note-body" dir="auto"`, "Budget confirmed.\nSee the Q4 plan.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("notes markup missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, ">You<") != 1 {
		t.Fatal("an own note with no resolved name said You twice")
	}
	if strings.Contains(out, "<b>both</b>") {
		t.Fatal("a note body was rendered as markup instead of text")
	}
	if !regexp.MustCompile(`<textarea[^>]*id="note-body"`).MatchString(out) || !strings.Contains(out, `>Add note</button>`) {
		t.Fatalf("composer missing:\n%s", out)
	}
	if !strings.Contains(out, `role="status"`) {
		t.Fatal("the composer's confirmation is not announced")
	}
}

func TestNotesComposerCountsAndShowsWhenItIsSending(t *testing.T) {
	view := notesFixture()
	for value, state := range map[string]string{"short": "ok", strings.Repeat("a", 19): "near", strings.Repeat("a", 21): "over"} {
		out := mustRenderNode(t, notesSectionLocale(live{locale: "en-US", values: map[string]string{"note-body": value},
			onFieldChange: func(string, string) {}}, view))
		if !strings.Contains(out, `data-state="`+state+`"`) {
			t.Errorf("count for %d characters is not %s:\n%s", len(value), state, out)
		}
	}
	view.Composer.Busy = true
	out := mustRenderNode(t, notesSectionLocale(live{locale: "en-US"}, view))
	if !regexp.MustCompile(`<button[^>]*disabled[^>]*>Adding…</button>`).MatchString(out) || !strings.Contains(out, `aria-busy="true"`) {
		t.Fatalf("busy composer:\n%s", out)
	}
}

func TestNotesSectionEmptyStateAndAbsence(t *testing.T) {
	if notesSectionLocale(live{locale: "en-US"}, nil) != nil {
		t.Fatal("a nil notes view rendered a panel")
	}
	out := mustRenderNode(t, notesSectionLocale(live{locale: "de-DE"}, &NotesView{}))
	if !strings.Contains(out, "Noch keine Notizen") || strings.Contains(out, "jn-notes-count") || strings.Contains(out, "<form") {
		t.Fatalf("empty, read-only notes panel:\n%s", out)
	}
}
