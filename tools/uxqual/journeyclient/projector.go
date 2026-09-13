package journeyclient

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// The projector is the whole of this client's judgment about what the
// engine's answers mean on a page, and it is written as pure functions of
// (configuration, answer, notice, field values) so every one of those
// judgments is a table test rather than a browser session.
//
// Two rules hold everywhere below. Nothing here invents an HCM fact: the
// stage is the engine's, the pay figures are the engine's decimal strings
// formatted, and a section whose data has not arrived is absent rather than
// empty. And nothing here escapes anything: the renderer owns escaping (GWC
// emits text nodes, never raw markup), so a projection that pre-escaped a
// value would double-escape it on screen.

// Brand is the product name in the masthead.
const Brand = "Human Capital Management Suite"

// Stage tokens as the contract carries them: the enum's own name with the
// generated prefix removed, so a page that shows a stage shows the engine's
// word for it.
const (
	stagePrefix           = "JOURNEY_STAGE_"
	stageProposed         = "PROPOSED"
	stageBlocked          = "BLOCKED"
	stageAwaitingApproval = "AWAITING_APPROVAL"
	stageCompleted        = "COMPLETED"
	stageRejected         = "REJECTED"
	stageFailed           = "FAILED"
	stageFinanceApproval  = "FINANCE_APPROVAL"
	stageManagerApproval  = "MANAGER_APPROVAL"
	stageWaitingEffective = "WAITING_EFFECTIVE_DATE"
	stageRevalidation     = "REVALIDATION"
	stageReapproval       = "REAPPROVAL"
	stageExecuted         = "EXECUTED"
	stageObservingEffects = "OBSERVING_EFFECTS"
	stageRecorded         = "RECORDED"
	stageRepairRequired   = "REPAIR_REQUIRED"
)

// Action identifiers. They are the vocabulary [App.Submit] switches on, so
// they are constants shared by the projection that emits them and the state
// machine that receives them rather than two lists of string literals.
const (
	ActionPropose = "propose"
	ActionExecute = "execute"
	ActionApprove = "approve"
	ActionReject  = "reject"
	// ActionProposeFor and ActionCreateWorker are the two the People panel
	// emits. They are not spelled here for this client's convenience: the
	// renderer's Wire binds those rows and that form to exactly these
	// strings (tools/uxqual/render/journey/live.go), so a disagreement about
	// either spelling is a click that silently does nothing.
	ActionProposeFor   = "propose-for"
	ActionCreateWorker = "create-worker"
	// ActionWithdraw, ActionCancel and ActionEditProposal are PROMOUX-013's
	// three typed interventions. Withdraw and Cancel are the same governed
	// capability at different stages (see actions' own doc comment);
	// EditProposal is Cancel-then-repropose.
	ActionWithdraw     = "withdraw"
	ActionCancel       = "cancel"
	ActionEditProposal = "edit-proposal"
)

// Proposal form field identifiers (Page.Values is keyed by these) and the
// request field names the submitted map is keyed by (Field.Name).
const (
	FieldWorker    = "propose-worker"
	FieldJobCode   = "propose-job"
	FieldGrade     = "propose-grade"
	FieldPosition  = "propose-position"
	FieldBase      = "propose-base"
	FieldEffective = "propose-effective"
	FieldReason    = "propose-reason"

	NameWorker    = "worker_ref"
	NameJobCode   = "target_job_code"
	NameGrade     = "target_grade"
	NamePosition  = "target_position_id"
	NameBase      = "proposed_base"
	NameEffective = "effective_date"
	NameReason    = "business_reason"

	// FieldApproveReason and FieldRejectReason are the two decision fields.
	// They are separate ids carrying the same request name so that a reason
	// typed into one is not silently carried into the other: they are
	// different sentences, and one of them is required.
	FieldApproveReason = "approve-reason"
	FieldRejectReason  = "reject-reason"
	NameDecisionReason = "reason"

	// FieldWithdrawReason and FieldCancelReason are the two typed-
	// intervention reason fields (PROMOUX-013). Separate ids for the same
	// reason a decision's own two reason fields are separate: a reason typed
	// into one must not silently carry into the other.
	FieldWithdrawReason    = "withdraw-reason"
	FieldCancelReason      = "cancel-reason"
	NameInterventionReason = "reason"

	// FieldEdit* are EditProposal's own editable fields, seeded from the
	// journey's current values so the reader corrects a real form rather
	// than starting blank. NameEdit* are the request field names.
	FieldEditJobCode        = "edit-job"
	FieldEditGrade          = "edit-grade"
	FieldEditBase           = "edit-base"
	FieldEditEffective      = "edit-effective"
	FieldEditBusinessReason = "edit-business-reason"
	FieldEditReason         = "edit-reason"

	NameEditJobCode        = "target_job_code"
	NameEditGrade          = "target_grade"
	NameEditBase           = "proposed_base"
	NameEditEffective      = "effective_date"
	NameEditBusinessReason = "business_reason"
	NameEditReason         = "reason"
)

// New-employee form field identifiers (Page.Values is keyed by these) and
// the CreateWorkerRequest field names the submitted map is keyed by. The
// names are the proto field names, so a violation the engine reports by
// field_path names a field this form actually has.
const (
	FieldWorkerLegalName     = "worker-legal-name"
	FieldWorkerPreferredName = "worker-preferred-name"
	FieldWorkerJobCode       = "worker-job-code"
	FieldWorkerGrade         = "worker-grade"
	FieldWorkerOrgUnit       = "worker-org-unit"
	FieldWorkerPosition      = "worker-position"
	FieldWorkerLocation      = "worker-location"
	FieldWorkerPayZone       = "worker-pay-zone"
	FieldWorkerBasePay       = "worker-base-pay"
	FieldWorkerCurrency      = "worker-currency"
	FieldWorkerBonusTarget   = "worker-bonus-target"
	FieldWorkerHireDate      = "worker-hire-date"
	FieldWorkerManager       = "worker-manager"

	NameLegalName      = "legal_name"
	NamePreferredName  = "preferred_name"
	NameWorkerJobCode  = "job_code"
	NameWorkerGrade    = "grade"
	NameOrgUnit        = "org_unit"
	NameWorkerPosition = "position_id"
	NameLocation       = "location"
	NamePayZone        = "pay_zone"
	NameWorkerBasePay  = "base_pay"
	NameCurrency       = "currency"
	NameBonusTarget    = "bonus_target"
	NameHireDate       = "hire_date"
	NameManagerRef     = "manager_ref"
)

// Defaults the new-employee form seeds when the cell's options name none.
//
// They are values the engine accepts rather than placeholders: a form whose
// default is refused teaches its reader that the form is broken, and this
// one is the first thing a reader meets in an empty cell.
const (
	// DefaultPositionID is used when the cell published no positions. It is
	// deliberately not one of the corpus positions: a new employee occupies
	// a new position unless the reader says otherwise.
	DefaultPositionID = "POS-NEW-001"
	// DefaultCurrency is only ever used when the pay-band catalog declared
	// no currency of its own, which the composed cell always does.
	DefaultCurrency = "USD"
	// DefaultBonusTarget is a fraction, not a percentage: five percent.
	DefaultBonusTarget = "0.0500"
)

// workerFieldIDs maps each request field name back to the form field that
// carries it, so a refusal the engine reports by field_path -- and the
// values the reader already typed -- land on the right control.
var workerFieldIDs = map[string]string{
	NameLegalName:      FieldWorkerLegalName,
	NamePreferredName:  FieldWorkerPreferredName,
	NameWorkerJobCode:  FieldWorkerJobCode,
	NameWorkerGrade:    FieldWorkerGrade,
	NameOrgUnit:        FieldWorkerOrgUnit,
	NameWorkerPosition: FieldWorkerPosition,
	NameLocation:       FieldWorkerLocation,
	NamePayZone:        FieldWorkerPayZone,
	NameWorkerBasePay:  FieldWorkerBasePay,
	NameCurrency:       FieldWorkerCurrency,
	NameBonusTarget:    FieldWorkerBonusTarget,
	NameHireDate:       FieldWorkerHireDate,
	NameManagerRef:     FieldWorkerManager,
}

// Tone and state vocabularies, restated here because the renderer's own
// constants are unexported. tools/uxqual/render/journey normalises anything
// outside them, so a drift degrades to a plain chip rather than breaking.
const (
	toneNeutral = "neutral"
	toneInfo    = "info"
	toneSuccess = "success"
	toneWarning = "warning"
	toneDanger  = "danger"

	stepDone     = "done"
	stepActive   = "active"
	stepUpcoming = "upcoming"
	stepFailed   = "failed"

	severityBlocking = "blocking"
	severityWarning  = "warning"
	severityInfo     = "info"
	severitySuccess  = "success"

	kindText     = "text"
	kindNumber   = "number"
	kindDate     = "date"
	kindSelect   = "select"
	kindTextarea = "textarea"
	kindHidden   = "hidden"

	// Worker provenance, as workspace.WorkerSource* carries it on the wire.
	sourceCorpus  = "CORPUS"
	sourceCreated = "CREATED"
)

// Timeline event kinds the engine composes (internal/intent/app's
// JourneyEvent* constants, carried on the wire as TimelineEvent.kind).
const (
	eventIntentCreated   = "INTENT_CREATED"
	eventSimulated       = "SIMULATED"
	eventInstanceStarted = "INSTANCE_STARTED"
	eventNode            = "NODE"
	eventWorkItem        = "WORK_ITEM"
	eventLedgerRecorded  = "LEDGER_RECORDED"
)

// emDash is what a table cell shows for a fact that does not exist yet. A
// blank cell reads as a rendering failure; a dash reads as an answer.
const emDash = "—"

// stageOf reduces the enum to the token the contract carries.
func stageOf(s journeyv1.JourneyStage) string {
	return strings.TrimPrefix(s.String(), stagePrefix)
}

// stageLabel is the stage in the page's own words, and stageTone the chip
// colour. Both delegate to StagePresentation, the one stage vocabulary My
// Work and Journeys share (PROMOUX-012): before it, Journeys said "Proposed"
// where My Work said "Ready to start approval".
func stageLabel(stage string) string {
	label, _ := StagePresentation(journeyv1.JourneyStage(journeyv1.JourneyStage_value[stagePrefix+stage]))
	return label
}

func stageTone(stage string) string {
	_, tone := StagePresentation(journeyv1.JourneyStage(journeyv1.JourneyStage_value[stagePrefix+stage]))
	return tone
}

