// Package presentation compiles lifecycle dimensions into a truthful,
// participant-facing projection. It never owns lifecycle or authorization.
package presentation

import (
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"slices"
)

type Authorization string

const (
	AuthorizationUnknown Authorization = "UNKNOWN"
	AuthorizationAllowed Authorization = "ALLOWED"
	AuthorizationDenied  Authorization = "DENIED"
	AuthorizationExpired Authorization = "EXPIRED"
)

type Freshness string

const (
	FreshnessUnknown Freshness = "UNKNOWN"
	FreshnessFresh   Freshness = "FRESH"
	FreshnessStale   Freshness = "STALE"
	FreshnessPartial Freshness = "PARTIAL"
)

type OperationalState string

const (
	OperationalReady    OperationalState = "READY"
	OperationalLoading  OperationalState = "LOADING"
	OperationalWaiting  OperationalState = "WAITING"
	OperationalDegraded OperationalState = "DEGRADED"
	OperationalUnknown  OperationalState = "UNKNOWN"
	OperationalFailed   OperationalState = "FAILED"
)

type State string

const (
	StateLoading           State = "LOADING"
	StateEmpty             State = "EMPTY"
	StateDraft             State = "DRAFT"
	StateValidationBlocked State = "VALIDATION_BLOCKED"
	StateGovernanceDenied  State = "GOVERNANCE_DENIED"
	StateSimulationReady   State = "SIMULATION_READY"
	StateSubmitted         State = "SUBMITTED"
	StateRunning           State = "RUNNING"
	StateWaiting           State = "WAITING"
	StateStaleReplan       State = "STALE_REPLAN"
	StatePartialDegraded   State = "PARTIAL_DEGRADED"
	StateUnknownAmbiguous  State = "UNKNOWN_AMBIGUOUS"
	StateRepairRequired    State = "REPAIR_REQUIRED"
	StateCompleted         State = "COMPLETED"
	StateCancelled         State = "CANCELLED"
	StateCorrected         State = "CORRECTED"
	StateSuperseded        State = "SUPERSEDED"
)

type Action string

const (
	ActionWait         Action = "WAIT"
	ActionRefresh      Action = "REFRESH"
	ActionRequestHelp  Action = "REQUEST_HELP"
	ActionCancelIfSafe Action = "CANCEL_IF_SAFE"
	ActionCorrect      Action = "CORRECT"
	ActionOpenRepair   Action = "OPEN_REPAIR"
	ActionReview       Action = "REVIEW"
	ActionSubmit       Action = "SUBMIT"
	ActionSaveDraft    Action = "SAVE_DRAFT"
	ActionReplan       Action = "REPLAN"
	ActionAppeal       Action = "APPEAL"
	ActionInvestigate  Action = "INVESTIGATE"
)

// Input contains every truth-bearing dimension used by this projection.
type Input struct {
	Dimensions           lifecycle.Dimensions
	Authorization        Authorization
	Freshness            Freshness
	Operational          OperationalState
	HasResult            bool
	HasValidationErrors  bool
	HasSimulation        bool
	HasEvidence          bool
	ExternalOutcomeKnown bool
	EvidenceRefs         []string
	Locale               string
}

type ActionDescriptor struct {
	Action  Action
	Enabled bool
	Reason  string
}
type Presentation struct {
	State        State
	Locale       string
	StateLabel   string
	Description  string
	Dimensions   lifecycle.Dimensions
	EvidenceRefs []string
	Actions      []ActionDescriptor
}

