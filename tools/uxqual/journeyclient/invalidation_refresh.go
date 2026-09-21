package journeyclient

import (
	"context"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
)

// RefreshListQuietly re-reads the journey list in place after a live
// invalidation hint (REV-091-03) and reports whether it scheduled a read.
//
// It is deliberately narrower than a route load. It runs only while the
// reader is on an already-loaded list; it re-reads the one dataset a
// promotion transition changes (ListJourneys, never the workforce); it shows
// no busy state, so rows do not flicker; and it keeps whatever notice and
// form values are on screen. An answer that arrives after the reader has
// moved on is dropped by the same route-generation fence every other read
// uses. The open detail needs none of this: WatchJourney already streams it.
func (a *App) RefreshListQuietly() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	if a.route.Kind != RouteList || !a.listLoaded || a.ctx == nil {
		a.mu.Unlock()
		return false
	}
	generation := a.generation
	ctx := a.ctx
	a.mu.Unlock()

	a.runTask(ctx, taskmux.Spec{Key: "journey:invalidation-list", Priority: taskmux.Background, Duplicate: taskmux.ReplaceExisting}, func(ctx context.Context) {
		journeys, err := a.svc.ListJourneys(ctx, &journeyv1.ListJourneysRequest{})
		if err != nil || ctx.Err() != nil {
			// A failed background refresh leaves the last authorized list
			// in place; the next hint or navigation reads again.
			return
		}
		a.mu.Lock()
		if a.generation != generation || a.route.Kind != RouteList {
			a.mu.Unlock()
			return
		}
		a.list = journeys.GetJourneys()
		a.mu.Unlock()
		a.showCurrent(generation, a.currentNotice())
	})
	return true
}