// StagePresentation is a stage's shared English label and chip tone. It is
// total: an unknown stage is a schema drift, and says so plainly.
func StagePresentation(stage journeyv1.JourneyStage) (label, tone string) {
	switch stage {
	case journeyv1.JourneyStage_JOURNEY_STAGE_PROPOSED:
		return "Ready to start approval", toneNeutral
	case journeyv1.JourneyStage_JOURNEY_STAGE_BLOCKED:
		return "Blocked", toneWarning
	case journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_APPROVAL:
		return "Awaiting approval", toneWarning
	case journeyv1.JourneyStage_JOURNEY_STAGE_COMPLETED:
		return "Completed", toneSuccess
	case journeyv1.JourneyStage_JOURNEY_STAGE_RECORDED:
		return "Recorded", toneSuccess
	case journeyv1.JourneyStage_JOURNEY_STAGE_FINANCE_APPROVAL:
		return "Finance approval", toneWarning
	case journeyv1.JourneyStage_JOURNEY_STAGE_MANAGER_APPROVAL:
		return "Manager approval", toneWarning
	case journeyv1.JourneyStage_JOURNEY_STAGE_WAITING_EFFECTIVE_DATE:
		return "Waiting for effective date", toneNeutral
	case journeyv1.JourneyStage_JOURNEY_STAGE_REVALIDATION:
		return "Final checks", toneNeutral
	case journeyv1.JourneyStage_JOURNEY_STAGE_REAPPROVAL:
		return "Approval required again", toneWarning
	case journeyv1.JourneyStage_JOURNEY_STAGE_EXECUTED:
		return "Recording promotion", toneNeutral
	case journeyv1.JourneyStage_JOURNEY_STAGE_OBSERVING_EFFECTS:
		return "Checking downstream effects", toneNeutral
	case journeyv1.JourneyStage_JOURNEY_STAGE_REPAIR_REQUIRED:
		return "Needs repair", toneDanger
	case journeyv1.JourneyStage_JOURNEY_STAGE_REJECTED:
		return "Rejected", toneNeutral
	case journeyv1.JourneyStage_JOURNEY_STAGE_FAILED:
		return "Failed", toneDanger
	default:
		return "Status unavailable", toneWarning
	}
}

// chrome is everything both views share: the masthead, who is signed in,
// the navigation and the footer.
//
// UXAUDIT-007: Principal.LogoutHref carries cfg.LogoutPath through so the
// masthead's exit control has a destination whenever Purpose does -- the
// config island already plumbed logout_path to the client (see
// journeywasm's own loader), but nothing here forwarded it into the
// rendered Principal, so a reviewer told "Purpose: compensation_review" had
// an explanation with no paired way to leave it.
func chrome(cfg Config, title string, notice *journey.Notice, values map[string]string, currentDetail bool) journey.Page {
	return journey.Page{
		Title:       title,
		Brand:       Brand,
		TenantLabel: cfg.Tenant,
		Principal: journey.Principal{
			Subject:    cfg.Subject,
			Roles:      cfg.Roles,
			Purpose:    cfg.Purpose,
			LogoutHref: cfg.LogoutPath,
		},
		Nav: []journey.NavLink{
			// The workspace link is an ordinary document link to another
			// server-rendered page: it leaves this client entirely, so it is
			// never wired to a live route change (see App.wire).
			{Label: "Workspace", Href: WorkspacePath},
			{Label: "Journeys", Href: ListHref(), Current: !currentDetail},
		},
		Notice: notice,
		Footer: journey.Footer{
			Lines: []string{
				"Every figure here was read live from this cell over the gRPC tunnel, under the purpose above. Nothing on this page is cached in the browser.",
			},
		},
		Values: values,
	}
}

// ListData is everything the list route projects from: the two reads the
// route makes, the reader's selection, and any refusal the last
// new-employee submission collected.
//
// It is a struct rather than five more parameters because these five travel
// together everywhere -- App holds them as one piece of state, loads them in
// one round, and hands them here unchanged.
type ListData struct {
	// Journeys is the engine's own order (newest first); this projection
	// does not re-sort it, because the order a list is read in is the
	// engine's answer too.
	Journeys []*journeyv1.Journey
	// Workers and Options are ListWorkersResponse, split. When both are
	// zero the People panel is absent rather than empty: a page that could
	// not read the workforce must not claim there is nobody in it.
	Workers []*journeyv1.Worker
	Options *journeyv1.WorkforceOptions
	// SelectedRef is the employee the reader picked, from the route.
	SelectedRef string
	// WorkerErrors are per-field refusals from the last CreateWorker
	// attempt, keyed by the request field name the engine named.
	WorkerErrors map[string]string
}

// ListPage projects the journeys overview: the workforce, the journeys and
// the two forms that add to either.
func ListPage(cfg Config, data ListData, notice *journey.Notice, values map[string]string) journey.Page {
	p := chrome(cfg, "Promotion journeys · "+Brand, notice, values, false)
	cards := make([]journey.JourneyCard, 0, len(data.Journeys))
	for _, j := range data.Journeys {
		if j == nil {
			continue
		}
		cards = append(cards, card(cfg, j))
	}
	// UXAUDIT-017: Journeys is a lifecycle tracker grouped by subject and
	// status, not the flat list this loop just built in server-recency
	// order.
	cards = groupJourneyCards(cards)
	form := ProposalForm(values, data.Workers, data.SelectedRef, data.Options)
	if len(cfg.PagePermissions) > 0 && !cfg.CanPageAction("journeys", "create") {
		form.Disabled = true
		form.DisabledReason = "Your assigned role can review promotion journeys but cannot create one."
	}
	applyProposalCurrency(&form, workerCurrency(findWorker(data.Workers, data.SelectedRef), data.Options))
	p.List = &journey.ListView{
		Journeys: cards,
		Groups:   journeySubjectGroups(cards),
		Empty:    "No promotion has been proposed in this tenant yet. The form below starts one.",
		Form:     form,
		People:   PeopleView(cfg, data, values),
		// Whether the cell was composed with the P1B execution authority is
		// not a fact this page can read: the engine answers it by refusing
		// ExecuteJourney with UNAVAILABLE. Claiming execution is off before
		// anyone has asked would be inventing an answer, so the form is
		// offered and a refusal becomes a notice.
		EngineAvailable: true,
	}
	return p
}

// ProposalPage projects the focused transaction start reached from one
// person's profile. The worker named by the route is rendered as immutable
// context and carried as a hidden form value; no unrelated worker or
// journey is put on the page.
func ProposalPage(cfg Config, data ListData, notice *journey.Notice, values map[string]string) journey.Page {
	worker := findWorker(data.Workers, data.SelectedRef)
	title := "Start a promotion · " + Brand
	if worker != nil && workerName(worker) != "" {
		title = "Promote " + workerName(worker) + " · " + Brand
	}
	p := chrome(cfg, title, notice, values, true)
	form := focusedProposalForm(values, data.SelectedRef, data.Options, worker)
	if len(cfg.PagePermissions) > 0 && !cfg.CanPageAction("journeys", "create") {
		form.Disabled = true
		form.DisabledReason = "Your assigned role can review promotion journeys but cannot create one."
	}
	applyProposalCurrency(&form, workerCurrency(worker, data.Options))
	loading := worker == nil && notice != nil && notice.Title == "Working…"
	if worker == nil && !loading {
		form.Disabled = true
		form.DisabledReason = "This employee is not available under the current access purpose. Return to their profile and try again."
	}
	p.Proposal = &journey.ProposalView{
		Subject:         promotionSubject(worker, data.Options),
		Form:            form,
		Loading:         loading,
		BackHref:        personHref(data.SelectedRef),
		JourneysLink:    journey.NavLink{Label: "View all promotion journeys", Href: ListHref()},
		EngineAvailable: true,
	}
	return p
}

func findWorker(workers []*journeyv1.Worker, ref string) *journeyv1.Worker {
	ref = strings.TrimSpace(ref)
	for _, worker := range workers {
		if worker != nil && worker.GetWorkerRef() == ref {
			return worker
		}
	}
	return nil
}

func promotionSubject(worker *journeyv1.Worker, options *journeyv1.WorkforceOptions) *journey.PromotionSubject {
	if worker == nil {
		return nil
	}
	return &journey.PromotionSubject{
		Ref:      worker.GetWorkerRef(),
		Name:     workerName(worker),
		PhotoURL: worker.GetProfilePhotoUrl(),
		Number:   worker.GetWorkerNumber(),
		Title:    JobTitle(worker.GetJobCode()),
		JobCode:  worker.GetJobCode(),
		Grade:    worker.GetGrade(),
		OrgUnit:  worker.GetOrgUnit(),
		Location: worker.GetLocation(),
		PayLine:  orDash(formatAmount(workerCurrency(worker, options), worker.GetBasePay())),
	}
}

func personHref(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "/workspace/app/people"
	}
	return "/workspace/app/person?person=" + url.QueryEscape(ref)
}
func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// card is one journey as the list shows it and as the detail header repeats
// it. cfg supplies PROMOUX-008's diagnostics authorization: a server-derived
// verdict from the same page permissions the shell island already carries,
// never a client guess drawn from whether the wire fields happen to be set.
func card(cfg Config, j *journeyv1.Journey) journey.JourneyCard {
	stage := stageOf(j.GetStage())
	return journey.JourneyCard{
		IntentID:              j.GetIntentId(),
		Href:                  DetailHref(j.GetIntentId()),
		WorkerName:            j.GetWorkerName(),
		WorkerRef:             j.GetWorkerRef(),
		Headline:              headline(j.GetCurrent().GetJobCode(), j.GetCurrent().GetGrade(), j.GetTarget().GetJobCode(), j.GetTarget().GetGrade()),
		PayLine:               payLine(j.GetCurrency(), j.GetCurrentBase(), j.GetProposedBase()),
		EffectiveDate:         formatDate(j.GetEffectiveDate()),
		Stage:                 stage,
		StageLabel:            stageLabel(stage),
		StageTone:             stageTone(stage),
		NextStep:              NextStepLabel(JourneyStatusDimension(j).NextStep),
		Closed:                JourneyClosed(j),
		Updated:               formatTime(j.GetUpdatedAt()),
		InstanceID:            j.GetInstanceId(),
		DiagnosticsAuthorized: cfg.CanPageAction(diagnosticsPageID, "view"),
	}
}

// groupJourneyCards orders one tenant's journey cards so every subject's
// journeys are adjacent (UXAUDIT-017, GREEN: "a lifecycle tracker grouped by
// subject and status"). It is a stable reordering, never a filter: every
// card ListPage built is still present, only rearranged.
//
// Subjects keep the engine's own recency order for where their group
// appears -- the first card for a subject, in the order ListJourneys
// returned it, anchors that subject's position -- so grouping never
// invents an ordering the server did not already imply. Within one
// subject's group, an open journey always sorts ahead of a terminal one
// (the server's lifecycle closure, the same open/closed dimension the People
// table's per-row count already shares), so the active thread reads before its own
// history; journeys of equal openness keep the engine's order between them.
func groupJourneyCards(cards []journey.JourneyCard) []journey.JourneyCard {
	order := make([]string, 0, len(cards))
	seen := make(map[string]bool, len(cards))
	bySubject := make(map[string][]journey.JourneyCard, len(cards))
	for _, c := range cards {
		key := journeySubjectKey(c)
		if !seen[key] {
			seen[key] = true
			order = append(order, key)
		}
		bySubject[key] = append(bySubject[key], c)
	}
	grouped := make([]journey.JourneyCard, 0, len(cards))
	for _, key := range order {
		group := bySubject[key]
		sort.SliceStable(group, func(i, j int) bool {
			return journeyOpenRank(group[i]) < journeyOpenRank(group[j])
		})
		grouped = append(grouped, group...)
	}
	return grouped
}

// journeySubjectKey names the subject a card groups under: the worker
// reference when the wire supplied one, or the worker's name when it did
// not (a defensive fallback, not an expected production path).
func journeySubjectKey(c journey.JourneyCard) string {
	if c.WorkerRef != "" {
		return c.WorkerRef
	}
	return c.WorkerName
}

