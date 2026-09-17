// BEN-006: process qualifying life events.
//
// ProcessQLE maps one life event (type, date, evidence, reporting delay)
// onto the permitted election delta. Late, missing or uncertain evidence
// never rewrites coverage: it creates REVIEW_REQUIRED and preserves the
// existing election. The function is kernel-pure.
package benefits

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// LifeEventType is the closed BEN-006 event vocabulary.
type LifeEventType string

const (
	LifeEventBirth          LifeEventType = "BIRTH"
	LifeEventAdoption       LifeEventType = "ADOPTION"
	LifeEventMarriage       LifeEventType = "MARRIAGE"
	LifeEventDivorce        LifeEventType = "DIVORCE"
	LifeEventDependentDeath LifeEventType = "DEPENDENT_DEATH"
	LifeEventLossOfCoverage LifeEventType = "LOSS_OF_COVERAGE"
	LifeEventGainOfCoverage LifeEventType = "GAIN_OF_COVERAGE"
)

// Valid reports whether the event type is declared.
func (e LifeEventType) Valid() bool {
	switch e {
	case LifeEventBirth, LifeEventAdoption, LifeEventMarriage, LifeEventDivorce,
		LifeEventDependentDeath, LifeEventLossOfCoverage, LifeEventGainOfCoverage:
		return true
	default:
		return false
	}
}

// EvidenceState is the verification state of the life-event evidence.
type EvidenceState string

const (
	EvidenceProvided  EvidenceState = "PROVIDED"
	EvidencePending   EvidenceState = "PENDING"
	EvidenceUncertain EvidenceState = "UNCERTAIN"
	EvidenceMissing   EvidenceState = "MISSING"
)

// Valid reports whether the evidence state is declared.
func (s EvidenceState) Valid() bool {
	switch s {
	case EvidenceProvided, EvidencePending, EvidenceUncertain, EvidenceMissing:
		return true
	default:
		return false
	}
}

// ElectionDelta is the permitted election change for one event.
type ElectionDelta string

const (
	DeltaAddDependent    ElectionDelta = "ADD_DEPENDENT"
	DeltaRemoveDependent ElectionDelta = "REMOVE_DEPENDENT"
	DeltaChangeTier      ElectionDelta = "CHANGE_TIER"
	DeltaDropCoverage    ElectionDelta = "DROP_COVERAGE"
	DeltaNone            ElectionDelta = "NONE"
)

// QLEStatus is the processing outcome. REVIEW_REQUIRED preserves existing
// coverage; it never silently permits or denies.
type QLEStatus string

const (
	QLEPermitted      QLEStatus = "PERMITTED"
	QLEReviewRequired QLEStatus = "REVIEW_REQUIRED"
	QLEDenied         QLEStatus = "DENIED"
)

var (
	// ErrQLERejected is the BEN-006 sentinel for malformed event input.
	ErrQLERejected = errors.New("BEN_006_REJECTED")
)

// QLERejection is the stable BEN-006 failure shape.
type QLERejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *QLERejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrQLERejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the BEN_006_REJECTED sentinel to errors.Is.
func (r *QLERejection) Unwrap() error { return ErrQLERejected }

func qleReject(field, state, reason string) error {
	return &QLERejection{Field: field, State: state, Version: ElectionVersion, Reason: reason}
}

// QLEInput is one qualifying-life-event report. WindowDays is the plan
// election window in days after the event; ReportDate is when the worker
// reported it.
type QLEInput struct {
	Tenant        string
	WorkerRef     string
	Event         LifeEventType
	EventDate     time.Time
	ReportDate    time.Time
	Evidence      EvidenceState
	EvidenceRef   string
	WindowDays    int
	PreservedTier TierCode
	DependentRef  string
}

// QLEDecision is the deterministic processing outcome. WindowStart/End is
// the permitted election window; Delta is the only permitted change;
// PreservedTier names the coverage that stays in force pending review.
type QLEDecision struct {
	Status        QLEStatus
	Delta         ElectionDelta
	WindowStart   time.Time
	WindowEnd     time.Time
	PreservedTier TierCode
	Reason        string
	Digest        string
}

