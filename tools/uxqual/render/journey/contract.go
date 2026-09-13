// Package journey is the GoWebComponents renderer for the Promotion journey
// page: the vertical slice that follows one governed promotion from the
// manager's proposal, through the P1B execution authority gate, to the
// approver's decision and the ledger fact the workflow records.
//
// Like tools/uxqual/render/gwc it is renderer-only: it knows nothing about
// the cell, the database or the engine. Everything it draws arrives in a
// [Page] of plain strings the serving package (internal/humanwork/workspace)
// projects from the engine's answers, including the routes its forms post
// to. It builds one component tree (Build) and renders it either to a string
// with GWC's native SSR path (RenderToString / Document) or, under
// GOOS=js GOARCH=wasm, live into the DOM.
package journey

// Page is the whole document. Exactly one of List, Proposal and Detail is
// set.
type Page struct {
	// Title is the document title.
	Title string
	// Brand is the product name in the masthead.
	Brand string
	// TenantLabel is the human name of the tenant the page is served for.
	TenantLabel string
	// Principal is who is signed in.
	Principal Principal
	// Nav is the masthead navigation.
	Nav []NavLink
	// Notice, when set, is the one status message for this response (an
	// action's outcome or refusal).
	Notice *Notice

	List     *ListView
	Proposal *ProposalView
	Detail   *DetailView

	Footer Footer

	// Values is the live client's current value for every editable field,
	// keyed by Field.ID. A field with no entry here renders its own
	// Field.Value, so the server-rendered path and the first live render are
	// identical. The live client owns this map: it is the single place a
	// keystroke lands, and it is what OnSubmit is called with.
	Values map[string]string
	// OnFieldChange, when set, makes every control a controlled input: the
	// renderer wires it to each field's input event and stops emitting the
	// value as a plain attribute the browser owns. Nil (the SSR and test
	// path) leaves the controls uncontrolled and the forms plain.
	OnFieldChange func(fieldID, value string)
}

// Principal is the signed-in caller as the masthead shows them.
type Principal struct {
	Subject string
	Roles   []string
	Purpose string
	// LogoutHref is empty when there is no dev sign-in session to end.
	LogoutHref string
}

// NavLink is one masthead link.
type NavLink struct {
	Label   string
	Href    string
	Current bool

	// OnNavigate, when set, makes this link a live route change: the
	// renderer prevents the browser's own navigation and calls it instead.
	// Href is still emitted, so the link keeps its middle-click, copy-link
	// and no-script behaviour.
	OnNavigate func()
}

// Notice is one status message.
type Notice struct {
	// Tone is one of info, success, warning, danger.
	Tone   string
	Title  string
	Detail string
}

// Footer carries the provenance line every page ends with.
type Footer struct {
	PolicyVersion string
	CellID        string
	BuildRef      string
	// Lines are any further short provenance statements.
	Lines []string
}

// ListView is the journeys overview with the new-proposal form.
type ListView struct {
	Journeys []JourneyCard
	// Groups is Journeys clustered into named subject groups (UXAUDIT-017,
	// GREEN: "a lifecycle tracker grouped by subject and status"). Nil or
	// empty falls back to Journeys' flat rendering unchanged -- a page
	// built before this field existed, or one whose projector never sets
	// it, renders exactly as it always has.
	Groups []JourneySubjectGroup
	// Empty is shown instead of the list when there are no journeys.
	Empty string
	Form  ProposalForm
	// EngineAvailable is false when the cell was not composed with the
	// execution authority; the form is still shown but explains that
	// execution is off.
	EngineAvailable bool
	EngineNotice    string

	// People is the workforce panel the list view opens with: who this cell
	// knows about, which of them the reader has picked, and the form that
	// records a new one. Nil leaves the panel out entirely, which is what a
	// projection that cannot read the workforce table should do rather than
	// showing an empty one.
	People *PeopleView
}

