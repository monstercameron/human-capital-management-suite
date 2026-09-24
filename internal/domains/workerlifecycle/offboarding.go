package workerlifecycle

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrOffboardingRejected is the WORKER-LIFE-004 refusal boundary. Employment
// completion and external closure are independent dimensions; gaps create
// scoped repair, never silent completion.
var ErrOffboardingRejected = errors.New("WORKER_LIFE_004_REJECTED")

// ClosureDomain is the closed offboarding-closure vocabulary.
type ClosureDomain string

const (
	ClosureAccess   ClosureDomain = "ACCESS"
	ClosureAssets   ClosureDomain = "ASSETS"
	ClosurePayroll  ClosureDomain = "PAYROLL"
	ClosureBenefits ClosureDomain = "BENEFITS"
	ClosureRecords  ClosureDomain = "RECORDS"
)

func (d ClosureDomain) Valid() bool {
	switch d {
	case ClosureAccess, ClosureAssets, ClosurePayroll, ClosureBenefits, ClosureRecords:
		return true
	default:
		return false
	}
}

// ClosureState is the per-domain closure outcome vocabulary.
type ClosureState string

const (
	ClosureExpected ClosureState = "EXPECTED"
	ClosureObserved ClosureState = "OBSERVED"
	ClosureGap      ClosureState = "GAP"
	ClosureUnknown  ClosureState = "UNKNOWN"
)

// PrivilegedRevoker is the role that must revoke access.
const PrivilegedRevoker = "SECURITY_OFFICER"

// ClosureEvidence is one domain-owned closure observation.
type ClosureEvidence struct {
	ObservationID string
	At            values.LocalDate
	By            string
	Evidence      values.EntityRef
}

func (e ClosureEvidence) Validate() error {
	if strings.TrimSpace(e.ObservationID) == "" || strings.TrimSpace(e.By) == "" {
		return fmt.Errorf("%w: observation id and revoker role are required", ErrOffboardingRejected)
	}
	if err := e.At.Validate(); err != nil {
		return fmt.Errorf("%w: observation date: %v", ErrOffboardingRejected, err)
	}
	return e.Evidence.Validate()
}

func (e ClosureEvidence) Canonical() []byte {
	if err := e.Validate(); err != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.workerlifecycle.ClosureEvidence", 1).
		String("observation_id", e.ObservationID).Value("at", e.At).
		String("by", e.By).Value("evidence", e.Evidence)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// DomainClosure tracks one domain's expected closure and its observation.
type DomainClosure struct {
	Domain   ClosureDomain
	Expected []values.EntityRef
	Observed *ClosureEvidence
	State    ClosureState
}

// OffboardRepair is a scoped repair obligation for one closure gap.
type OffboardRepair struct {
	Domain   ClosureDomain
	Detail   string
	Resolved bool
}

// ClosePolicy is the declared final-close policy.
type ClosePolicy struct {
	Name string
}

// OffboardingTracker composes employment completion with per-domain closure
// evidence. Domain owners define closure evidence; the lifecycle reports it.
type OffboardingTracker struct {
	PlanDigest            string
	Worker                values.EntityRef
	Employment            values.EntityRef
	EventDate             values.LocalDate
	EmploymentCompleted   bool
	EmploymentCompletedAt values.LocalDate
	RehireEligibility     *people.RehireEligibility
	ApprovalRevoked       bool
	ApprovalRevocation    string
	Closures              []DomainClosure
	Repairs               []OffboardRepair
	ObservedIDs           []string
	Closed                bool
	Digest                string
}

// BeginOffboarding opens closure tracking for one termination plan and the
// expected evidence per domain.
func BeginOffboarding(plan WorkerLifecyclePlan, expected map[ClosureDomain][]values.EntityRef) (OffboardingTracker, error) {
	fail := func(format string, args ...any) (OffboardingTracker, error) {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, fmt.Errorf(format, args...))
	}
	if err := plan.Validate(); err != nil {
		return fail("plan: %v", err)
	}
	if plan.Event != EventEnd {
		return fail("offboarding needs an END plan, have %s", plan.Event)
	}
	if len(expected) == 0 {
		return fail("at least one closure domain is required")
	}
	tracker := OffboardingTracker{
		PlanDigest: plan.CanonicalDigest,
		Worker:     plan.Worker,
		Employment: plan.Employment,
		EventDate:  plan.EventDate,
	}
	if tracker.PlanDigest == "" {
		digest, err := plan.Digest()
		if err != nil {
			return fail("plan digest: %v", err)
		}
		tracker.PlanDigest = digest
	}
	domains := make([]ClosureDomain, 0, len(expected))
	for domain := range expected {
		domains = append(domains, domain)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i] < domains[j] })
	for _, domain := range domains {
		if !domain.Valid() {
			return fail("domain %q is not declared", domain)
		}
		refs := expected[domain]
		if len(refs) == 0 {
			return fail("domain %s: expected evidence is required", domain)
		}
		for _, ref := range refs {
			if err := ref.Validate(); err != nil {
				return fail("domain %s evidence: %v", domain, err)
			}
			if ref.Tenant != plan.Worker.Tenant {
				return fail("domain %s evidence tenant differs", domain)
			}
		}
		tracker.Closures = append(tracker.Closures, DomainClosure{
			Domain: domain, Expected: append([]values.EntityRef(nil), refs...), State: ClosureExpected,
		})
	}
	tracker.Digest = canonicalbytes.Digest(tracker.body())
	return tracker, nil
}