// Resolve is deterministic and copies all caller-owned slices.
func Resolve(in Input) Presentation {
	locale := normalizeLocale(in.Locale)
	state := stateFor(in)
	p := Presentation{State: state, Locale: locale, Dimensions: in.Dimensions, EvidenceRefs: slices.Clone(in.EvidenceRefs)}
	p.StateLabel, p.Description = localizedState(state, locale)
	if state == StateGovernanceDenied || in.Dimensions.Validate() != nil {
		p.Dimensions = lifecycle.Dimensions{}
		p.EvidenceRefs = nil
		if state != StateGovernanceDenied {
			p.State = StateUnknownAmbiguous
			p.StateLabel, p.Description = localizedState(p.State, locale)
		}
		return p
	}
	for _, a := range actionsFor(p.State) {
		d := ActionDescriptor{Action: a, Enabled: true}
		if in.Authorization != AuthorizationAllowed {
			d.Enabled = false
			d.Reason = "Action unavailable until authorization and current evidence are established"
		} else if in.Freshness != FreshnessFresh && !safeWhenStale(a) {
			d.Enabled = false
			d.Reason = "Action unavailable until authorization and current evidence are established"
		} else if in.StateActionBlocked(a) {
			d.Enabled = false
			d.Reason = "Action is not safe in the current state"
		}
		p.Actions = append(p.Actions, d)
	}
	return p
}

func (in Input) StateActionBlocked(a Action) bool {
	if a == ActionSubmit && in.HasValidationErrors {
		return true
	}
	if a == ActionCancelIfSafe && in.Dimensions.Execution != lifecycle.ExecutionScheduled && in.Dimensions.Execution != lifecycle.ExecutionNotPlanned {
		return true
	}
	if a == ActionRefresh && in.Operational == OperationalLoading {
		return true
	}
	return false
}

func stateFor(in Input) State {
	if in.Authorization != AuthorizationAllowed {
		return StateGovernanceDenied
	}
	if !in.Dimensions.Request.Valid() || !in.Dimensions.Execution.Valid() || !in.Dimensions.Business.Valid() || !in.Dimensions.Consistency.Valid() || !in.Dimensions.Obligation.Valid() {
		return StateUnknownAmbiguous
	}
	if in.Operational == OperationalLoading {
		return StateLoading
	}
	if in.Operational == OperationalFailed || in.Operational == OperationalUnknown || in.Freshness == FreshnessUnknown {
		return StateUnknownAmbiguous
	}
	if in.Operational == OperationalDegraded {
		return StatePartialDegraded
	}
	if in.Operational == OperationalWaiting {
		return StateWaiting
	}
	if in.Freshness == FreshnessStale {
		return StateStaleReplan
	}
	if in.Freshness == FreshnessPartial || in.Dimensions.Consistency == lifecycle.ConsistencyDegraded {
		return StatePartialDegraded
	}
	if in.Dimensions.Business == lifecycle.BusinessUnknown || in.Dimensions.Consistency == lifecycle.ConsistencyUnknown || in.Dimensions.Obligation == lifecycle.ObligationUnknown || !in.ExternalOutcomeKnown && in.Dimensions.Execution == lifecycle.ExecutionCommitted {
		return StateUnknownAmbiguous
	}
	if in.Dimensions.Execution == lifecycle.ExecutionRepairRequired || in.Dimensions.Consistency == lifecycle.ConsistencyRepairing {
		return StateRepairRequired
	}
	if in.HasValidationErrors {
		return StateValidationBlocked
	}
	if in.HasSimulation {
		return StateSimulationReady
	}
	if in.Dimensions.Execution == lifecycle.ExecutionExecuting || in.Dimensions.Execution == lifecycle.ExecutionRevalidating {
		return StateRunning
	}
	if in.Dimensions.Consistency == lifecycle.ConsistencyPendingObservation || in.Dimensions.Obligation == lifecycle.ObligationPending || in.Dimensions.Obligation == lifecycle.ObligationOverdue {
		return StateWaiting
	}
	switch in.Dimensions.Request {
	case lifecycle.RequestUnspecified:
		return StateEmpty
	case lifecycle.RequestDraft:
		if !in.HasResult {
			return StateEmpty
		}
		return StateDraft
	case lifecycle.RequestSubmitted:
		return StateSubmitted
	case lifecycle.RequestClosed:
		if in.Dimensions.Business == lifecycle.BusinessCompleted &&
			(in.Dimensions.Consistency == lifecycle.ConsistencyConsistent || in.Dimensions.Consistency == lifecycle.ConsistencyNotApplicable) &&
			(in.Dimensions.Obligation == lifecycle.ObligationSatisfied || in.Dimensions.Obligation == lifecycle.ObligationWaived || in.Dimensions.Obligation == lifecycle.ObligationNotApplicable) {
			return StateCompleted
		}
		return StateUnknownAmbiguous
	case lifecycle.RequestCancelled:
		return StateCancelled
	case lifecycle.RequestSuperseded:
		return StateSuperseded
	case lifecycle.RequestReopened:
		return StateCorrected
	}
	if !in.HasResult {
		return StateEmpty
	}
	return StateSubmitted
}

