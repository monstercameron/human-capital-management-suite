package cell

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/journeyinvalidation"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

func TestJourneyInvalidationsNeverWrapsANilHub(t *testing.T) {
	if got := journeyInvalidations(nil); got != nil {
		t.Fatalf("nil cell = %#v, want a nil source", got)
	}
	if got := journeyInvalidations(&app.Cell{}); got != nil {
		t.Fatalf("cell without a hub = %#v, want a nil interface, not a typed nil", got)
	}
	hub := journeyinvalidation.NewHub(journeyinvalidation.Options{})
	if got := journeyInvalidations(&app.Cell{JourneyInvalidations: hub}); got != hub {
		t.Fatalf("cell hub = %#v, want the cell's own hub", got)
	}
}
