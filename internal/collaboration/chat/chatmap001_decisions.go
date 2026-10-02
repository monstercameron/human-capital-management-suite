package chat

import (
	"fmt"
	"strings"
)

// LocationDecision is one recorded answer to the research questions of
// CHATMAP-001. The record is code so a test can hold the product to it: the
// map origin, the retention defaults and the separation from time capture are
// all asserted against these entries.
type LocationDecision struct {
	Question   int
	Topic      string
	Decision   string
	Sources    []string
	NeedsOwner string
}

// LocationDecisionsRecordedOn is when the record was written. Sources are
// named from knowledge held then and were not re-fetched; counsel confirms the
// legal entries before a country is switched on.
const LocationDecisionsRecordedOn = "2026-10-02"

// LocationDecisions returns the decisions in question order.
func LocationDecisions() []LocationDecision {
	return []LocationDecision{
		{1, "map data and drawing",
			"The product draws every map itself from its own origin. Default: a schematic grid with the pin, the accuracy circle, a scale bar and the label, drawn by Go code with no map data. Real street maps come from OpenStreetMap data hosted inside the deployment as a single tile archive (ODbL 1.0: attribution \"OpenStreetMap contributors\" shown on every map, a share-alike duty on derived databases only; a country extract is hundreds of megabytes to a few gigabytes, the planet roughly seventy gigabytes or more; extracts refresh daily to weekly depending on the publisher). A commercial map service is acceptable only behind the product's relay, which strips anything identifying the reader, records one usage line per call and obeys a monthly budget, and only if its terms permit relaying and caching. The public community tile servers are never a production source: their usage policy rules out heavy or commercial use and they log readers.",
			[]string{"OpenStreetMap Foundation: licence and attribution guidelines (ODbL 1.0)", "OpenStreetMap Foundation: tile usage policy", "PMTiles single-file tile archive specification"},
			"Approve one data set: an OpenStreetMap extract per customer region (size and refresh cadence above) or a named commercial service with relay and caching rights. Until then the schematic map is the only map."},
		{2, "how the map is drawn",
			"The page security policy stays as it is (no frames, no third-party loads) apart from images from the product's own origin. The picture is composed on the server and handed to the page as an image from the product's own origin; pan and zoom ask for another picture. There is no client map library (Go-first client rule). The schematic picture is a few kilobytes and renders in well under a millisecond; the cost of tile composition was not measured because no tile data is in the repository.",
			[]string{"internal/humanwork/workspace/csp.go: img-src 'self' blob:, frame-src 'none'"},
			"Measure tile composition on a phone connection once a tile source is approved."},
		{3, "address lookup",
			"Lookup sits behind a port the product owns (AddressLookup). The default stores the typed text, with coordinates only when the person drops a pin, and a lookup returns suggestions only from a source the deployment hosts or relays. A typed address is personal data and goes to whoever does the lookup, so it is never sent to a service the owner has not approved; each lookup is written to the usage ledger as a digest and the operation, never the text. Public geocoding services are not used in production: their policy limits rate and forbids autocomplete.",
			[]string{"Nominatim usage policy (one request per second, no autocomplete)"},
			"Approve a geocoding data set hosted in the deployment (an OpenStreetMap-derived index per customer country) or a commercial geocoder reached through the relay, with its price and per-country coverage."},
		{4, "the device position",
			"The browser gives a position and an accuracy radius in metres, per site permission, on secure pages only. Accuracy is metres outdoors with a satellite fix and hundreds of metres to kilometres indoors or on a desktop. The position is read only when the person presses the control, never in the background; a refusal, no fix or a rough fix has its own message and way forward (address, drop a pin, try again); a position worse than 100 metres is labelled rough and is offered as approximate, never silently sent as exact. With no connection the share is queued with its capture time and discarded if it has expired by the time the connection returns.",
			[]string{"W3C Geolocation API (secure contexts, permission, coords.accuracy)"},
			""},
		{5, "law and policy for worker location",
			"Consent given to an employer is weak because the worker cannot freely refuse, so the product does not rely on it alone: sharing is the worker's own act each time, workspace administrators can switch sharing off per country or legal entity and record the basis, and nothing is collected in the background. Where employee representatives must agree first (for example works council co-determination over technical monitoring in Germany, union or labour inspectorate agreement in Italy), the setting stays off until the agreement exists. Monitoring outside working time is not offered: live sharing is bounded, only while the product is open, and ends by itself. Retention is short and administrators can shorten it, never lengthen it past 24 hours.",
			[]string{"GDPR Articles 6, 9, 35 and 88", "Article 29 Working Party Opinion 2/2017 on data processing at work", "BetrVG section 87(1) number 6 (Germany)", "Statuto dei lavoratori article 4 (Italy)", "CNIL guidance on geolocation of employees (France)"},
			"Counsel confirms each customer country before the per-country setting is turned on."},
		{6, "the decisions",
			"Sharing is always the worker's own act. Kinds offered: a point now, a typed address, a point picked on the map, a job site, and live for 15 minutes, 1 hour or 8 hours. Defaults: approximate position (500 m grid, label and address dropped), one hour, never open-ended for live. No page lists where people are unless they are sharing live to that conversation at that moment. A Chat location is never evidence for time, pay or discipline: Chat and time capture do not read each other. Kept: only the latest position of a live share, deleted at its end; a static point is deleted at its expiry; no route history exists; administrators cannot read past positions because none exist.",
			[]string{"CHATMAP-002 through CHATMAP-006", "FTIME-010 (time capture is a separate, governed path)"},
			""},
	}
}

// RenderLocationDecisions is the stable text form used by the golden test.
func RenderLocationDecisions() string {
	var b strings.Builder
	fmt.Fprintf(&b, "CHATMAP-001 decisions recorded %s\n", LocationDecisionsRecordedOn)
	for _, d := range LocationDecisions() {
		fmt.Fprintf(&b, "\n%d. %s\n%s\n", d.Question, d.Topic, d.Decision)
		for _, s := range d.Sources {
			fmt.Fprintf(&b, "  source: %s\n", s)
		}
		if d.NeedsOwner != "" {
			fmt.Fprintf(&b, "  needs owner: %s\n", d.NeedsOwner)
		}
	}
	return b.String()
}
