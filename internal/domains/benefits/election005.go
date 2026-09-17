// BEN-005: record election revisions append-only.
//
// RecordElection binds one election to plan, tier, dependents, effective
// date, cost and waiver evidence under a canonical digest. A revision never
// overwrites its predecessor: the caller supplies the head digest it
// observed, and a stale head or a changed replay is refused with
// BEN_005_REJECTED. The function is kernel-pure: it keeps no state and
// emits no event, outbox entry, work item or provider request.
package benefits

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ElectionVersion is the rejection version carried by ElectionRejection.
const ElectionVersion = "benefits-election/v1"

var (
	// ErrElectionRejected is the BEN-005 sentinel. Stale window, plan,
	// rate, dependent or changed-replay input fails with this error
	// carrying the offending field, state and version.
	ErrElectionRejected = errors.New("BEN_005_REJECTED")
)

// ElectionRejection is the stable BEN-005 failure shape.
type ElectionRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *ElectionRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrElectionRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the BEN_005_REJECTED sentinel to errors.Is.
func (r *ElectionRejection) Unwrap() error { return ErrElectionRejected }

func electionReject(field, state, reason string) error {
	return &ElectionRejection{Field: field, State: state, Version: ElectionVersion, Reason: reason}
}

// ElectionInput is one append-only election revision request. PriorDigest
// is the head digest the caller observed; empty only for the genesis
// revision. Costs are exact decimals; Waiver requires WaiverEvidence.
type ElectionInput struct {
	Tenant         string
	WorkerRef      string
	PlanRef        string
	PlanRevision   string
	RateVersion    string
	WindowID       string
	Tier           TierCode
	Dependents     []string
	EffectiveDate  time.Time
	EmployeeCost   values.Decimal
	EmployerCost   values.Decimal
	Waiver         bool
	WaiverEvidence string
	PriorDigest    string
	Supersedes     string
}

// ElectionRevision is one sealed election revision. Revision starts at 1
// and increments by exactly one per successor; CanonicalDigest seals every
// bound field.
type ElectionRevision struct {
	Tenant          string
	WorkerRef       string
	PlanRef         string
	PlanRevision    string
	RateVersion     string
	WindowID        string
	Tier            TierCode
	Dependents      []string
	EffectiveDate   time.Time
	EmployeeCost    values.Decimal
	EmployerCost    values.Decimal
	Waiver          bool
	WaiverEvidence  string
	Revision        uint64
	Supersedes      string
	PriorDigest     string
	CanonicalDigest string
}

// Verify rechecks the seal. A tampered revision never verifies.
func (r ElectionRevision) Verify() error {
	if r.CanonicalDigest == "" || r.CanonicalDigest != r.computedDigest() {
		return electionReject("election.digest", "MISMATCH", "canonical digest mismatch")
	}
	return nil
}

func (r ElectionRevision) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.benefits.ElectionRevision", 1).
		String("tenant", r.Tenant).
		String("worker_ref", r.WorkerRef).
		String("plan_ref", r.PlanRef).
		String("plan_revision", r.PlanRevision).
		String("rate_version", r.RateVersion).
		String("window_id", r.WindowID).
		String("tier", string(r.Tier)).
		SortedStrings("dependents", append([]string(nil), r.Dependents...)).
		String("effective_date", r.EffectiveDate.UTC().Format(time.RFC3339)).
		Value("employee_cost", r.EmployeeCost).
		Value("employer_cost", r.EmployerCost).
		Bool("waiver", r.Waiver).
		String("waiver_evidence", r.WaiverEvidence).
		String("supersedes", r.Supersedes).
		String("prior_digest", r.PriorDigest).
		Int("revision", int64(r.Revision))
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func (in ElectionInput) validateShape() error {
	if strings.TrimSpace(in.Tenant) == "" {
		return electionReject("election.tenant", "MISSING", "tenant is required")
	}
	if strings.TrimSpace(in.WorkerRef) == "" {
		return electionReject("election.worker_ref", "MISSING", "worker ref is required")
	}
	if strings.TrimSpace(in.PlanRef) == "" {
		return electionReject("election.plan_ref", "MISSING", "plan ref is required")
	}
	if strings.TrimSpace(in.PlanRevision) == "" {
		return electionReject("election.plan_revision", "STALE", "plan revision is required")
	}
	if strings.TrimSpace(in.RateVersion) == "" {
		return electionReject("election.rate_version", "STALE", "rate version is required")
	}
	if strings.TrimSpace(in.WindowID) == "" {
		return electionReject("election.window_id", "STALE", "enrollment window is required")
	}
	if !in.Tier.Valid() {
		return electionReject("election.tier", "UNDECLARED", fmt.Sprintf("tier %q is not declared", in.Tier))
	}
	if in.EffectiveDate.IsZero() {
		return electionReject("election.effective_date", "MISSING", "effective date is required")
	}
	if err := in.EmployeeCost.Validate(); err != nil {
		return electionReject("election.employee_cost", "INVALID", "employee cost is not a valid decimal")
	}
	if err := in.EmployerCost.Validate(); err != nil {
		return electionReject("election.employer_cost", "INVALID", "employer cost is not a valid decimal")
	}
	if in.EmployeeCost.Sign() < 0 || in.EmployerCost.Sign() < 0 {
		return electionReject("election.cost", "NEGATIVE", "costs must not be negative")
	}
	if in.Waiver && strings.TrimSpace(in.WaiverEvidence) == "" {
		return electionReject("election.waiver_evidence", "MISSING", "waiver requires evidence")
	}
	seen := make(map[string]struct{}, len(in.Dependents))
	for _, d := range in.Dependents {
		if strings.TrimSpace(d) == "" {
			return electionReject("election.dependents", "INVALID", "dependent refs must not be blank")
		}
		if _, dup := seen[d]; dup {
			return electionReject("election.dependents", "DUPLICATE", fmt.Sprintf("dependent %q repeats", d))
		}
		seen[d] = struct{}{}
	}
	return nil
}

