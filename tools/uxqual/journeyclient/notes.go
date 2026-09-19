package journeyclient

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
)

// Journey notes: free-standing, append-only notes on one journey (see
// internal/humanwork/workspace/journey_notes.go for the server contract).

const (
	// ActionAddNote is the notes composer's submit.
	ActionAddNote = "add-note"
	// FieldNoteBody keys the composer's draft in Page.Values; NameNoteBody is
	// the submitted field name.
	FieldNoteBody = "note-body"
	NameNoteBody  = "note_body"
	// maxNoteRunes mirrors workspace.MaxJourneyNoteRunes (this module does
	// not import the workspace port for a constant); the server re-checks.
	maxNoteRunes = 2000
)

// Server reason references a note refusal can carry, mirrored from
// internal/humanwork/workspace/journey_notes.go.
const (
	noteReasonEmpty       = "journey.note.empty"
	noteReasonTooLong     = "journey.note.too_long"
	noteReasonInvalidText = "journey.note.invalid_text"
	noteReasonKeyReused   = "journey.note.idempotency_key_reused"
	noteReasonLimit       = "journey.note.limit_reached"
)

// NoteService is implemented by the production gRPC client. It is separate
// from [Service] so a fake without notes still drives the whole page; the
// composer reports such a client as unable to add notes.
type NoteService interface {
	AddJourneyNote(ctx context.Context, in *journeyv1.AddJourneyNoteRequest) (*journeyv1.AddJourneyNoteResponse, error)
}

// noteComposerState is the composer's client-owned state: whether a
// submission is in flight, the catalog key of its last refusal, and the
// catalog key of its last confirmation.
type noteComposerState struct {
	busy      bool
	errorKey  string
	statusKey string
}

// notesViewLocale projects a journey's notes and the composer. value is the
// composer's draft, kept in Page.Values so it survives every re-projection
// (a watch refresh, another reviewer's note arriving) until it is added.
func notesViewLocale(locale string, notes []*journeyv1.JourneyNote, value string, state noteComposerState, action string) *journey.NotesView {
	copy := productui.ResolveProductLocale(locale)
	view := &journey.NotesView{}
	for _, note := range notes {
		if note == nil {
			continue
		}
		entry := journey.NoteEntry{
			ID: note.GetNoteId(), Author: strings.TrimSpace(note.GetAuthorDisplay()),
			Own: note.GetAuthoredByViewer(), Body: note.GetBody(),
		}
		if entry.Own && entry.Author == "" {
			entry.Author = copy.Text("journey.note_you")
		}
		entry.Initials = noteInitials(entry.Author)
		if stage := stageOf(note.GetStage()); stage != "" {
			entry.Stage = stageLabelLocale(locale, stage)
		}
		if at := note.GetCreatedAt(); at != nil && at.IsValid() {
			entry.At = formatTimeLocale(locale, at)
			entry.ISO = at.AsTime().UTC().Format(time.RFC3339)
		}
		view.Notes = append(view.Notes, entry)
	}
	field := journey.Field{
		ID: FieldNoteBody, Name: NameNoteBody, Kind: kindTextarea, Value: value,
		Label: copy.Text("journey.note_body_label"), Help: copy.Text("journey.note_body_help"),
		Placeholder: copy.Text("journey.note_body_placeholder"),
	}
	if state.errorKey != "" {
		field.Error = copy.Text(state.errorKey)
	}
	view.Composer = &journey.NoteComposer{Field: field, MaxRunes: maxNoteRunes, Busy: state.busy, Action: action}
	if state.statusKey != "" {
		view.Composer.Status = copy.Text(state.statusKey)
	}
	return view
}

// noteInitials is up to two letters from the first and last words of a
// display name; a name with no letters yields none (the renderer shows a
// neutral mark).
func noteInitials(name string) string {
	var letters []rune
	for _, word := range strings.Fields(name) {
		for _, r := range word {
			if unicode.IsLetter(r) {
				letters = append(letters, unicode.ToUpper(r))
				break
			}
		}
	}
	switch len(letters) {
	case 0:
		return ""
	case 1:
		return string(letters[0])
	default:
		return string(letters[0]) + string(letters[len(letters)-1])
	}
}

// localNoteRefusal reports the catalog key for a draft that cannot be sent,
// or "" when it may be. The server applies the same rules; checking here
// answers at once and keeps the reader's text in place.
func localNoteRefusal(body string) string {
	trimmed := strings.TrimSpace(body)
	switch {
	case trimmed == "":
		return "journey.note_error_empty"
	case utf8.RuneCountInString(trimmed) > maxNoteRunes:
		return "journey.note_error_too_long"
	}
	return ""
}

