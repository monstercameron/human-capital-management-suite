// Reevaluation on credential/fact expiry: QUAL-005 compares a fresh
// evaluation against a prior one for the same requirement revision and
// emits exactly one reevaluation record.
//
// A regression emits a bounded follow-up intent per changed ref — a
// WARNING for newly expiring evidence, a RESTRICTION for newly missing
// evidence, and a REMOVAL_PROPOSAL once warned evidence lapses. Removal
// is always a proposal requiring approval: this package executes no
// irreversible action. An unchanged evaluation records Trigger NONE
// with zero intents rather than silently continuing the prior.
// Reevaluation is pure: malformed inputs reject with QUAL_005_REJECTED
// naming field/version and persist nothing.
package qualification

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ReevaluationRejectedCode is the stable machine-readable refusal code.
const ReevaluationRejectedCode = "QUAL_005_REJECTED"

// ReevaluationRejectedError names the offending field and version.
type ReevaluationRejectedError struct {
	Code    string
	Field   string
	State   string
	Version int
}

// Error implements error.
func (e *ReevaluationRejectedError) Error() string {
	return e.Code + ": field " + e.Field + " state " + e.State
}

// AsReevaluationRejected unwraps a QUAL_005_REJECTED refusal.
func AsReevaluationRejected(err error) (*ReevaluationRejectedError, bool) {
	if err == nil {
		return nil, false
	}
	if rejected, ok := err.(*ReevaluationRejectedError); ok && rejected.Code == ReevaluationRejectedCode {
		return rejected, true
	}
	return nil, false
}

func reevaluationRejected(field, state string) *ReevaluationRejectedError {
	return &ReevaluationRejectedError{Code: ReevaluationRejectedCode, Field: field, State: state, Version: schemaVersion}
}

// ReevaluationTrigger is the only QUAL-005 trigger vocabulary.
type ReevaluationTrigger string

// The reevaluation triggers.
const (
	TriggerNone       ReevaluationTrigger = "NONE"
	TriggerExpiry     ReevaluationTrigger = "EXPIRY"
	TriggerFactChange ReevaluationTrigger = "FACT_CHANGE"
)

func (t ReevaluationTrigger) Valid() bool {
	return t == TriggerNone || t == TriggerExpiry || t == TriggerFactChange
}

// FollowUpKind is the only bounded follow-up vocabulary. Every kind is
// descriptive: WARNING and RESTRICTION bound what a caller may do next,
// and REMOVAL_PROPOSAL always requires approval before anything ends.
type FollowUpKind string

// The follow-up intents.
const (
	FollowUpWarning         FollowUpKind = "WARNING"
	FollowUpRestriction     FollowUpKind = "RESTRICTION"
	FollowUpRemovalProposal FollowUpKind = "REMOVAL_PROPOSAL"
)

func (k FollowUpKind) Valid() bool {
	return k == FollowUpWarning || k == FollowUpRestriction || k == FollowUpRemovalProposal
}

// FollowUpIntent is one bounded, reversible follow-up. Reason carries a
// stable token and the requirement ref only — never evidence content.
type FollowUpIntent struct {
	Kind             FollowUpKind
	Ref              string
	Reason           string
	RequiresApproval bool
}

// Reevaluation is the single detached record of one expiry check.
type Reevaluation struct {
	RequirementID   string
	Revision        uint64
	PriorDigest     string
	AsOf            values.Instant
	Trigger         ReevaluationTrigger
	ChangedRefs     []string
	Evaluation      Evaluation
	Intents         []FollowUpIntent
	CanonicalDigest string
}