// journeyOpenRank ranks a card's lifecycle openness for the within-subject
// sort: 0 for open, 1 for closed (the server's JourneyViewerProjection.closed).
func journeyOpenRank(c journey.JourneyCard) int {
	if c.Closed {
		return 1
	}
	return 0
}

// journeySubjectGroups partitions an already subject-clustered card slice
// (see groupJourneyCards, which every caller of this function has already
// run) into the named groups journey.ListView.Groups renders. It draws the
// group boundaries the clustering already produced; it does not reorder or
// filter anything, so a caller that skips groupJourneyCards first would get
// meaningless groups -- ListPage always runs both in that order.
//
// A subject with exactly one journey still gets its own single-journey
// group: GREEN asks for a lifecycle tracker "grouped by subject and
// status", and a tenant where every subject happens to have one open
// journey is a real, common state, not an edge case excused from grouping.
func journeySubjectGroups(clustered []journey.JourneyCard) []journey.JourneySubjectGroup {
	groups := make([]journey.JourneySubjectGroup, 0, len(clustered))
	for _, c := range clustered {
		key := journeySubjectKey(c)
		if n := len(groups); n > 0 && journeySubjectKey(groups[n-1].Journeys[0]) == key {
			last := &groups[n-1]
			last.Journeys = append(last.Journeys, c)
			last.Statuses = appendJourneyStatusChip(last.Statuses, c)
			continue
		}
		groups = append(groups, journey.JourneySubjectGroup{
			Subject:  c.WorkerName,
			Journeys: []journey.JourneyCard{c},
			Statuses: appendJourneyStatusChip(nil, c),
		})
	}
	return groups
}

// appendJourneyStatusChip adds c's status to a group's distinct-status list,
// deduplicated by label in first-seen order, so a group whose journeys share
// a stage shows that status once rather than once per journey.
func appendJourneyStatusChip(statuses []journey.JourneyStatusChip, c journey.JourneyCard) []journey.JourneyStatusChip {
	for _, existing := range statuses {
		if existing.Label == c.StageLabel {
			return statuses
		}
	}
	return append(statuses, journey.JourneyStatusChip{Label: c.StageLabel, Tone: c.StageTone})
}

// DefaultEffectiveDate is the proposal form's seeded effective date: the
// first day of the month three months out.
//
// It is a computed planning default rather than a promise of eligibility.
// The server evaluates the selected date against the proposal's policy context.
func DefaultEffectiveDate(now time.Time) string {
	t := now.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, 3, 0).Format(isoDate)
}

// ProposalForm is the manager's new-proposal form.
//
// Hidden is empty and stays empty: this client submits over gRPC, so there
// is no cross-site request to forge and no CSRF token to carry. The
// no-script fallback the contract still renders posts to the page's own
// address and is refused there, which is the honest degradation for a
// surface that has no HTTP/JSON equivalent.
// workers is the population the select lists -- the corpus and this
// tenant's own created employees alike, because a promotion may be proposed
// for either -- and selectedRef the one the People table has picked.
func ProposalForm(values map[string]string, workers []*journeyv1.Worker, selectedRef string, optionSet ...*journeyv1.WorkforceOptions) journey.ProposalForm {
	effective := values[FieldEffective]
	if effective == "" {
		effective = DefaultEffectiveDate(time.Now())
	}
	worker := values[FieldWorker]
	if worker == "" {
		worker = selectedRef
	}
	var options *journeyv1.WorkforceOptions
	if len(optionSet) > 0 {
		options = optionSet[0]
	}
	workerObj := findWorker(workers, worker)
	jobCodes, grades := governedProposalChoices(options, workerObj, values[FieldJobCode])
	return journey.ProposalForm{
		Action: ListHref(),
		Hidden: map[string]string{},
		Submit: "Propose and simulate",
		// PROMOUX-010: Start's own final action is guarded by the same
		// shared review surface Approve and Reject use (actionConfirmation
		// below feeds those). There is no journey.JourneyCard yet -- this
		// form is what creates one -- so proposalConfirmation builds the
		// identical Employee/Placement/Base pay/Effective date vocabulary
		// from the worker record and the fields as currently filled,
		// through the same headline/payLine/workerName helpers
		// actionConfirmation's callers already use elsewhere in this file.
		Confirmation:     proposalConfirmation(workerObj, options, values[FieldJobCode], values[FieldGrade], values[FieldBase], effective),
		ConfirmationNote: "The proposal is simulated on submit; nothing is executed until the gate admits it.",
		Fields: []journey.Field{
			{
				ID: FieldWorker, Name: NameWorker, Label: "Worker", Kind: kindSelect, Required: true,
				Value: worker,
				Help:  "Only workers whose records you may read under this purpose can be proposed. The current placement, pay and budget authority are read from the record, never from this form.",
				// The list is the workforce the cell just answered with,
				// never a list this client keeps: an employee added a minute
				// ago is proposable, and one the caller may not read is not
				// offered.
				Options: workerOptions(workers, worker),
			},
			{
				ID: FieldJobCode, Name: NameJobCode, Label: "Target job code", Kind: kindSelect,
				Required: true, Options: stringOptions("Select a governed job code", jobCodes, values[FieldJobCode]),
				Help: "Published by this organization's compensation catalog; an arbitrary code cannot be submitted.",
			},
			{
				ID: FieldGrade, Name: NameGrade, Label: "Target grade", Kind: kindSelect, Required: true,
				Options: stringOptions("Select target grade", grades, values[FieldGrade]),
				Help:    "Published grades only. The simulation verifies the job-code and grade combination and routes any required additional approval.",
			},
			{
				ID: FieldPosition, Name: NamePosition, Label: "Target position", Kind: kindText,
				Required: true, Placeholder: "e.g. POS-HRBP-301",
				Help: "Required. The position is the governed assignment the promoted worker will occupy.",
			},
			{
				ID: FieldBase, Name: NameBase, Label: "Proposed base pay", Kind: kindNumber,
				Required: true, Step: "0.01", Min: "0", Placeholder: "0.00",
				Suffix: "per year",
				Help:   "In the worker's current currency. The budget envelope is checked during simulation.",
			},
			{
				ID: FieldEffective, Name: NameEffective, Label: "Effective date", Kind: kindDate,
				Required: true, Value: effective,
				Help: "Choose when the promotion should take effect. Simulation checks the date against the applicable promotion rules; recording still requires approvals and final checks.",
			},
			{
				ID: FieldReason, Name: NameReason, Label: "Business reason", Kind: kindTextarea,
				Required: true, Placeholder: "Describe the business need and expanded responsibilities.",
				Help: "Kept with the intent and carried into the ledger event's evidence.",
			},
		},
	}
}

// FocusedProposalForm removes the worker chooser from ProposalForm and pins
// the route's worker as a hidden value. On a person-scoped transaction page,
// changing the subject inside the form would be a dangerous context switch;
// choosing another person belongs on the People page.
func FocusedProposalForm(values map[string]string, selectedRef string, optionSet ...*journeyv1.WorkforceOptions) journey.ProposalForm {
	var options *journeyv1.WorkforceOptions
	if len(optionSet) > 0 {
		options = optionSet[0]
	}
	return focusedProposalForm(values, selectedRef, options, nil)
}

func focusedProposalForm(values map[string]string, selectedRef string, options *journeyv1.WorkforceOptions, worker *journeyv1.Worker) journey.ProposalForm {
	form := ProposalForm(values, nil, selectedRef, options)
	jobCodes, grades := governedProposalChoices(options, worker, values[FieldJobCode])
	if worker != nil && len(jobCodes) == 0 {
		form.Disabled = true
		form.DisabledReason = "No promotion path is available for this employee's current job and grade. Ask your HR administrator to publish an eligible next role in the job ladder, then return to this employee's profile."
	}
	form.Fields[1].Label = "Valid next role"
	form.Fields[1].Options = promotionJobOptions(options, worker, jobCodes, values[FieldJobCode])
	form.Fields[1].Help = "Published by this organization's job architecture. Pay bands alone do not make an unrelated role a valid promotion target."
	form.Fields[2].Options = stringOptions("Select target grade", grades, values[FieldGrade])
	form.Fields[2].Help = "Pinned to the selected ladder edge; the server refuses a job and grade that are not published together."
	if path := selectedPromotionPath(options, worker, values[FieldJobCode], values[FieldGrade]); path != nil {
		form.Fields[4].Help = promotionPathRuleHelp(path)
	} else {
		form.Fields[4].Help = "Select a valid next role to see its exact base-pay guardrail and benefit-eligibility rules."
	}
	form.Action = ProposalHref(selectedRef)
	form.Fields[0] = journey.Field{
		ID: FieldWorker, Name: NameWorker, Kind: kindHidden, Value: strings.TrimSpace(selectedRef),
	}
	// ProposalForm above resolved no worker object (it was called with a
	// nil workers list, since the focused route pins the subject rather
	// than offering a chooser), so its Confirmation named the generic
	// "Employee" fallback. worker here is the real record this route was
	// given, so refine the same review facts with it now that the worker
	// chooser field above has been replaced by the pinned hidden value.
	effective := values[FieldEffective]
	if effective == "" {
		effective = DefaultEffectiveDate(time.Now())
	}
	form.Confirmation = proposalConfirmation(worker, options, values[FieldJobCode], values[FieldGrade], values[FieldBase], effective)
	return form
}

// HasPromotionChoices shares the proposal form's published-choice resolution
// with employee launchers. It is presentation eligibility, not authorization.
func HasPromotionChoices(options *journeyv1.WorkforceOptions, worker *journeyv1.Worker) bool {
	jobs, _ := governedProposalChoices(options, worker, "")
	return len(jobs) > 0
}

func governedProposalChoices(options *journeyv1.WorkforceOptions, worker *journeyv1.Worker, selectedJob string) ([]string, []string) {
	if options == nil || len(options.GetPlacements()) == 0 {
		return options.GetJobCodes(), options.GetGrades()
	}
	jobs, grades := map[string]struct{}{}, map[string]struct{}{}
	payZone, currency := "", options.GetCurrency()
	if worker != nil {
		payZone = worker.GetPayZone()
		if worker.GetCurrency() != "" {
			currency = worker.GetCurrency()
		}
	}
	if len(options.GetPromotionPaths()) > 0 && worker != nil {
		for _, path := range options.GetPromotionPaths() {
			if path.GetSourceJobCode() != worker.GetJobCode() || path.GetSourceGrade() != worker.GetGrade() {
				continue
			}
			if !placementAvailable(options, path.GetTargetJobCode(), path.GetTargetGrade(), payZone, currency) {
				continue
			}
			jobs[path.GetTargetJobCode()] = struct{}{}
			if selectedJob == "" || path.GetTargetJobCode() == selectedJob {
				grades[path.GetTargetGrade()] = struct{}{}
			}
		}
		return sortedKeys(jobs), sortedKeys(grades)
	}
	for _, placement := range options.GetPlacements() {
		if (payZone != "" && placement.GetPayZone() != payZone) || (currency != "" && placement.GetCurrency() != currency) {
			continue
		}
		jobs[placement.GetJobCode()] = struct{}{}
		if selectedJob == "" || placement.GetJobCode() == selectedJob {
			grades[placement.GetGrade()] = struct{}{}
		}
	}
	return sortedKeys(jobs), sortedKeys(grades)
}

