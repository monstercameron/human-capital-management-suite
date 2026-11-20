package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The journey detail carries the "Discussed in" slot where Chat is available,
// and nothing where it is not.
func TestIssue3726_JourneyDetailCarriesTheDiscussedInSlotOnlyWhereChatIsAvailable(t *testing.T) {
	with, err := ui.RenderToString(detailView(Page{Locale: "de-DE"}, DetailView{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(with, `data-projectui-discussed="journey:current"`) || !strings.Contains(with, "Besprochen in") {
		t.Fatalf("no slot in the German detail: %s", with)
	}
	without, err := ui.RenderToString(detailView(Page{JourneyCapabilities: &productui.JourneyCapabilityAvailability{Projects: true}}, DetailView{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(without, "projectui-discussed") {
		t.Fatalf("the slot is offered without Chat: %s", without)
	}
}
