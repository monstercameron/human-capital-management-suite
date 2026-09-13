package productui

// PROMOUX-011: "Invalidate promotion counts and timestamps after every
// durable transition."
//
// This file is the presentation half of the todo: internal/domains/promotion
// already decided which regions a committed transition affects and how a
// subscriber's own gap-free sequence is minted (promoux011.go there); this
// file only turns that decision into markup. Two GREEN clauses land here:
//
//   - "displayed last-updated time equals the latest business transition":
//     [LatestTransitionLastUpdatedLabel] formats the newest transition's own
//     OccurredAt, never time.Now(), and an empty transition set renders
//     nothing rather than a label that would silently claim freshness
//     nothing established.
//   - "shell count, Journeys, My Work, Person and detail refresh only their
//     affected regions": [PromotionRegionContainer] gives each of
//     promotion.Regions() its own addressable DOM subtree keyed by its own
//     wire projection name, so a real client can patch one region's markup
//     without touching another's -- REFACTOR's "no page triggers a
//     full-shell reload to achieve consistency" has nothing to reload
//     against if every region is already its own independent container.
import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
)

// TransitionLastUpdatedLabel formats occurredAt -- a promotion transition's
// own business timestamp -- as the "Updated" label. It never reads the
// clock: a zero-value occurredAt (no transition to report on) yields "",
// which callers render as an explicit "not yet updated" state rather than
// falling back to a value that would misrepresent freshness.
//
// The layout matches tools/uxqual/productclient's existing timestampLabel
// convention (UTC, "2 Jan 2006 · 15:04 UTC") so a promotion's displayed
// timestamp does not silently diverge in format from other already-shipped
// timestamp displays in this product.
func TransitionLastUpdatedLabel(occurredAt time.Time) string {
	if occurredAt.IsZero() {
		return ""
	}
	return occurredAt.UTC().Format("2 Jan 2006 · 15:04 UTC")
}

// LatestTransitionLastUpdatedLabel formats the newest of a set of
// transitions' own OccurredAt timestamps. RED names the specific failure
// this guards against: "Updated" staying older than the newest transition
// because something computed it once and cached it, or because it silently
// fell back to "now" instead of the actual latest business event. An empty
// slice returns "" rather than the current time.
func LatestTransitionLastUpdatedLabel(transitions []promotion.Transition) string {
	var latest time.Time
	for _, transition := range transitions {
		if transition.OccurredAt.After(latest) {
			latest = transition.OccurredAt
		}
	}
	return TransitionLastUpdatedLabel(latest)
}

// PromotionRegionContainer wraps content in one of GREEN's five
// independently invalidated region containers, tagged with the region's own
// wire projection name (data-promotion-region) so a real client's targeted
// DOM patch and this file's server-rendered markup agree on the same
// addressing scheme -- the shared "cache keys and sequence handling"
// REFACTOR requires, expressed here as a shared region-to-container mapping
// rather than a second, presentation-only naming scheme.
//
// An unrecognized region (promotion.ErrUnknownRegion) still renders -- a
// markup helper failing an entire page over an enum gap would be a worse
// failure mode than a container an invalidation could never target -- but it
// is tagged "unknown" rather than silently reusing another region's name,
// which would misroute a future targeted patch.
func PromotionRegionContainer(region promotion.Region, content ui.Node) ui.Node {
	projection, err := region.Projection()
	if err != nil {
		projection = "unknown"
	}
	return html.Div(html.Props{Class: "promotion-region", Raw: map[string]any{"data-promotion-region": projection}}, content)
}
