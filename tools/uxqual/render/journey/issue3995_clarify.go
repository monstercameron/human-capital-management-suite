package journey

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// journeyClarifySlot is the clarification slot under the "Discussed in" slot
// (issue 3995). It exists only where Chat is available to the viewer; the
// browser host asks the server whether this viewer holds the decision or is
// part of a question, and reveals the slot only then.
func journeyClarifySlot(p Page, v DetailView) ui.Node {
	// A closed journey takes no further question, so its page offers none.
	if v.Journey.Closed {
		return nil
	}
	if !valueOrJourneyCapabilities(p.JourneyCapabilities).Chat {
		return nil
	}
	return productui.ProjectJourneyClarify(p.Locale)
}
