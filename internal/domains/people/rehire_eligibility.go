package people

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrRehireEligibilityInvalid = errors.New("people: invalid rehire eligibility")

// RehireStatus is the closed rehire screening outcome vocabulary.
type RehireStatus string

const (
	RehireEligible    RehireStatus = "ELIGIBLE"
	RehireNotEligible RehireStatus = "NOT_ELIGIBLE"
	RehireConditional RehireStatus = "CONDITIONAL"
)

// RehireReason is the governed reason-code vocabulary. The raw code is
// disclosed only by the role-scoped recruiting projection.
type RehireReason string

const (
	RehireReasonGoodStanding RehireReason = "GOOD_STANDING"
	RehireReasonReduction    RehireReason = "REDUCTION_IN_FORCE"
	RehireReasonPerformance  RehireReason = "PERFORMANCE"
	RehireReasonMisconduct   RehireReason = "MISCONDUCT"
	RehireReasonPolicy       RehireReason = "POLICY_REVIEW"
	RehireReasonAgreement    RehireReason = "AGREEMENT_REQUIRED"
)

func (s RehireStatus) Valid() bool {
	switch s {
	case RehireEligible, RehireNotEligible, RehireConditional:
		return true
	default:
		return false
	}
}

func (r RehireReason) Valid() bool {
	switch r {
	case RehireReasonGoodStanding, RehireReasonReduction, RehireReasonPerformance,
		RehireReasonMisconduct, RehireReasonPolicy, RehireReasonAgreement:
		return true
	default:
		return false
	}
}

func (r RehireReason) validFor(status RehireStatus) bool {
	switch status {
	case RehireEligible:
		return r == RehireReasonGoodStanding || r == RehireReasonReduction
	case RehireNotEligible:
		return r == RehireReasonPerformance || r == RehireReasonMisconduct
	case RehireConditional:
		return r == RehireReasonPolicy || r == RehireReasonAgreement
	default:
		return false
	}
}

// RehireEligibility is an immutable, versioned status recorded at exit
// completion. A later correction is a new record with a higher revision.
type RehireEligibility struct {
	Worker       values.EntityRef
	Employment   values.EntityRef
	Status       RehireStatus
	Reason       RehireReason
	EffectiveAt  values.LocalDate
	RecordedAt   values.LocalDate
	Revision     uint64
	AuthorityRef string
}

func (r RehireEligibility) Validate() error {
	if err := r.Worker.Validate(); err != nil || r.Worker.Kind != "worker" {
		return fmt.Errorf("%w: worker reference is invalid", ErrRehireEligibilityInvalid)
	}
	if err := r.Employment.Validate(); err != nil || r.Employment.Kind != "employment" || r.Employment.Tenant != r.Worker.Tenant {
		return fmt.Errorf("%w: employment reference is invalid", ErrRehireEligibilityInvalid)
	}
	if !r.Status.Valid() || !r.Reason.validFor(r.Status) {
		return fmt.Errorf("%w: status and reason code are not a declared combination", ErrRehireEligibilityInvalid)
	}
	if strings.TrimSpace(r.AuthorityRef) == "" || r.Revision == 0 {
		return fmt.Errorf("%w: authority and positive revision are required", ErrRehireEligibilityInvalid)
	}
	if err := r.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective date: %v", ErrRehireEligibilityInvalid, err)
	}
	if err := r.RecordedAt.Validate(); err != nil {
		return fmt.Errorf("%w: recorded date: %v", ErrRehireEligibilityInvalid, err)
	}
	return nil
}

func (r RehireEligibility) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.people.RehireEligibility", 1).
		Value("worker", r.Worker).Value("employment", r.Employment).
		String("status", string(r.Status)).String("reason", string(r.Reason)).
		Value("effective_at", r.EffectiveAt).Value("recorded_at", r.RecordedAt).
		Int("revision", int64(r.Revision)).String("authority_ref", r.AuthorityRef).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// RehireEligibilityHistory is an append-only history for one worker.
type RehireEligibilityHistory struct {
	Worker  values.EntityRef
	Records []RehireEligibility
}

// AppendRehireEligibility preserves every prior record and requires a
// monotonic revision for the same worker.
func AppendRehireEligibility(history RehireEligibilityHistory, record RehireEligibility) (RehireEligibilityHistory, error) {
	if err := record.Validate(); err != nil {
		return RehireEligibilityHistory{}, err
	}
	for i, prior := range history.Records {
		if err := prior.Validate(); err != nil {
			return RehireEligibilityHistory{}, fmt.Errorf("%w: prior revision %d: %v", ErrRehireEligibilityInvalid, i+1, err)
		}
		if prior.Worker != history.Worker || prior.Revision != uint64(i+1) {
			return RehireEligibilityHistory{}, fmt.Errorf("%w: existing history is inconsistent", ErrRehireEligibilityInvalid)
		}
	}
	if history.Worker != (values.EntityRef{}) && history.Worker != record.Worker {
		return RehireEligibilityHistory{}, fmt.Errorf("%w: worker changed within history", ErrRehireEligibilityInvalid)
	}
	if record.Revision != uint64(len(history.Records)+1) {
		return RehireEligibilityHistory{}, fmt.Errorf("%w: revision must append the next history entry", ErrRehireEligibilityInvalid)
	}
	next := RehireEligibilityHistory{Worker: record.Worker, Records: append([]RehireEligibility(nil), history.Records...)}
	next.Records = append(next.Records, record)
	return next, nil
}
