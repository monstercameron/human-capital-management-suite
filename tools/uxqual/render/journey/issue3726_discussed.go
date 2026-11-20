package journey

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// journeyDiscussedSlot is the "Discussed in" slot under the detail toolbar
// (issue 3726). It exists only where Chat is available to the viewer; the
// browser host fills it with the conversations the viewer may open.
func journeyDiscussedSlot(p Page) ui.Node {
	if !valueOrJourneyCapabilities(p.JourneyCapabilities).Chat {
		return nil
	}
	return productui.ProjectJourneyDiscussed(p.Locale)
}
