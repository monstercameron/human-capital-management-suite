package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXAUDIT_017_JourneysGroupByLifecycleWithoutLosingSubjects(t *testing.T) {
	cards := []JourneyCard{
		{IntentID: "closed", WorkerName: "Closed Worker", Href: "/closed", Group: JourneyGroupClosed},
		{IntentID: "review-a", WorkerName: "Review Worker A", Href: "/review-a", Group: JourneyGroupReview},
		{IntentID: "wait", WorkerName: "Waiting Worker", Href: "/wait", Group: JourneyGroupWaiting},
		{IntentID: "review-b", WorkerName: "Review Worker B", Href: "/review-b", Group: JourneyGroupReview},
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := ui.RenderToString(journeysSection(locale, ListView{Journeys: cards}))
		if err != nil {
			t.Fatal(err)
		}
		for _, subject := range []string{"Closed Worker", "Review Worker A", "Waiting Worker", "Review Worker B"} {
			if strings.Count(markup, subject) != 1 {
				t.Fatalf("%s subject %q omitted or duplicated", locale, subject)
			}
		}
		if strings.Index(markup, `id="journeys-group-review"`) >= strings.Index(markup, `id="journeys-group-closed"`) {
			t.Fatalf("%s open review should precede closed history", locale)
		}
		if strings.Index(markup, "Review Worker A") >= strings.Index(markup, "Review Worker B") {
			t.Fatalf("%s service order within review group changed", locale)
		}
		for _, id := range []string{"review", "waiting", "closed"} {
			if !strings.Contains(markup, `aria-labelledby="journeys-group-`+id+`"`) {
				t.Fatalf("%s missing accessible group %s", locale, id)
			}
		}
	}
}