func normalizeLocale(locale string) string {
	switch locale {
	case "de", "de-DE":
		return "de-DE"
	case "ar":
		return "ar"
	default:
		return "en-US"
	}
}

func localizedState(state State, locale string) (string, string) {
	labels := map[State]map[string][2]string{
		StateLoading:           {"en-US": {"Loading", "The current state is being resolved."}, "de-DE": {"Wird geladen", "Der aktuelle Status wird ermittelt."}, "ar": {"جارٍ التحميل", "يجري التحقق من الحالة الحالية."}},
		StateEmpty:             {"en-US": {"No current item", "No permitted result or action is available."}, "de-DE": {"Kein aktueller Eintrag", "Es ist kein zulässiges Ergebnis oder keine Aktion verfügbar."}, "ar": {"لا يوجد عنصر حالي", "لا توجد نتيجة أو عملية متاحة."}},
		StateDraft:             {"en-US": {"Draft", "This request is still a draft."}, "de-DE": {"Entwurf", "Diese Anfrage ist noch ein Entwurf."}, "ar": {"مسودة", "لا يزال هذا الطلب مسودة."}},
		StateValidationBlocked: {"en-US": {"Changes needed", "Correct the listed issues before continuing."}, "de-DE": {"Änderungen erforderlich", "Beheben Sie die aufgeführten Probleme, bevor Sie fortfahren."}, "ar": {"يلزم إجراء تغييرات", "صحح المشكلات المذكورة قبل المتابعة."}},
		StateGovernanceDenied:  {"en-US": {"Unavailable", "This item or action is unavailable."}, "de-DE": {"Nicht verfügbar", "Dieser Eintrag oder diese Aktion ist nicht verfügbar."}, "ar": {"غير متاح", "هذا العنصر أو الإجراء غير متاح."}},
		StateSimulationReady:   {"en-US": {"Review ready", "Review the expected changes and remaining uncertainty."}, "de-DE": {"Prüfung bereit", "Prüfen Sie die erwarteten Änderungen und verbleibenden Unsicherheiten."}, "ar": {"جاهز للمراجعة", "راجع التغييرات المتوقعة وما تبقى من عدم اليقين."}},
		StateSubmitted:         {"en-US": {"Submitted", "The request was submitted."}, "de-DE": {"Eingereicht", "Die Anfrage wurde eingereicht."}, "ar": {"تم الإرسال", "تم إرسال الطلب."}},
		StateRunning:           {"en-US": {"In progress", "The request is being processed."}, "de-DE": {"In Bearbeitung", "Die Anfrage wird verarbeitet."}, "ar": {"قيد التنفيذ", "يجري معالجة الطلب."}},
		StateWaiting:           {"en-US": {"Waiting", "An observation or obligation remains open."}, "de-DE": {"Wartet", "Eine Beobachtung oder Verpflichtung ist noch offen."}, "ar": {"بانتظار", "لا تزال هناك ملاحظة أو التزام مفتوح."}},
		StateStaleReplan:       {"en-US": {"Refresh required", "The information changed; review the current state before acting."}, "de-DE": {"Aktualisierung erforderlich", "Die Informationen haben sich geändert. Prüfen Sie den aktuellen Status."}, "ar": {"يلزم التحديث", "تغيرت المعلومات؛ راجع الحالة الحالية قبل اتخاذ إجراء."}},
		StatePartialDegraded:   {"en-US": {"Partially complete", "Some effects are known; reconciliation or repair may remain."}, "de-DE": {"Teilweise abgeschlossen", "Einige Auswirkungen sind bekannt; Abgleich oder Reparatur kann noch nötig sein."}, "ar": {"مكتمل جزئياً", "بعض النتائج معروفة؛ قد تظل المطابقة أو الإصلاح مطلوباً."}},
		StateUnknownAmbiguous:  {"en-US": {"Outcome unknown", "The outcome is not established. Do not repeat the effect."}, "de-DE": {"Ergebnis unbekannt", "Das Ergebnis ist nicht bestätigt. Wiederholen Sie den Vorgang nicht."}, "ar": {"النتيجة غير معروفة", "لم يتم تأكيد النتيجة. لا تكرر العملية."}},
		StateRepairRequired:    {"en-US": {"Repair required", "Observed and expected results differ."}, "de-DE": {"Reparatur erforderlich", "Beobachtetes und erwartetes Ergebnis unterscheiden sich."}, "ar": {"يلزم الإصلاح", "تختلف النتيجة المرصودة عن المتوقعة."}},
		StateCompleted:         {"en-US": {"Business outcome complete", "Business outcome is complete; review consistency and obligations below."}, "de-DE": {"Geschäftsergebnis abgeschlossen", "Das Geschäftsergebnis ist abgeschlossen; prüfen Sie unten Konsistenz und Verpflichtungen."}, "ar": {"اكتملت نتيجة العمل", "اكتملت نتيجة العمل؛ راجع الاتساق والالتزامات أدناه."}},
		StateCancelled:         {"en-US": {"Cancelled", "The request was cancelled; its history remains available."}, "de-DE": {"Abgebrochen", "Die Anfrage wurde abgebrochen; ihr Verlauf bleibt verfügbar."}, "ar": {"تم الإلغاء", "تم إلغاء الطلب؛ يظل سجله متاحاً."}},
		StateCorrected:         {"en-US": {"Corrected", "A correction was recorded; review the history."}, "de-DE": {"Korrigiert", "Eine Korrektur wurde erfasst; prüfen Sie den Verlauf."}, "ar": {"تم التصحيح", "تم تسجيل تصحيح؛ راجع السجل."}},
		StateSuperseded:        {"en-US": {"Superseded", "A successor request replaced this one."}, "de-DE": {"Abgelöst", "Eine Folgeanfrage hat diese ersetzt."}, "ar": {"تم الاستبدال", "حل طلب لاحق محل هذا الطلب."}},
	}
	entry := labels[state]
	value, ok := entry[locale]
	if !ok {
		value = entry["en-US"]
	}
	return value[0], value[1]
}

