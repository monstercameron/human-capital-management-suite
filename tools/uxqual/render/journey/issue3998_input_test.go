package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The journey detail carries the input slot where Chat is available, in the
// page's language, and nothing where it is not.
func TestIssue3998_JourneyDetailCarriesTheInputSlotOnlyWhereChatIsAvailable(t *testing.T) {
	with, err := ui.RenderToString(detailView(Page{Locale: "de-DE"}, DetailView{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(with, `data-projectui-input="journey:current"`) || !strings.Contains(with, "Um Einschätzung bitten") {
		t.Fatalf("no input slot in the German detail: %s", with)
	}
	without, err := ui.RenderToString(detailView(Page{JourneyCapabilities: &productui.JourneyCapabilityAvailability{Projects: true}}, DetailView{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(without, "projectui-journey-input") {
		t.Fatalf("the slot is offered without Chat: %s", without)
	}
}