func (r Reevaluation) body() []byte {
	changed := append([]string(nil), r.ChangedRefs...)
	sort.Strings(changed)
	intents := append([]FollowUpIntent(nil), r.Intents...)
	sort.Slice(intents, func(i, j int) bool {
		if intents[i].Ref != intents[j].Ref {
			return intents[i].Ref < intents[j].Ref
		}
		return intents[i].Kind < intents[j].Kind
	})
	w := canonicalbytes.New("hcmnext.domains.qualification.Reevaluation", schemaVersion).
		String("requirement_id", r.RequirementID).Int("revision", int64(r.Revision)).
		String("prior_digest", r.PriorDigest).Value("as_of", r.AsOf).
		String("trigger", string(r.Trigger)).SortedStrings("changed_ref", changed).
		Count("intents", len(intents))
	for _, intent := range intents {
		w.String("intent.kind", string(intent.Kind)).String("intent.ref", intent.Ref).
			String("intent.reason", intent.Reason).Bool("intent.requires_approval", intent.RequiresApproval)
	}
	w.Count("results", len(r.Evaluation.Results))
	for _, result := range r.Evaluation.Results {
		w.String("result.kind", string(result.Kind)).String("result.ref", result.Ref).
			Int("result.required_level", int64(result.RequiredLevel)).String("result.status", string(result.Status)).
			String("result.gap", result.Gap)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Explain renders the bounded human-readable account: trigger, change
// counts and intent counts only — never evidence content.
func (r Reevaluation) Explain() string {
	return fmt.Sprintf("reevaluation %s: changed=%d intents=%d", r.Trigger, len(r.ChangedRefs), len(r.Intents))
}

// Reevaluate runs one expiry check: it evaluates requirement against
// held at asOf, diffs the result against prior, and binds the bounded
// follow-up intents. It is pure: no rows, events, outbox entries,
// human work or provider requests.
func Reevaluate(requirement QualificationRequirement, prior Evaluation, held []HeldCredential, asOf values.Instant) (Reevaluation, error) {
	if err := requirement.Validate(); err != nil {
		return Reevaluation{}, reevaluationRejected("requirement", "invalid")
	}
	if err := prior.Validate(); err != nil {
		return Reevaluation{}, reevaluationRejected("prior", "invalid")
	}
	if prior.RequirementID != requirement.RequirementID || prior.Revision != requirement.Revision {
		return Reevaluation{}, reevaluationRejected("prior", "requirement-mismatch")
	}
	if err := asOf.Validate(); err != nil {
		return Reevaluation{}, reevaluationRejected("as-of", "invalid")
	}
	fresh, err := requirement.Evaluate(held)
	if err != nil {
		return Reevaluation{}, reevaluationRejected("held", "invalid")
	}
	priorByRef := make(map[string]Status, len(prior.Results))
	for _, result := range prior.Results {
		priorByRef[result.Ref] = result.Status
	}
	reevaluation := Reevaluation{
		RequirementID: requirement.RequirementID, Revision: requirement.Revision,
		PriorDigest: prior.CanonicalDigest, AsOf: asOf, Trigger: TriggerNone,
		Evaluation: fresh,
	}
	regressed := false
	for _, result := range fresh.Results {
		previous, ok := priorByRef[result.Ref]
		if !ok {
			reevaluation.ChangedRefs = append(reevaluation.ChangedRefs, result.Ref)
			regressed = true
			reevaluation.Intents = append(reevaluation.Intents, FollowUpIntent{Kind: FollowUpRestriction, Ref: result.Ref, Reason: "new-requirement-unsatisfied"})
			continue
		}
		if previous == result.Status {
			continue
		}
		reevaluation.ChangedRefs = append(reevaluation.ChangedRefs, result.Ref)
		switch {
		case result.Status == StatusExpiring && previous == StatusSatisfied:
			regressed = true
			reevaluation.Intents = append(reevaluation.Intents, FollowUpIntent{Kind: FollowUpWarning, Ref: result.Ref, Reason: "expiring-validity"})
		case result.Status == StatusUnsatisfied && previous == StatusExpiring:
			regressed = true
			reevaluation.Intents = append(reevaluation.Intents, FollowUpIntent{Kind: FollowUpRemovalProposal, Ref: result.Ref, Reason: "lapsed-after-warning", RequiresApproval: true})
		case result.Status == StatusUnsatisfied:
			regressed = true
			reevaluation.Intents = append(reevaluation.Intents, FollowUpIntent{Kind: FollowUpRestriction, Ref: result.Ref, Reason: "missing-evidence"})
		}
	}
	sort.Strings(reevaluation.ChangedRefs)
	sort.Slice(reevaluation.Intents, func(i, j int) bool {
		if reevaluation.Intents[i].Ref != reevaluation.Intents[j].Ref {
			return reevaluation.Intents[i].Ref < reevaluation.Intents[j].Ref
		}
		return reevaluation.Intents[i].Kind < reevaluation.Intents[j].Kind
	})
	switch {
	case regressed:
		reevaluation.Trigger = TriggerExpiry
	case len(reevaluation.ChangedRefs) > 0:
		reevaluation.Trigger = TriggerFactChange
	}
	reevaluation.CanonicalDigest = canonicalbytes.Digest(reevaluation.body())
	return reevaluation, nil
}