func (d QLEDecision) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.benefits.QLEDecision", 1).
		String("status", string(d.Status)).
		String("delta", string(d.Delta)).
		String("window_start", d.WindowStart.UTC().Format(time.RFC3339)).
		String("window_end", d.WindowEnd.UTC().Format(time.RFC3339)).
		String("preserved_tier", string(d.PreservedTier)).
		String("reason", d.Reason)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func eventDelta(event LifeEventType) ElectionDelta {
	switch event {
	case LifeEventBirth, LifeEventAdoption:
		return DeltaAddDependent
	case LifeEventMarriage:
		return DeltaChangeTier
	case LifeEventDivorce, LifeEventDependentDeath:
		return DeltaRemoveDependent
	case LifeEventGainOfCoverage:
		return DeltaDropCoverage
	case LifeEventLossOfCoverage:
		return DeltaChangeTier
	default:
		return DeltaNone
	}
}

// ProcessQLE determines the permitted election delta for one reported life
// event. Late reporting or missing/uncertain evidence yields
// REVIEW_REQUIRED with existing coverage preserved. An undeclared event
// type or malformed report is refused.
func ProcessQLE(in QLEInput) (QLEDecision, error) {
	if strings.TrimSpace(in.Tenant) == "" {
		return QLEDecision{}, qleReject("qle.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(in.WorkerRef) == "" {
		return QLEDecision{}, qleReject("qle.worker_ref", "MISSING", "worker ref is required")
	}
	if !in.Event.Valid() {
		return QLEDecision{}, qleReject("qle.event", "UNDECLARED", fmt.Sprintf("event %q is not declared", in.Event))
	}
	if !in.Evidence.Valid() {
		return QLEDecision{}, qleReject("qle.evidence", "UNDECLARED", fmt.Sprintf("evidence state %q is not declared", in.Evidence))
	}
	if in.EventDate.IsZero() || in.ReportDate.IsZero() {
		return QLEDecision{}, qleReject("qle.dates", "MISSING", "event and report dates are required")
	}
	if in.ReportDate.Before(in.EventDate) {
		return QLEDecision{}, qleReject("qle.report_date", "IMPOSSIBLE", "report cannot precede the event")
	}
	if in.WindowDays <= 0 {
		return QLEDecision{}, qleReject("qle.window_days", "INVALID", "election window must be positive")
	}
	if (in.Event == LifeEventBirth || in.Event == LifeEventAdoption || in.Event == LifeEventMarriage) &&
		strings.TrimSpace(in.DependentRef) == "" && in.Evidence == EvidenceProvided {
		return QLEDecision{}, qleReject("qle.dependent_ref", "MISSING", "gaining-coverage events name the dependent")
	}
	start := in.EventDate.UTC()
	end := start.AddDate(0, 0, in.WindowDays)
	decision := QLEDecision{
		Delta: eventDelta(in.Event), WindowStart: start, WindowEnd: end,
		PreservedTier: in.PreservedTier,
	}
	switch {
	case in.ReportDate.After(end):
		decision.Status = QLEReviewRequired
		decision.Delta = DeltaNone
		decision.Reason = "reported after the election window: existing coverage preserved"
	case in.Evidence == EvidenceMissing || in.Evidence == EvidenceUncertain:
		decision.Status = QLEReviewRequired
		decision.Delta = DeltaNone
		decision.Reason = "evidence missing or uncertain: existing coverage preserved"
	case in.Evidence == EvidencePending:
		decision.Status = QLEReviewRequired
		decision.Reason = "evidence pending: conditional window held, coverage preserved"
	default:
		decision.Status = QLEPermitted
		decision.Reason = "event, window and evidence permit the election delta"
	}
	decision.Digest = decision.computedDigest()
	return decision, nil
}
