package journey_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// noteEngine is fakeEngine plus the optional notes port.
type noteEngine struct {
	*fakeEngine
	mu       sync.Mutex
	gotID    string
	gotInput workspace.JourneyNoteInput
	err      error
}

var _ workspace.JourneyNoteEngine = (*noteEngine)(nil)

const notePrincipal = "principal-that-must-not-leak"

func (n *noteEngine) AddNote(_ context.Context, intentID string, in workspace.JourneyNoteInput) (workspace.JourneyNote, workspace.JourneyDetail, error) {
	n.mu.Lock()
	n.gotID, n.gotInput = intentID, in
	err := n.err
	n.mu.Unlock()
	if err != nil {
		return workspace.JourneyNote{}, workspace.JourneyDetail{}, err
	}
	note := workspace.JourneyNote{
		NoteID: "note-1", AuthorRef: notePrincipal, AuthorDisplay: "Thomas Baker", AuthoredByViewer: true,
		Body: in.Body, Stage: workspace.JourneyStageFinanceApproval,
		CreatedAt: time.Date(2026, 9, 19, 7, 0, 0, 0, time.UTC),
	}
	detail := fixtureDetail()
	detail.Notes = []workspace.JourneyNote{note}
	return note, detail, nil
}

func TestAddJourneyNoteForwardsAndNeverDisclosesTheAuthorPrincipal(t *testing.T) {
	engine := &noteEngine{fakeEngine: newFakeEngine()}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))

	resp, err := client.AddJourneyNote(testContext(t), &journeyv1.AddJourneyNoteRequest{
		IntentId: fixtureIntentID, Body: "Budget confirmed.", IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("AddJourneyNote: %v", err)
	}
	engine.mu.Lock()
	gotID, gotInput := engine.gotID, engine.gotInput
	engine.mu.Unlock()
	if gotID != fixtureIntentID || gotInput.Body != "Budget confirmed." || gotInput.IdempotencyKey != "key-1" {
		t.Fatalf("engine saw %q %+v", gotID, gotInput)
	}
	note := resp.GetNote()
	if note.GetNoteId() != "note-1" || note.GetAuthorDisplay() != "Thomas Baker" || !note.GetAuthoredByViewer() ||
		note.GetStage() != journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL || note.GetCreatedAt().AsTime().IsZero() {
		t.Fatalf("note = %+v", note)
	}
	if len(resp.GetDetail().GetNotes()) != 1 || resp.GetDetail().GetDetailDigest() == "" {
		t.Fatalf("detail notes = %+v digest %q", resp.GetDetail().GetNotes(), resp.GetDetail().GetDetailDigest())
	}
	wire, err := proto.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), notePrincipal) {
		t.Fatal("the author's principal identifier crossed the wire")
	}
}

func TestAddJourneyNoteProjectsTypedRefusals(t *testing.T) {
	engine := &noteEngine{fakeEngine: newFakeEngine(), err: &workspace.JourneyInputError{
		FieldPath: "body", ReasonRef: workspace.JourneyNoteReasonTooLong, Detail: "private detail text",
	}}
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: engine}))
	_, err := client.AddJourneyNote(testContext(t), &journeyv1.AddJourneyNoteRequest{
		IntentId: fixtureIntentID, Body: "x", IdempotencyKey: "key-1",
	})
	st := status.Convert(err)
	if st.Code() != codes.InvalidArgument || strings.Contains(st.Message(), "private detail") {
		t.Fatalf("refusal = %v", err)
	}
	found := false
	for _, detail := range st.Details() {
		if typed, ok := detail.(*commonv1.ErrorDetail); ok {
			for _, v := range typed.GetFieldViolations() {
				if v.GetFieldPath() == "body" && v.GetRuleRef() == workspace.JourneyNoteReasonTooLong {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("the too-long refusal did not name the body field: %v", st.Details())
	}
}

func TestAddJourneyNoteOnACellWithoutNotesIsUnavailable(t *testing.T) {
	client := dialJourneyClient(startTestServer(t, journey.Dependencies{Engine: newFakeEngine()}))
	_, err := client.AddJourneyNote(testContext(t), &journeyv1.AddJourneyNoteRequest{
		IntentId: fixtureIntentID, Body: "x", IdempotencyKey: "key-1",
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("a cell without notes answered %v, want UNAVAILABLE", err)
	}
}