func safeWhenStale(a Action) bool {
	switch a {
	case ActionWait, ActionRefresh, ActionRequestHelp, ActionReview, ActionInvestigate, ActionOpenRepair, ActionReplan:
		return true
	}
	return false
}

func actionsFor(s State) []Action {
	switch s {
	case StateLoading:
		return []Action{ActionWait, ActionCancelIfSafe}
	case StateEmpty:
		return []Action{ActionRequestHelp}
	case StateDraft:
		return []Action{ActionSaveDraft, ActionSubmit}
	case StateValidationBlocked:
		return []Action{ActionCorrect, ActionSaveDraft}
	case StateGovernanceDenied:
		return nil
	case StateSimulationReady:
		return []Action{ActionSubmit, ActionReview}
	case StateSubmitted, StateRunning:
		return []Action{ActionWait, ActionCancelIfSafe, ActionReview}
	case StateWaiting:
		return []Action{ActionWait, ActionRefresh, ActionRequestHelp}
	case StateStaleReplan:
		return []Action{ActionRefresh, ActionReplan, ActionRequestHelp}
	case StatePartialDegraded:
		return []Action{ActionRefresh, ActionOpenRepair, ActionRequestHelp}
	case StateUnknownAmbiguous:
		return []Action{ActionInvestigate, ActionRefresh, ActionRequestHelp}
	case StateRepairRequired:
		return []Action{ActionOpenRepair, ActionReview, ActionRequestHelp}
	case StateCompleted:
		return []Action{ActionReview, ActionCorrect}
	case StateCancelled, StateCorrected, StateSuperseded:
		return []Action{ActionReview, ActionRequestHelp}
	default:
		return nil
	}
}

// Actions returns a defensive copy of the action descriptors.
func (p Presentation) ActionsCopy() []ActionDescriptor { return slices.Clone(p.Actions) }