// RecordElection appends one election revision after head. head is the
// currently accepted revision or nil for genesis. The input PriorDigest
// must equal head's CanonicalDigest; anything else is a stale or changed
// replay and is refused. The predecessor is never mutated.
func RecordElection(head *ElectionRevision, in ElectionInput) (ElectionRevision, error) {
	if err := in.validateShape(); err != nil {
		return ElectionRevision{}, err
	}
	var revision uint64 = 1
	if head == nil {
		if in.PriorDigest != "" {
			return ElectionRevision{}, electionReject("election.prior_digest", "STALE", "genesis revision takes no prior digest")
		}
	} else {
		if err := head.Verify(); err != nil {
			return ElectionRevision{}, err
		}
		if head.Tenant != in.Tenant || head.WorkerRef != in.WorkerRef {
			return ElectionRevision{}, electionReject("election.scope", "MISMATCH", "successor must stay in the head tenant and worker scope")
		}
		if in.PriorDigest != head.CanonicalDigest {
			return ElectionRevision{}, electionReject("election.prior_digest", "STALE", "prior digest does not match the accepted head")
		}
		revision = head.Revision + 1
	}
	deps := append([]string(nil), in.Dependents...)
	sort.Strings(deps)
	rev := ElectionRevision{
		Tenant: in.Tenant, WorkerRef: in.WorkerRef, PlanRef: in.PlanRef,
		PlanRevision: in.PlanRevision, RateVersion: in.RateVersion, WindowID: in.WindowID,
		Tier: in.Tier, Dependents: deps, EffectiveDate: in.EffectiveDate.UTC(),
		EmployeeCost: in.EmployeeCost, EmployerCost: in.EmployerCost,
		Waiver: in.Waiver, WaiverEvidence: in.WaiverEvidence,
		Revision: revision, Supersedes: in.Supersedes, PriorDigest: in.PriorDigest,
	}
	rev.CanonicalDigest = rev.computedDigest()
	if rev.CanonicalDigest == "" {
		return ElectionRevision{}, electionReject("election.digest", "UNENCODABLE", "revision is not digestible")
	}
	return rev, nil
}

// ReplayElections rebuilds the head from an ordered revision history,
// refusing a broken chain. It is the BEN-005 recovery path: the ledger is
// the log, and the log re-verifies from genesis.
func ReplayElections(history []ElectionRevision) (ElectionRevision, error) {
	if len(history) == 0 {
		return ElectionRevision{}, electionReject("election.history", "MISSING", "history is empty")
	}
	for i, rev := range history {
		if err := rev.Verify(); err != nil {
			return ElectionRevision{}, err
		}
		if rev.Revision != uint64(i+1) {
			return ElectionRevision{}, electionReject("election.revision", "GAP", fmt.Sprintf("revision %d out of sequence at index %d", rev.Revision, i))
		}
		if i > 0 && rev.PriorDigest != history[i-1].CanonicalDigest {
			return ElectionRevision{}, electionReject("election.prior_digest", "BROKEN_CHAIN", fmt.Sprintf("revision %d does not chain to its predecessor", rev.Revision))
		}
	}
	return history[len(history)-1], nil
}
