package productui

import (
	"strings"
	"testing"
)

func TestTodo_REV_075_02(t *testing.T) {
	view := testView(PageReviewParticipants)
	view.ReviewParticipants = &ReviewParticipantsProjection{Cycles: []ReviewParticipantsCycleProjection{{
		CycleID: "cycle-1", CycleRevision: 2, GraphRevision: 1, GraphDigest: "sha256:test",
		Assignments: []ReviewParticipantAssignmentProjection{{ParticipantID: "worker-a", ReviewerID: "worker-b", Relationship: "MANAGER"}},
	}}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, visible := range []string{"worker-a", "worker-b", "Review cycle", "Manager review"} {
		if !strings.Contains(doc, visible) {
			t.Fatalf("authorized projection omits %q: %s", visible, doc)
		}
	}
	if strings.Contains(doc, "sha256:test") {
		t.Fatal("internal graph digest leaked into the participant page")
	}
}

func TestTodo_REV_075_02_LocalizedPopulatedState(t *testing.T) {
	for _, localeCode := range []string{"de-DE", "ar"} {
		view := ApplyLocale(testView(PageReviewParticipants), ResolveProductLocale(localeCode))
		view.ReviewParticipants = &ReviewParticipantsProjection{Cycles: []ReviewParticipantsCycleProjection{{
			CycleID: "cycle-1", CycleRevision: 2,
			Assignments: []ReviewParticipantAssignmentProjection{{ParticipantID: "worker-a", ReviewerID: "worker-b", Relationship: "MANAGER"}},
		}}}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"page.review_participants.title", "review_participants.cycle_heading", "review_participants.relationship.MANAGER"} {
			localized := view.Locale.Text(key, map[string]string{"revision": "2"})
			if localized == "" || strings.Contains(doc, "⟦") || !strings.Contains(doc, localized) {
				t.Fatalf("locale %s omitted %s=%q: %s", localeCode, key, localized, doc)
			}
		}
		if strings.Contains(doc, "Review cycle") || strings.Contains(doc, "Manager review") || strings.Contains(doc, "is reviewed by") {
			t.Fatalf("locale %s fell back to English populated-state copy: %s", localeCode, doc)
		}
	}
}

func TestTodo_REV_075_02_EmptyVsUnavailable(t *testing.T) {
	view := testView(PageReviewParticipants)
	view.ReviewParticipants = &ReviewParticipantsProjection{Cycles: []ReviewParticipantsCycleProjection{}}
	empty, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	view.ReviewParticipants = nil
	unavailable, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty, view.Locale.Text("review_participants.empty_title")) || strings.Contains(empty, view.Locale.Text("review_participants.unavailable_title")) {
		t.Fatalf("authorized empty projection was not presented as empty: %s", empty)
	}
	if !strings.Contains(unavailable, view.Locale.Text("review_participants.unavailable_title")) || strings.Contains(unavailable, view.Locale.Text("review_participants.empty_title")) {
		t.Fatalf("unavailable service state was presented as an authorized empty result: %s", unavailable)
	}
}
