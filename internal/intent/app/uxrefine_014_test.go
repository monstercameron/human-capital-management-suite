package app

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
)

func TestTodo_UXBLIND_014_StoredInitiatorSurvivesViewerProjection(t *testing.T) {
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	detail := workspace.JourneyDetail{Timeline: []workspace.JourneyEvent{{
		At: at, Kind: JourneyEventIntentCreated, Title: "Promotion proposed",
	}}}
	assignJourneyInitiator(&detail, "principal:requester")

	// The directory resolver is tenant-scoped by its caller. It knows the
	// recorded requester but deliberately has no entry for the viewer; the
	// viewer must never be substituted as the actor.
	projectJourneyHistory(&detail, func(ref string) string {
		if ref == "principal:requester" {
			return "Ana Flores"
		}
		return ""
	})
	if len(detail.Timeline) != 1 || detail.Timeline[0].Actor != "Ana Flores" {
		t.Fatalf("stored initiator projection = %+v, want Ana Flores", detail.Timeline)
	}
}

func TestAssignJourneyInitiatorLeavesMissingStoredActorUninvented(t *testing.T) {
	detail := workspace.JourneyDetail{Timeline: []workspace.JourneyEvent{{Kind: JourneyEventIntentCreated}}}
	assignJourneyInitiator(&detail, " ")
	if detail.Timeline[0].Actor != "" {
		t.Fatalf("empty stored initiator became %q", detail.Timeline[0].Actor)
	}
}
