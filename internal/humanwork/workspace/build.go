package workspace

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
)

// Action identifiers this workspace declares.
//
// Exactly one of them is ever offered. The other two are declared and then
// masked on every request, which is the honest way to say "this release
// grants no write authority": the surface names the actions a released
// authority topology would carry, and the visibility decision - not a gap in
// the markup - is what keeps them off the page.
const (
	ActionRunSimulation     = "run_simulation"
	ActionSubmitForApproval = "submit_for_approval"
	ActionForceExecute      = "force_execute"
)

// Labels for the declared actions. They are constants because
// [MaskedNeedles] returns the masked ones verbatim and the conformance test
// greps the rendered document for them: a label that drifted from the needle
// would make the masking check pass by accident.
const (
	labelRunSimulation     = "Run simulation"
	labelSubmitForApproval = "Submit for approval"
	labelForceExecute      = "Force execute"
)

// TransitionRunSimulation is the transition value the one offered action
// submits. The handler accepts nothing else.
const TransitionRunSimulation = "run_simulation"

// Page is one fully-resolved workspace render: the masked contract a renderer
// consumes, plus everything the routes need that the contract deliberately
// does not carry.
type Page struct {
	// Contract is the masked payload. It is the only value a renderer sees.
	Contract contract.WorkspaceContract
	// Source, Fields and Actions are the unmasked record and the visibility
	// decision it was projected through. They never leave the server; tests
	// use them to compute what must be absent.
	Source  contract.SourceRecord
	Fields  contract.FieldVisibility
	Actions contract.ActionVisibility

	// Query is the string form the request form is pre-filled from.
	Query Query
	// ReceiptDigest is the promotion simulation's own result digest, which is
	// the identity the receipt route addresses. It is empty when the
	// compensation half was not disclosed and therefore never simulated.
	ReceiptDigest string
	// Reading is the live answer the page was built from.
	Reading Reading
	// Approvals is the workflow-simulation chronology behind the timeline.
	Approvals ApprovalTimeline
	// Locale is presentation-only context resolved at the workspace edge. It
	// does not participate in legal, policy, payroll, or authorization
	// decisions; those remain inputs to the live governed read.
	Locale LocaleContext
	// TranslationDiagnostics reports catalog gaps that were rendered as an
	// explicit visible fallback instead of being silently omitted.
	TranslationDiagnostics []TranslationDiagnostic
}

// BuildPage projects one live [Reading] into the frozen workspace contract,
// with the caller's own authorization decision already applied.
//
// Masking happens exactly once, here, and by construction: every field and
// action is declared on the source record, the visibility maps say which of
// them this caller may have, and contract.NewWorkspaceContract drops the rest
// before any renderer exists. There is no second path that renders a field
// without going through this projection.
func BuildPage(q Query, req Request, r Reading, approvals ApprovalTimeline) Page {
	return BuildPageLocalized(q, req, r, approvals, ResolveLocale(DefaultLocale))
}

// BuildPageLocalized projects a live Reading into the frozen workspace
// contract under a presentation-only locale. The locale is applied only after
// the governed read and simulation inputs have been resolved, so changing it
// cannot affect legal, policy, authorization, or calculation semantics.
func BuildPageLocalized(q Query, req Request, r Reading, approvals ApprovalTimeline, locale LocaleContext) Page {
	source := sourceRecord(q, req, r, approvals)
	fields := fieldVisibility(r)
	actions := actionVisibility()
	source, diagnostics := localizeSourceRecord(source, q, locale, fields, actions)

	page := Page{
		Contract:               contract.NewWorkspaceContract(source, fields, actions),
		Source:                 source,
		Fields:                 fields,
		Actions:                actions,
		Query:                  q,
		Reading:                r,
		Approvals:              approvals,
		Locale:                 locale,
		TranslationDiagnostics: diagnostics,
	}
	if r.CompensationDisclosed {
		page.ReceiptDigest = r.Simulation.ResultDigest
	}
	return page
}