// JourneySubjectGroup is one subject's cluster of journeys for the list
// view's grouped-by-subject-and-status rendering. Every string here is
// already display-ready, exactly like JourneyCard.
type JourneySubjectGroup struct {
	// Subject is the group heading: the worker's display name.
	Subject string
	// Journeys are this subject's cards. The projector orders them (open
	// before terminal); this type does not reorder them again.
	Journeys []JourneyCard
	// Statuses is the distinct set of this group's journeys' StageLabel and
	// StageTone, deduplicated in first-seen order, so a reader can see the
	// group's status mix (e.g. "Blocked" and "Recorded" both present) at a
	// glance without opening every card in it.
	Statuses []JourneyStatusChip
}

// JourneyStatusChip is one distinct status shown at the group level.
type JourneyStatusChip struct {
	Label string
	Tone  string
}

// ProposalView is the person-scoped start of a promotion. It intentionally
// contains one subject and no workforce or journey collection: a reader who
// arrived from a person's profile should not have to re-establish who the
// transaction is for among unrelated names.
type ProposalView struct {
	Subject *PromotionSubject
	Form    ProposalForm
	// Loading keeps the subject and form components unmounted until the
	// worker read resolves, so transient disabled props cannot leak into the
	// ready form during DOM reconciliation.
	Loading bool

	// BackHref returns to the subject's product profile. JourneysLink opens
	// the tenant-wide operational overview as an explicit secondary choice
	// and carries the live router callback when the WASM client is mounted.
	BackHref     string
	BackNavigate func()
	JourneysLink NavLink

	EngineAvailable bool
	EngineNotice    string
}

// PromotionSubject is the compact, immutable worker context above a focused
// proposal form. Every string is already formatted by the projector.
type PromotionSubject struct {
	Ref      string
	Name     string
	PhotoURL string
	Number   string
	Title    string
	JobCode  string
	Grade    string
	OrgUnit  string
	Location string
	PayLine  string
}

// PeopleView is the workforce-first half of the list view. It exists because
// a promotion is proposed *for someone*: the page has to let a reader see
// the workforce, add to it, and pick the person the proposal below is about,
// in that order, before the proposal form means anything.
type PeopleView struct {
	Workers []WorkerCard
	// DirectoryLink hands large-workforce browsing back to the canonical,
	// filterable product directory. Embedded clients upgrade it to software
	// navigation while SSR retains a real fallback href.
	DirectoryLink NavLink
	// Empty is shown instead of the table when the cell knows no workers.
	Empty string
	// Form records a new employee as a fact in this cell's workforce table.
	Form WorkerForm
	// SelectedRef is the WorkerCard.Ref the reader has picked. It marks that
	// row and names the person in the proposal panel's heading; an empty
	// string, or a ref no row carries, simply selects nobody.
	SelectedRef string
	// Note is one line under the People heading, e.g. what adding an
	// employee actually does. Empty leaves it out.
	Note string
}

// WorkerCard is one employee as the People table shows them. Every string is
// already formatted for display by the projecting lane; the renderer neither
// parses nor computes any of them.
type WorkerCard struct {
	Ref    string
	Name   string
	Number string
	// Title is the position's human name, e.g. "Senior HR Business Partner".
	Title    string
	JobCode  string
	Grade    string
	OrgUnit  string
	Location string
	// PayLine is the current base, e.g. "USD 93,000.00".
	PayLine  string
	HireDate string
	// Source is one of CORPUS (loaded with the cell) or CREATED (recorded
	// through this page). Anything else reads as CORPUS.
	Source string
	// SourceLabel is the chip's own word for Source. Empty falls back to a
	// default, so the chip is never a bare color.
	SourceLabel string
	// Tone is empty or one of info, warning, success, danger. It is a
	// styling hook only: everything it tints is also said in words.
	Tone string
	// Selected marks the row the proposal panel is about.
	Selected bool
	// OpenJourneys is how many promotion journeys are open for this person.
	OpenJourneys int
	// ProposeHref is where the row's action link points when there is no
	// live client, e.g. "#/journeys/new?worker=<ref>".
	ProposeHref string

	// OnSelect, when set, makes the row's name a live toggle: the renderer
	// wires it to a click handler instead of leaving the name plain text.
	OnSelect func()
	// OnPropose, when set, makes the row's action a live call instead of a
	// plain link to ProposeHref.
	OnPropose func()
}

