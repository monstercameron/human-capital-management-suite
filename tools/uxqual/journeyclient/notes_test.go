package journeyclient

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// noteFake is fakeService plus the notes port. Each call pops one scripted
// error (nil means success) and records the request.
type noteFake struct {
	*fakeService
	mu       sync.Mutex
	requests []*journeyv1.AddJourneyNoteRequest
	errs     []error
	release  chan struct{}
}

func (n *noteFake) AddJourneyNote(_ context.Context, in *journeyv1.AddJourneyNoteRequest) (*journeyv1.AddJourneyNoteResponse, error) {
	if n.release != nil {
		<-n.release
	}
	n.mu.Lock()
	n.requests = append(n.requests, in)
	var err error
	if len(n.errs) > 0 {
		err, n.errs = n.errs[0], n.errs[1:]
	}
	n.mu.Unlock()
	if err != nil {
		return nil, err
	}
	note := &journeyv1.JourneyNote{NoteId: "note-" + in.GetIdempotencyKey(), AuthorDisplay: "Thomas Baker", AuthoredByViewer: true,
		Body: in.GetBody(), Stage: journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL,
		CreatedAt: timestamppb.New(time.Date(2026, 9, 19, 7, 0, 0, 0, time.UTC))}
	n.fakeService.mu.Lock()
	detail := n.fakeService.detail
	n.fakeService.mu.Unlock()
	detail.Notes = append(detail.Notes, note)
	return &journeyv1.AddJourneyNoteResponse{Note: note, Detail: detail}, nil
}

func (n *noteFake) sent() []*journeyv1.AddJourneyNoteRequest {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]*journeyv1.AddJourneyNoteRequest(nil), n.requests...)
}

func notesHarness(t *testing.T, errs ...error) (*App, *journey.Store, *noteFake) {
	t.Helper()
	base := newHarness(t)
	base.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	svc := &noteFake{fakeService: base.svc, errs: errs}
	store := journey.NewStore(journey.Page{})
	app := New(testConfig(), svc, store, func() time.Time { return time.Date(2026, 9, 19, 7, 0, 0, 0, time.UTC) })
	app.WatchRetry = 0
	app.Start(context.Background(), DetailHref(base.svc.detail.GetJourney().GetIntentId()))
	waitStore(t, store, "the notes composer", func(p journey.Page) bool {
		return p.Detail != nil && p.Detail.Notes != nil && p.Detail.Notes.Composer != nil && p.Notice == nil
	})
	return app, store, svc
}

