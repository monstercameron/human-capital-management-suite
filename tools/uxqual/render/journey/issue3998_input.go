package journey

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// journeyInputSlot is the "Ask for input" slot under the clarification slot
// (issue 3998). It exists only where Chat is available to the viewer; the
// browser host asks the server whether this viewer holds the decision or has
// asked something, and reveals the slot only then.
func journeyInputSlot(p Page, v DetailView) ui.Node {
	// A closed journey takes no further input, so its page asks for none.
	if v.Journey.Closed {
		return nil
	}
	if !valueOrJourneyCapabilities(p.JourneyCapabilities).Chat {
		return nil
	}
	return productui.ProjectJourneyInput(p.Locale)
}