func placementAvailable(options *journeyv1.WorkforceOptions, jobCode, grade, payZone, currency string) bool {
	for _, placement := range options.GetPlacements() {
		if placement.GetJobCode() == jobCode && placement.GetGrade() == grade &&
			(payZone == "" || placement.GetPayZone() == payZone) &&
			(currency == "" || placement.GetCurrency() == currency) {
			return true
		}
	}
	return false
}

func selectedPromotionPath(options *journeyv1.WorkforceOptions, worker *journeyv1.Worker, jobCode, grade string) *journeyv1.PromotionPathOption {
	if options == nil || worker == nil {
		return nil
	}
	for _, path := range options.GetPromotionPaths() {
		if path.GetSourceJobCode() == worker.GetJobCode() && path.GetSourceGrade() == worker.GetGrade() &&
			path.GetTargetJobCode() == jobCode && (grade == "" || path.GetTargetGrade() == grade) {
			return path
		}
	}
	return nil
}

func promotionJobOptions(options *journeyv1.WorkforceOptions, worker *journeyv1.Worker, jobCodes []string, selected string) []journey.Option {
	out := stringOptions("Select a valid next role", jobCodes, selected)
	for i := 1; i < len(out); i++ {
		if path := selectedPromotionPath(options, worker, out[i].Value, ""); path != nil && path.GetTargetTitle() != "" {
			out[i].Label = out[i].Value + " — " + path.GetTargetTitle()
		}
	}
	return out
}

func promotionPathRuleHelp(path *journeyv1.PromotionPathOption) string {
	if path == nil {
		return ""
	}
	help := "This ladder edge allows a base increase from " + fractionPercentLabel(path.GetMinimumBaseIncrease()) + " to " + fractionPercentLabel(path.GetMaximumBaseIncrease()) + "."
	if policy := strings.TrimSpace(path.GetCompensationPolicyRef()); policy != "" {
		help += " Compensation is evaluated under " + policy + "."
	}
	if rules := path.GetBenefitRuleRefs(); len(rules) > 0 {
		help += " Benefit eligibility is reevaluated under " + strings.Join(rules, ", ") + "; existing elections are not changed directly."
	}
	return help
}

func fractionPercentLabel(fraction string) string {
	d, err := values.NewDecimal(fraction, 4, values.RoundingHalfEven)
	if err != nil {
		return strings.TrimSpace(fraction)
	}
	hundred := values.MustDecimal("100", 0, values.RoundingExactRequired)
	percent, err := d.Mul(hundred, 2, values.RoundingHalfEven)
	if err != nil {
		return strings.TrimSpace(fraction)
	}
	return percent.String() + "%"
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

// applyProposalCurrency labels the pay input with the denomination the live
// workforce answer supplied for the selected employee. With no selected
// employee it stays blank rather than asserting a tenant-wide currency that
// may not be true for the eventual subject.
func applyProposalCurrency(form *journey.ProposalForm, currency string) {
	if form == nil {
		return
	}
	for i := range form.Fields {
		if form.Fields[i].ID == FieldBase {
			form.Fields[i].Prefix = strings.TrimSpace(currency)
			return
		}
	}
}

// ---------------------------------------------------------------------
// The workforce
// ---------------------------------------------------------------------

// PeopleNote is the one line under the People heading. It says what adding
// an employee here actually does, because "add an employee" in a governed
// cell is a durable fact rather than a row in a demo table, and a reader who
// does not know that will not understand why it cannot be edited away.
const PeopleNote = "Employees you add are recorded as facts in this cell's workforce table and become visible to every governed read."

// PeopleEmpty is the empty state. It names the form below it, so an empty
// cell reads as a starting point rather than as a failure.
const PeopleEmpty = "This cell knows no employees under this purpose yet. The form below records the first one."

// jobTitles is the human name for each job code the release's corpus and
// pay-band catalog carry.
//
// It is a lookup here rather than a field on the wire because the engine
// does not publish one: the corpus carries codes, and a code is not a job
// title. An unknown code degrades to the code itself, which is the honest
// answer -- inventing a title from a code's shape would put a fact on the
// page that no system asserted.
var jobTitles = map[string]string{
	"OPS-HRBP2":  "HR Business Partner",
	"OPS-HRBP3":  "Senior HR Business Partner",
	"ENG-SWE3":   "Software Engineer III",
	"ENG-MGR1":   "Engineering Manager",
	"CLN-NURSE4": "Registered Nurse IV",
}

// JobTitle is the human name for a job code, or the code when there is none.
func JobTitle(jobCode string) string {
	code := strings.TrimSpace(jobCode)
	if title, ok := jobTitles[code]; ok {
		return title
	}
	return code
}

// PeopleView projects the workforce panel, or nil when the workforce read
// has not answered.
//
// The nil is load-bearing: the renderer leaves the panel out entirely for a
// nil view, and a panel that said "no employees" because the read was
// refused would be this page asserting something it does not know.
func PeopleView(cfg Config, data ListData, values map[string]string) *journey.PeopleView {
	if len(data.Workers) == 0 && data.Options == nil {
		return nil
	}
	form := WorkerForm(data.Options, values, data.WorkerErrors)
	if len(cfg.PagePermissions) > 0 && !cfg.CanPageAction("people", "create") {
		form.Disabled = true
		form.DisabledReason = "Your assigned role can review people but cannot create an employee record."
	}
	return &journey.PeopleView{
		Workers:       workerCards(data.Workers, data.Journeys, data.SelectedRef, data.Options),
		DirectoryLink: journey.NavLink{Label: "Open the full People directory", Href: "/workspace/app/people"},
		Empty:         PeopleEmpty,
		Form:          form,
		SelectedRef:   data.SelectedRef,
		Note:          PeopleNote,
	}
}

// workerCards is one row per employee.
func workerCards(workers []*journeyv1.Worker, list []*journeyv1.Journey, selectedRef string, options *journeyv1.WorkforceOptions) []journey.WorkerCard {
	if len(workers) == 0 {
		return nil
	}
	open := openJourneyCounts(list)
	out := make([]journey.WorkerCard, 0, len(workers))
	for _, w := range workers {
		if w == nil {
			continue
		}
		out = append(out, workerCard(w, open, selectedRef, options))
	}
	return out
}

func workerCard(w *journeyv1.Worker, open map[string]int, selectedRef string, options *journeyv1.WorkforceOptions) journey.WorkerCard {
	ref := w.GetWorkerRef()
	created := w.GetSource() == sourceCreated
	tone := ""
	if created {
		tone = toneInfo
	}
	return journey.WorkerCard{
		Ref:          ref,
		Name:         workerName(w),
		Number:       w.GetWorkerNumber(),
		Title:        JobTitle(w.GetJobCode()),
		JobCode:      w.GetJobCode(),
		Grade:        w.GetGrade(),
		OrgUnit:      w.GetOrgUnit(),
		Location:     w.GetLocation(),
		PayLine:      orDash(formatAmount(workerCurrency(w, options), w.GetBasePay())),
		HireDate:     orDash(formatDate(w.GetHireDate())),
		Source:       sourceOf(w.GetSource()),
		SourceLabel:  sourceLabel(w.GetSource()),
		Tone:         tone,
		Selected:     ref != "" && ref == selectedRef,
		OpenJourneys: openFor(w, open),
		ProposeHref:  ProposalHref(ref),
	}
}

// workerName prefers what the workspace calls the person over what their
// contract does, and falls back rather than rendering an empty row header.
func workerName(w *journeyv1.Worker) string {
	if name := strings.TrimSpace(w.GetPreferredName()); name != "" {
		return name
	}
	return strings.TrimSpace(w.GetLegalName())
}

// workerCurrency is the currency a base pay is denominated in: the worker's
// own when the record carries one, and the catalog's otherwise. A corpus
// worker carries neither a base nor a currency, which is why this never
// invents "USD" -- an amount-less row shows a dash, not a bare currency.
func workerCurrency(w *journeyv1.Worker, options *journeyv1.WorkforceOptions) string {
	if c := strings.TrimSpace(w.GetCurrency()); c != "" {
		return c
	}
	return strings.TrimSpace(options.GetCurrency())
}

func sourceOf(source string) string {
	if strings.TrimSpace(source) == sourceCreated {
		return sourceCreated
	}
	return sourceCorpus
}

// sourceLabel is the chip's word. It is supplied rather than left to the
// renderer's fallback so the two provenances are named in this page's own
// vocabulary in exactly one place.
func sourceLabel(source string) string {
	if sourceOf(source) == sourceCreated {
		return "Created"
	}
	return "Corpus"
}

// openJourneyCounts counts the journeys that are still moving, by the
// reference each one names its worker with.
func openJourneyCounts(list []*journeyv1.Journey) map[string]int {
	counts := map[string]int{}
	for _, j := range list {
		if j == nil {
			continue
		}
		if JourneyClosed(j) {
			continue
		}
		if ref := j.GetWorkerRef(); ref != "" {
			counts[ref]++
		}
	}
	return counts
}

// openFor is how many open journeys one employee has.
//
// Both of the worker's identifiers are consulted because a journey names its
// subject by whichever one the read that created it used: a corpus journey
// carries the governed read's entity id, a journey proposed through this
// page carries the reference the picker submitted. Counting only one of them
// would show a zero beside a person whose promotion is on the same screen.
func openFor(w *journeyv1.Worker, open map[string]int) int {
	ref, id := w.GetWorkerRef(), w.GetWorkerId()
	n := open[ref]
	if id != "" && id != ref {
		n += open[id]
	}
	return n
}

// workerOptions is the proposal form's worker picker: an empty first option
// so the form never submits a person nobody chose, then every employee the
// cell answered with.
func workerOptions(workers []*journeyv1.Worker, selectedRef string) []journey.Option {
	out := make([]journey.Option, 0, len(workers)+1)
	out = append(out, journey.Option{Value: "", Label: "Select a worker", Selected: selectedRef == ""})
	for _, w := range workers {
		if w == nil || w.GetWorkerRef() == "" {
			continue
		}
		out = append(out, journey.Option{
			Value:    w.GetWorkerRef(),
			Label:    workerOptionLabel(w),
			Selected: w.GetWorkerRef() == selectedRef,
		})
	}
	return out
}

// workerOptionLabel names one option the way the People table names the same
// row, so picking from the select and picking from the table are visibly the
// same act.
func workerOptionLabel(w *journeyv1.Worker) string {
	label := workerName(w)
	if placement := joinPlacement(w.GetJobCode(), w.GetGrade()); placement != "" {
		label += " — " + placement
	}
	return label
}

// DefaultHireDate is the new-employee form's seeded hire date: the first day
// of next month.
//
// It is computed rather than fixed for the same reason the proposal form's
// effective date is: a date the engine would refuse teaches the reader that
// the form is broken. The first of next month is the ordinary start date for
// a new employee and is never in the past.
func DefaultHireDate(now time.Time) string {
	t := now.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, 1, 0).Format(isoDate)
}