// CompleteEmployment records employment-history completion independently of
// external closure. History is immutable once recorded.
func CompleteEmployment(tracker OffboardingTracker, at values.LocalDate) (OffboardingTracker, error) {
	if tracker.EmploymentCompleted {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, errors.New("employment history is already complete"))
	}
	if err := at.Validate(); err != nil {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, fmt.Errorf("completion date: %v", err))
	}
	next := tracker
	next.EmploymentCompleted = true
	next.EmploymentCompletedAt = at
	next.Digest = canonicalbytes.Digest(next.body())
	return next, nil
}

// CompleteEmploymentWithRehire records the governed rehire decision as part
// of employment completion. The old CompleteEmployment entry point remains
// available for callers that have not yet adopted rehire capture.
func CompleteEmploymentWithRehire(tracker OffboardingTracker, at values.LocalDate, status people.RehireStatus, reason people.RehireReason, authorityRef string) (OffboardingTracker, error) {
	if tracker.EmploymentCompleted {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, errors.New("employment history is already complete"))
	}
	completed, err := CompleteEmployment(tracker, at)
	if err != nil {
		return OffboardingTracker{}, err
	}
	record := people.RehireEligibility{
		Worker: tracker.Worker, Employment: tracker.Employment,
		Status: status, Reason: reason, EffectiveAt: at, RecordedAt: at,
		Revision: 1, AuthorityRef: authorityRef,
	}
	if err := record.Validate(); err != nil {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, err)
	}
	completed.RehireEligibility = &record
	completed.Digest = canonicalbytes.Digest(completed.body())
	return completed, nil
}

// RevokeApproval records a withdrawn approval; close is blocked while set.
func RevokeApproval(tracker OffboardingTracker, reason string) (OffboardingTracker, error) {
	if strings.TrimSpace(reason) == "" {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, errors.New("revocation reason is required"))
	}
	if tracker.Closed {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, errors.New("tracker is closed"))
	}
	next := tracker
	next.ApprovalRevoked = true
	next.ApprovalRevocation = reason
	next.Digest = canonicalbytes.Digest(next.body())
	return next, nil
}

// ObserveClosure records one domain observation. A retried observation id
// is a no-op, never a duplicate; unrecognized evidence is a gap with scoped
// repair, never completion.
func ObserveClosure(tracker OffboardingTracker, domain ClosureDomain, evidence ClosureEvidence) (OffboardingTracker, error) {
	fail := func(format string, args ...any) (OffboardingTracker, error) {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, fmt.Errorf(format, args...))
	}
	if err := evidence.Validate(); err != nil {
		return fail("evidence: %v", err)
	}
	if evidence.Evidence.Tenant != tracker.Employment.Tenant {
		return fail("evidence tenant differs")
	}
	if evidence.At.Compare(tracker.EventDate) < 0 {
		return fail("evidence predates the termination event")
	}
	for _, seen := range tracker.ObservedIDs {
		if seen == evidence.ObservationID {
			return tracker, nil
		}
	}
	next := tracker
	next.Closures = append([]DomainClosure(nil), tracker.Closures...)
	for i, closure := range next.Closures {
		if closure.Domain != domain {
			continue
		}
		if closure.State == ClosureObserved {
			return fail("domain %s is already closed", domain)
		}
		next.ObservedIDs = append(append([]string(nil), tracker.ObservedIDs...), evidence.ObservationID)
		observed := evidence
		next.Closures[i].Observed = &observed
		matched := false
		for _, want := range closure.Expected {
			if want == evidence.Evidence {
				matched = true
				break
			}
		}
		switch {
		case !matched:
			next.Closures[i].State = ClosureGap
			next.Repairs = append(append([]OffboardRepair(nil), tracker.Repairs...),
				OffboardRepair{Domain: domain, Detail: "unrecognized closure evidence"})
		case domain == ClosureAccess && evidence.By != PrivilegedRevoker:
			next.Closures[i].State = ClosureGap
			next.Repairs = append(append([]OffboardRepair(nil), tracker.Repairs...),
				OffboardRepair{Domain: domain, Detail: fmt.Sprintf("revoker %s lacks privilege", evidence.By)})
		default:
			next.Closures[i].State = ClosureObserved
		}
		next.Digest = canonicalbytes.Digest(next.body())
		return next, nil
	}
	return fail("domain %s is not tracked", domain)
}

