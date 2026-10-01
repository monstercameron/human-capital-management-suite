package journey

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"

func valueOrJourneyCapabilities(value *productui.JourneyCapabilityAvailability) productui.JourneyCapabilityAvailability {
	if value == nil {
		return productui.JourneyCapabilityAvailability{Chat: true, Projects: true}
	}
	return *value
}