// WorkerForm is the new-employee form.
//
// Every placement the cell publishes a closed set for is a select over that
// set, and everything else is free text: offering a free-text job code would
// let a reader record an employee whose every promotion is later refused for
// a reason that has nothing to do with the promotion (the placement lands
// outside every pay band), which is exactly the failure the engine's own
// INVALID_ARGUMENT on this call exists to prevent.
//
// Hidden is empty for the same reason ProposalForm's is: this client submits
// over gRPC, so there is no cross-site request to forge.
func WorkerForm(options *journeyv1.WorkforceOptions, values map[string]string, fieldErrors map[string]string) journey.WorkerForm {
	currency := strings.TrimSpace(options.GetCurrency())
	fields := []journey.Field{
		{
			ID: FieldWorkerLegalName, Name: NameLegalName, Label: "Legal name", Kind: kindText,
			Required: true, Value: values[FieldWorkerLegalName],
			Placeholder: "As it appears on the contract",
			Help:        "Recorded verbatim. A correction is a later fact, not an edit to this one.",
		},
		{
			ID: FieldWorkerPreferredName, Name: NamePreferredName, Label: "Preferred name", Kind: kindText,
			Value: values[FieldWorkerPreferredName], Placeholder: "How the workspace addresses them",
			Help: "Leave blank and the engine derives one from the legal name.",
		},
		selectField(FieldWorkerJobCode, NameWorkerJobCode, "Job code", values,
			labelledOptions(options.GetJobCodes(), jobCodeLabel), true,
			"Only jobs the pay-band catalog covers are listed: a placement no band covers cannot be scored."),
		selectField(FieldWorkerGrade, NameWorkerGrade, "Grade", values,
			labelledOptions(options.GetGrades(), nil), true, ""),
		selectField(FieldWorkerOrgUnit, NameOrgUnit, "Org unit", values,
			labelledOptions(options.GetOrgUnits(), nil), true,
			"The org units this cell observes. It is also where the location falls back to."),
		{
			ID: FieldWorkerPosition, Name: NameWorkerPosition, Label: "Position id", Kind: kindText,
			Value:       valueOr(values, FieldWorkerPosition, firstOr(options.GetPositions(), DefaultPositionID)),
			Placeholder: DefaultPositionID,
			Help:        "The position this employee occupies.",
		},
		{
			ID: FieldWorkerLocation, Name: NameLocation, Label: "Location", Kind: kindText,
			Value: values[FieldWorkerLocation], Placeholder: "City, country",
			Help: "Leave blank and the record falls back to the org unit.",
		},
		selectField(FieldWorkerPayZone, NamePayZone, "Pay zone", values,
			labelledOptions(options.GetPayZones(), nil), true,
			"Decides which pay band the record is scored against."),
		{
			ID: FieldWorkerBasePay, Name: NameWorkerBasePay, Label: "Base pay", Kind: kindNumber,
			Required: true, Value: values[FieldWorkerBasePay], Step: "0.01", Min: "0",
			Prefix: currencyOr(currency), Suffix: "per year",
			Help: "Must be greater than zero and inside the band for the placement above.",
		},
		currencyField(currency, values),
		{
			ID: FieldWorkerBonusTarget, Name: NameBonusTarget, Label: "Bonus target", Kind: kindNumber,
			Value: valueOr(values, FieldWorkerBonusTarget, DefaultBonusTarget),
			Step:  "0.0001", Min: "0", Suffix: "ratio",
			Help: "A fraction of base pay at plan: 0.0500 is five percent.",
		},
		{
			ID: FieldWorkerHireDate, Name: NameHireDate, Label: "Hire date", Kind: kindDate,
			Required: true,
			Value:    valueOr(values, FieldWorkerHireDate, DefaultHireDate(time.Now())),
			Help:     "The date the employment relationship starts, not the date you record it.",
		},
		{
			ID: FieldWorkerManager, Name: NameManagerRef, Label: "Manager", Kind: kindText,
			Value: values[FieldWorkerManager], Placeholder: "worker:NW-00000",
			Help: "Whose approval routes for this person's changes. Leave blank and the engine derives one.",
		},
	}
	for i := range fields {
		fields[i].Error = fieldErrors[fields[i].Name]
	}
	return journey.WorkerForm{
		Action: ListHref(),
		Hidden: map[string]string{},
		Submit: "Add employee",
		Fields: fields,
	}
}

// currencyField is the one control whose kind depends on the cell: when the
// pay-band catalog declares its currency there is nothing to choose, so the
// value travels as a hidden input rather than as a text box asking the
// reader to retype a fact the cell already stated.
func currencyField(currency string, values map[string]string) journey.Field {
	if currency != "" {
		return journey.Field{
			ID: FieldWorkerCurrency, Name: NameCurrency, Label: "Currency",
			Kind: kindHidden, Value: currency,
		}
	}
	return journey.Field{
		ID: FieldWorkerCurrency, Name: NameCurrency, Label: "Currency", Kind: kindText,
		Required: true, Value: valueOr(values, FieldWorkerCurrency, DefaultCurrency),
		Placeholder: DefaultCurrency,
		Help:        "This cell's pay-band catalog declared no currency, so the record needs one.",
	}
}

// selectField is one closed-set control. Its value is the reader's, or the
// first option -- a select whose value is empty on the live path renders
// with nothing chosen, and would submit nothing.
func selectField(id, name, label string, values map[string]string, options []journey.Option, required bool, help string) journey.Field {
	value := valueOr(values, id, firstOptionValue(options))
	for i := range options {
		options[i].Selected = options[i].Value == value
	}
	return journey.Field{
		ID: id, Name: name, Label: label, Kind: kindSelect,
		Required: required, Value: value, Options: options, Help: help,
	}
}

// labelledOptions turns one of the cell's closed sets into options. label
// may be nil, which leaves the value as its own label.
func labelledOptions(items []string, label func(string) string) []journey.Option {
	out := make([]journey.Option, 0, len(items))
	for _, item := range items {
		text := item
		if label != nil {
			text = label(item)
		}
		out = append(out, journey.Option{Value: item, Label: text})
	}
	return out
}

// jobCodeLabel names a job code by what it is as well as by its code, so the
// picker can be used by someone who does not have the code list memorised.
func jobCodeLabel(code string) string {
	if title := JobTitle(code); title != code {
		return code + " — " + title
	}
	return code
}

func firstOptionValue(options []journey.Option) string {
	if len(options) == 0 {
		return ""
	}
	return options[0].Value
}

func firstOr(items []string, fallback string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return item
		}
	}
	return fallback
}

func currencyOr(currency string) string {
	if currency != "" {
		return currency
	}
	return DefaultCurrency
}

// valueOr is the reader's value for a field, or a default when they have not
// touched it.
func valueOr(values map[string]string, id, fallback string) string {
	if v := values[id]; v != "" {
		return v
	}
	return fallback
}

// DetailPage projects one journey. It offers no typed intervention (PROMOUX-013's
// Withdraw/Cancel/EditProposal): see [DetailPageWithInterventions] for the
// production path App.show actually drives; every caller here that has not
// yet read the two PreviewJourneyIntervention answers (every test written
// before PROMOUX-013, and any embedding that has not wired the two extra
// reads) still gets a working detail page, exactly as before.
func DetailPage(cfg Config, detail *journeyv1.JourneyDetail, notice *journey.Notice, values map[string]string) journey.Page {
	return DetailPageWithInterventions(cfg, detail, notice, values, nil, nil)
}

// DetailPageWithInterventions is DetailPage plus PROMOUX-013's two typed-
// intervention previews (WITHDRAW, CANCEL; EditProposal has no preview kind
// of its own -- see interventionActions' own doc comment). Either or both
// may be nil (the call has not answered yet, or failed): the Withdraw/
// Cancel/EditProposal actions still render in that case, gated purely by
// the journey's own stage, with a shorter consequence note than the
// server's own worded preview would supply.
func DetailPageWithInterventions(
	cfg Config, detail *journeyv1.JourneyDetail, notice *journey.Notice, values map[string]string,
	withdrawPreview, cancelPreview *journeyv1.PreviewJourneyInterventionResponse,
) journey.Page {
	summary := detail.GetJourney()
	head := card(cfg, summary)
	title := "Promotion journey · " + Brand
	if name := summary.GetWorkerName(); name != "" {
		title = name + " · Promotion journey · " + Brand
	}
	p := chrome(cfg, title, notice, values, true)
	detailActions := actions(head, detail.GetApprover(), detail.GetWorkItems(), withdrawPreview, cancelPreview)
	if len(cfg.PagePermissions) > 0 && !cfg.CanPageAction("journeys", "update") && !cfg.CanPageAction("work", "update") {
		detailActions = nil
	}
	p.Detail = &journey.DetailView{
		Journey:         head,
		BackLink:        journey.NavLink{Label: "Back to " + nonEmpty(summary.GetWorkerName(), "employee") + " in People", Href: personHref(summary.GetWorkerRef())},
		JourneysLink:    journey.NavLink{Label: "View all promotion journeys", Href: ListHref()},
		Steps:           steps(head.Stage, detail.GetTimeline()),
		Proposal:        proposalFacts(detail),
		Comparison:      comparison(summary),
		Findings:        findings(detail.GetFindings()),
		WaitExplanation: waitExplanationFacts(detail.GetFindings()),
		Engine:          engineFacts(detail.GetInstance()),
		Nodes:           nodes(detail.GetNodes()),
		WorkItems:       workItems(detail.GetWorkItems()),
		Ledger:          ledger(detail.GetLedger(), summary.GetEffectiveDate()),
		Evidence:        detail.GetEvidenceIds(),
		Timeline:        timeline(detail.GetTimeline()),
		Actions:         detailActions,
		EffectiveWindow: effectiveWindow(summary),
	}
	// PayBand and Budget stay nil: the detail carries no band and no
	// envelope, and a gauge drawn from numbers the engine did not send would
	// be the page inventing an answer. The section is absent until the
	// simulation's quantitative answers reach the wire.
	return p
}

// stepIDs and stepLabels are the four stages of the stepper, in order.
var (
	stepIDs        = [4]string{"proposed", "executed", "approval", "recorded"}
	stepLabels     = [4]string{"Proposal", "Execution", "Approval", "Record"}
	stepDoneDetail = [4]string{
		"The proposal was saved and checked against promotion rules.",
		"The workflow was authorized and started.",
		"The required approval step was completed.",
		"The promotion was recorded in the audit ledger.",
	}
	// stepStates is the whole of this page's reading of a stage. Each row is
	// the four steps' states at one stage, so "what does BLOCKED look like"
	// is one line to read rather than a walk through conditionals.
	stepStates = map[string][4]string{
		stageProposed:         {stepDone, stepActive, stepUpcoming, stepUpcoming},
		stageBlocked:          {stepFailed, stepUpcoming, stepUpcoming, stepUpcoming},
		stageAwaitingApproval: {stepDone, stepDone, stepActive, stepUpcoming},
		stageFinanceApproval:  {stepDone, stepDone, stepActive, stepUpcoming},
		stageManagerApproval:  {stepDone, stepDone, stepActive, stepUpcoming},
		stageReapproval:       {stepDone, stepDone, stepActive, stepUpcoming},
		stageWaitingEffective: {stepDone, stepDone, stepDone, stepActive},
		stageRevalidation:     {stepDone, stepActive, stepDone, stepUpcoming},
		stageExecuted:         {stepDone, stepDone, stepDone, stepActive},
		stageObservingEffects: {stepDone, stepDone, stepDone, stepActive},
		stageCompleted:        {stepDone, stepDone, stepDone, stepDone},
		stageRecorded:         {stepDone, stepDone, stepDone, stepDone},
		stageRejected:         {stepDone, stepDone, stepFailed, stepUpcoming},
		stageFailed:           {stepDone, stepFailed, stepUpcoming, stepUpcoming},
		stageRepairRequired:   {stepDone, stepFailed, stepUpcoming, stepUpcoming},
	}
)