// WorkerForm records a new employee. Its conventions are ProposalForm's: a
// plain POST to Action when OnSubmit is nil, a client-owned submission when
// it is not.
type WorkerForm struct {
	// Action is the route the form posts to.
	Action string
	Hidden map[string]string
	Fields []Field
	Submit string
	// Disabled is set with a reason when the cell cannot record workers.
	Disabled       bool
	DisabledReason string

	// OnSubmit, when set, makes this form a live submission: the renderer
	// prevents the browser's own POST and calls it with every hidden entry
	// plus the current value of every field, keyed by Field.Name.
	OnSubmit func(values map[string]string)
}

// JourneyCard is one journey in the list and the header of its detail.
type JourneyCard struct {
	IntentID   string
	Href       string
	WorkerName string
	WorkerRef  string
	// Headline is the placement change, e.g. "OPS-HRBP2 · P2 → OPS-HRBP3 · P3".
	Headline string
	// PayLine is the pay change, e.g. "USD 93,000.00 → 98,000.00 (+5.4%)".
	PayLine string
	// EffectiveDate is already formatted for display.
	EffectiveDate string
	Stage         string
	StageLabel    string
	// StageTone is one of neutral, info, warning, success, danger.
	StageTone string
	Updated   string
	// InstanceID is empty before execution.
	InstanceID string

	// OnOpen, when set, opens this journey in the live client instead of
	// letting the browser follow Href.
	OnOpen func()
}

// ProposalForm is the manager's new-proposal form.
type ProposalForm struct {
	// Action is the route the form posts to.
	Action string
	Hidden map[string]string
	Fields []Field
	Submit string
	// Disabled is set with a reason when execution is off.
	Disabled       bool
	DisabledReason string

	// OnSubmit, when set, makes this form a live submission: the renderer
	// prevents the browser's own POST and calls it with every hidden entry
	// plus the current value of every field, keyed by Field.Name.
	OnSubmit func(values map[string]string)
}

// Option is one <select> option.
type Option struct {
	Value    string
	Label    string
	Selected bool
}

// Field is one form control.
type Field struct {
	ID    string
	Name  string
	Label string
	// Kind is one of text, number, date, select, textarea, hidden.
	Kind        string
	Value       string
	Placeholder string
	Help        string
	Error       string
	Required    bool
	Options     []Option
	// Prefix and Suffix are short adornments (e.g. currency code).
	Prefix string
	Suffix string
	Step   string
	Min    string
}

// DetailView is one journey.
type DetailView struct {
	Journey JourneyCard
	// BackLink returns to the employee context that launched this journey.
	// JourneysLink remains available as the broader operational escape hatch.
	BackLink     NavLink
	JourneysLink NavLink

	// Steps is the stage stepper, in order.
	Steps []Step

	// Proposal is the request as facts, and Comparison the before/after
	// table.
	Proposal   []Fact
	Comparison []ComparisonRow

	Findings []Finding

	// Engine is the workflow instance as facts (instance id, version, plan
	// digest, status, current node).
	Engine []Fact
	Nodes  []NodeRow

	WorkItems []WorkItemCard

	// Ledger is nil until the terminal write is recorded.
	Ledger *LedgerCard

	Evidence []string

	Timeline []TimelineEvent

	Actions []Action

	// PayBand, Budget and EffectiveWindow are the simulation's quantitative
	// answers, drawn rather than tabulated: where the proposal lands in the
	// target grade's pay band, what it takes out of the org unit's approved
	// envelope, and how the effective date sits against the cycle. Each is
	// nil when the simulation did not produce it, and the section is simply
	// absent rather than empty.
	PayBand         *PayBand
	Budget          *Budget
	EffectiveWindow *EffectiveWindow
}