// MaskedNeedles returns every string a masked field or action would have
// contributed to a rendered document: the identifier, the label and the raw
// value.
//
// It is the input to the qualification fixture's masking criterion, and it is
// derived from the same source record and visibility decision the page was
// built from rather than hand-listed, so a field that starts being masked is
// automatically checked for.
func (p Page) MaskedNeedles() []string {
	var out []string
	for _, f := range p.Source.AllFields {
		if p.Fields[f.ID] {
			continue
		}
		out = append(out, f.ID, f.Label)
		if v := strings.TrimSpace(f.Value); v != "" {
			out = append(out, v)
		}
	}
	for _, a := range p.Source.AllActions {
		if p.Actions[a.ID] {
			continue
		}
		out = append(out, a.ID, a.Label)
	}
	return out
}

// MaskedActionNeedles returns the identifier and label of every action this
// workspace declares and never offers.
//
// It exists so a conformance test can assert their absence from a rendered
// document without composing a page first, and so that the strings it checks
// are the same constants the source record is built from rather than literals
// a test author retyped.
func MaskedActionNeedles() []string {
	always := actionVisibility()
	var out []string
	for _, a := range []struct{ id, label string }{
		{ActionRunSimulation, labelRunSimulation},
		{ActionSubmitForApproval, labelSubmitForApproval},
		{ActionForceExecute, labelForceExecute},
	} {
		if always[a.id] {
			continue
		}
		out = append(out, a.id, a.label)
	}
	return out
}

// fieldVisibility derives the field allowlist from what the cell actually
// disclosed.
//
// Nothing here re-evaluates policy. A worker-state field is visible when the
// governed read returned it as AUTHORIZED, and the compensation fields are
// visible when the cell reports that the compensation-gated capabilities ran
// at all. A field the caller supplied - the proposed placement, the effective
// date, the business reason - is always visible, because it is the caller's
// own request coming back, not a disclosure about the worker.
func fieldVisibility(r Reading) contract.FieldVisibility {
	visible := contract.FieldVisibility{
		FieldProposedJobTitle: true,
		FieldProposedGrade:    true,
		FieldTargetPosition:   true,
		FieldTargetOrgUnit:    true,
		FieldEffectiveDate:    true,
		FieldBusinessReason:   true,
	}
	if disclosed(r.Explanation, people.FieldLegalName) || disclosed(r.Explanation, people.FieldPreferredName) {
		visible[FieldWorkerName] = true
	}
	if disclosed(r.Explanation, people.FieldJobCode) {
		visible[FieldCurrentJobTitle] = true
	}
	if disclosed(r.Explanation, people.FieldGrade) {
		visible[FieldCurrentGrade] = true
	}
	if r.CompensationDisclosed {
		visible[FieldCurrentBasePay] = true
		visible[FieldProposedComp] = true
		visible[FieldBandPosition] = true
		visible[FieldAnnualizedIncrease] = true
	} else {
		visible[FieldCompensationWithheld] = true
	}
	return visible
}

// actionVisibility offers the one action this release can honour.
//
// SubmitIntent, CancelIntent and SupersedeIntent are real governed writes as
// of EP-INTENT-003, but this workspace surface has not been wired to call
// them: it has no claim/version/reason-ref form for any of the three, and
// offering a control with nothing behind it would be a button whose only
// outcome is a client-side error. The two write actions stay declared and
// masked until this package grows that form.
func actionVisibility() contract.ActionVisibility {
	return contract.Allow(ActionRunSimulation)
}

func disclosed(e people.Explanation, field people.FieldID) bool {
	_, ok := e.Value(field)
	return ok
}

// factValue returns the disclosed value of a worker-state field, or an empty
// string. An undisclosed field's absence is handled by the visibility map;
// this never substitutes a placeholder, because a placeholder in a masked
// field's slot is still a statement about the withheld value.
func factValue(e people.Explanation, field people.FieldID) string {
	v, _ := e.Value(field)
	return v
}

