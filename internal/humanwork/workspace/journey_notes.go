package workspace

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Journey notes are free-standing, append-only notes on one journey. They let
// the proposer and the reviewers hand context from one stage to the next and
// leave a record a later reader can consult. A note is not a decision: it
// carries no authority, moves no lifecycle dimension and is never edited or
// removed.

const (
	// MaxJourneyNoteRunes bounds one note's body, counted in characters
	// after surrounding whitespace is trimmed. It sits well inside the
	// transport's per-string byte bound for every script.
	MaxJourneyNoteRunes = 2000
	// MaxJourneyNotes bounds how many notes one journey may carry, so one
	// journey's detail read stays bounded.
	MaxJourneyNotes = 200
	// MaxJourneyNoteKeyRunes bounds an idempotency key.
	MaxJourneyNoteKeyRunes = 128
)

// Reason references a note refusal carries. Transport projects these, never
// the Detail text.
const (
	JourneyNoteReasonEmpty       = "journey.note.empty"
	JourneyNoteReasonTooLong     = "journey.note.too_long"
	JourneyNoteReasonInvalidText = "journey.note.invalid_text"
	JourneyNoteReasonKeyInvalid  = "journey.note.idempotency_key_invalid"
	JourneyNoteReasonKeyReused   = "journey.note.idempotency_key_reused"
	JourneyNoteReasonLimit       = "journey.note.limit_reached"
)

// JourneyNote is one recorded note. AuthorRef is the author's principal and
// stays on the server; AuthorDisplay is what a reader is shown.
type JourneyNote struct {
	NoteID           string
	AuthorRef        string
	AuthorDisplay    string
	AuthoredByViewer bool
	Body             string
	Stage            JourneyStage
	CreatedAt        time.Time
}

// JourneyNoteInput is one submission.
type JourneyNoteInput struct {
	Body           string
	IdempotencyKey string
}

// JourneyNoteEngine is implemented by an engine that records journey notes.
// It is a separate port from [JourneyEngine] so an engine without note
// storage is still a complete journey engine; transport reports such a cell
// as not offering notes rather than failing to compose.
type JourneyNoteEngine interface {
	// AddNote records one note on intentID as the principal in ctx and
	// returns it together with the journey detail read in the same call.
	// The caller must be admitted to the journey's detail. A retry with the
	// same idempotency key returns the note the first call recorded.
	AddNote(ctx context.Context, intentID string, in JourneyNoteInput) (JourneyNote, JourneyDetail, error)
}

// NormalizeJourneyNote validates one submission and returns it with the body
// trimmed and line endings normalized to "\n". Every refusal is a
// [*JourneyInputError] naming "body" or "idempotency_key".
func NormalizeJourneyNote(in JourneyNoteInput) (JourneyNoteInput, error) {
	body := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(in.Body, "\r\n", "\n"), "\r", "\n"))
	key := strings.TrimSpace(in.IdempotencyKey)
	switch {
	case key == "" || utf8.RuneCountInString(key) > MaxJourneyNoteKeyRunes || !utf8.ValidString(key):
		return JourneyNoteInput{}, &JourneyInputError{FieldPath: "idempotency_key", ReasonRef: JourneyNoteReasonKeyInvalid,
			Detail: "an idempotency key of 1 to 128 characters is required"}
	case !utf8.ValidString(body):
		return JourneyNoteInput{}, &JourneyInputError{FieldPath: "body", ReasonRef: JourneyNoteReasonInvalidText,
			Detail: "the note is not valid UTF-8 text"}
	case body == "":
		return JourneyNoteInput{}, &JourneyInputError{FieldPath: "body", ReasonRef: JourneyNoteReasonEmpty,
			Detail: "the note is empty"}
	case utf8.RuneCountInString(body) > MaxJourneyNoteRunes:
		return JourneyNoteInput{}, &JourneyInputError{FieldPath: "body", ReasonRef: JourneyNoteReasonTooLong,
			Detail: "the note exceeds 2000 characters"}
	}
	for _, r := range body {
		// Newlines and tabs are text; every other control character (NUL,
		// escape sequences, bidi overrides that would reorder what a later
		// reader sees) is refused rather than stored invisibly.
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) || isBidiControl(r) {
			return JourneyNoteInput{}, &JourneyInputError{FieldPath: "body", ReasonRef: JourneyNoteReasonInvalidText,
				Detail: "the note contains a control character"}
		}
	}
	return JourneyNoteInput{Body: body, IdempotencyKey: key}, nil
}

// isBidiControl reports the explicit directional embedding, override and
// isolate characters. Ordinary right-to-left text needs none of them, and
// they can make a stored note read differently from what was written.
func isBidiControl(r rune) bool {
	switch {
	case r >= '‪' && r <= '‮':
		return true
	case r >= '⁦' && r <= '⁩':
		return true
	}
	return false
}
