// Package pagedef defines WEB-002's versioned PageDefinition contract: the
// governed, renderer-independent shape a production page is described by
// before any GWC/SSR renderer, GoWebComponents component tree, or Go/WASM
// island ever sees it (planning/specs/production-frontend-and-page-composition.md).
//
// A PageDefinition never becomes business truth (principle 2 of that plan).
// It names:
//   - the page's own identity and a versioned floorplan reference (the
//     floorplan registry itself is WEB-003's contract; this package only
//     carries the reference, never a second floorplan catalog);
//   - an ordered set of semantic regions, each tagged with a region kind
//     drawn from the closed vocabulary in [RegionKind] -- the eight-step
//     page anatomy the frontend plan defines, not a vocabulary this package
//     invents;
//   - widget slots that reference a governed widget by id only (the
//     widget-registry lifecycle is WEB-005's contract; a slot never carries
//     widget configuration, markup, or code);
//   - data bindings and actions that name a registered RPC by reference
//     only -- resolved against the real generated grpc.ServiceDesc of the
//     journey, intents, registry, and admin services (see rpcregistry.go)
//     so a binding can never invent an RPC that does not exist;
//   - accessibility requirements (landmarks, heading order, a live region)
//     as first-class, validated fields rather than post-release polish
//     (principle 7); and
//   - brand token references, never raw color/CSS/pixel values (principle 8).
//
// Like tools/uxqual/contract, this package is a plain Go value with no
// GoWebComponents type, no html/template type, and no import of
// internal/intent/app or internal/domains: it is a contract other lanes'
// renderers and servers consume, not a place business logic lives. Its one
// deliberate dependency on generated code is read-only: it inspects the
// generated grpc.ServiceDesc values so "every binding names an RPC that
// exists" can be checked against the real registered service surface
// instead of a second, hand-maintained list that could drift from it.
package pagedef

// RegionKind names one of the eight regions the frontend plan's "Page
// anatomy" section defines, in the order a full page resolves them. It is a
// closed vocabulary: [PageDefinition.Validate] refuses any other value, and
// regionKindDocs documents every member so a later reviewer (or
// TestTodo_WEB_002_Conformance) can confirm none was added without an
// explanation of what it is for.
type RegionKind string

// The closed region-kind vocabulary. Values and ordering follow
// specs/production-frontend-and-page-composition.md's "Page anatomy"
// section verbatim: every full page resolves in this order, though any
// given page may omit a region it has no content for.
const (
	// RegionShell is the application shell: product, tenant/company scope,
	// search, navigation, attention, locale/accessibility, and account
	// context.
	RegionShell RegionKind = "shell"
	// RegionPageIdentity is the page's title, resource/intent identity,
	// status, freshness, and page-wide actions.
	RegionPageIdentity RegionKind = "page_identity"
	// RegionAuthorityContext is the acting role, delegation,
	// organization/legal-entity scope, confidentiality, purpose, effective
	// time, and elevated-access state.
	RegionAuthorityContext RegionKind = "authority_context"
	// RegionLocalNavigation is sections for objects, steps for guided
	// journeys, or views for collections. Steps and tabs are never the same
	// concept, but both live in this region kind.
	RegionLocalNavigation RegionKind = "local_navigation"
	// RegionPrimary is the current job: one dominant hierarchy and one
	// primary action per action group. Every page needs exactly this: a
	// PageDefinition with no RegionPrimary region is refused.
	RegionPrimary RegionKind = "primary"
	// RegionSupporting is authoritative current state, explanation,
	// evidence, or summary. It reflows below the primary region on narrow
	// surfaces.
	RegionSupporting RegionKind = "supporting"
	// RegionUtility is user-opened activity, help, history, attachments, or
	// evidence. It is never the only place for required content.
	RegionUtility RegionKind = "utility"
	// RegionCompletion is review, consequences, save/cancel/continue, and a
	// focus-safe final action.
	RegionCompletion RegionKind = "completion"
)

// regionKindDocs is the one place every closed RegionKind is documented,
// read by TestTodo_WEB_002_Conformance to prove the vocabulary is fully
// explained rather than left to the doc comments above alone.
var regionKindDocs = map[RegionKind]string{
	RegionShell:            "Application shell: product, tenant/company scope, search, navigation, attention, locale/accessibility, and account context.",
	RegionPageIdentity:     "Page identity: title, resource/intent identity, status, freshness, and page-wide actions.",
	RegionAuthorityContext: "Authority context: acting role, delegation, organization/legal-entity scope, confidentiality, purpose, effective time, and elevated-access state.",
	RegionLocalNavigation:  "Local navigation: sections for objects, steps for guided journeys, or views for collections.",
	RegionPrimary:          "Primary region: the current job, one dominant hierarchy, and one primary action per action group.",
	RegionSupporting:       "Supporting region: authoritative current state, explanation, evidence, or summary; reflows below primary content on narrow surfaces.",
	RegionUtility:          "Utility surface: user-opened activity, help, history, attachments, or evidence; never the only place for required content.",
	RegionCompletion:       "Completion layer: review, consequences, save/cancel/continue, and a focus-safe final action.",
}

// RegionKinds returns the closed vocabulary's members in the frontend
// plan's own order. Callers (and tests) use this instead of ranging over
// regionKindDocs so iteration order never depends on Go's randomized map
// order.
func RegionKinds() []RegionKind {
	return []RegionKind{
		RegionShell,
		RegionPageIdentity,
		RegionAuthorityContext,
		RegionLocalNavigation,
		RegionPrimary,
		RegionSupporting,
		RegionUtility,
		RegionCompletion,
	}
}