// sourceRecord assembles the full, unmasked record: every field and action
// this workspace declares, whether or not this caller may see it.
func sourceRecord(q Query, req Request, r Reading, approvals ApprovalTimeline) contract.SourceRecord {
	name := workerName(r)
	if name == "" {
		// A caller who may not learn the name still needs the page addressed
		// to something. The reference they typed is the only string that is
		// theirs rather than the worker's, so it is what the title carries.
		name = q.WorkerRef
	}
	messages := validationMessages(r)

	return contract.SourceRecord{
		WorkspaceID: "promotion/" + q.WorkerRef,
		Title:       "Promotion: " + name,
		WorkerID:    r.Worker.Id,
		WorkerName:  name,
		AllFields:   requestFields(q, r, messages),
		Preflight:   preflightFindings(r),
		Simulation:  simulationResult(r, approvals),
		Timeline:    approvals.Events,
		AllActions: []contract.AvailableAction{
			{
				ID: ActionRunSimulation, Label: labelRunSimulation,
				Transition: TransitionRunSimulation, Variant: contract.ActionPrimary,
			},
			{
				ID: ActionSubmitForApproval, Label: labelSubmitForApproval,
				Transition: "submit_for_approval", Variant: contract.ActionSecondary,
			},
			{
				ID: ActionForceExecute, Label: labelForceExecute,
				Transition: "force_execute", Variant: contract.ActionDanger,
			},
		},
		Provenance: contract.Provenance{
			CapabilityID:      r.CapabilityID,
			CapabilityVersion: r.CapabilityVersion,
			SourceSystem:      "hcm-next",
			AsOf:              r.AsOf,
		},
	}
}

// requestFields builds the ordered field list. Order is significant: DOM
// order is how the qualification fixture derives tab order, so the sequence
// reads current state, then the proposal, then what the simulation made of
// it.
func requestFields(q Query, r Reading, messages map[string]string) []contract.RequestField {
	required := contract.FieldValidation{Required: true}
	withMessage := func(id string, v contract.FieldValidation) contract.FieldValidation {
		v.Message = messages[id]
		return v
	}
	fields := []contract.RequestField{
		{
			ID: FieldWorkerName, Label: "Worker",
			Kind: contract.FieldKindReadOnly, Value: workerName(r),
		},
		{
			ID: FieldCurrentJobTitle, Label: "Current job code",
			Kind: contract.FieldKindReadOnly, Value: factValue(r.Explanation, people.FieldJobCode),
		},
		{
			ID: FieldCurrentGrade, Label: "Current grade",
			Kind: contract.FieldKindReadOnly, Value: factValue(r.Explanation, people.FieldGrade),
		},
		{
			ID: FieldCurrentBasePay, Label: "Current base pay",
			Kind: contract.FieldKindReadOnly, Value: currentBasePay(r),
		},
		{
			ID: FieldProposedJobTitle, Label: "Proposed job code",
			Kind: contract.FieldKindLookup, Value: q.TargetJobCode,
			Validation: withMessage(FieldProposedJobTitle, required),
		},
		{
			ID: FieldProposedGrade, Label: "Proposed grade",
			Kind: contract.FieldKindText, Value: q.TargetGrade,
			Validation: withMessage(FieldProposedGrade, required),
		},
		{
			ID: FieldTargetPosition, Label: "Target position",
			Kind: contract.FieldKindReadOnly, Value: q.TargetPositionID,
		},
		{
			ID: FieldTargetOrgUnit, Label: "Target organization",
			Kind: contract.FieldKindReadOnly, Value: q.TargetOrgUnit,
		},
		{
			ID: FieldProposedComp, Label: "Proposed base pay",
			Kind: contract.FieldKindMoney, Value: q.ProposedBase,
			Validation: withMessage(FieldProposedComp, required),
		},
		{
			ID: FieldEffectiveDate, Label: "Effective date",
			Kind: contract.FieldKindDate, Value: q.EffectiveDate,
			Validation: withMessage(FieldEffectiveDate, required),
		},
		{
			ID: FieldBusinessReason, Label: "Business reason",
			Kind: contract.FieldKindTextarea, Value: q.BusinessReason,
			Validation: withMessage(FieldBusinessReason, required),
		},
		{
			ID: FieldBandPosition, Label: "Pay band position",
			Kind: contract.FieldKindReadOnly, Value: bandPosition(r),
		},
		{
			ID: FieldAnnualizedIncrease, Label: "Annualized increase",
			Kind: contract.FieldKindReadOnly, Value: annualizedIncrease(r),
		},
		{
			ID: FieldCompensationWithheld, Label: "Compensation disclosure",
			Kind:  contract.FieldKindReadOnly,
			Value: compensationNotice(r),
		},
	}
	return fields
}