// steps builds the stepper. The times come from the engine's own timeline
// rather than from a second reading of the same rows: whichever event kind
// marks a step is where that step's timestamp comes from, and a step with no
// such event carries no time rather than a guess.
func steps(stage string, events []*journeyv1.TimelineEvent) []journey.Step {
	states, ok := stepStates[stage]
	if !ok {
		states = [4]string{stepUpcoming, stepUpcoming, stepUpcoming, stepUpcoming}
	}
	at := stepTimes(events)
	out := make([]journey.Step, 0, len(stepIDs))
	for i := range stepIDs {
		out = append(out, journey.Step{
			ID:     stepIDs[i],
			Label:  stepLabels[i],
			Detail: stepDescription(i, states[i]),
			State:  states[i],
			At:     at[i],
		})
	}
	return out
}

func stepDescription(index int, state string) string {
	if index < 0 || index >= len(stepDoneDetail) {
		return ""
	}
	if state == stepDone {
		return stepDoneDetail[index]
	}
	active := [4]string{
		"Review the simulation findings and correct any blocked proposal details.",
		"Ready to review and start the approval workflow.",
		"Waiting for the assigned approver's decision.",
		"Waiting for the effective date and final checks before recording.",
	}
	upcoming := [4]string{
		"Submit the proposed change for policy checks.",
		"An authorized reviewer can start the workflow.",
		"The required approvers will receive a review request.",
		"The promotion will be recorded after approvals and final checks.",
	}
	failed := [4]string{
		"The proposal cannot advance until the simulation findings are resolved.",
		"Execution stopped before an approval could be completed.",
		"The proposal was rejected; no promotion fact was recorded.",
		"The terminal fact could not be recorded and needs attention.",
	}
	switch state {
	case stepActive:
		return active[index]
	case stepFailed:
		return failed[index]
	default:
		return upcoming[index]
	}
}

// stepTimes maps timeline kinds onto the four steps: the intent's creation,
// the instance start, the last work item transition (a step is "reached"
// when the latest thing that happened to it happened), and the ledger write.
func stepTimes(events []*journeyv1.TimelineEvent) [4]string {
	var at [4]string
	for _, e := range events {
		if e == nil {
			continue
		}
		when := formatTime(e.GetAt())
		if when == "" {
			continue
		}
		switch e.GetKind() {
		case eventIntentCreated:
			at[0] = when
		case eventInstanceStarted:
			at[1] = when
		case eventWorkItem:
			at[2] = when
		case eventLedgerRecorded:
			at[3] = when
		}
	}
	return at
}

// proposalFacts is the request as the engine recorded it.
func proposalFacts(detail *journeyv1.JourneyDetail) []journey.Fact {
	j := detail.GetJourney()
	facts := []journey.Fact{}
	if v := j.GetBusinessReason(); v != "" {
		facts = append(facts, journey.Fact{Label: "Business reason", Value: v})
	}
	if v := j.GetWorkerRef(); v != "" {
		facts = append(facts, journey.Fact{Label: "Worker", Value: v, Mono: true})
	}
	if v := j.GetIntentId(); v != "" {
		facts = append(facts, journey.Fact{Label: "Intent", Value: v, Mono: true})
	}
	if v := j.GetCorrelationId(); v != "" {
		facts = append(facts, journey.Fact{Label: "Correlation", Value: v, Mono: true})
	}
	if v := j.GetProposalRevisionId(); v != "" {
		facts = append(facts, journey.Fact{Label: "Proposal revision", Value: v, Mono: true})
	}
	if v := j.GetMaterialDigest(); v != "" {
		facts = append(facts, journey.Fact{Label: "Material digest", Value: v, Mono: true})
	}
	for _, w := range detail.GetPlannedWrites() {
		facts = append(facts, journey.Fact{Label: "Planned write", Value: w, Mono: true})
	}
	if len(facts) == 0 {
		return nil
	}
	return facts
}

// comparison is the before/after table.
func comparison(j *journeyv1.Journey) []journey.ComparisonRow {
	if j == nil {
		return nil
	}
	cur, tgt := j.GetCurrent(), j.GetTarget()
	rows := []journey.ComparisonRow{
		row("Job code", cur.GetJobCode(), tgt.GetJobCode()),
		row("Grade", cur.GetGrade(), tgt.GetGrade()),
		row("Position", cur.GetPositionId(), tgt.GetPositionId()),
		row("Org unit", cur.GetOrgUnit(), tgt.GetOrgUnit()),
	}

	pay := journey.ComparisonRow{
		Label:    "Base pay",
		Current:  orDash(formatAmount(j.GetCurrency(), j.GetCurrentBase())),
		Proposed: orDash(formatAmount(j.GetCurrency(), j.GetProposedBase())),
	}
	if delta, ok := amountDelta(j.GetCurrency(), j.GetCurrentBase(), j.GetProposedBase()); ok {
		pay.Delta = delta
		pay.Changed = true
		if pct, okPct := percentDelta(j.GetCurrentBase(), j.GetProposedBase()); okPct {
			pay.Delta = delta + " (" + pct + ")"
		}
	}
	rows = append(rows, pay)

	effective := formatDate(j.GetEffectiveDate())
	rows = append(rows, journey.ComparisonRow{
		Label:    "Effective date",
		Current:  emDash,
		Proposed: orDash(effective),
		Changed:  effective != "",
	})
	return rows
}

func row(label, current, proposed string) journey.ComparisonRow {
	return journey.ComparisonRow{
		Label:    label,
		Current:  orDash(current),
		Proposed: orDash(proposed),
		Changed:  current != proposed && proposed != "",
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return emDash
	}
	return s
}

// findings normalises the engine's severities onto the renderer's four.
//
// PROMOUX-014's six WAIT_* codes are excluded: they are not a simulation
// check (this section's own heading is "Preflight and simulation") and
// [waitExplanationFacts] projects them onto their own, correctly labelled
// section instead.
func findings(in []*journeyv1.Finding) []journey.Finding {
	if len(in) == 0 {
		return nil
	}
	out := make([]journey.Finding, 0, len(in))
	for _, f := range in {
		if f == nil || isWaitExplanationCode(f.GetCode()) {
			continue
		}
		out = append(out, journey.Finding{
			Severity: severityOf(f.GetSeverity()),
			Code:     f.GetCode(),
			Message:  f.GetMessage(),
		})
	}
	return out
}

// PROMOUX-014: the wire codes internal/intent/app's journeyWaitFindings
// mints for a WAITING_EFFECTIVE_DATE journey (mirrored here, not imported,
// for the same reason every other wire vocabulary in this file is
// restated -- definitions/architecture/dependency-roles.yaml keeps this
// module out of internal/).
const (
	codeWaitEffectiveInstant = "WAIT_EFFECTIVE_INSTANT"
	codeWaitOwner            = "WAIT_OWNER"
	codeWaitScheduledAction  = "WAIT_SCHEDULED_ACTION"
	codeWaitRemainingChecks  = "WAIT_REMAINING_CHECKS"
	codeWaitNotification     = "WAIT_NOTIFICATION"
	codeWaitIntervention     = "WAIT_INTERVENTION"
)

// waitExplanationLabels orders and labels the six PROMOUX-014 codes for
// display. The order is the reading order a person wants: when, who signed
// off, what happens, what is left, whether they will hear about it, and
// what they are allowed to do about it.
var waitExplanationLabels = []struct{ code, label string }{
	{codeWaitEffectiveInstant, "Effective instant"},
	{codeWaitOwner, "Owner"},
	{codeWaitScheduledAction, "Scheduled action"},
	{codeWaitRemainingChecks, "Remaining checks"},
	{codeWaitNotification, "Notification"},
	{codeWaitIntervention, "Authorized intervention"},
}

func isWaitExplanationCode(code string) bool {
	for _, row := range waitExplanationLabels {
		if row.code == code {
			return true
		}
	}
	return false
}

// waitExplanationFacts projects PROMOUX-014's WAIT_* findings onto the
// labelled facts list the "Waiting for effective date" subsection renders.
// It is nil unless the engine actually sent every one of these codes: a
// partial explanation would read as the missing facts having been decided
// to be unimportant, when the true reason is that this journey is not
// (or no longer) parked on the wait at all.
func waitExplanationFacts(in []*journeyv1.Finding) []journey.Fact {
	byCode := make(map[string]string, len(in))
	for _, f := range in {
		if f != nil {
			byCode[f.GetCode()] = f.GetMessage()
		}
	}
	facts := make([]journey.Fact, 0, len(waitExplanationLabels))
	for _, row := range waitExplanationLabels {
		msg, ok := byCode[row.code]
		if !ok {
			return nil
		}
		facts = append(facts, journey.Fact{Label: row.label, Value: msg})
	}
	return facts
}

// severityOf maps every severity word the engine and its simulators use onto
// the four the renderer draws. An unrecognised one is informational rather
// than blocking: over-stating a finding's severity would misreport the
// engine's answer in the direction that stops work.
func severityOf(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "blocking", "block", "error", "fatal", "critical":
		return severityBlocking
	case "warning", "warn":
		return severityWarning
	case "success", "ok", "pass", "passed":
		return severitySuccess
	default:
		return severityInfo
	}
}

// engineFacts is the workflow instance. It is nil before execution, so the
// section is absent rather than a table of dashes.
func engineFacts(inst *journeyv1.Instance) []journey.Fact {
	if inst == nil {
		return nil
	}
	facts := []journey.Fact{
		{Label: "Instance", Value: inst.GetInstanceId(), Mono: true},
		{Label: "Version", Value: strconv.FormatInt(inst.GetInstanceVersion(), 10), Mono: true},
		{Label: "Workflow", Value: workflowRef(inst), Mono: true},
		{Label: "Plan digest", Value: inst.GetPlanDigest(), Mono: true},
		{Label: "Status", Value: inst.GetStatus(), Tone: statusTone(inst.GetStatus())},
	}
	if nodes := inst.GetCurrentNodeIds(); len(nodes) > 0 {
		facts = append(facts, journey.Fact{Label: "Current node", Value: strings.Join(nodes, ", "), Mono: true})
	}
	if v := inst.GetCorrelationId(); v != "" {
		facts = append(facts, journey.Fact{Label: "Correlation", Value: v, Mono: true})
	}
	return facts
}

func workflowRef(inst *journeyv1.Instance) string {
	id := inst.GetWorkflowId()
	if id == "" {
		return ""
	}
	return id + "@" + strconv.FormatUint(uint64(inst.GetWorkflowVersion()), 10)
}