// waitStore polls until the store's page satisfies cond; the client runs
// its reads and writes asynchronously, as it does in the browser.
func waitStore(t *testing.T, store *journey.Store, what string, cond func(journey.Page) bool) journey.Page {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p := store.Page(); cond(p) {
			return p
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
	return journey.Page{}
}

func composerSettled(p journey.Page) bool {
	return p.Detail != nil && p.Detail.Notes != nil && p.Detail.Notes.Composer != nil && !p.Detail.Notes.Composer.Busy
}

func typeNote(store *journey.Store, text string) {
	store.Page().OnFieldChange(FieldNoteBody, text)
}

func submitNote(store *journey.Store) {
	p := store.Page()
	p.Detail.Notes.Composer.OnSubmit(map[string]string{NameNoteBody: p.Values[FieldNoteBody]})
}

func TestAddingANoteShowsItClearsTheDraftAndConfirms(t *testing.T) {
	_, store, svc := notesHarness(t)
	typeNote(store, "  Budget line confirmed with Q4 plan.  ")
	submitNote(store)
	waitStore(t, store, "the added note", func(p journey.Page) bool {
		return composerSettled(p) && len(p.Detail.Notes.Notes) == 1 && p.Detail.Notes.Composer.Status != ""
	})

	sent := svc.sent()
	if len(sent) != 1 || sent[0].GetBody() != "Budget line confirmed with Q4 plan." || sent[0].GetIdempotencyKey() == "" {
		t.Fatalf("sent = %+v", sent)
	}
	p := store.Page()
	notes := p.Detail.Notes
	if len(notes.Notes) != 1 || notes.Notes[0].Body != "Budget line confirmed with Q4 plan." || !notes.Notes[0].Own {
		t.Fatalf("notes after adding = %+v", notes.Notes)
	}
	if p.Values[FieldNoteBody] != "" || notes.Composer.Field.Value != "" {
		t.Fatalf("draft kept after a recorded note: %q", p.Values[FieldNoteBody])
	}
	if notes.Composer.Status == "" || notes.Composer.Busy || notes.Composer.Field.Error != "" {
		t.Fatalf("composer after success = %+v", notes.Composer)
	}
	// The confirmation answers the note just added; the next keystroke
	// starts a new note and clears it.
	typeNote(store, "N")
	if got := store.Page().Detail.Notes.Composer.Status; got != "" {
		t.Fatalf("confirmation survived a new draft: %q", got)
	}
}

func TestAFailedNoteKeepsTheTextAndRetriesWithTheSameKey(t *testing.T) {
	_, store, svc := notesHarness(t, status.Error(codes.Unavailable, "connection lost"))
	typeNote(store, "Manager: aligned with the team plan.")
	submitNote(store)
	p := waitStore(t, store, "the inline failure", func(p journey.Page) bool {
		return composerSettled(p) && p.Detail.Notes.Composer.Field.Error != ""
	})
	if p.Values[FieldNoteBody] != "Manager: aligned with the team plan." {
		t.Fatalf("a failed submission lost the draft: %q", p.Values[FieldNoteBody])
	}
	if p.Detail.Notes.Composer.Field.Error == "" || len(p.Detail.Notes.Notes) != 0 {
		t.Fatalf("failure not reported inline: %+v", p.Detail.Notes)
	}
	submitNote(store)
	waitStore(t, store, "the retried note", func(p journey.Page) bool {
		return composerSettled(p) && len(p.Detail.Notes.Notes) == 1
	})
	sent := svc.sent()
	if len(sent) != 2 || sent[0].GetIdempotencyKey() != sent[1].GetIdempotencyKey() {
		t.Fatalf("a retry of unchanged text used a new key: %+v", sent)
	}
	if len(store.Page().Detail.Notes.Notes) != 1 {
		t.Fatal("the retried note did not appear")
	}

	typeNote(store, "A different note.")
	submitNote(store)
	waitStore(t, store, "the second note", func(p journey.Page) bool {
		return composerSettled(p) && len(p.Detail.Notes.Notes) == 2
	})
	sent = svc.sent()
	if sent[2].GetIdempotencyKey() == sent[1].GetIdempotencyKey() {
		t.Fatal("a new note reused the previous note's key")
	}
}

func TestInvalidNotesAreRefusedWithoutACallAndServerReasonsAreMapped(t *testing.T) {
	_, store, svc := notesHarness(t)
	copy := productui.ResolveProductLocale("")
	typeNote(store, "   ")
	submitNote(store)
	if got := store.Page().Detail.Notes.Composer.Field.Error; got != copy.Text("journey.note_error_empty") {
		t.Fatalf("empty note error = %q", got)
	}
	typeNote(store, strings.Repeat("x", maxNoteRunes+1))
	submitNote(store)
	if got := store.Page().Detail.Notes.Composer.Field.Error; got != copy.Text("journey.note_error_too_long") {
		t.Fatalf("long note error = %q", got)
	}
	if len(svc.sent()) != 0 {
		t.Fatal("a locally invalid note was sent")
	}

	for reason, want := range map[string]string{
		noteReasonInvalidText: "journey.note_error_invalid",
		noteReasonLimit:       "journey.note_error_limit",
		noteReasonTooLong:     "journey.note_error_too_long",
	} {
		st, _ := status.New(codes.InvalidArgument, "private").WithDetails(&commonv1.ErrorDetail{
			FieldViolations: []*commonv1.FieldViolation{{FieldPath: "body", RuleRef: reason}},
		})
		if got, _ := noteRefusalKey(st.Err()); got != want {
			t.Errorf("reason %s mapped to %s, want %s", reason, got, want)
		}
	}
	if got, _ := noteRefusalKey(status.Error(codes.PermissionDenied, "no")); got != "journey.note_error_denied" {
		t.Errorf("denied mapped to %s", got)
	}
	st, _ := status.New(codes.InvalidArgument, "x").WithDetails(&commonv1.ErrorDetail{
		FieldViolations: []*commonv1.FieldViolation{{FieldPath: "idempotency_key", RuleRef: noteReasonKeyReused}},
	})
	if _, reused := noteRefusalKey(st.Err()); !reused {
		t.Error("a reused key was not reported, so the next attempt would repeat it")
	}
}

func TestADoubleSubmitWhileAddingSendsOneNote(t *testing.T) {
	base := newHarness(t)
	base.svc.detail = testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL)
	svc := &noteFake{fakeService: base.svc, release: make(chan struct{})}
	store := journey.NewStore(journey.Page{})
	app := New(testConfig(), svc, store, time.Now)
	app.WatchRetry = 0
	app.Start(context.Background(), DetailHref(base.svc.detail.GetJourney().GetIntentId()))
	waitStore(t, store, "the notes composer", func(p journey.Page) bool {
		return composerSettled(p) && p.Notice == nil
	})
	typeNote(store, "One note.")
	submitNote(store)
	waitStore(t, store, "the note in flight", func(p journey.Page) bool {
		return p.Detail != nil && p.Detail.Notes != nil && p.Detail.Notes.Composer.Busy
	})
	submitNote(store)
	close(svc.release)
	waitStore(t, store, "the recorded note", func(p journey.Page) bool {
		return composerSettled(p) && len(p.Detail.Notes.Notes) == 1
	})
	if n := len(svc.sent()); n != 1 {
		t.Fatalf("a double submit sent %d notes", n)
	}
}