// workerName is the disclosed name of the worker the promotion is about,
// paired with the entity id the governed read resolved.
//
// It is empty when neither name field was disclosed, and the field it fills
// is gated on exactly that disclosure - so a caller who may not learn the
// name does not get the identifier under a "Worker" label either, which
// would be the same disclosure wearing a different hat.
func workerName(r Reading) string {
	name := factValue(r.Explanation, people.FieldPreferredName)
	if name == "" {
		name = factValue(r.Explanation, people.FieldLegalName)
	}
	if name == "" {
		return ""
	}
	return name + " (" + r.Worker.Id + ")"
}

// currentBasePay formats the declared compensation baseline the simulation
// ran on. It reads the simulation's own projection rather than the submitted
// string, so the page shows the number the domain used.
func currentBasePay(r Reading) string {
	if !r.CompensationDisclosed {
		return ""
	}
	return r.Compensation.Current.AnnualizedBase.String()
}

// bandPosition states where the proposed amount sits in the governing band.
func bandPosition(r Reading) string {
	if !r.CompensationDisclosed {
		return ""
	}
	p := r.PayBand.Position
	if p.BandID == "" {
		return "no governing band resolved"
	}
	return fmt.Sprintf("%s@%s %s, compa-ratio %s (%s)",
		p.BandID, p.BandVersion, p.Placement, p.CompaRatio, r.PayBand.Outcome)
}

// annualizedIncrease states the annualized base movement the compensation
// simulation computed.
func annualizedIncrease(r Reading) string {
	if !r.CompensationDisclosed {
		return ""
	}
	return fmt.Sprintf("%s (%s%%)",
		r.Compensation.Delta.AnnualizedBase, r.Compensation.Delta.IncreasePercent)
}

// compensationNotice is what stands in for the compensation half when the
// policy withheld it. It names the refusal and never the withheld number.
func compensationNotice(r Reading) string {
	if r.CompensationDisclosed {
		return ""
	}
	reason := r.CompensationDenial
	if reason == "" {
		reason = "no compensation grant under the resolved purpose"
	}
	return "Compensation is not disclosed to this caller: " + reason +
		". The pay band, the compensation simulation and the promotion preflight were not run."
}

// validationMessages attaches each blocking preflight finding to the field a
// person can act on.
//
// Only findings that stop the promotion become field errors. An advisory
// finding is a thing to weigh, not a thing to fix, and rendering it as a
// validation error on a control would tell the reader to change a value that
// is not wrong.
func validationMessages(r Reading) map[string]string {
	out := map[string]string{}
	if !r.CompensationDisclosed {
		return out
	}
	for _, f := range r.Preflight.Blocking() {
		id := fieldForCode(f.Code)
		if id == "" {
			continue
		}
		if _, seen := out[id]; seen {
			continue
		}
		out[id] = f.Message
	}
	return out
}

// fieldForCode maps a promotion finding code onto the field whose value
// produced it, or "" when the finding is about the request as a whole.
func fieldForCode(code string) string {
	switch code {
	case promotion.CodeBusinessReasonRequired:
		return FieldBusinessReason
	case promotion.CodeEffectiveAtRequired, promotion.CodeEffectiveAtTooFarPast,
		promotion.CodeEffectiveBeforeHire:
		return FieldEffectiveDate
	case promotion.CodeTargetJobRequired:
		return FieldProposedJobTitle
	case promotion.CodeTargetGradeRequired, promotion.CodeSameGrade:
		return FieldProposedGrade
	}
	switch {
	case strings.HasPrefix(code, "compensation."),
		strings.HasPrefix(code, "promotion.pay_"),
		strings.HasPrefix(code, "promotion.budget_"):
		return FieldProposedComp
	}
	return ""
}