// statusTone reads a workflow, node or work item status word. The
// vocabularies overlap by design (all three are the engine's own status
// strings), so one function reads all three.
func statusTone(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "COMPLETED", "APPROVED", "SUCCEEDED", "DONE", "RECORDED":
		return toneSuccess
	case "RUNNING", "OPEN", "READY", "CLAIMED", "IN_PROGRESS":
		return toneWarning
	case "FAILED", "REJECTED", "CANCELLED", "CANCELED", "EXPIRED", "REFUSED", "TIMED_OUT":
		return toneDanger
	case "PENDING", "":
		return toneNeutral
	default:
		return toneInfo
	}
}

func nodes(in []*journeyv1.NodeExecution) []journey.NodeRow {
	if len(in) == 0 {
		return nil
	}
	out := make([]journey.NodeRow, 0, len(in))
	for _, n := range in {
		if n == nil {
			continue
		}
		out = append(out, journey.NodeRow{
			NodeID:    n.GetNodeId(),
			StepType:  n.GetStepType(),
			Status:    n.GetStatus(),
			Attempt:   strconv.FormatInt(int64(n.GetAttempt()), 10),
			Started:   formatTimeOr(n.GetStartedAt(), emDash),
			Completed: formatTimeOr(n.GetCompletedAt(), emDash),
			Tone:      statusTone(n.GetStatus()),
		})
	}
	return out
}

func workItems(in []*journeyv1.WorkItem) []journey.WorkItemCard {
	if len(in) == 0 {
		return nil
	}
	out := make([]journey.WorkItemCard, 0, len(in))
	for _, w := range in {
		if w == nil {
			continue
		}
		out = append(out, journey.WorkItemCard{
			ID:        w.GetWorkItemId(),
			Kind:      w.GetKind(),
			Status:    w.GetStatus(),
			Owner:     owner(w),
			NodeID:    w.GetNodeId(),
			Deadline:  formatTimeOr(w.GetDeadlineAt(), emDash),
			Claimed:   claimed(w),
			Completed: completed(w),
			Tone:      statusTone(w.GetStatus()),
		})
	}
	return out
}

// owner prefers the routed owner the engine chose over the declared owner
// reference, because the chosen owner is who will actually decide.
func owner(w *journeyv1.WorkItem) string {
	if v := w.GetChosenOwner(); v != "" {
		return v
	}
	return w.GetOwnerRef()
}

func claimed(w *journeyv1.WorkItem) string {
	at := formatTime(w.GetClaimedAt())
	by := w.GetClaimedBy()
	return whoWhen(by, at)
}

func completed(w *journeyv1.WorkItem) string {
	at := formatTime(w.GetCompletedAt())
	by := w.GetCompletedBy()
	return whoWhen(by, at)
}

// whoWhen renders "somebody, at some time", degrading to whichever half is
// known and to "" when neither is.
func whoWhen(who, when string) string {
	switch {
	case who == "" && when == "":
		return ""
	case who == "":
		return when
	case when == "":
		return who
	}
	return who + ", " + when
}

func ledger(l *journeyv1.LedgerEvent, proposalEffectiveDate string) *journey.LedgerCard {
	if l == nil {
		return nil
	}
	effectiveAt := formatDateOf(l.GetEffectiveAt())
	if effectiveAt == "" {
		effectiveAt = formatDate(proposalEffectiveDate)
	}
	return &journey.LedgerCard{
		StreamKey:      l.GetStreamKey(),
		Sequence:       strconv.FormatInt(l.GetSequence(), 10),
		SchemaRef:      l.GetSchemaRef(),
		Digest:         l.GetDigest(),
		IdempotencyKey: l.GetIdempotencyKey(),
		RecordedAt:     formatTime(l.GetRecordedAt()),
		EffectiveAt:    effectiveAt,
	}
}

// timeline renders the engine's chronology newest first. The engine composes
// it oldest first (causal order, which is how it reasons); a reader opening
// the page wants the last thing that happened at the top, which is how the
// reference page reads.
func timeline(in []*journeyv1.TimelineEvent) []journey.TimelineEvent {
	if len(in) == 0 {
		return nil
	}
	out := make([]journey.TimelineEvent, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		e := in[i]
		if e == nil {
			continue
		}
		out = append(out, journey.TimelineEvent{
			At:     formatTime(e.GetAt()),
			Actor:  e.GetActor(),
			Title:  e.GetTitle(),
			Detail: e.GetDetail(),
			Ref:    e.GetRef(),
			Tone:   eventTone(e),
		})
	}
	return out
}

// eventTone colours one chronology entry.
//
// The ledger write is the one thing on this page that is finished and
// irreversible, so it is the one success. A work item or node that ended
// badly is a refusal the reader must not scroll past, so it is danger. Every
// other entry is a step in a running process: informational, or neutral for
// the two that merely record that a proposal exists.
func eventTone(e *journeyv1.TimelineEvent) string {
	switch e.GetKind() {
	case eventLedgerRecorded:
		return toneSuccess
	case eventIntentCreated, eventSimulated:
		return toneNeutral
	case eventWorkItem:
		// The engine titles a work item entry with the status it moved to
		// and details it with the reason.
		switch tone := statusTone(e.GetTitle()); tone {
		case toneSuccess, toneDanger:
			return tone
		}
		return toneInfo
	case eventNode:
		if statusTone(e.GetDetail()) == toneDanger {
			return toneDanger
		}
		return toneInfo
	default:
		return toneInfo
	}
}

// effectiveWindow places the effective date against when the proposal was
// made and when this page's answers were read.
//
// It is the one gauge this projection can fill honestly today: Start is the
// day the intent was created, EffectiveDate the day the change takes effect,
// and KnownAt the instant the engine last updated the record -- which is the
// as-known-at time every figure on the page was read at.
func effectiveWindow(j *journeyv1.Journey) *journey.EffectiveWindow {
	if j == nil {
		return nil
	}
	effective := formatDate(j.GetEffectiveDate())
	start := formatDateOf(j.GetCreatedAt())
	knownAt := formatTime(j.GetUpdatedAt())
	if effective == "" && start == "" {
		return nil
	}
	return &journey.EffectiveWindow{
		Start:         start,
		EffectiveDate: effective,
		KnownAt:       knownAt,
		Note:          "Every figure above was read as known at that instant; a later correction to the same period would not change this page retroactively.",
	}
}

// actions is what the signed-in person can do now.
//
// The stage decides, and the engine decides again: an action offered here is
// still authorized server-side, and one refused there becomes a notice. What
// this projection must not do is offer a decision on a journey that has none
// (a completed journey's approval work item is closed) or hide the reason an
// action is unavailable.
func actions(head journey.JourneyCard, approver string, workItems []*journeyv1.WorkItem, withdrawPreview, cancelPreview *journeyv1.PreviewJourneyInterventionResponse) []journey.Action {
	href := DetailHref(head.IntentID)
	confirm := actionConfirmation(head)
	var out []journey.Action
	switch head.Stage {
	case stageProposed:
		out = []journey.Action{{
			ID: ActionExecute, Label: "Start approval workflow", Variant: "primary",
			Description:      "Review the proposal, then start its approval workflow. Required approvals and final checks still apply; starting does not record the promotion.",
			Action:           href,
			Hidden:           map[string]string{},
			Confirmation:     confirm,
			ConfirmationNote: "Starting sends the proposal through its required approvals. It does not record the promotion yet.",
		}}
	case stageBlocked:
		out = []journey.Action{{
			ID: ActionExecute, Label: "Start approval workflow", Variant: "primary",
			Description: "Start the approval workflow after the proposal passes its required checks.",
			Action:      href,
			Hidden:      map[string]string{},
			Disabled:    true,
			DisabledReason: "The simulation produced no executable plan, so the execution authority would refuse this " +
				"with p1b.no_executable_plan. Re-propose with the findings above addressed.",
		}}
	case stageAwaitingApproval, stageFinanceApproval, stageManagerApproval, stageReapproval:
		if !hasActionableApproval(workItems) {
			out = []journey.Action{{
				ID: ActionApprove, Label: "Preparing approval", Variant: "primary",
				Description: "The workflow is creating and routing the durable approval work item.",
				Action:      href, Hidden: map[string]string{}, Disabled: true,
				DisabledReason: "This page will enable the decision as soon as the assigned work item is durable and actionable.",
			}}
			break
		}
		out = []journey.Action{
			{
				ID: ActionApprove, Label: "Approve", Variant: "primary",
				Description:      "Claims and completes the current approval work item and resumes the workflow. Another approval or the effective-date wait may follow.",
				Action:           href,
				Hidden:           map[string]string{},
				ActsAs:           approver,
				ActsAsLabel:      approverLabel(head.Stage, approver),
				Confirmation:     confirm,
				ConfirmationNote: "Approval resumes the workflow. The promotion is recorded only after every required approval and effective-date gate completes. Review the worker, date, and compensation before confirming.",
				Fields: []journey.Field{{
					ID: FieldApproveReason, Name: NameDecisionReason, Label: "Reason for the record",
					Kind: kindTextarea, Placeholder: "What made this the right call?",
					Help: "Optional. Kept on the work item transition and carried into the ledger event's evidence.",
				}},
			},
			{
				ID: ActionReject, Label: "Reject", Variant: "danger",
				Description:      "Rejects the proposal. The instance ends at its REJECTED terminal and no promotion fact is written.",
				Action:           href,
				Hidden:           map[string]string{},
				ActsAs:           approver,
				ActsAsLabel:      approverLabel(head.Stage, approver),
				Confirmation:     confirm,
				ConfirmationNote: "Rejection ends this workflow without recording a promotion fact. The manager will see the reason verbatim.",
				Fields: []journey.Field{{
					ID: FieldRejectReason, Name: NameDecisionReason, Label: "What needs to change",
					Kind: kindTextarea, Required: true,
					Help: "Required. The manager sees this verbatim.",
				}},
			},
		}
	default:
		// COMPLETED, REJECTED, FAILED, RECORDED and anything unrecognised
		// are terminal for the ordinary lifecycle actions: nothing above is
		// offered. Withdraw/Cancel/EditProposal below still render, as
		// Disabled actions naming the terminal reason -- GREEN's own
		// "unavailable stages explain why", not merely "actions disappear".
	}
	if head.IntentID == "" {
		// No journey has actually loaded (the route names one whose answer
		// has not arrived, or arrived as a refusal): there is nothing to
		// withdraw, cancel or edit yet, and offering those actions -- even
		// as disabled ones -- over an identity this page does not hold
		// would be inventing a fact.
		return out
	}
	return interventionActions(out, head, withdrawPreview, cancelPreview)
}

// PROMOUX-013's own reason references (internal/intent/app/journey_
// intervention.go), reproduced here as the stable keys the mapping below
// switches on. This package cannot import that kernel package (it is
// compiled into the browser/wasm client), so the two are kept in agreement
// by inspection rather than by sharing code across that boundary; a value
// this package does not recognize renders as the generic fallback rather
// than panicking or guessing.
const (
	reasonAlreadyTerminal  = "journey.intervention.unavailable.already_terminal"
	reasonAlreadyStarted   = "journey.intervention.unavailable.already_started"
	reasonNotYetStarted    = "journey.intervention.unavailable.not_yet_started"
	reasonAlreadyCommitted = "journey.intervention.unavailable.already_committed"
)

