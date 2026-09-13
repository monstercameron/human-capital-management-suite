package productui

import (
	"errors"
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AsyncRegionState is the single closed set of presentation states an
// asynchronous, server-backed region of the product UI can be in.
// UXAUDIT-012's REFACTOR requires "one async-region state model covers
// loading, empty, stale, failure and resolved states across components" --
// this is that model. Both methods below switch over every member
// explicitly and refuse an unrecognized value rather than defaulting, the
// same fail-loudly idiom UXAUDIT-007's AuthorityState and PROMOUX-011's
// Region use for the same reason: a sixth state silently reusing another
// state's wiring is exactly the kind of defect a permissive default hides.
//
// This model composes with, rather than duplicates, PROMOUX-011's Region
// (internal/domains/promotion): that type names *which* presentation area a
// promotion invalidation targets (SHELL_COUNT, JOURNEYS, MY_WORK, PERSON,
// DETAIL) and is intentionally scoped to the promotion domain's five
// business regions. AsyncRegionState answers a different, orthogonal
// question that applies to every region in every domain -- People, Work,
// Organization, a promotion region, or any future one -- namely "what is
// this region's own presentation state right now", using the same region
// key strings the rest of this package already keys refreshes by (View.
// RefreshingRegion, RefreshRegionPeopleDirectory) rather than inventing a
// second region-identity type. BuildAsyncRegion below is the seam: it takes
// a region's already-known state and PageID and renders through the one
// shared shape, so a People-directory refresh, a promotion Person-region
// refresh, and a cold Home load all go through the same decision instead of
// three ad hoc ones.
//
// The zero value is AsyncRegionLoading, not AsyncRegionResolved. That
// ordering is deliberate and is the fail-safe default the todo's proof
// standard requires: a region descriptor that forgets to set its state must
// render through the size-compatible loading proxy, never resolved content.
// The alternative -- a zero value that reads as resolved -- is the exact
// shift RED describes: an empty box renders first, because nothing actually
// populated a "resolved" state nobody set on purpose, then real content
// appears once someone remembers to set the field, and the box changes
// size. Failing safe to Loading means the worst case of forgetting to set
// this field is an indefinite skeleton, never a false-resolved empty box.
type AsyncRegionState int

const (
	// AsyncRegionLoading means no answer has ever arrived for this region.
	// It is the zero value.
	AsyncRegionLoading AsyncRegionState = iota
	// AsyncRegionEmpty means the region resolved and the authoritative
	// answer is genuinely zero records -- not "we have not asked yet".
	AsyncRegionEmpty
	// AsyncRegionStale means a previously resolved answer remains mounted
	// while a newer one is in flight (a warm refresh, e.g. View.Refreshing).
	// The region keeps rendering its last resolved content and layout
	// unchanged; only a busy affordance is added.
	AsyncRegionStale
	// AsyncRegionFailure means the fetch for this region did not complete
	// successfully and no usable answer is mounted.
	AsyncRegionFailure
	// AsyncRegionResolved means the region shows a real, current answer.
	AsyncRegionResolved
)

// asyncRegionStateNames names every member for String and error messages.
// Kept private and exhaustive-by-construction: extending the const block
// without adding a name here makes String fall back to the numeric form
// rather than silently claiming a name that means something else.
var asyncRegionStateNames = map[AsyncRegionState]string{
	AsyncRegionLoading:  "loading",
	AsyncRegionEmpty:    "empty",
	AsyncRegionStale:    "stale",
	AsyncRegionFailure:  "failure",
	AsyncRegionResolved: "resolved",
}

func (s AsyncRegionState) String() string {
	if name, ok := asyncRegionStateNames[s]; ok {
		return name
	}
	return fmt.Sprintf("AsyncRegionState(%d)", int(s))
}

// ErrUnknownAsyncRegionState means a value outside the five declared
// constants reached a function that must handle every state explicitly.
var ErrUnknownAsyncRegionState = errors.New("productui: unknown async region state")

// UsesGeometryProxy reports whether a region in state s must render through
// the shared size-compatible proxy shape (loadingProxyBody) instead of its
// own natural content. Loading and Failure have no real answer to size
// themselves by; Empty has a real answer, but it is zero records, which
// carries no natural height of its own either, so it borrows the same
// declared shape rather than collapsing to a single line. Stale and
// Resolved render real, already-sized content.
//
// There is no default case. A value outside the five declared constants
// returns [ErrUnknownAsyncRegionState] instead of silently taking Resolved's
// branch (which would render whatever stale/absent data a caller had lying
// around as if it were current) or Loading's (which would shimmer forever
// for a state nobody intended to be indefinite).
func (s AsyncRegionState) UsesGeometryProxy() (bool, error) {
	switch s {
	case AsyncRegionLoading, AsyncRegionEmpty, AsyncRegionFailure:
		return true, nil
	case AsyncRegionStale, AsyncRegionResolved:
		return false, nil
	default:
		return false, fmt.Errorf("%w: %v", ErrUnknownAsyncRegionState, s)
	}
}

// Announcement returns the assistive-technology status text a region in
// state s must expose, and whether it must be delivered assertively
// (interrupting) rather than politely. Failure is the one state that must
// interrupt: a user who has stopped attending to a quietly-updating region
// should still be told when their data could not be produced. Loading is
// polite background progress. Empty, Stale and Resolved return no generic
// text here -- callers already announce those through their own resolved
// content or busy affordance (e.g. the people-directory refresh's own
// aria-busy region), and inventing a second, generic announcement for
// content that already speaks for itself would be redundant rather than
// helpful.
//
// There is no default case, matching UsesGeometryProxy.
func (s AsyncRegionState) Announcement(locale LocaleContext, detail string) (text string, assertive bool, err error) {
	switch s {
	case AsyncRegionLoading:
		return locale.Text("shell.loading_authorized"), false, nil
	case AsyncRegionEmpty, AsyncRegionStale, AsyncRegionResolved:
		return "", false, nil
	case AsyncRegionFailure:
		text = locale.Text("shell.live_unavailable")
		if detail != "" {
			text += ": " + detail
		}
		return text, true, nil
	default:
		return "", false, fmt.Errorf("%w: %v", ErrUnknownAsyncRegionState, s)
	}
}

// BuildAsyncRegion renders one region according to its state: either the
// shared size-compatible proxy (Loading, Empty, Failure) or the caller's own
// resolved node (Stale, Resolved). failureMessage is used only when state is
// AsyncRegionFailure and is surfaced through Announcement, never guessed.
//
// This is the seam REFACTOR asks for: any region -- a full page's content
// outlet, or one scoped region such as the people directory -- decides what
// to render by calling this function with its own PageID and state, rather
// than re-deriving "loading vs failed vs empty vs fine" with a fresh
// if/else chain per component.
func BuildAsyncRegion(page PageID, state AsyncRegionState, resolved ui.Node, failureMessage string) (ui.Node, error) {
	usesProxy, err := state.UsesGeometryProxy()
	if err != nil {
		return nil, err
	}
	if !usesProxy {
		return resolved, nil
	}
	return ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: page, State: state, Message: failureMessage}), nil
}
