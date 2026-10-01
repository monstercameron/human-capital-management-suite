package journeyclient

import (
	"testing"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTodo_UXBLIND_014 covers the browser-facing journey contract: a note is
// saved by one client submit, the resulting note is attributed, and history
// entries retain an actor. The final visual proof still belongs to the Codex
// browser pass against the running client.
func TestTodo_UXBLIND_014(t *testing.T) {
	t.Run("one note submit records the note and author", func(t *testing.T) {
		_, store, service := notesHarness(t)
		typeNote(store, "Budget line confirmed with Q4 plan.")
		submitNote(store)
		page := waitStore(t, store, "the recorded note", func(p journey.Page) bool {
			return composerSettled(p) && p.Detail != nil && p.Detail.Notes != nil && len(p.Detail.Notes.Notes) == 1
		})

		requests := service.sent()
		if len(requests) != 1 || requests[0].GetBody() != "Budget line confirmed with Q4 plan." {
			t.Fatalf("one client submit sent requests = %+v", requests)
		}
		if got := page.Detail.Notes.Notes[0].Author; got != "Thomas Baker" {
			t.Fatalf("saved note author = %q, want Thomas Baker", got)
		}
		if page.Values[FieldNoteBody] != "" || page.Detail.Notes.Composer.Status == "" {
			t.Fatalf("saved note did not clear and confirm the composer: %+v", page.Detail.Notes.Composer)
		}
	})

	t.Run("history entries name the actor", func(t *testing.T) {
		detail := testDetail(t, journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED)
		detail.Timeline[0].Actor = "Ana Flores"
		detail.Timeline[1].Actor = "System"
		page := DetailPage(testConfig(), detail, nil, nil)
		for _, title := range []string{"Promotion requested", "Proposal checks completed"} {
			var found *journey.TimelineEvent
			for i := range page.Detail.Timeline {
				if page.Detail.Timeline[i].Title == title {
					found = &page.Detail.Timeline[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("history entry %q is missing", title)
			}
			if found.Actor == "" {
				t.Errorf("history entry %q has no actor", title)
			}
		}
	})

	t.Run("failed save keeps draft and exposes recovery", func(t *testing.T) {
		_, store, service := notesHarness(t, status.Error(codes.Unavailable, "note service unavailable"))
		typeNote(store, "Retry after the connection recovers.")
		submitNote(store)
		page := waitStore(t, store, "the note recovery state", func(p journey.Page) bool {
			return composerSettled(p) && p.Detail != nil && p.Detail.Notes != nil && p.Detail.Notes.Composer.Field.Error != ""
		})
		if page.Values[FieldNoteBody] != "Retry after the connection recovers." {
			t.Fatalf("failed save lost draft: %q", page.Values[FieldNoteBody])
		}
		if len(service.sent()) != 1 || page.Detail.Notes.Composer.Status != "" {
			t.Fatalf("failed save recovery state = %+v", page.Detail.Notes.Composer)
		}
	})
}