// InterventionAvailability mirrors internal/intent/app's own
// interventionUnavailableAtStage: a pure function of the stage this page was
// already authorized to read (InspectJourney's own authorization boundary
// covers this projection too, so computing this client-side discloses
// nothing a viewer was not already entitled to see), returning the exact
// same reason reference the server's PreviewJourneyIntervention would.
// Every non-eligible stage maps to exactly one reference, so the same stage
// always produces the same text -- the disclosure-by-value property
// PROMOUX-001/PROMOUX-004 established: presence alone leaks nothing, because
// there is nothing conditional on who is asking.
//
// It is exported, and kind/stage are plain strings rather than this
// package's own typed constants, for exactly one reason:
// internal/intent/app's own test suite imports this function to prove the
// two implementations agree over the full cross product of every wire
// JourneyStage and JourneyInterventionKind
// (TestTodo_PROMOUX_013_ClientServerAvailabilityAgreement). Nothing in this
// package's own production code calls it any differently than the
// unexported form did.
func InterventionAvailability(kind string, stage string) (reasonRef string, available bool) {
	terminal := stage == stageCompleted || stage == stageRejected || stage == stageFailed || stage == stageRecorded
	unstarted := stage == stageProposed || stage == stageBlocked
	committed := stage == stageExecuted || stage == stageObservingEffects
	if terminal {
		return reasonAlreadyTerminal, false
	}
	switch kind {
	case ActionWithdraw:
		if !unstarted {
			return reasonAlreadyStarted, false
		}
	case ActionCancel:
		if unstarted {
			return reasonNotYetStarted, false
		}
		if committed {
			return reasonAlreadyCommitted, false
		}
	}
	return "", true
}

// interventionReasonText is the one place a reason reference becomes prose.
// It is total (every reference this package can produce has an entry) and
// pure (the same reference always renders the same sentence), which is what
// makes the disclosure-by-value property checkable: two differently-staged
// journeys that happen to share a reference render byte-identical text.
func interventionReasonText(ref string) string {
	switch ref {
	case reasonAlreadyTerminal:
		return "This proposal has already reached a terminal outcome. There is nothing left to withdraw, cancel or edit."
	case reasonAlreadyStarted:
		return "This proposal has already started its approval workflow. Use Cancel instead of Withdraw."
	case reasonNotYetStarted:
		return "This proposal has not started its approval workflow yet. Use Withdraw instead of Cancel."
	case reasonAlreadyCommitted:
		return "The governed execution has already run. This can no longer be cancelled or edited."
	default:
		return "This action is not available for this proposal right now."
	}
}

// interventionActions appends PROMOUX-013's Withdraw, Cancel and EditProposal
// actions to base, in that order, for every stage they are legally offered
// at -- always offered as a Disabled action naming why when the current
// stage is not eligible, per GREEN's "unavailable stages explain why"
// clause, never simply omitted (an omitted action and a denied one must not
// be told apart by a viewer who is authorized to see this page at all).
// consequence, when non-nil, supplies the server's own worded preview
// (PreviewJourneyIntervention); its absence (a preview call that has not
// yet answered, or failed) still allows the review surface to open with the
// same reused reviewSurface component and a shorter fact list, because the
// facts required to identify what will be stopped -- the employee, the
// change, the effective date -- never depended on that call succeeding.
func interventionActions(base []journey.Action, head journey.JourneyCard, withdraw, cancel *journeyv1.PreviewJourneyInterventionResponse) []journey.Action {
	href := DetailHref(head.IntentID)
	facts := actionConfirmation(head)

	appendOne := func(kind, actionID, label, variant, description string, preview *journeyv1.PreviewJourneyInterventionResponse, reasonField, reasonName string, extraFields []journey.Field) {
		reasonRef, available := InterventionAvailability(kind, head.Stage)
		if !available {
			base = append(base, journey.Action{
				ID: actionID, Label: label, Variant: variant, Description: description,
				Action: href, Hidden: map[string]string{}, Disabled: true,
				DisabledReason: interventionReasonText(reasonRef),
			})
			return
		}
		note := ""
		if preview != nil {
			note = preview.GetConsequenceSummary()
		}
		if note == "" {
			note = description
		}
		fields := append([]journey.Field{{
			ID: reasonField, Name: reasonName, Label: "Reason", Kind: kindTextarea, Required: true,
			Placeholder: "Why is this being done?",
			Help:        "Required. Retained as evidence on the governed record.",
		}}, extraFields...)
		base = append(base, journey.Action{
			ID: actionID, Label: label, Variant: variant, Description: description,
			Action: href, Hidden: map[string]string{}, Fields: fields,
			Confirmation:     facts,
			ConfirmationNote: note,
		})
	}

	appendOne(ActionWithdraw, ActionWithdraw, "Withdraw", "secondary",
		"Stops this proposal before any approval has been recorded. No business effect has occurred.",
		withdraw, FieldWithdrawReason, NameInterventionReason, nil)
	appendOne(ActionCancel, ActionCancel, "Request cancellation", "secondary",
		"Asks the engine to stop at its next safe point. If the safe point has already passed, the promotion completes instead.",
		cancel, FieldCancelReason, NameInterventionReason, nil)

	// EditProposal has no PreviewJourneyIntervention kind of its own (it is
	// Cancel-then-repropose): it is available whenever either WITHDRAW or
	// CANCEL is, and its refusal reason is whichever of the two actually
	// applies -- "already terminal" is checked first because it is the
	// strongest, most specific fact when it holds.
	withdrawRef, withdrawOK := InterventionAvailability(ActionWithdraw, head.Stage)
	cancelRef, cancelOK := InterventionAvailability(ActionCancel, head.Stage)
	editAvailable := withdrawOK || cancelOK
	if !editAvailable {
		editRef := withdrawRef
		if editRef == "" || editRef != reasonAlreadyTerminal && cancelRef == reasonAlreadyTerminal {
			editRef = cancelRef
		}
		base = append(base, journey.Action{
			ID: ActionEditProposal, Label: "Edit proposal", Variant: "secondary",
			Description: "Corrects the target role, base pay, effective date or business reason.",
			Action:      href, Hidden: map[string]string{}, Disabled: true,
			DisabledReason: interventionReasonText(editRef),
		})
	} else {
		base = append(base, journey.Action{
			ID: ActionEditProposal, Label: "Edit proposal", Variant: "secondary",
			Description: "Corrects the target role, base pay, effective date or business reason. " +
				"This cancels the current proposal and creates a corrected successor; any recorded approval no longer applies.",
			Action: href, Hidden: map[string]string{},
			Fields: []journey.Field{
				{ID: FieldEditJobCode, Name: NameEditJobCode, Label: "Target job code", Kind: kindText, Value: head.Headline, Required: true},
				{ID: FieldEditGrade, Name: NameEditGrade, Label: "Target grade", Kind: kindText, Required: true},
				{ID: FieldEditBase, Name: NameEditBase, Label: "Proposed base pay", Kind: kindText, Value: head.PayLine, Required: true},
				{ID: FieldEditEffective, Name: NameEditEffective, Label: "Effective date", Kind: kindDate, Value: head.EffectiveDate, Required: true},
				{ID: FieldEditBusinessReason, Name: NameEditBusinessReason, Label: "Business reason", Kind: kindTextarea, Required: true},
				{ID: FieldEditReason, Name: NameEditReason, Label: "Reason for this edit", Kind: kindTextarea, Required: true,
					Help: "Required. Retained as evidence on the governed record."},
			},
			Confirmation:     facts,
			ConfirmationNote: "Editing cancels this proposal and creates a corrected successor. Any recorded approval is left with the original and does not carry over.",
		})
	}
	return base
}

func actionConfirmation(head journey.JourneyCard) []journey.Fact {
	facts := []journey.Fact{{Label: "Employee", Value: nonEmpty(head.WorkerName, "Employee")}}
	if head.Headline != "" {
		facts = append(facts, journey.Fact{Label: "Placement", Value: head.Headline})
	}
	if head.PayLine != "" {
		facts = append(facts, journey.Fact{Label: "Base pay", Value: head.PayLine})
	}
	if head.EffectiveDate != "" {
		facts = append(facts, journey.Fact{Label: "Effective date", Value: head.EffectiveDate})
	}
	return facts
}

// proposalConfirmation builds the Employee/Placement/Base pay/Effective
// date facts the shared review surface (tools/uxqual/render/journey's
// reviewSurface) shows for Start, the same four the fields it already
// carries and the same labels actionConfirmation gives Approve and Reject.
// It cannot take a journey.JourneyCard the way actionConfirmation does --
// this form is what creates one, so no journey exists yet -- so it reads
// the worker record and the fields as currently filled instead, through
// the same headline/payLine-shaped helpers (headline, formatAmount,
// formatDate, workerName, workerCurrency) card and promotionSubject
// already use elsewhere in this file. A field the reader has not reached
// yet (no job code chosen, no worker resolved) simply omits that fact
// rather than printing an empty or placeholder value.
func proposalConfirmation(worker *journeyv1.Worker, options *journeyv1.WorkforceOptions, jobCode, grade, base, effective string) []journey.Fact {
	name, currentJob, currentGrade, currency := "Employee", "", "", ""
	if worker != nil {
		name = nonEmpty(workerName(worker), name)
		currentJob, currentGrade = worker.GetJobCode(), worker.GetGrade()
		currency = workerCurrency(worker, options)
	}
	facts := []journey.Fact{{Label: "Employee", Value: name}}
	if placement := headline(currentJob, currentGrade, jobCode, grade); placement != "" {
		facts = append(facts, journey.Fact{Label: "Placement", Value: placement})
	}
	if pay := formatAmount(currency, base); pay != "" {
		facts = append(facts, journey.Fact{Label: "Base pay", Value: pay})
	}
	if date := formatDate(effective); date != "" {
		facts = append(facts, journey.Fact{Label: "Effective date", Value: date})
	}
	return facts
}

func hasActionableApproval(items []*journeyv1.WorkItem) bool {
	for _, item := range items {
		if item == nil || !strings.EqualFold(item.GetKind(), "APPROVAL") {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(item.GetStatus())) {
		case "ASSIGNED", "OPEN", "READY", "ROUTED":
			return true
		}
	}
	return false
}

func approverLabel(stage, principal string) string {
	_ = principal // The principal is displayed separately; stage names the authority being exercised.
	switch stage {
	case stageFinanceApproval:
		return "Finance approver authorization"
	case stageManagerApproval:
		return "Manager approver authorization"
	case stageReapproval:
		return "Reapproval authorization"
	default:
		return "Compensation Approver authorization"
	}
}

func stringOptions(prompt string, values []string, selected string) []journey.Option {
	out := make([]journey.Option, 0, len(values)+1)
	out = append(out, journey.Option{Value: "", Label: prompt, Selected: selected == ""})
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, journey.Option{Value: value, Label: value, Selected: value == selected})
	}
	if selected != "" {
		found := false
		for _, option := range out {
			found = found || option.Value == selected
		}
		if !found && len(values) == 0 {
			out = append(out, journey.Option{Value: selected, Label: selected, Selected: true})
			out[0].Selected = false
		}
	}
	return out
}