// noteRefusalKey maps a refused AddJourneyNote onto composer copy. Only the
// typed reason reference is read, never the server's prose.
func noteRefusalKey(err error) (key string, keyReused bool) {
	st, ok := status.FromError(err)
	if !ok {
		return "journey.note_error_failed", false
	}
	switch st.Code() {
	case codes.PermissionDenied, codes.Unauthenticated, codes.NotFound:
		return "journey.note_error_denied", false
	case codes.InvalidArgument:
		for _, detail := range st.Details() {
			typed, ok := detail.(*commonv1.ErrorDetail)
			if !ok {
				continue
			}
			for _, v := range typed.GetFieldViolations() {
				switch v.GetRuleRef() {
				case noteReasonEmpty:
					return "journey.note_error_empty", false
				case noteReasonTooLong:
					return "journey.note_error_too_long", false
				case noteReasonInvalidText:
					return "journey.note_error_invalid", false
				case noteReasonLimit:
					return "journey.note_error_limit", false
				case noteReasonKeyReused:
					return "journey.note_error_failed", true
				}
			}
		}
	}
	return "journey.note_error_failed", false
}

// addNote submits the composer's draft. One idempotency key is minted per
// (journey, text) and kept across retries, so a timeout followed by a second
// click records one note, not two; any edit to the text starts a new key.
func (a *App) addNote(ctx context.Context, generation int, intentID, body string) {
	if intentID == "" {
		return
	}
	service, ok := a.svc.(NoteService)
	if !ok {
		a.setNoteState(noteComposerState{errorKey: "journey.note_error_failed"})
		return
	}
	if key := localNoteRefusal(body); key != "" {
		a.setNoteState(noteComposerState{errorKey: key})
		return
	}
	text := strings.TrimSpace(body)

	a.mu.Lock()
	if a.note.busy {
		a.mu.Unlock()
		return
	}
	if a.noteAttemptKey == "" || a.noteAttemptIntent != intentID || a.noteAttemptBody != text {
		a.noteAttemptKey, a.noteAttemptIntent, a.noteAttemptBody = uuid.NewString(), intentID, text
	}
	key := a.noteAttemptKey
	a.note = noteComposerState{busy: true}
	a.mu.Unlock()
	a.show(a.store.Page().Notice)

	a.runTask(ctx, taskmux.Spec{Key: "journey:note:" + intentID, Priority: taskmux.Interactive, Duplicate: taskmux.KeepExisting}, func(ctx context.Context) {
		resp, err := service.AddJourneyNote(ctx, &journeyv1.AddJourneyNoteRequest{IntentId: intentID, Body: text, IdempotencyKey: key})
		a.mu.Lock()
		if a.generation != generation {
			a.note = noteComposerState{}
			a.mu.Unlock()
			return
		}
		if err != nil {
			refusal, reused := noteRefusalKey(err)
			// A key the server says already names a different note is
			// retired so the next attempt mints a fresh one. Every other
			// failure keeps the key: the note may have been recorded before
			// the answer was lost, and resending the same key is what makes
			// that retry land on the same note instead of a duplicate.
			if reused {
				a.noteAttemptKey = ""
			}
			a.note = noteComposerState{errorKey: refusal}
			a.mu.Unlock()
			a.show(a.store.Page().Notice)
			return
		}
		a.note = noteComposerState{statusKey: "journey.note_added"}
		a.noteAttemptKey, a.noteAttemptIntent, a.noteAttemptBody = "", "", ""
		a.mu.Unlock()
		// The draft is cleared only once the note is recorded; a refusal or a
		// dropped connection leaves the reader's text exactly as typed.
		a.store.Update(func(page *journey.Page) {
			if page.Values != nil {
				page.Values[FieldNoteBody] = ""
			}
		})
		a.applyDetail(generation, resp.GetDetail(), a.store.Page().Notice)
	})
}

func (a *App) setNoteState(state noteComposerState) {
	a.mu.Lock()
	a.note = state
	a.mu.Unlock()
	a.show(a.store.Page().Notice)
}

// editNoteDraft records a keystroke in the composer. A refusal or a
// confirmation on screen answers the text that was submitted, so the first
// edit after one clears it.
func (a *App) editNoteDraft(value string) {
	a.mu.Lock()
	stale := !a.note.busy && (a.note.errorKey != "" || a.note.statusKey != "")
	if stale {
		a.note = noteComposerState{}
	}
	a.mu.Unlock()
	a.store.Update(func(page *journey.Page) {
		if page.Values == nil {
			page.Values = map[string]string{}
		}
		page.Values[FieldNoteBody] = value
	})
	if stale {
		a.show(a.store.Page().Notice)
	}
}
