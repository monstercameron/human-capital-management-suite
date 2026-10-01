package workspace

import (
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// ClockConfig is the JSON-island form of productui.ClockAvailabilityProjection
// (UXBLIND-123): the workspace half of the time clock's availability. The
// per-worker half is a decision only the clock service can make, so it is read
// by the browser from the worker self-clock boundary and never guessed here.
type ClockConfig struct {
	// Enabled reports whether this cell serves the worker self clock at all.
	Enabled bool `json:"enabled"`
	// ViewerIsAdmin reports whether the viewer administers access; only an
	// administrator is told how to turn the clock on.
	ViewerIsAdmin bool `json:"viewer_is_admin"`
}

// clockAdmin is the viewer's authority to administer who can use what: the
// same authority as the Roles & access page.
func clockAdmin(access productAccess) bool {
	if access.configured {
		return access.can(productui.PageRoles, roleaccess.ActionUpdate)
	}
	return access.can(productui.PageRoles, roleaccess.ActionView)
}

// resolveClock composes the viewer's workspace-level clock availability from
// server-derived inputs only: whether the cell composed the service, and the
// administrator bit from the durable role policy.
func (h *Handler) resolveClock(access productAccess) *ClockConfig {
	return &ClockConfig{Enabled: h.clockEnabled, ViewerIsAdmin: clockAdmin(access)}
}

// ProductClockAvailability converts the island form into the page contract.
// A missing island fails closed: the clock is reported as not running here.
func ProductClockAvailability(config *ClockConfig) productui.ClockAvailabilityProjection {
	if config == nil {
		return productui.ClockAvailabilityProjection{}
	}
	return productui.ClockAvailabilityProjection{Enabled: config.Enabled, ViewerIsAdmin: config.ViewerIsAdmin}
}
