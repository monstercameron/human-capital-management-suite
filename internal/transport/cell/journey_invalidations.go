package cell

import (
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	transportjourney "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
)

// journeyInvalidations returns the cell's committed-transition hub as the
// journey transport's InvalidationSource (REV-091-03), or a nil interface
// when the cell composed no journey engine. Returning the nil *Hub inside
// the interface would defeat the transport's own nil check and turn an
// UNAVAILABLE into a nil-pointer failure.
func journeyInvalidations(c *app.Cell) transportjourney.InvalidationSource {
	if c == nil || c.JourneyInvalidations == nil {
		return nil
	}
	return c.JourneyInvalidations
}
