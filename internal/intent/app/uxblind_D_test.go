package app

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

func TestTodo_UXBLIND_014(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	detail := workspace.JourneyDetail{
		Timeline: []workspace.JourneyEvent{{At: at, Kind: JourneyEventIntentCreated, Title: "Promotion proposed"}, {At: at.Add(time.Minute), Kind: JourneyEventWorkItem, Actor: "principal-1", Title: "Finance review assigned"}},
		Notes:    []workspace.JourneyNote{{CreatedAt: at.Add(2 * time.Minute), AuthorRef: "principal-2", AuthorDisplay: "Loretta Finance", Body: "Please confirm the budget line."}},
	}
	projectJourneyHistory(&detail, func(ref string) string {
		if ref == "principal-1" {
			return "Thomas Manager"
		}
		return ""
	})
	if len(detail.Timeline) != 3 {
		t.Fatalf("timeline entries=%d, want actor-bearing history plus note: %+v", len(detail.Timeline), detail.Timeline)
	}
	if detail.Timeline[0].Actor != "system" || detail.Timeline[1].Actor != "Thomas Manager" || detail.Timeline[2].Kind != JourneyEventNote || detail.Timeline[2].Actor != "Loretta Finance" {
		t.Fatalf("history actors/notes = %+v", detail.Timeline)
	}
}
