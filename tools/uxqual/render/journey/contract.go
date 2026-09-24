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

import "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"

// Page is the whole document. Exactly one of List, Proposal and Detail is
// set.
type Page struct {
	// Title is the document title.
	Title string
	// Locale is the canonical BCP-47 presentation locale for the document.
	// Empty uses DefaultLocale so older callers still emit an explicit
	// language rather than the ambiguous generic `en` tag.
	Locale string
	// Direction is the resolved text direction for Locale (ltr or rtl).
	// Empty is inferred from Locale.
	Direction string
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
	// FocusInvalidRevision changes on each rejected live proposal attempt.
	// The browser adapter uses it after render to focus the first invalid
	// control, including when the same invalid form is submitted twice.
	FocusInvalidRevision uint64

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
	// OnFocusField upgrades error-summary anchors to in-place focus without
	// changing the journey's hash route. Nil leaves ordinary fragment links.
	OnFocusField func(fieldID string)
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
	// Busy marks an in-flight client request. The optional catalog keys
	// localize notices without parsing rendered copy or changing their state.
	Busy       bool
	TitleKey   string
	MessageKey string
	// RecoveryHref and RecoveryLabel are set for an unauthenticated response.
	// They use the same signed-out destination as the product shell panel.
	RecoveryHref  string
	RecoveryLabel string
	// SupportReference is an opaque server-issued request id. The renderer
	// exposes it only inside a closed, copyable support disclosure, never in
	// the ordinary status sentence.
	SupportReference string
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
	// Grouping is the reader's chosen grouping when it is not the default
	// person grouping (UXLIVE-031): "status" renders the lifecycle groups,
	// "none" one flat list. Empty keeps the existing behaviour.
	Grouping string
	// Filter is the tracker's search, filter, sort and grouping form and
	// its result statement. Nil renders no filter, exactly as before.
	Filter *JourneyFilterView
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
type JourneyGroup string

const (
	JourneyGroupReview  JourneyGroup = "review"
	JourneyGroupWaiting JourneyGroup = "waiting"
	JourneyGroupIssue   JourneyGroup = "issue"
	JourneyGroupClosed  JourneyGroup = "closed"
)

// EditDefaults is one proposal's own current values, in the types the
// governed edit form submits.
type EditDefaults struct {
	JobCode string
	Grade   string
	// Base is decimal text with no currency, separators or delta.
	Base string
	// EffectiveISO is yyyy-mm-dd, which is what a date input accepts.
	EffectiveISO string
}

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
	// Group is a semantic lifecycle bucket resolved by the projector, not
	// inferred from localized status copy by the renderer.
	Group JourneyGroup
	// Edit is the proposal's own current values, in the types a correction
	// form submits: a job code, a grade, a decimal base and an ISO date.
	// The display strings beside them (Headline, PayLine, EffectiveDate) are
	// sentences about those values and are not interchangeable with them --
	// prefilling the edit form from those sentences is how it came to open
	// with "WRK-CO2 · P2 → WRK-MGR · M2" in a job-code field (UXLIVE-004).
	// A value the journey does not carry stays empty.
	Edit EditDefaults
	// StageTone is one of neutral, info, warning, success, danger.
	StageTone string
	// NextStep is the display wording of the single next step the stage names
	// (UXAUDIT-017's shared status dimension, journeyclient.NextStepLabel).
	// Empty for a terminal or unknown stage, which renders no next-step line.
	NextStep string
	// Closed is the server's lifecycle closure for this journey (PROMOUX-012):
	// the one open/closed dimension every surface shares.
	Closed  bool
	Updated string
	// InstanceID is empty before execution.
	InstanceID string
	// DiagnosticsAuthorized is PROMOUX-008's authorized-diagnostics verdict
	// for the signed-in viewer, computed server-side and carried down with
	// the page permissions this cell already hands the client -- never
	// guessed from whether WorkerRef, IntentID or InstanceID happen to be
	// set. It is the only thing that gates the card's and the hero's
	// Technical details disclosure: an unauthorized viewer's disclosure is
	// absent regardless of what this journey's underlying state is, so its
	// presence, count and layout carry no information about that state.
	DiagnosticsAuthorized bool

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

	// Confirmation and ConfirmationNote mirror Action's own fields: the
	// employee/change/date/consequence summary a reader confirms before
	// this proposal is created. PROMOUX-010: when either is set, Submit
	// renders behind the same shared review surface actionCard uses for
	// Approve and Reject, instead of firing directly. Empty means this
	// caller has not supplied one yet and the form submits directly,
	// matching every ProposalForm built before PROMOUX-010.
	Confirmation     []Fact
	ConfirmationNote string
	// Busy is true while this proposal's own submission is in flight; see
	// Action.Busy for what it does once Confirmation makes this form route
	// through the review surface.
	Busy      bool
	BusyLabel string

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

// VacancyOption is one position a promotion may target, as the form offers
// it. Reference is the only value that leaves the browser: it is the
// server-issued position revision reference, so a value the server did not
// publish cannot be submitted as one.
type VacancyOption struct {
	Reference        string
	Title            string
	Organization     string
	Manager          string
	Location         string
	JobCode          string
	OrgUnit          string
	VacancyEndISO    string
	ReservationState string
}

// Field is one form control.
type Field struct {
	ID    string
	Name  string
	Label string
	// Kind is one of text, number, date, select, textarea, hidden,
	// positionpicker.
	Kind        string
	Value       string
	Placeholder string
	Help        string
	Error       string
	Required    bool
	Options     []Option
	// Vacancies is the governed choice set for a positionpicker field
	// (UXLIVE-011). It is the whole control: a position that is not in this
	// list cannot be chosen, and an empty list renders the explicit
	// no-vacancy state rather than falling back to a text box.
	Vacancies []VacancyOption
	// EmptyTitle and EmptyDetail are what a positionpicker says when it has
	// nothing to offer. They are required for that kind, because an empty
	// picker that says nothing is indistinguishable from a broken one.
	EmptyTitle  string
	EmptyDetail string
	// Prefix and Suffix are short adornments (e.g. currency code).
	Prefix string
	Suffix string
	Step   string
	Min    string
}

// DetailView is one journey.
type DetailView struct {
	Journey JourneyCard
	// Unavailable marks a route whose detail was refused or no longer exists.
	// The renderer keeps recovery navigation but must not invent an empty
	// journey, unknown stage, or future workflow steps for that route.
	Unavailable bool
	// Diagnostics admits the operator-only disclosure containing protocol
	// identifiers, workflow nodes, work-item routing and evidence references.
	// Ordinary approvers never receive that machinery in their component tree.
	Diagnostics bool
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

	// WaitExplanation is PROMOUX-014's answer to a reader looking at a
	// WAITING_EFFECTIVE_DATE journey: the effective instant and its
	// timezone, who approved it, what runs when it fires, what checks
	// remain, whether a notification is sent, and what intervention (if
	// any) is authorized. Nil unless the engine sent every one of those
	// facts (tools/uxqual/journeyclient's waitExplanationFacts), which in
	// practice means the journey is not currently at that stage.
	WaitExplanation []Fact

	// Engine is the workflow instance as facts (instance id, version, plan
	// digest, status, current node).
	Engine []Fact
	Nodes  []NodeRow

	WorkItems []WorkItemCard

	// Ledger is nil until the terminal write is recorded.
	Ledger *LedgerCard
	// PendingOutcome explains precisely what remains before the employee
	// record changes. It is derived from the durable business stage.
	PendingOutcome string
	// EffectiveDateWait is the typed, server-projected explanation of a
	// durable effective-date wait. It is nil until the journey transport
	// carries the wait requirement; the renderer must not manufacture one
	// from a date-only journey summary.
	EffectiveDateWait *wait.EffectiveDateWait

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

	// Notes is the journey's notes panel; nil renders no panel.
	Notes *NotesView

	// Review is REV-091-02's reporting-line and pay-range cards, already
	// localized by the client from the server's authorized projection; nil
	// renders no section.
	Review *ReviewCards
}

// NotesView is the free-standing, append-only notes on one journey and, when
// the viewer may add one, the composer.
type NotesView struct {
	Notes []NoteEntry
	// Composer is nil when the viewer cannot add notes.
	Composer *NoteComposer
}

// NoteEntry is one note as a reader sees it. Every string is already
// localized and formatted by the client.
type NoteEntry struct {
	ID       string
	Author   string
	Initials string
	// Own marks the viewer's own note.
	Own bool
	// Stage names the stage the note was written at ("Finance review").
	Stage string
	// At is the formatted time; ISO is the machine-readable one for <time>.
	At   string
	ISO  string
	Body string
}

// NoteComposer is the add-a-note form. Field is the controlled textarea (its
// Value, Error and label); MaxRunes drives the visible character count.
type NoteComposer struct {
	Field    Field
	MaxRunes int
	Busy     bool
	// Revision changes each time a note is recorded. The textarea is keyed
	// by it: a browser keeps what was typed into a textarea regardless of
	// its text content, so remounting it is what empties the box.
	Revision int
	// Status is a transient confirmation ("Note added"), announced politely.
	Status   string
	Action   string
	OnSubmit func(values map[string]string)
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
	// Severity is one of blocking, warning, needs-data, info, success.
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
	// Recorded reports whether the terminal record this card describes
	// actually recorded the promotion. A terminal record exists for every
	// terminal outcome, including the ones that recorded a refusal
	// (UXLIVE-001), so its presence alone never means the employee record
	// changed. Only a recorded promotion shows EffectiveAt, because only a
	// recorded promotion has a date on which anything took effect.
	Recorded bool
	// StatusLabel is the terminal outcome's own display wording, localized
	// by the projector -- the same label the hero chip carries. Empty falls
	// back to the recorded/not-recorded wording.
	StatusLabel string
	// StatusTone is one of neutral, info, warning, success, danger. Empty
	// falls back to success when Recorded and warning when not.
	StatusTone string
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
	// PROMOUX-010: this is now rendered by the shared reviewSurface, so
	// Confirmation/ConfirmationNote also carry the busy state below.
	Confirmation     []Fact
	ConfirmationNote string
	// Busy is true while this action's own submission is in flight. The
	// review surface keeps the action bar mounted and disables the submit
	// control rather than collapsing, so a second click cannot fire a
	// duplicate submission. BusyLabel defaults to "Submitting…" when Busy
	// is true and this is empty.
	Busy      bool
	BusyLabel string
	// ConfirmTitle and ReviewLabel are localized, action-specific copy from
	// the projection. Empty values use the shared generic review wording.
	ConfirmTitle string
	ReviewLabel  string

	// OnSubmit, when set, makes this action a live call: the renderer
	// prevents the browser's own POST and calls it with every hidden entry
	// plus the current value of every field, keyed by Field.Name.
	OnSubmit func(values map[string]string)
}