// preflightFindings projects the domain's typed findings onto the contract's
// severity vocabulary.
func preflightFindings(r Reading) []contract.PreflightFinding {
	if !r.CompensationDisclosed {
		return []contract.PreflightFinding{{
			ID:       "compensation-withheld",
			Severity: contract.SeverityBlocking,
			Label:    "Compensation withheld",
			Detail:   compensationNotice(r),
		}}
	}
	out := []contract.PreflightFinding{{
		ID:       "preflight-status",
		Severity: statusSeverity(r.Preflight.Status),
		Label:    "Preflight " + r.Preflight.Status.String(),
		Detail: fmt.Sprintf("evaluated under %s against rule pack %s",
			r.Preflight.PolicyVersion, r.Preflight.RulePackVersion),
	}}
	for _, f := range r.Preflight.Findings {
		out = append(out, contract.PreflightFinding{
			ID:       f.Code,
			Severity: findingSeverity(f.Severity),
			Label:    f.Code,
			Detail:   f.Message,
		})
	}
	return out
}

func statusSeverity(s promotion.Status) contract.Severity {
	switch s {
	case promotion.StatusReady:
		return contract.SeveritySuccess
	case promotion.StatusNeedsData:
		return contract.SeverityWarning
	default:
		return contract.SeverityBlocking
	}
}

func findingSeverity(s promotion.Severity) contract.Severity {
	switch s {
	case promotion.SeverityAdvisory:
		return contract.SeverityWarning
	case promotion.SeverityUnspecified:
		return contract.SeverityInfo
	default:
		return contract.SeverityBlocking
	}
}

// simulationResult is the status banner and the check list.
//
// The zero-effect line is not decoration: it reports the domain's own effect
// counters, which is the difference between a page that says nothing happened
// and a page that shows the receipt saying so.
func simulationResult(r Reading, approvals ApprovalTimeline) contract.SimulationResult {
	if !r.CompensationDisclosed {
		return contract.SimulationResult{
			Status:      contract.SimulationNeedsReview,
			Summary:     "Not simulated: the compensation half of this promotion is not disclosed to this caller.",
			GeneratedAt: r.AsOf,
			Checks: []contract.SimulationCheck{{
				Label:  "Authorization",
				Status: contract.SeverityBlocking,
				Detail: compensationNotice(r),
			}},
		}
	}

	checks := []contract.SimulationCheck{
		{
			Label:  "Preflight",
			Status: statusSeverity(r.Preflight.Status),
			Detail: "status " + r.Preflight.Status.String(),
		},
		{
			Label:  "Pay band",
			Status: bandSeverity(r.PayBand.Outcome),
			Detail: bandPosition(r),
		},
		{
			Label:  "Compensation",
			Status: contract.SeverityInfo,
			Detail: "annualized base moves by " + annualizedIncrease(r),
		},
		{
			Label:  "Zero effect",
			Status: contract.SeveritySuccess,
			Detail: zeroEffectDetail(r),
		},
	}
	if approvals.Terminal != "" {
		checks = append(checks, contract.SimulationCheck{
			Label:  "Approval route",
			Status: contract.SeverityWarning,
			Detail: fmt.Sprintf("the reference workflow reaches %s with outstanding %s (plan %s, receipt %s)",
				approvals.Terminal, strings.Join(approvals.Outstanding, ", "),
				approvals.PlanDigest, approvals.ReceiptDigest),
		})
	}

	status := contract.SimulationReady
	summary := "Simulation completed with no blocking findings."
	switch {
	case r.Preflight.Status == promotion.StatusNeedsData:
		status, summary = contract.SimulationNeedsReview, "Simulation needs data before it can be proposed."
	case r.Preflight.Status != promotion.StatusReady, !r.Simulation.Executable:
		status, summary = contract.SimulationFailed, "Simulation completed; the promotion is blocked as proposed."
	}
	return contract.SimulationResult{
		Status:      status,
		Summary:     summary,
		GeneratedAt: r.AsOf,
		Checks:      checks,
	}
}

func bandSeverity(outcome rewards.BandOutcome) contract.Severity {
	if outcome == rewards.BandOutcomeWithin {
		return contract.SeveritySuccess
	}
	return contract.SeverityWarning
}

// zeroEffectDetail restates the domain's own receipt.
func zeroEffectDetail(r Reading) string {
	counters := r.Simulation.Effects
	return fmt.Sprintf(
		"receipt %s in mode %s: %d domain writes, %d provider calls, %d outbox entries, %d work items",
		r.Simulation.ResultDigest, r.Simulation.Receipt.Mode,
		counters.DomainWrites, counters.ProviderCalls, counters.OutboxEntries, counters.WorkItems)
}
