package journeyclient

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"

// JourneyCapabilityAvailability projects the host's published page-action
// permissions into the journey renderer's optional share targets. An empty
// permission map preserves the standalone client's legacy behavior; once the
// host publishes permissions, an absent action is treated as unavailable.
func JourneyCapabilityAvailability(cfg Config) *productui.JourneyCapabilityAvailability {
	if len(cfg.PagePermissions) == 0 {
		return nil
	}
	return &productui.JourneyCapabilityAvailability{
		Chat:     cfg.CanPageAction("chat", "view"),
		Projects: cfg.CanPageAction("projects", "view"),
	}
}