// RegionKindDoc returns the documentation for kind, or "" when kind is not
// a member of the closed vocabulary.
func RegionKindDoc(kind RegionKind) string {
	return regionKindDocs[kind]
}

// LiveRegionPoliteness names the ARIA politeness of the page's one live
// region requirement. It is a closed, explicit choice: a PageDefinition
// must say which of the three it is rather than leaving the question open,
// because "no live region" and "forgot to say" must never be the same bit
// pattern.
type LiveRegionPoliteness string

const (
	// LiveRegionOff declares that this page has no live region: a page
	// author's explicit statement that nothing on the page needs one, not
	// the absence of a statement.
	LiveRegionOff LiveRegionPoliteness = "off"
	// LiveRegionPolite declares an aria-live="polite" region: announced
	// after the screen reader finishes its current utterance.
	LiveRegionPolite LiveRegionPoliteness = "polite"
	// LiveRegionAssertive declares an aria-live="assertive" region:
	// announced immediately, interrupting the current utterance.
	LiveRegionAssertive LiveRegionPoliteness = "assertive"
)

// Heading is one heading a region contributes to the page's outline.
// Level follows HTML's own 1-6 scale.
type Heading struct {
	Level int
	Text  string
}

// WidgetSlot references one governed widget by id. It never carries widget
// configuration, markup, styling, or code: the widget-registry lifecycle
// (WEB-005) is the sole authority over what a widget id may resolve to and
// what it is trusted to do.
type WidgetSlot struct {
	// ID identifies this slot within its region (e.g. "journeys-list"), not
	// the widget it holds.
	ID string
	// WidgetRef is the governed widget's id (e.g. "widget.table.v1"). This
	// package does not resolve it against a widget registry -- WEB-005 owns
	// that -- it only requires the reference to be present and free of
	// markup.
	WidgetRef string
}

// DataBinding names one read this region's widgets are populated from. RPC
// must resolve against the real generated grpc.ServiceDesc of the journey,
// intents, registry, or admin service (see [KnownRPCs]); a page definition
// can never bind to an RPC that does not exist, and it can never bind to
// anything else -- there is no "raw query" or "arbitrary endpoint" escape
// hatch.
type DataBinding struct {
	// ID identifies this binding within its region.
	ID string
	// RPC is "<generated ServiceDesc.ServiceName>/<MethodName>", e.g.
	// "hcmnext.journey.v1.JourneyService/ListJourneys". [RPCRef] builds this
	// string from the generated service name so it can never be hand-typed
	// out of sync with the .proto-derived service name.
	RPC string
}

// ActionRef names one semantic action this region offers, and the role
// required to take it. Like a binding, RPC must resolve against a real
// registered service method; unlike a binding, every action also carries a
// RequiredRole, because principle 1 of the frontend plan requires the
// server to resolve what may be acted on before it reaches the component
// tree, and an action with no declared role would be un-resolvable by
// definition.
type ActionRef struct {
	ID           string
	RPC          string
	RequiredRole string
}

// Region is one semantic region of the page, tagged with a RegionKind from
// the closed vocabulary. Order within PageDefinition.Regions is
// significant: it is the region's position in the page anatomy and (via
// Heading) the page's heading order.
type Region struct {
	ID   string
	Kind RegionKind
	// Heading is this region's contribution to the page outline, or nil
	// when the region carries no heading of its own (e.g. a pure
	// authority-context strip).
	Heading  *Heading
	Widgets  []WidgetSlot
	Bindings []DataBinding
	Actions  []ActionRef
}

// Accessibility carries the page-level accessibility requirements the
// frontend plan treats as publication properties (principle 7), not
// post-release polish: the landmark structure, and the one explicit live
// region declaration. Heading order is validated across the whole page's
// regions, not carried here, because it is a property of Regions' order and
// each Region's own Heading rather than a separate list.
type Accessibility struct {
	// Landmarks are the ARIA landmark roles this page's shell renders, in
	// document order (e.g. "banner", "navigation", "main", "contentinfo").
	Landmarks []string `json:"landmarks"`
	// LiveRegion is this page's one explicit live-region declaration.
	LiveRegion LiveRegionPoliteness `json:"live_region"`
}

// PageDefinition is the whole of WEB-002's versioned contract: a page id,
// a version, a floorplan reference, semantic regions, and the page-level
// accessibility and brand-token requirements.
//
// A PageDefinition is data, never behavior: it contains no free HTML, no
// script, no pixel coordinates, and no arbitrary query. [PageDefinition.Validate]
// enforces that structurally; it does not merely document it.
type PageDefinition struct {
	// PageID identifies this page across versions (e.g.
	// "promotion.journeys.list"). Version, not PageID, changes between
	// revisions of the same page.
	PageID string
	// Version is this page definition's revision number. It starts at 1;
	// [PageDefinition.Digest] changes whenever any semantically meaningful
	// field changes, including Version itself.
	Version int
	// FloorplanRef names the versioned floorplan (WEB-003's contract) this
	// page is composed on. This package does not validate it against a
	// floorplan registry -- WEB-003 does not exist yet -- only that it is
	// present and free of markup.
	FloorplanRef string
	// Regions are the page's semantic regions in document order.
	Regions []Region
	// Accessibility carries the page's landmark and live-region
	// requirements.
	Accessibility Accessibility
	// BrandTokens are semantic brand token references (e.g.
	// "brand.color.primary"), never raw color, CSS, or pixel values.
	BrandTokens []string
}