// PayBand is the target grade's pay range with the current and proposed
// base marked on it. The strings are already formatted for display; only
// the two percentages are numeric, because the gauge has to draw them.
type PayBand struct {
	Min      string
	Mid      string
	Max      string
	Current  string
	Proposed string
	Currency string
	// CurrentPct and ProposedPct are 0-100 positions along the band, min to
	// max. Values outside that range are clamped by the renderer rather
	// than drawn off the track.
	CurrentPct  float64
	ProposedPct float64
	// Note is the one-line reading of the gauge, e.g. "4.2% above the P3
	// midpoint". It carries the meaning for anyone who cannot see the
	// drawing, so it is never decorative.
	Note string
}

// Budget is what the proposal takes out of the approved envelope.
type Budget struct {
	Available string
	Committed string
	Requested string
	// UsedPct is 0-100: the share of the envelope already committed plus
	// this request. Clamped by the renderer.
	UsedPct float64
	// Note is the one-line reading, e.g. "USD 36,200.00 would remain".
	Note string
}

// EffectiveWindow places the effective date against the compensation cycle
// and the as-known-at time the answers were read at.
type EffectiveWindow struct {
	Start         string
	EffectiveDate string
	KnownAt       string
	Note          string
}

// Step is one stage of the stepper.
type Step struct {
	ID    string
	Label string
	// Detail is the one-line explanation under the label.
	Detail string
	// State is one of done, active, upcoming, failed.
	State string
	// At is the formatted time the step was reached, when known.
	At string
}

// Fact is one labelled value.
type Fact struct {
	Label string
	Value string
	// Mono renders the value in a monospace face (identifiers, digests).
	Mono bool
	// Tone is empty or one of info, warning, success, danger.
	Tone string
}

// ComparisonRow is one line of the before/after table.
type ComparisonRow struct {
	Label    string
	Current  string
	Proposed string
	// Delta is empty when there is no meaningful difference to show.
	Delta string
	// Changed marks rows whose value differs.
	Changed bool
}

// Finding is one simulation finding.
type Finding struct {
	// Severity is one of blocking, warning, info, success.
	Severity string
	Code     string
	Message  string
}

// NodeRow is one node execution.
type NodeRow struct {
	NodeID    string
	StepType  string
	Status    string
	Attempt   string
	Started   string
	Completed string
	// Tone is one of neutral, info, warning, success, danger.
	Tone string
}

// WorkItemCard is one work item.
type WorkItemCard struct {
	ID       string
	Kind     string
	Status   string
	Owner    string
	NodeID   string
	Deadline string
	Claimed  string
	// Completed is "by whom, when" once completed.
	Completed string
	Tone      string
}

// LedgerCard is the recorded promotion outcome.
type LedgerCard struct {
	StreamKey      string
	Sequence       string
	SchemaRef      string
	Digest         string
	IdempotencyKey string
	RecordedAt     string
	EffectiveAt    string
}

// TimelineEvent is one chronological entry.
type TimelineEvent struct {
	At     string
	Actor  string
	Title  string
	Detail string
	Ref    string
	// Tone is one of neutral, info, warning, success, danger.
	Tone string
}

// Action is one thing the signed-in person can do now. Each renders as its
// own form posting to Action with Hidden inputs and any Fields.
type Action struct {
	ID    string
	Label string
	// Variant is one of primary, secondary, danger.
	Variant     string
	Description string
	Action      string
	Hidden      map[string]string
	Fields      []Field
	// Disabled is set with DisabledReason when the action exists at this
	// stage but the caller may not take it.
	Disabled       bool
	DisabledReason string
	// ActsAs, when set, names the principal the engine will act as (the
	// routed approver), so the page never implies the caller is that person.
	ActsAs      string
	ActsAsLabel string
	// Confirmation keeps consequential HR actions two-step without inventing
	// a second modal state machine: the reader expands a native disclosure,
	// reviews these exact facts, and only then reaches the submit button.
	Confirmation     []Fact
	ConfirmationNote string

	// OnSubmit, when set, makes this action a live call: the renderer
	// prevents the browser's own POST and calls it with every hidden entry
	// plus the current value of every field, keyed by Field.Name.
	OnSubmit func(values map[string]string)
}