// ResolveRepair marks a domain's open repairs resolved under a governed
// resolution reference.
func ResolveRepair(tracker OffboardingTracker, domain ClosureDomain, resolution values.EntityRef) (OffboardingTracker, error) {
	if err := resolution.Validate(); err != nil {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, fmt.Errorf("resolution: %v", err))
	}
	matched := false
	next := tracker
	next.Repairs = append([]OffboardRepair(nil), tracker.Repairs...)
	for i, repair := range next.Repairs {
		if repair.Domain == domain && !repair.Resolved {
			next.Repairs[i].Resolved = true
			matched = true
		}
	}
	if !matched {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, fmt.Errorf("domain %s has no open repair", domain))
	}
	next.Digest = canonicalbytes.Digest(next.body())
	return next, nil
}

// CloseOffboarding closes under the declared policy without rerunning
// termination: employment history, approvals, every required closure and
// every repair must stand resolved.
func CloseOffboarding(tracker OffboardingTracker, policy ClosePolicy) (OffboardingTracker, error) {
	fail := func(format string, args ...any) (OffboardingTracker, error) {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(policy.Name) == "" {
		return fail("a declared close policy is required")
	}
	if tracker.Closed {
		return fail("tracker is already closed")
	}
	if !tracker.EmploymentCompleted {
		return fail("employment is not complete")
	}
	if tracker.ApprovalRevoked {
		return fail("approval is revoked")
	}
	for _, closure := range tracker.Closures {
		if closure.State != ClosureObserved {
			return fail("domain %s is %s, not observed", closure.Domain, closure.State)
		}
	}
	for _, repair := range tracker.Repairs {
		if !repair.Resolved {
			return fail("domain %s repair stands open", repair.Domain)
		}
	}
	next := tracker
	next.Closed = true
	next.Digest = canonicalbytes.Digest(next.body())
	return next, nil
}

// ClosureStore is the port through which trackers persist. Implementations
// compare-and-swap on the digest so concurrent writers cannot silently win.
type ClosureStore interface {
	SaveTracker(tracker OffboardingTracker, expectDigest string) error
	LoadTracker(planDigest string) (OffboardingTracker, error)
}

// MemoryClosureStore is a kernel-pure ClosureStore for tests and harnesses.
type MemoryClosureStore struct {
	mu   sync.RWMutex
	held map[string]OffboardingTracker
}

// NewMemoryClosureStore returns an empty tracker store.
func NewMemoryClosureStore() *MemoryClosureStore {
	return &MemoryClosureStore{held: map[string]OffboardingTracker{}}
}

// SaveTracker persists one tracker when the expected digest matches.
func (m *MemoryClosureStore) SaveTracker(tracker OffboardingTracker, expectDigest string) error {
	if err := tracker.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.held[tracker.PlanDigest]
	if !ok {
		if expectDigest != "" {
			return errors.Join(ErrOffboardingRejected, errors.New("no stored tracker for compare-and-swap"))
		}
		m.held[tracker.PlanDigest] = tracker
		return nil
	}
	if current.Digest != expectDigest {
		return errors.Join(ErrOffboardingRejected, fmt.Errorf("stale write: have %s, want %s", expectDigest, current.Digest))
	}
	m.held[tracker.PlanDigest] = tracker
	return nil
}

// LoadTracker returns the stored tracker for one plan digest.
func (m *MemoryClosureStore) LoadTracker(planDigest string) (OffboardingTracker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tracker, ok := m.held[planDigest]
	if !ok {
		return OffboardingTracker{}, errors.Join(ErrOffboardingRejected, fmt.Errorf("no tracker for %q", planDigest))
	}
	return tracker, nil
}

func (t OffboardingTracker) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.workerlifecycle.OffboardingTracker", 1).
		String("plan_digest", t.PlanDigest).Value("employment", t.Employment).
		Value("event_date", t.EventDate).Bool("employment_completed", t.EmploymentCompleted).
		Bool("approval_revoked", t.ApprovalRevoked).String("approval_revocation", t.ApprovalRevocation).
		Bool("closed", t.Closed).Count("closures", len(t.Closures))
	w.Optional("employment_completed_at", t.EmploymentCompleted, t.EmploymentCompletedAt)
	if t.RehireEligibility != nil {
		w.Value("worker", t.Worker).Value("rehire_eligibility", rehireEligibilityValue(t.RehireEligibility))
	}
	for _, closure := range t.Closures {
		w.String("domain", string(closure.Domain)).String("state", string(closure.State)).
			Count("expected", len(closure.Expected))
		for _, ref := range closure.Expected {
			w.Value("expected_ref", ref)
		}
		w.Optional("observed", closure.State == ClosureObserved && closure.Observed != nil, closureEvidenceValue(closure.Observed))
	}
	w.Count("repairs", len(t.Repairs))
	for _, repair := range t.Repairs {
		w.String("repair_domain", string(repair.Domain)).String("repair_detail", repair.Detail).
			Bool("repair_resolved", repair.Resolved)
	}
	w.SortedStrings("observed_ids", t.ObservedIDs)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func rehireEligibilityValue(record *people.RehireEligibility) people.RehireEligibility {
	if record == nil {
		return people.RehireEligibility{}
	}
	return *record
}

func closureEvidenceValue(evidence *ClosureEvidence) ClosureEvidence {
	if evidence == nil {
		return ClosureEvidence{}
	}
	return *evidence
}

// Validate rechecks employment, closure, repair and digest invariants.
func (t OffboardingTracker) Validate() error {
	fail := func(format string, args ...any) error {
		return errors.Join(ErrOffboardingRejected, fmt.Errorf(format, args...))
	}
	if strings.TrimSpace(t.PlanDigest) == "" {
		return fail("plan lineage is required")
	}
	if t.EmploymentCompleted {
		if err := t.EmploymentCompletedAt.Validate(); err != nil {
			return fail("completion date: %v", err)
		}
	}
	if t.RehireEligibility != nil {
		if !t.EmploymentCompleted {
			return fail("rehire eligibility exists before employment completion")
		}
		if err := t.RehireEligibility.Validate(); err != nil {
			return fail("rehire eligibility: %v", err)
		}
		if t.RehireEligibility.Worker != t.Worker || t.RehireEligibility.Employment != t.Employment || t.RehireEligibility.EffectiveAt != t.EmploymentCompletedAt {
			return fail("rehire eligibility does not match completed employment")
		}
	}
	if len(t.Closures) == 0 {
		return fail("at least one closure domain is required")
	}
	seen := map[ClosureDomain]struct{}{}
	for _, closure := range t.Closures {
		if !closure.Domain.Valid() {
			return fail("domain %q is not declared", closure.Domain)
		}
		if _, ok := seen[closure.Domain]; ok {
			return fail("duplicate domain %s", closure.Domain)
		}
		seen[closure.Domain] = struct{}{}
		switch closure.State {
		case ClosureExpected, ClosureGap, ClosureUnknown:
		case ClosureObserved:
			if closure.Observed == nil {
				return fail("domain %s observed without evidence", closure.Domain)
			}
		default:
			return fail("domain %s state %q is not declared", closure.Domain, closure.State)
		}
		if t.Closed && closure.State != ClosureObserved {
			return fail("closed with domain %s %s", closure.Domain, closure.State)
		}
	}
	if t.Closed {
		if !t.EmploymentCompleted || t.ApprovalRevoked {
			return fail("closed without completed employment or under revoked approval")
		}
		for _, repair := range t.Repairs {
			if !repair.Resolved {
				return fail("closed with open repair")
			}
		}
	}
	if t.Digest != canonicalbytes.Digest(t.body()) {
		return fail("canonical digest mismatch")
	}
	return nil
}
