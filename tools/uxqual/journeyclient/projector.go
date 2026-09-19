package journeyclient

import (
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
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
	ActionWithdraw = "withdraw"
	ActionCancel   = "cancel"
	// ActionRepair is UXLIVE-006's governed repair. It is presented, never
	// submitted from here: see repairAction.
	ActionRepair       = "repair"
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
	// kindPositionPicker is UXLIVE-011's governed position choice; the
	// renderer draws it with productui.PositionPicker.
	kindPositionPicker = "positionpicker"

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
	return stageLabelLocale("en-US", stage)
}

func stageLabelLocale(locale, stage string) string {
	if locale == "" || locale == "en-US" {
		label, _ := StagePresentation(journeyv1.JourneyStage(journeyv1.JourneyStage_value[stagePrefix+stage]))
		return label
	}
	copy := productui.ResolveProductLocale(locale)
	switch stage {
	case stageProposed:
		return copy.Text("journey.stage_proposed")
	case stageBlocked:
		return copy.Text("journey.stage_blocked")
	case stageAwaitingApproval:
		return copy.Text("journey.stage_awaiting_approval")
	case stageCompleted:
		return copy.Text("journey.stage_completed")
	case stageRejected:
		return copy.Text("journey.stage_rejected")
	case stageFailed:
		return copy.Text("journey.stage_failed")
	case stageFinanceApproval:
		return copy.Text("journey.stage_finance_approval")
	case stageManagerApproval:
		return copy.Text("journey.stage_manager_approval")
	case stageWaitingEffective:
		return copy.Text("journey.stage_waiting_effective")
	case stageRevalidation:
		return copy.Text("journey.stage_revalidation")
	case stageReapproval:
		return copy.Text("journey.stage_reapproval")
	case stageExecuted:
		return copy.Text("journey.stage_updating_record")
	case stageObservingEffects:
		return copy.Text("journey.stage_updating_record")
	case stageRecorded:
		return copy.Text("journey.stage_recorded")
	case stageRepairRequired:
		return copy.Text("journey.stage_repair_required")
	default:
		return copy.Text("journey.stage_unknown")
	}
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
	case journeyv1.JourneyStage_JOURNEY_STAGE_AWAITING_ACKNOWLEDGEMENT:
		return "Awaiting acknowledgement", toneWarning
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
	copy := productui.ResolveProductLocale(cfg.Locale)
	if notice != nil && (notice.TitleKey != "" || notice.MessageKey != "") {
		localized := *notice
		if notice.TitleKey != "" {
			localized.Title = copy.Text(notice.TitleKey)
		}
		if notice.MessageKey != "" {
			localized.Detail = copy.Text(notice.MessageKey)
		}
		notice = &localized
	}
	return journey.Page{
		Title:       title,
		Locale:      copy.Resolved,
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
				copy.Text("journey.footer"),
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
	// ProposalErrors are already-sanitized messages keyed by proposal field
	// id. The projector only binds them to controls; it never interprets raw
	// server failures.
	ProposalErrors map[string]string
}

// ListPage projects the journeys overview: the workforce, the journeys and
// the two forms that add to either.
func ListPage(cfg Config, data ListData, notice *journey.Notice, values map[string]string) journey.Page {
	copy := productui.ResolveProductLocale(cfg.Locale)
	p := chrome(cfg, copy.Text("journey.list_title")+" · "+Brand, notice, values, false)
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
	applyProposalErrors(&form, data.ProposalErrors)
	if len(cfg.PagePermissions) > 0 && !cfg.CanPageAction("journeys", "create") {
		form.Disabled = true
		form.DisabledReason = copy.Text("journey.form_no_access")
	}
	applyProposalCurrency(&form, workerCurrency(findWorker(data.Workers, data.SelectedRef), data.Options))
	localizeProposalForm(&form, copy, false, nil, nil)
	p.List = &journey.ListView{
		Journeys: cards,
		Groups:   journeySubjectGroups(cards),
		Empty:    copy.Text("journey.list_empty"),
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
	copy := productui.ResolveProductLocale(cfg.Locale)
	worker := findWorker(data.Workers, data.SelectedRef)
	title := copy.Text("journey.list_title") + " · " + Brand
	if worker != nil && workerName(worker) != "" {
		title = copy.Text("journey.promote_person", map[string]string{"name": workerName(worker)}) + " · " + Brand
	}
	p := chrome(cfg, title, notice, values, true)
	form := focusedProposalForm(values, data.SelectedRef, data.Options, worker)
	applyProposalErrors(&form, data.ProposalErrors)
	if len(cfg.PagePermissions) > 0 && !cfg.CanPageAction("journeys", "create") {
		form.Disabled = true
		form.DisabledReason = copy.Text("journey.form_no_access")
	}
	applyProposalCurrency(&form, workerCurrency(worker, data.Options))
	loading := worker == nil && notice != nil && notice.Busy
	if worker == nil && !loading {
		form.Disabled = true
		form.DisabledReason = copy.Text("journey.form_no_employee")
	}
	if jobs, _ := governedProposalChoices(data.Options, worker, ""); worker != nil && (len(cfg.PagePermissions) == 0 || cfg.CanPageAction("journeys", "create")) && len(jobs) == 0 {
		form.Disabled = true
		form.DisabledReason = copy.Text("journey.form_no_path")
	}
	path := selectedPromotionPath(data.Options, worker, values[FieldJobCode], values[FieldGrade])
	localizeProposalForm(&form, copy, true, path, proposalPayRangeFor(worker, data.Options, path))
	p.Proposal = &journey.ProposalView{
		Subject:         promotionSubject(worker, data.Options, copy),
		Form:            form,
		Loading:         loading,
		BackHref:        personHref(data.SelectedRef),
		JourneysLink:    journey.NavLink{Label: copy.Text("journey.all_link"), Href: ListHref()},
		EngineAvailable: true,
	}
	return p
}

// positionField builds UXLIVE-011's governed target-position control.
//
// The list is narrowed to the role being proposed when one has been chosen,
// and is the whole authorized set when none has. Narrowing here is
// presentation only: every option in it was already proved authorized, real
// and open by the cell, so hiding some of them can refuse a choice but can
// never admit one. When the chosen role has no open position the picker
// renders empty and says why -- which is the answer, not a prompt to type
// something.
//
// The selected value is carried as the server-issued reference and nothing
// else. A stored value that is no longer on offer is dropped rather than
// resubmitted: the position it named may have been filled since, and the
// proposal would be refused on exactly that ground.
func positionField(selected string, options *journeyv1.WorkforceOptions, jobCode string) journey.Field {
	vacancies := make([]journey.VacancyOption, 0, len(options.GetPositionVacancies()))
	offered := map[string]bool{}
	for _, vacancy := range options.GetPositionVacancies() {
		if reference := strings.TrimSpace(vacancy.GetReference()); reference == "" {
			continue
		}
		if jobCode != "" && vacancy.GetJobCode() != "" && vacancy.GetJobCode() != jobCode {
			continue
		}
		offered[vacancy.GetReference()] = true
		vacancies = append(vacancies, journey.VacancyOption{
			Reference:        vacancy.GetReference(),
			Title:            vacancy.GetTitle(),
			Organization:     vacancy.GetOrganization(),
			Manager:          vacancy.GetManager(),
			Location:         vacancy.GetLocation(),
			JobCode:          vacancy.GetJobCode(),
			OrgUnit:          vacancy.GetOrgUnit(),
			VacancyEndISO:    vacancy.GetVacancyEnd(),
			ReservationState: vacancy.GetReservationState(),
		})
	}
	if !offered[selected] {
		selected = ""
	}
	return journey.Field{
		ID: FieldPosition, Name: NamePosition, Kind: kindPositionPicker,
		Value: selected, Vacancies: vacancies,
	}
}

func localizeProposalForm(form *journey.ProposalForm, copy productui.LocaleContext, focused bool, path *journeyv1.PromotionPathOption, payRange *proposalPayRange) {
	if form == nil {
		return
	}
	form.Submit = copy.Text("journey.form_submit")
	for i := range form.Fields {
		field := &form.Fields[i]
		switch field.ID {
		case FieldWorker:
			field.Label, field.Help = copy.Text("journey.form_worker"), copy.Text("journey.form_worker_help")
			if len(field.Options) > 0 {
				field.Options[0].Label = copy.Text("journey.form_worker_option")
			}
		case FieldJobCode:
			field.Label, field.Help = copy.Text("journey.form_next_role"), copy.Text("journey.form_next_role_help")
			if len(field.Options) > 0 {
				field.Options[0].Label = copy.Text("journey.form_next_role_option")
			}
		case FieldGrade:
			field.Label, field.Help = copy.Text("journey.form_grade"), copy.Text("journey.form_grade_help")
			if len(field.Options) > 0 {
				field.Options[0].Label = copy.Text("journey.form_grade_option")
			}
		case FieldPosition:
			field.Label, field.Help = copy.Text("journey.form_position"), copy.Text("journey.form_position_help")
			field.EmptyTitle, field.EmptyDetail = copy.Text("journey.form_position_none"), copy.Text("journey.form_position_none_help")
		case FieldBase:
			field.Label, field.Help = copy.Text("journey.form_base"), copy.Text("journey.form_base_help")
			field.Suffix = copy.Text("journey.form_base_year")
			if focused {
				field.Help = copy.Text("journey.form_choose_role_rule")
				if path != nil {
					field.Help = promotionPathRuleHelpLocale(path, copy)
					if bounds := payRange.localizedBounds(copy); bounds != nil {
						field.Help += " " + copy.Text("journey.form_base_amounts", bounds)
					}
				}
			}
		case FieldEffective:
			field.Label, field.Help = copy.Text("journey.form_effective"), copy.Text("journey.form_effective_help")
		case FieldReason:
			field.Label, field.Help = copy.Text("journey.form_reason"), copy.Text("journey.form_reason_help")
			field.Placeholder = copy.Text("journey.form_reason_placeholder")
		}
	}
}

func applyProposalErrors(form *journey.ProposalForm, fieldErrors map[string]string) {
	if form == nil || len(fieldErrors) == 0 {
		return
	}
	for i := range form.Fields {
		if message := strings.TrimSpace(fieldErrors[form.Fields[i].ID]); message != "" {
			form.Fields[i].Error = message
		}
	}
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

func promotionSubject(worker *journeyv1.Worker, options *journeyv1.WorkforceOptions, copy productui.LocaleContext) *journey.PromotionSubject {
	if worker == nil {
		return nil
	}
	return &journey.PromotionSubject{
		Ref:      worker.GetWorkerRef(),
		Name:     workerName(worker),
		PhotoURL: worker.GetProfilePhotoUrl(),
		Number:   worker.GetWorkerNumber(),
		Title:    nonEmpty(worker.GetJobTitle(), JobTitle(worker.GetJobCode())),
		JobCode:  worker.GetJobCode(),
		Grade:    worker.GetGrade(),
		OrgUnit:  productui.DisplayLabel(worker.GetOrgUnit()),
		Location: worker.GetLocation(),
		PayLine:  orDash(copy.FormatMoney(worker.GetBasePay(), workerCurrency(worker, options), 2)),
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
		IntentID:      j.GetIntentId(),
		Href:          DetailHref(j.GetIntentId()),
		WorkerName:    j.GetWorkerName(),
		WorkerRef:     j.GetWorkerRef(),
		Headline:      headline(j.GetCurrent().GetJobCode(), j.GetCurrent().GetGrade(), j.GetTarget().GetJobCode(), j.GetTarget().GetGrade()),
		PayLine:       payLineLocale(cfg.Locale, j.GetCurrency(), j.GetCurrentBase(), j.GetProposedBase()),
		EffectiveDate: formatDateLocale(cfg.Locale, j.GetEffectiveDate()),
		Edit: journey.EditDefaults{
			JobCode:      j.GetTarget().GetJobCode(),
			Grade:        j.GetTarget().GetGrade(),
			Base:         j.GetProposedBase(),
			EffectiveISO: j.GetEffectiveDate(),
		},
		Stage:                 stage,
		StageLabel:            stageLabelLocale(cfg.Locale, stage),
		Group:                 journeyGroup(stage),
		StageTone:             stageTone(stage),
		NextStep:              NextStepLabel(JourneyStatusDimension(j).NextStep),
		Closed:                JourneyClosed(j),
		Updated:               formatTimeLocale(cfg.Locale, j.GetUpdatedAt()),
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

func journeyGroup(stage string) journey.JourneyGroup {
	switch stage {
	case stageProposed, stageAwaitingApproval, stageFinanceApproval, stageManagerApproval, stageRevalidation, stageReapproval:
		return journey.JourneyGroupReview
	case stageWaitingEffective, stageExecuted, stageObservingEffects:
		return journey.JourneyGroupWaiting
	case stageBlocked, stageFailed, stageRepairRequired:
		return journey.JourneyGroupIssue
	case stageCompleted, stageRecorded, stageRejected:
		return journey.JourneyGroupClosed
	default:
		return journey.JourneyGroupIssue
	}
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
	form := journey.ProposalForm{
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
		ConfirmationNote: productui.ResolveProductLocale("").Text("journey.form_submit_help"),
		Fields: []journey.Field{
			{
				ID: FieldWorker, Name: NameWorker, Kind: kindSelect, Required: true,
				Value: worker,
				// The list is the workforce the cell just answered with,
				// never a list this client keeps: an employee added a minute
				// ago is proposable, and one the caller may not read is not
				// offered.
				Options: workerOptions(workers, worker),
			},
			{
				ID: FieldJobCode, Name: NameJobCode, Kind: kindSelect,
				Required: true, Value: values[FieldJobCode], Options: stringOptions("Select a governed job code", jobCodes, values[FieldJobCode]),
			},
			{
				ID: FieldGrade, Name: NameGrade, Kind: kindSelect, Required: true,
				Value: values[FieldGrade], Options: stringOptions("Select target grade", grades, values[FieldGrade]),
			},
			// UXLIVE-011: a governed choice, never a text box. The list is
			// whatever the cell proved is authorized, real and still open
			// (WorkforceOptions.position_vacancies); an empty list renders
			// the picker's own no-vacancy state, which is why there is no
			// free-text fallback to fall back to.
			positionField(values[FieldPosition], options, values[FieldJobCode]),
			{
				ID: FieldBase, Name: NameBase, Kind: kindNumber,
				Required: true, Value: values[FieldBase], Step: "0.01", Min: "0", Placeholder: "0.00",
			},
			{
				ID: FieldEffective, Name: NameEffective, Kind: kindDate,
				Required: true, Value: effective,
			},
			{
				ID: FieldReason, Name: NameReason, Kind: kindTextarea, Required: true,
				Value: values[FieldReason],
			},
		},
	}
	localizeProposalForm(&form, productui.ResolveProductLocale(""), false, nil, nil)
	return form
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
		form.DisabledReason = productui.ResolveProductLocale("").Text("journey.form_no_path")
	}
	form.Fields[1].Options = promotionJobOptions(options, worker, jobCodes, values[FieldJobCode])
	form.Fields[2].Options = stringOptions("Select target grade", grades, values[FieldGrade])
	path := selectedPromotionPath(options, worker, values[FieldJobCode], values[FieldGrade])
	localizeProposalForm(&form, productui.ResolveProductLocale(""), true, path, proposalPayRangeFor(worker, options, path))
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
	return promotionPathRuleHelpLocale(path, productui.ResolveProductLocale(""))
}

func promotionPathRuleHelpLocale(path *journeyv1.PromotionPathOption, copy productui.LocaleContext) string {
	if path == nil {
		return ""
	}
	minimum := fractionPercentLabel(path.GetMinimumBaseIncrease())
	maximum := fractionPercentLabel(path.GetMaximumBaseIncrease())
	help := copy.Text("journey.form_base_rule_unavailable")
	if minimum != "" && maximum != "" {
		help = copy.Text("journey.form_base_rule", map[string]string{"minimum": minimum, "maximum": maximum})
	}
	if rules := path.GetBenefitRuleRefs(); len(rules) > 0 {
		help += " " + copy.Text("journey.form_benefit_rule")
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
const PeopleNote = "Employees you add appear in the directory after the request succeeds."

// PeopleEmpty is the empty state. It names the form below it, so an empty
// cell reads as a starting point rather than as a failure.
const PeopleEmpty = "No employees are available in this view yet. Add the first employee below."

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
		OrgUnit:      productui.DisplayLabel(w.GetOrgUnit()),
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
			"Choose the employee's organization. It also supplies the default location when none is provided."),
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
		Help:        "Choose the currency used for this employee's base pay.",
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
	return DetailPageWithInterventions(cfg, detail, notice, values, nil, nil, nil)
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
	withdrawPreview, cancelPreview, repairPreview *journeyv1.PreviewJourneyInterventionResponse,
) journey.Page {
	copy := productui.ResolveProductLocale(cfg.Locale)
	if detail == nil {
		p := chrome(cfg, copy.Text("journey.detail_title")+" · "+Brand, notice, values, true)
		p.Detail = &journey.DetailView{
			Unavailable:  true,
			JourneysLink: journey.NavLink{Label: copy.Text("journey.all_link"), Href: ListHref()},
		}
		return p
	}
	summary := detail.GetJourney()
	head := card(cfg, summary)
	title := copy.Text("journey.detail_title") + " · " + Brand
	if name := summary.GetWorkerName(); name != "" {
		title = name + " · " + copy.Text("journey.detail_title") + " · " + Brand
	}
	p := chrome(cfg, title, notice, values, true)
	detailActions := actionsLocale(cfg.Locale, head, detail.GetApprover(), detail.GetWorkItems(), withdrawPreview, cancelPreview, repairPreview)
	if approvalStage(head.Stage) && !detail.GetCanDecide() {
		allowed := detailActions[:0]
		for _, action := range detailActions {
			if action.ID != ActionApprove && action.ID != ActionReject {
				allowed = append(allowed, action)
			}
		}
		detailActions = allowed
	}
	if len(cfg.PagePermissions) > 0 && !cfg.CanPageAction("journeys", "update") && !cfg.CanPageAction("work", "update") {
		detailActions = nil
	}
	p.Detail = &journey.DetailView{
		Journey:         head,
		Diagnostics:     detail.GetDiagnosticsAvailable() && head.DiagnosticsAuthorized,
		BackLink:        journey.NavLink{Label: copy.Text("journey.back_to_profile", map[string]string{"name": nonEmpty(summary.GetWorkerName(), copy.Text("journey.employee_label"))}), Href: personHref(summary.GetWorkerRef())},
		JourneysLink:    journey.NavLink{Label: copy.Text("journey.all_link"), Href: ListHref()},
		Steps:           stepsLocale(cfg.Locale, head.Stage, detail.GetTimeline(), summary.GetEffectiveDate()),
		Proposal:        proposalFactsLocale(cfg.Locale, detail),
		Comparison:      comparisonLocale(cfg.Locale, summary),
		Findings:        findingsLocale(cfg.Locale, detail.GetFindings()),
		WaitExplanation: waitExplanationFacts(detail.GetFindings()),
		Engine:          engineFacts(detail.GetInstance()),
		Nodes:           nodes(detail.GetNodes()),
		WorkItems:       workItems(detail.GetWorkItems()),
		Ledger:          ledgerLocale(cfg.Locale, detail.GetLedger(), summary.GetEffectiveDate(), head),
		PendingOutcome:  pendingOutcomeLocale(cfg.Locale, head.Stage, head.EffectiveDate),
		Evidence:        detail.GetEvidenceIds(),
		Timeline:        timelineLocale(cfg.Locale, detail.GetTimeline()),
		Actions:         detailActions,
		EffectiveWindow: effectiveWindowLocale(cfg.Locale, summary),
	}
	// PayBand and Budget stay nil: the detail carries no band and no
	// envelope, and a gauge drawn from numbers the engine did not send would
	// be the page inventing an answer. The section is absent until the
	// simulation's quantitative answers reach the wire.
	return p
}

func pendingOutcome(stage, effectiveDate string) string {
	return pendingOutcomeLocale("en-US", stage, effectiveDate)
}

func pendingOutcomeLocale(locale, stage, effectiveDate string) string {
	copy := productui.ResolveProductLocale(locale)
	switch stage {
	case stageWaitingEffective:
		return copy.Text("journey.outcome_waiting", map[string]string{"date": nonEmpty(effectiveDate, copy.Text("journey.effective_date_fallback"))})
	case stageManagerApproval, stageReapproval:
		return copy.Text("journey.outcome_manager")
	case stageAwaitingApproval, stageFinanceApproval:
		return copy.Text("journey.outcome_finance")
	case stageBlocked, stageRejected, stageFailed, stageRepairRequired:
		return copy.Text("journey.outcome_blocked")
	default:
		return copy.Text("journey.outcome_default")
	}
}

func approvalStage(stage string) bool {
	switch stage {
	case stageAwaitingApproval, stageFinanceApproval, stageManagerApproval, stageReapproval:
		return true
	default:
		return false
	}
}

// stepIDs are the business stages a reader needs to follow.
// Finance and manager review remain distinct because they are performed by
// different people with different authority; execution machinery is kept in
// diagnostics instead of masquerading as a user task.
var (
	stepIDs = [5]string{"proposal", "finance-review", "manager-review", "effective-date", "recorded"}
	// stepStates is the whole of this page's reading of a stage. Each row is
	// the five steps' states at one stage, so "what does BLOCKED look like"
	// is one line to read rather than a walk through conditionals.
	stepStates = map[string][5]string{
		stageProposed:         {stepDone, stepActive, stepUpcoming, stepUpcoming, stepUpcoming},
		stageBlocked:          {stepFailed, stepUpcoming, stepUpcoming, stepUpcoming, stepUpcoming},
		stageAwaitingApproval: {stepDone, stepActive, stepUpcoming, stepUpcoming, stepUpcoming},
		stageFinanceApproval:  {stepDone, stepActive, stepUpcoming, stepUpcoming, stepUpcoming},
		stageManagerApproval:  {stepDone, stepDone, stepActive, stepUpcoming, stepUpcoming},
		stageReapproval:       {stepDone, stepDone, stepActive, stepUpcoming, stepUpcoming},
		stageWaitingEffective: {stepDone, stepDone, stepDone, stepActive, stepUpcoming},
		stageRevalidation:     {stepDone, stepDone, stepDone, stepDone, stepActive},
		stageExecuted:         {stepDone, stepDone, stepDone, stepDone, stepActive},
		stageObservingEffects: {stepDone, stepDone, stepDone, stepDone, stepActive},
		stageCompleted:        {stepDone, stepDone, stepDone, stepDone, stepDone},
		stageRecorded:         {stepDone, stepDone, stepDone, stepDone, stepDone},
		stageRejected:         {stepDone, stepFailed, stepUpcoming, stepUpcoming, stepUpcoming},
		stageFailed:           {stepDone, stepFailed, stepUpcoming, stepUpcoming, stepUpcoming},
		stageRepairRequired:   {stepDone, stepDone, stepDone, stepFailed, stepUpcoming},
	}
)

// steps builds the stepper. The times come from the engine's own timeline
// rather than from a second reading of the same rows: whichever event kind
// marks a step is where that step's timestamp comes from, and a step with no
// such event carries no time rather than a guess.
func steps(stage string, events []*journeyv1.TimelineEvent, effectiveDate string) []journey.Step {
	return stepsLocale("en-US", stage, events, effectiveDate)
}

// workItemCompleted reports whether a work-item timeline event records that
// item completing. The engine composes the title for a reader -- "Finance
// review completed", "Manager review cancelled" -- while some producers
// carry the bare transition state, so both shapes are accepted. Matching
// only the bare state is how the live page came to show a completed
// approval as never started (UXLIVE-002).
func workItemCompleted(title string) bool {
	upper := strings.ToUpper(strings.TrimSpace(title))
	return upper == "COMPLETED" || strings.HasSuffix(upper, " COMPLETED")
}

// completedSteps reads the run's own history for the steps it proves
// finished. A step is complete only when the engine recorded the event that
// completes it; silence is never completion (UXLIVE-002).
//
// The two approval steps are the first two distinct work items in the same
// order stepTimesLocale takes their timestamps from, so a step's state and
// its time always describe the same item.
func completedSteps(stage string, events []*journeyv1.TimelineEvent) [5]bool {
	var done [5]bool
	items := make([]string, 0, 2)
	completed := make(map[string]bool, 2)
	started, recorded := false, false
	for _, e := range events {
		if e == nil {
			continue
		}
		switch e.GetKind() {
		case eventInstanceStarted:
			started = true
		case eventLedgerRecorded:
			recorded = true
		case eventWorkItem:
			ref := strings.TrimSpace(e.GetRef())
			if ref == "" {
				continue
			}
			if slices.Index(items, ref) < 0 && len(items) < 2 {
				items = append(items, ref)
			}
			if workItemCompleted(e.GetTitle()) {
				completed[ref] = true
			}
		}
	}
	// Starting the approval workflow is what completes the proposal: the
	// engine refuses to start one whose checks did not pass.
	done[0] = started
	for i, ref := range items {
		done[i+1] = completed[ref]
	}
	// A terminal record exists for every terminal outcome, so the
	// effective-date wait and the recording are complete only when the
	// record is a recorded promotion.
	done[3] = recorded && recordedPromotion(stage)
	done[4] = done[3]
	return done
}

// stoppedStage reports the terminal stages that stopped a run short of
// recording its promotion. Only those mark a failed step, and they mark the
// first step the run did not complete -- a run stops once, wherever it got
// to (UXLIVE-002). This is deliberately not journeyGroup's issue bucket:
// that bucket also catches an unknown stage, which has no known position.
func stoppedStage(stage string) bool {
	switch stage {
	case stageBlocked, stageFailed, stageRejected, stageRepairRequired:
		return true
	default:
		return false
	}
}

// stepStatesFor is the stepper's shape for one run. The stage table supplies
// the shape of the stages where the run's position is unambiguous; anything
// the recorded history proves complete overrides it, and an outcome that
// stopped the run marks the first step it did not complete rather than
// assuming it stopped at the proposal (UXLIVE-002).
func stepStatesFor(stage string, events []*journeyv1.TimelineEvent) [5]string {
	states, ok := stepStates[stage]
	if !ok {
		// A stage this projection does not know is schema drift. Showing
		// nothing is the honest answer; reading progress into an unknown
		// lifecycle would be a guess with a confident face.
		return [5]string{stepUpcoming, stepUpcoming, stepUpcoming, stepUpcoming, stepUpcoming}
	}
	done := completedSteps(stage, events)
	for i := range states {
		if done[i] {
			states[i] = stepDone
		}
	}
	if !stoppedStage(stage) {
		return states
	}
	marked := false
	for i := range states {
		if done[i] {
			continue
		}
		if !marked {
			states[i] = stepFailed
			marked = true
			continue
		}
		states[i] = stepUpcoming
	}
	return states
}

func stepsLocale(locale, stage string, events []*journeyv1.TimelineEvent, effectiveDate string) []journey.Step {
	copy := productui.ResolveProductLocale(locale)
	states := stepStatesFor(stage, events)
	at := stepTimesLocale(locale, events)
	if states[3] == stepActive || states[3] == stepDone {
		at[3] = formatDateLocale(locale, effectiveDate)
	}
	// A step that did not happen carries no time. The recording step used to
	// borrow the terminal record's timestamp even when nothing was recorded,
	// which read as a completed step captioned "Not started".
	if states[4] != stepDone {
		at[4] = ""
	}
	out := make([]journey.Step, 0, len(stepIDs))
	for i := range stepIDs {
		detail := copy.Text("journey.step." + stepIDs[i] + "." + states[i])
		if i == 1 && stage == stageProposed {
			detail = copy.Text("journey.step.finance-review.pending-start")
		}
		out = append(out, journey.Step{
			ID:     stepIDs[i],
			Label:  copy.Text("journey.step." + stepIDs[i] + ".label"),
			Detail: detail,
			State:  states[i],
			At:     at[i],
		})
	}
	return out
}

func stepTimesLocale(locale string, events []*journeyv1.TimelineEvent) [5]string {
	var at [5]string
	workItems := make([]string, 0, 2)
	for _, e := range events {
		if e == nil {
			continue
		}
		when := formatTimeLocale(locale, e.GetAt())
		if when == "" {
			continue
		}
		switch e.GetKind() {
		case eventIntentCreated:
			at[0] = when
		case eventWorkItem:
			ref := strings.TrimSpace(e.GetRef())
			index := slices.Index(workItems, ref)
			if index < 0 && ref != "" && len(workItems) < 2 {
				workItems = append(workItems, ref)
				index = len(workItems) - 1
			}
			if index >= 0 && index < 2 {
				at[index+1] = when
			}
		case eventLedgerRecorded:
			at[4] = when
		}
	}
	return at
}

// tokenShaped reports whether a stored free-text value is a machine token
// rather than prose: no whitespace, and separated by the underscores or
// hyphens an identifier uses. A single word is not enough -- "Reorganisation"
// is prose -- so a separator is required.
func tokenShaped(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.ContainsAny(trimmed, " \t\n") {
		return false
	}
	return strings.ContainsAny(trimmed, "_-")
}

func proposalFactsLocale(locale string, detail *journeyv1.JourneyDetail) []journey.Fact {
	copy := productui.ResolveProductLocale(locale)
	j := detail.GetJourney()
	facts := []journey.Fact{}
	if v := j.GetBusinessReason(); v != "" {
		// A business reason is prose an approver reads. Some stored values
		// are machine tokens (`promotion_into_senior_hrbp_fix_verify`), and
		// printing one as a sentence mislabels an identifier as a reason;
		// it is shown in the identifier treatment instead, so a reader can
		// see what it is rather than trying to read it (UXLIVE-009).
		facts = append(facts, journey.Fact{Label: copy.Text("journey.business_reason"), Value: v, Mono: tokenShaped(v)})
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

func comparisonLocale(locale string, j *journeyv1.Journey) []journey.ComparisonRow {
	if j == nil {
		return nil
	}
	copy := productui.ResolveProductLocale(locale)
	cur, tgt := j.GetCurrent(), j.GetTarget()
	rows := []journey.ComparisonRow{
		row(copy.Text("journey.compare_job"), cur.GetJobCode(), tgt.GetJobCode()),
		row(copy.Text("journey.compare_grade"), cur.GetGrade(), tgt.GetGrade()),
		row(copy.Text("journey.compare_position"), cur.GetPositionId(), tgt.GetPositionId()),
		row(copy.Text("journey.compare_org"), productui.DisplayLabel(cur.GetOrgUnit()), productui.DisplayLabel(tgt.GetOrgUnit())),
	}

	pay := journey.ComparisonRow{
		Label:    copy.Text("journey.compare_base"),
		Current:  orDash(formatAmountLocale(locale, j.GetCurrency(), j.GetCurrentBase())),
		Proposed: orDash(formatAmountLocale(locale, j.GetCurrency(), j.GetProposedBase())),
	}
	if delta, ok := amountDeltaLocale(locale, j.GetCurrency(), j.GetCurrentBase(), j.GetProposedBase()); ok {
		pay.Delta = delta
		pay.Changed = true
		if pct, okPct := percentDeltaLocale(locale, j.GetCurrentBase(), j.GetProposedBase()); okPct {
			pay.Delta = delta + " (" + pct + ")"
		}
	}
	rows = append(rows, pay)

	effective := formatDateLocale(locale, j.GetEffectiveDate())
	rows = append(rows, journey.ComparisonRow{
		Label:    copy.Text("journey.compare_effective"),
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

func findingsLocale(locale string, in []*journeyv1.Finding) []journey.Finding {
	if len(in) == 0 {
		return nil
	}
	out := make([]journey.Finding, 0, len(in))
	for _, f := range in {
		if f == nil || isWaitExplanationCode(f.GetCode()) {
			continue
		}
		message := strings.TrimSpace(f.GetMessage())
		// This server-authored finding has a stable semantic code. Localize
		// that code rather than parsing or translating its English prose.
		if strings.TrimSpace(f.GetCode()) == "promotion.budget_authority_observation_only" {
			message = productui.ResolveProductLocale(locale).Text("journey.finding_budget_observation")
		}
		out = append(out, journey.Finding{
			Severity: severityOf(f.GetSeverity()),
			Code:     strings.TrimSpace(f.GetCode()),
			Message:  message,
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
	case "needs_data", "needs-data", "needs data":
		return "needs-data"
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

// recordedPromotion reports whether a terminal record on this stage is a
// recorded promotion. The engine writes a terminal record for every terminal
// outcome, including the ones that recorded a refusal, so the record's
// existence is not the question -- the stage it closed on is (UXLIVE-001).
func recordedPromotion(stage string) bool {
	switch stage {
	case stageRecorded, stageCompleted:
		return true
	default:
		return false
	}
}

func ledgerLocale(locale string, l *journeyv1.LedgerEvent, proposalEffectiveDate string, head journey.JourneyCard) *journey.LedgerCard {
	if l == nil {
		return nil
	}
	effectiveAt := formatDateOfLocale(locale, l.GetEffectiveAt())
	if effectiveAt == "" {
		effectiveAt = formatDateLocale(locale, proposalEffectiveDate)
	}
	return &journey.LedgerCard{
		StreamKey:      l.GetStreamKey(),
		Sequence:       strconv.FormatInt(l.GetSequence(), 10),
		SchemaRef:      l.GetSchemaRef(),
		Digest:         l.GetDigest(),
		IdempotencyKey: l.GetIdempotencyKey(),
		RecordedAt:     formatTimeLocale(locale, l.GetRecordedAt()),
		EffectiveAt:    effectiveAt,
		Recorded:       recordedPromotion(head.Stage),
		StatusLabel:    head.StageLabel,
		StatusTone:     head.StageTone,
	}
}

// timeline renders the engine's chronology newest first. The engine composes
// it oldest first (causal order, which is how it reasons); a reader opening
// the page wants the last thing that happened at the top, which is how the
// reference page reads.
func timeline(in []*journeyv1.TimelineEvent) []journey.TimelineEvent {
	return timelineLocale("en-US", in)
}

func timelineLocale(locale string, in []*journeyv1.TimelineEvent) []journey.TimelineEvent {
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
			At:     formatTimeLocale(locale, e.GetAt()),
			Actor:  timelineActorLocale(locale, e),
			Title:  timelineTitleLocale(locale, e),
			Detail: timelineDetailLocale(locale, e),
			Tone:   eventTone(e),
		})
	}
	return collapseTimeline(out)
}

func timelineActorLocale(locale string, e *journeyv1.TimelineEvent) string {
	copy := productui.ResolveProductLocale(locale)
	actor := strings.TrimSpace(e.GetActor())
	if actor == "" {
		return ""
	}
	if actor == "workflow" || actor == "system" || strings.HasPrefix(actor, "system:") {
		return copy.Text("journey.timeline_system")
	}
	return copy.Text("journey.timeline_reviewer")
}

func timelineTitleLocale(locale string, e *journeyv1.TimelineEvent) string {
	copy := productui.ResolveProductLocale(locale)
	switch e.GetKind() {
	case eventIntentCreated:
		return copy.Text("journey.timeline_requested")
	case eventSimulated:
		return copy.Text("journey.timeline_checked")
	case eventInstanceStarted:
		return copy.Text("journey.timeline_started")
	case eventLedgerRecorded:
		return copy.Text("journey.timeline_recorded")
	case eventNode:
		// Node names belong to the authorized diagnostics projection, not
		// the business history. collapseTimeline drops an empty title.
		return ""
	case eventWorkItem:
		title := strings.TrimSpace(e.GetTitle())
		switch strings.ToUpper(title) {
		case "CREATED", "ROUTED", "ASSIGNED", "AVAILABLE", "OPEN", "READY":
			return copy.Text("journey.timeline_assigned")
		case "CLAIMED", "IN_PROGRESS":
			return copy.Text("journey.timeline_review_started")
		case "COMPLETED":
			return copy.Text("journey.timeline_approved")
		case "CANCELLED", "CANCELED":
			return copy.Text("journey.timeline_cancelled")
		case "EXPIRED":
			return copy.Text("journey.timeline_expired")
		}
		for _, review := range []struct{ prefix, key string }{
			{"Finance review", "journey.timeline_review_finance"},
			{"Manager review", "journey.timeline_review_manager"},
			{"Reapproval", "journey.timeline_review_reapproval"},
			{"Approval", "journey.timeline_review_approval"},
		} {
			for _, state := range []struct{ suffix, key string }{
				{" assigned", "journey.timeline_state_assigned"},
				{" started", "journey.timeline_state_started"},
				{" completed", "journey.timeline_state_completed"},
				{" cancelled", "journey.timeline_state_cancelled"},
				{" expired", "journey.timeline_state_expired"},
			} {
				if title == review.prefix+state.suffix {
					return copy.Text("journey.timeline_review_state", map[string]string{
						"review": copy.Text(review.key), "state": copy.Text(state.key),
					})
				}
			}
		}
		return copy.Text("journey.timeline_review_updated")
	}
	return copy.Text("journey.timeline_update")
}

func timelineDetailLocale(locale string, e *journeyv1.TimelineEvent) string {
	switch e.GetKind() {
	case eventIntentCreated:
		return strings.TrimSpace(e.GetDetail())
	case eventLedgerRecorded:
		return productui.ResolveProductLocale(locale).Text("journey.step.recorded.done")
	default:
		return ""
	}
}

func collapseTimeline(events []journey.TimelineEvent) []journey.TimelineEvent {
	out := make([]journey.TimelineEvent, 0, len(events))
	for _, event := range events {
		if event.Title == "" || strings.Contains(event.Title, "_") {
			continue
		}
		if len(out) > 0 && out[len(out)-1].At == event.At && out[len(out)-1].Title == event.Title {
			continue
		}
		out = append(out, event)
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
		title := strings.ToLower(strings.TrimSpace(e.GetTitle()))
		if strings.HasSuffix(title, " completed") {
			return toneSuccess
		}
		for _, ending := range []string{" cancelled", " expired", " rejected", " failed"} {
			if strings.HasSuffix(title, ending) {
				return toneDanger
			}
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

func effectiveWindowLocale(locale string, j *journeyv1.Journey) *journey.EffectiveWindow {
	if j == nil {
		return nil
	}
	effective := formatDateLocale(locale, j.GetEffectiveDate())
	start := formatDateOfLocale(locale, j.GetCreatedAt())
	knownAt := formatTimeLocale(locale, j.GetUpdatedAt())
	if effective == "" && start == "" {
		return nil
	}
	return &journey.EffectiveWindow{
		Start:         start,
		EffectiveDate: effective,
		KnownAt:       knownAt,
		Note:          productui.ResolveProductLocale(locale).Text("journey.effective_window_note"),
	}
}

func actionsLocale(locale string, head journey.JourneyCard, approver string, workItems []*journeyv1.WorkItem, withdrawPreview, cancelPreview, repairPreview *journeyv1.PreviewJourneyInterventionResponse) []journey.Action {
	copy := productui.ResolveProductLocale(locale)
	href := DetailHref(head.IntentID)
	confirm := actionConfirmationLocale(locale, head)
	var out []journey.Action
	switch head.Stage {
	case stageProposed:
		out = []journey.Action{{
			ID: ActionExecute, Label: copy.Text("journey.action_start"), Variant: "primary",
			Description:      copy.Text("journey.action_start_description"),
			Action:           href,
			Hidden:           map[string]string{},
			Confirmation:     confirm,
			ConfirmationNote: copy.Text("journey.action_start_note"),
			ConfirmTitle:     copy.Text("journey.action_confirm_execute"),
			ReviewLabel:      copy.Text("journey.action_review_execute"),
		}}
	case stageBlocked:
		// The findings explain a blocked proposal; no misleading primary action.
	case stageAwaitingApproval, stageFinanceApproval, stageManagerApproval, stageReapproval:
		if !hasActionableApproval(workItems) {
			out = []journey.Action{{
				ID: ActionApprove, Label: copy.Text("journey.action_preparing"), Variant: "primary",
				Description: copy.Text("journey.action_preparing_description"),
				Action:      href, Hidden: map[string]string{}, Disabled: true,
				DisabledReason: copy.Text("journey.action_preparing_reason"),
			}}
			break
		}
		out = []journey.Action{
			{
				ID: ActionApprove, Label: copy.Text("journey.action_approve"), Variant: "primary",
				Description:      copy.Text("journey.action_approve_description"),
				Action:           href,
				Hidden:           map[string]string{},
				ActsAs:           approver,
				ActsAsLabel:      approverLabelLocale(locale, head.Stage, approver),
				Confirmation:     confirm,
				ConfirmationNote: copy.Text("journey.action_approve_note"),
				ConfirmTitle:     copy.Text("journey.action_confirm_approve"),
				ReviewLabel:      copy.Text("journey.action_review_approve"),
				Fields: []journey.Field{{
					ID: FieldApproveReason, Name: NameDecisionReason, Label: copy.Text("journey.action_approve_reason"),
					Kind: kindTextarea, Placeholder: copy.Text("journey.action_approve_placeholder"),
					Help: copy.Text("journey.action_approve_help"),
				}},
			},
			{
				ID: ActionReject, Label: copy.Text("journey.action_reject"), Variant: "danger",
				Description:      copy.Text("journey.action_reject_description"),
				Action:           href,
				Hidden:           map[string]string{},
				ActsAs:           approver,
				ActsAsLabel:      approverLabelLocale(locale, head.Stage, approver),
				Confirmation:     confirm,
				ConfirmationNote: copy.Text("journey.action_reject_note"),
				ConfirmTitle:     copy.Text("journey.action_confirm_reject"),
				ReviewLabel:      copy.Text("journey.action_review_reject"),
				Fields: []journey.Field{{
					ID: FieldRejectReason, Name: NameDecisionReason, Label: copy.Text("journey.action_reject_reason"),
					Kind: kindTextarea, Required: true,
					Help: copy.Text("journey.action_reject_help"),
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
	return interventionActions(out, head, withdrawPreview, cancelPreview, repairPreview)
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

	// UXLIVE-006's repair references, from internal/intent/app/
	// journey_repair.go, kept in agreement with it by the same inspection
	// this file's other four rely on.
	reasonRepairNotRequired       = "journey.repair.unavailable.not_required"
	reasonRepairDoorUnavailable   = "journey.repair.unavailable.door_unavailable"
	reasonRepairAuthorityRequired = "journey.repair.unavailable.operator_authority_required"
	reasonRepairPlanRequired      = "journey.repair.unavailable.plan_required"
)

// journeyStarted reports whether this journey's approval workflow began.
// The card carries the instance id, which the contract documents as empty
// before execution, so the page knows this without a second read.
func journeyStarted(head journey.JourneyCard) bool {
	return strings.TrimSpace(head.InstanceID) != ""
}

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
func InterventionAvailability(kind string, stage string, started bool) (reasonRef string, available bool) {
	terminal := stage == stageCompleted || stage == stageRejected || stage == stageFailed || stage == stageRecorded
	// BLOCKED is reachable from a proposal that never started and from a run
	// whose approvals are recorded; only the run itself can say which
	// (UXLIVE-026).
	unstarted := stage == stageProposed || (stage == stageBlocked && !started)
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

// repairAction appends UXLIVE-006's governed repair to a repair-required
// journey.
//
// It is always disabled, and that is the finding, not a shortcut: the
// corrective effect runs against an authored, simulated repair plan through
// the operator repair door, under a JIT grant with dual control -- none of
// which a promotion page holds or should mint. What the page owes the reader
// is the thing it had been withholding: that the repair exists, what it
// demands, who may authorize it, and what its fence can and cannot
// establish. A button that always refused would be the defect UXLIVE-011
// removed from the target-position field, in a new place.
//
// The reason comes from the server's own preview whenever it answered,
// because only the server knows whether this viewer holds the grant. Without
// it the page still says the true, viewer-independent part -- the journey is
// waiting on a governed repair -- rather than guessing at authority.
func repairAction(base []journey.Action, head journey.JourneyCard, preview *journeyv1.PreviewJourneyInterventionResponse, href string) []journey.Action {
	if head.Stage != stageRepairRequired {
		return base
	}
	description := repairDescription(preview)
	reason := interventionReasonText(reasonRepairPlanRequired)
	if ref := strings.TrimSpace(preview.GetUnavailableReasonRef()); ref != "" {
		reason = interventionReasonText(ref)
	} else if preview == nil {
		reason = "This journey is waiting on a governed repair. Whether you may authorize one has not been answered yet."
	}
	return append(base, journey.Action{
		ID: ActionRepair, Label: "Governed repair", Variant: "secondary",
		Description: description, Action: href, Hidden: map[string]string{},
		Disabled: true, DisabledReason: reason,
	})
}

// repairDescription states what the repair does and what the door demands.
// The requirements are the server's, read off the preview: a page that
// restated them from memory could describe a lighter action than the one
// that exists. When the preview has not answered, the requirements are left
// unstated rather than assumed -- an understated governed action is worse
// than a silent one.
func repairDescription(preview *journeyv1.PreviewJourneyInterventionResponse) string {
	description := strings.TrimSpace(preview.GetConsequenceSummary())
	if description == "" {
		description = "This journey stopped in a state only a governed repair can resolve."
	}
	requirements := []string{}
	if preview.GetRequiresDualControl() {
		requirements = append(requirements, "a second approver distinct from whoever runs it")
	}
	if preview.GetRequiresSimulation() {
		requirements = append(requirements, "a simulation over exactly this scope")
	}
	if roles := preview.GetAuthorityRoleRefs(); len(roles) > 0 {
		requirements = append(requirements, "a current "+strings.Join(roles, " or ")+" grant")
	}
	if len(requirements) == 0 {
		return description
	}
	return description + " It requires " + joinRequirements(requirements) + "."
}

// joinRequirements reads a list the way a person would say it.
func joinRequirements(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
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
	case reasonRepairNotRequired:
		return "This journey has not asked for a repair."
	case reasonRepairDoorUnavailable:
		return "This deployment cannot run a governed repair: it is composed without the repair door."
	case reasonRepairAuthorityRequired:
		return "You do not hold a current grant to authorize a governed repair."
	case reasonRepairPlanRequired:
		return "You may authorize a governed repair. It runs against an authored, simulated repair plan through the operator repair door, which is not created from this page."
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
func interventionActions(base []journey.Action, head journey.JourneyCard, withdraw, cancel, repair *journeyv1.PreviewJourneyInterventionResponse) []journey.Action {
	href := DetailHref(head.IntentID)
	facts := actionConfirmation(head)

	appendOne := func(kind, actionID, label, variant, description string, preview *journeyv1.PreviewJourneyInterventionResponse, reasonField, reasonName string, extraFields []journey.Field) {
		reasonRef, available := InterventionAvailability(kind, head.Stage, journeyStarted(head))
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

	// UXLIVE-006: the repair a REPAIR_REQUIRED journey names as its own next
	// step, presented on that journey and only on it.
	//
	// That is a deliberate departure from the convention the three actions
	// below follow ("always render, disabled, naming why"). That convention
	// exists so a viewer cannot tell an omitted action from a denied one --
	// but "this journey does not need repair" is already on the page, in the
	// stage chip, for every reader. Rendering a disabled repair on every
	// healthy journey would add noise without adding an answer.
	base = repairAction(base, head, repair, href)

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
	withdrawRef, withdrawOK := InterventionAvailability(ActionWithdraw, head.Stage, journeyStarted(head))
	cancelRef, cancelOK := InterventionAvailability(ActionCancel, head.Stage, journeyStarted(head))
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
				{ID: FieldEditJobCode, Name: NameEditJobCode, Label: "Target job code", Kind: kindText, Value: head.Edit.JobCode, Required: true},
				{ID: FieldEditGrade, Name: NameEditGrade, Label: "Target grade", Kind: kindText, Value: head.Edit.Grade, Required: true},
				{ID: FieldEditBase, Name: NameEditBase, Label: "Proposed base pay", Kind: kindText, Value: head.Edit.Base, Required: true},
				{ID: FieldEditEffective, Name: NameEditEffective, Label: "Effective date", Kind: kindDate, Value: head.Edit.EffectiveISO, Required: true},
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
	return actionConfirmationLocale("en-US", head)
}

func actionConfirmationLocale(locale string, head journey.JourneyCard) []journey.Fact {
	copy := productui.ResolveProductLocale(locale)
	facts := []journey.Fact{{Label: copy.Text("journey.action_employee"), Value: nonEmpty(head.WorkerName, copy.Text("journey.action_employee"))}}
	if head.Headline != "" {
		facts = append(facts, journey.Fact{Label: copy.Text("journey.action_placement"), Value: head.Headline})
	}
	if head.PayLine != "" {
		facts = append(facts, journey.Fact{Label: copy.Text("journey.action_base"), Value: head.PayLine})
	}
	if head.EffectiveDate != "" {
		facts = append(facts, journey.Fact{Label: copy.Text("journey.action_effective"), Value: head.EffectiveDate})
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

// draftConfirmation is proposalConfirmation over a proposal form's current
// values, defaulting the effective date the same way the form does.
func draftConfirmation(values map[string]string, worker *journeyv1.Worker, options *journeyv1.WorkforceOptions) []journey.Fact {
	effective := values[FieldEffective]
	if effective == "" {
		effective = DefaultEffectiveDate(time.Now())
	}
	return proposalConfirmation(worker, options, values[FieldJobCode], values[FieldGrade], values[FieldBase], effective)
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

func approverLabelLocale(locale, stage, principal string) string {
	_ = principal // The principal is displayed separately; stage names the authority being exercised.
	copy := productui.ResolveProductLocale(locale)
	switch stage {
	case stageFinanceApproval:
		return copy.Text("journey.action_finance_review")
	case stageManagerApproval:
		return copy.Text("journey.action_manager_review")
	case stageReapproval:
		return copy.Text("journey.action_updated_review")
	default:
		return copy.Text("journey.action_comp_review")
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
