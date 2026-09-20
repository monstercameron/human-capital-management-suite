package journeyclient

import (
	"net/url"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// The journey page has two views and one address bar. Routing lives in the
// fragment rather than the path for one structural reason: the shell is
// served at exactly one path (/workspace/journey) with a no-store
// content-security-policy, and a path-based route would need the server to
// serve that same shell under every journey's address. A fragment costs the
// server nothing, is never sent to it, and survives a reload.

// RouteKind is which of the page's views an address names.
type RouteKind int

const (
	// RouteList is the journeys overview with the proposal form. It is also
	// what an unrecognised address resolves to, so a stale or hand-edited
	// fragment lands on a working page rather than a blank one.
	RouteList RouteKind = iota
	// RouteProposal is the focused new-promotion screen for one worker. It
	// is deliberately distinct from RouteList: starting a promotion from a
	// person's profile must not drop the reader into a tenant-wide dashboard
	// full of unrelated people and journeys.
	RouteProposal
	// RouteDetail is one journey, named by its intent id.
	RouteDetail
)

// Route is a parsed address.
type Route struct {
	Kind RouteKind
	// IntentID is set only for [RouteDetail].
	IntentID string
	// WorkerRef is set for [RouteList] when its People table has a selection,
	// and for [RouteProposal] as the fixed subject of the new promotion. It
	// lives in the address rather than client state so either context
	// survives a reload and can be linked to.
	WorkerRef string
	// Filter is the list's search, status, date range, sort and grouping
	// (UXLIVE-031). It is set only for [RouteList] and is always normalized,
	// so two addresses that mean the same list compare equal.
	Filter productui.JourneyListFilter
}

// Route fragments. The list route is spelled out rather than left as the
// empty fragment so that the address bar always says what is on screen.
const (
	routePrefix     = "#/journeys"
	routeDetailStem = routePrefix + "/"
	routeProposal   = routeDetailStem + "new"
	// routeWorkerKey is the one query parameter this client reads. It is
	// spelled the same as tools/uxqual/render/journey's own selection href
	// ("#/journeys?worker=<ref>"), which is what a client that supplied no
	// SelectWorker callback navigates to.
	routeWorkerKey = "worker="
)

// Parse resolves one location fragment.
//
// Everything it does not recognise is the list: an empty fragment (the page
// was opened at its own address), a fragment naming another surface, or a
// detail fragment with no intent id after the slash.
func Parse(hash string) Route {
	h := strings.TrimSpace(hash)
	if h != "" && !strings.HasPrefix(h, "#") {
		// location.hash carries its own "#", but a caller handing us the
		// fragment alone should not be punished for it.
		h = "#" + h
	}
	// The query is cut off first so a detail address is recognised whether
	// or not something appended a parameter to it, and so the parameter is
	// never mistaken for part of an intent id.
	path, query, _ := strings.Cut(h, "?")
	if path == routeProposal {
		return Route{Kind: RouteProposal, WorkerRef: workerParam(query)}
	}
	if rest, ok := strings.CutPrefix(path, routeDetailStem); ok {
		id := strings.TrimSpace(rest)
		if slash := strings.IndexByte(id, '/'); slash >= 0 {
			id = id[:slash]
		}
		if id != "" {
			// A detail route carries no selection: the People table is on
			// the list, and a selection the detail could not act on would
			// be a fact the address asserts and the page never shows.
			return Route{Kind: RouteDetail, IntentID: id}
		}
	}
	return Route{Kind: RouteList, WorkerRef: workerParam(query), Filter: listFilterParam(query)}
}

// workerParam reads ?worker=<ref> out of a fragment's query.
//
// Href percent-encodes the value, so this is the matching single decode.
// QueryUnescape also gives '+' its query-string meaning; a literal plus in a
// worker reference is emitted as %2B and therefore still round-trips.
func workerParam(query string) string {
	for _, part := range strings.Split(query, "&") {
		if ref, ok := strings.CutPrefix(strings.TrimSpace(part), routeWorkerKey); ok {
			decoded, err := url.QueryUnescape(strings.TrimSpace(ref))
			if err != nil {
				return ""
			}
			return strings.TrimSpace(decoded)
		}
	}
	return ""
}

// Href is the address for a route: the inverse of [Parse] for every route
// [Parse] can produce.
func Href(r Route) string {
	if r.Kind == RouteProposal {
		if r.WorkerRef != "" {
			return routeProposal + "?" + routeWorkerKey + url.QueryEscape(r.WorkerRef)
		}
		return routeProposal
	}
	if r.Kind == RouteDetail && r.IntentID != "" {
		return routeDetailStem + r.IntentID
	}
	query := make([]string, 0, 7)
	if r.WorkerRef != "" {
		query = append(query, routeWorkerKey+url.QueryEscape(r.WorkerRef))
	}
	query = append(query, listFilterQuery(r.Filter)...)
	if len(query) > 0 {
		return routePrefix + "?" + strings.Join(query, "&")
	}
	return routePrefix
}

// WorkerHref is the list route with one employee selected. It is what the
// People table's rows link to and what the client writes into the address
// bar when a row is picked.
func WorkerHref(ref string) string {
	return Href(Route{Kind: RouteList, WorkerRef: ref})
}

// ProposalHref is the focused new-promotion route for ref. Unlike
// WorkerHref it never means "filter the overview"; the worker is the fixed
// subject of the transaction the reader is about to propose.
func ProposalHref(ref string) string {
	return Href(Route{Kind: RouteProposal, WorkerRef: strings.TrimSpace(ref)})
}

// ListHref is the list route's address.
func ListHref() string { return routePrefix }

// DetailHref is one journey's address.
func DetailHref(intentID string) string {
	return Href(Route{Kind: RouteDetail, IntentID: intentID})
}