func TestNotesProjectionAttributesAndFormats(t *testing.T) {
	view := notesViewLocale("de-DE", []*journeyv1.JourneyNote{
		{NoteId: "a", AuthorDisplay: "Priyanka Sharma", Body: "Team plan ok.", Stage: journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL,
			CreatedAt: timestamppb.New(time.Date(2026, 9, 19, 7, 5, 0, 0, time.UTC))},
		{NoteId: "b", AuthoredByViewer: true, Body: "Meine Notiz."},
		nil,
	}, "Entwurf", noteComposerState{errorKey: "journey.note_error_failed"}, "#/journeys/x")
	if len(view.Notes) != 2 {
		t.Fatalf("notes = %+v", view.Notes)
	}
	first, own := view.Notes[0], view.Notes[1]
	if first.Initials != "PS" || first.Own || first.ISO != "2026-09-19T07:05:00Z" || first.At == "" ||
		first.Stage != stageLabelLocale("de-DE", stageManagerApproval) {
		t.Fatalf("first note = %+v", first)
	}
	if !own.Own || own.Author != productui.ResolveProductLocale("de-DE").Text("journey.note_you") {
		t.Fatalf("own note without a resolved name = %+v", own)
	}
	c := view.Composer
	if c.Field.Value != "Entwurf" || c.Field.Error == "" || c.MaxRunes != maxNoteRunes || c.Field.Label != productui.ResolveProductLocale("de-DE").Text("journey.note_body_label") {
		t.Fatalf("composer = %+v", c)
	}
	for name, want := range map[string]string{"Thomas Baker": "TB", "Cher": "C", "  ": "", "Mary-Jane van Dyke": "MD", "أحمد علي": "أع"} {
		if got := noteInitials(name); got != want {
			t.Errorf("noteInitials(%q) = %q, want %q", name, got, want)
		}
	}
}
