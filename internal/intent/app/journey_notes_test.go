package app_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

func notesEngine(t *testing.T) (workspace.JourneyEngine, workspace.JourneyNoteEngine, func(string) context.Context) {
	t.Helper()
	engine, principal := promoux012Engine(t)
	notes, ok := engine.(workspace.JourneyNoteEngine)
	if !ok {
		t.Fatal("the composed journey engine records no notes")
	}
	return engine, notes, principal
}

// TestJourneyNotesAreRecordedReadAndAttributedPerViewer runs against real
// PostgreSQL: a note written by one reviewer is returned in the same call,
// read back by every admitted viewer oldest first, stamped with the stage it
// was written at, and marked as the viewer's own only for its author.
func TestJourneyNotesAreRecordedReadAndAttributedPerViewer(t *testing.T) {
	engine, notes, as := notesEngine(t)
	author := as("alice-reviewer")
	proposed := promoux012Propose(t, engine, author, "omar-reyes", "2026-06-01")

	first, detail, err := notes.AddNote(author, proposed.IntentID, workspace.JourneyNoteInput{
		Body: "  Finance: budget line confirmed with Q4 plan.\r\nNo concerns.  ", IdempotencyKey: "note-1"})
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if first.Body != "Finance: budget line confirmed with Q4 plan.\nNo concerns." || !first.AuthoredByViewer ||
		first.Stage != workspace.JourneyStageProposed || first.NoteID == "" || first.CreatedAt.IsZero() {
		t.Fatalf("recorded note = %+v", first)
	}
	if len(detail.Notes) != 1 || detail.Notes[0].NoteID != first.NoteID {
		t.Fatalf("detail returned with the note = %+v", detail.Notes)
	}
	if _, _, err := notes.AddNote(author, proposed.IntentID, workspace.JourneyNoteInput{
		Body: "Second note.", IdempotencyKey: "note-2"}); err != nil {
		t.Fatalf("second AddNote: %v", err)
	}

	other := as("bob-reviewer")
	read, err := engine.Inspect(other, proposed.IntentID)
	if err != nil {
		t.Fatalf("Inspect as another reviewer: %v", err)
	}
	if len(read.Notes) != 2 || read.Notes[0].NoteID != first.NoteID || read.Notes[1].Body != "Second note." {
		t.Fatalf("notes read by another reviewer = %+v", read.Notes)
	}
	for _, note := range read.Notes {
		if note.AuthoredByViewer {
			t.Fatalf("another reviewer sees %s as their own note", note.NoteID)
		}
		if note.AuthorRef != "alice-reviewer" {
			t.Fatalf("note author = %q", note.AuthorRef)
		}
	}
}

// TestJourneyNoteRetriesConvergeAndReusedKeysAreRefused: a retried
// submission returns the first note, eight concurrent submissions with one
// key record exactly one note, and a key reused for different content is
// refused rather than answered with the earlier note.
func TestJourneyNoteRetriesConvergeAndReusedKeysAreRefused(t *testing.T) {
	engine, notes, as := notesEngine(t)
	author := as("alice-reviewer")
	proposed := promoux012Propose(t, engine, author, "omar-reyes", "2026-06-01")

	var wg sync.WaitGroup
	ids := make([]string, 8)
	errs := make([]error, 8)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			note, _, err := notes.AddNote(author, proposed.IntentID, workspace.JourneyNoteInput{
				Body: "Same note, submitted twice.", IdempotencyKey: "retry-key"})
			ids[i], errs[i] = note.NoteID, err
		}(i)
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil {
			t.Fatalf("concurrent submission %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("concurrent submissions recorded different notes: %v", ids)
		}
	}
	read, err := engine.Inspect(author, proposed.IntentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Notes) != 1 {
		t.Fatalf("one key recorded %d notes", len(read.Notes))
	}

	_, _, err = notes.AddNote(author, proposed.IntentID, workspace.JourneyNoteInput{
		Body: "Different content under the same key.", IdempotencyKey: "retry-key"})
	var input *workspace.JourneyInputError
	if !errors.As(err, &input) || input.ReasonRef != workspace.JourneyNoteReasonKeyReused {
		t.Fatalf("reused key = %v, want %s", err, workspace.JourneyNoteReasonKeyReused)
	}
	_, _, err = notes.AddNote(as("bob-reviewer"), proposed.IntentID, workspace.JourneyNoteInput{
		Body: "Same note, submitted twice.", IdempotencyKey: "retry-key"})
	if !errors.As(err, &input) || input.ReasonRef != workspace.JourneyNoteReasonKeyReused {
		t.Fatalf("another author reusing a key = %v, want %s", err, workspace.JourneyNoteReasonKeyReused)
	}
}

// TestJourneyNoteRefusalsRecordNothing: invalid text and unknown journeys are
// refused before anything is written.
func TestJourneyNoteRefusalsRecordNothing(t *testing.T) {
	engine, notes, as := notesEngine(t)
	author := as("alice-reviewer")
	proposed := promoux012Propose(t, engine, author, "omar-reyes", "2026-06-01")

	for _, in := range []workspace.JourneyNoteInput{
		{Body: "   ", IdempotencyKey: "k1"},
		{Body: strings.Repeat("x", workspace.MaxJourneyNoteRunes+1), IdempotencyKey: "k2"},
		{Body: "hidden\x1b[2Jtext", IdempotencyKey: "k3"},
		{Body: "no key"},
	} {
		if _, _, err := notes.AddNote(author, proposed.IntentID, in); !errors.Is(err, workspace.ErrJourneyInput) {
			t.Fatalf("AddNote(%.20q) = %v, want an input refusal", in.Body, err)
		}
	}
	if _, _, err := notes.AddNote(author, "01a0b867-0000-7000-8000-000000000000", workspace.JourneyNoteInput{
		Body: "orphan", IdempotencyKey: "k4"}); !errors.Is(err, workspace.ErrJourneyUnknown) {
		t.Fatalf("unknown journey = %v, want ErrJourneyUnknown", err)
	}
	if _, _, err := notes.AddNote(author, "not-a-uuid", workspace.JourneyNoteInput{
		Body: "orphan", IdempotencyKey: "k5"}); !errors.Is(err, workspace.ErrJourneyUnknown) {
		t.Fatalf("malformed journey id = %v, want ErrJourneyUnknown", err)
	}
	read, err := engine.Inspect(author, proposed.IntentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Notes) != 0 {
		t.Fatalf("refused submissions recorded notes: %+v", read.Notes)
	}
}
