// Package localcommit owns the pure consistency contract for Promotion's
// bounded local commit. It binds domain-owned mutation projections to the
// already-prepared transaction plan; storage adapters perform the physical
// append through the transaction coordinator.
package localcommit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/org"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction"
	transactionplan "github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
)

const contractVersion = 1

const (
	ParticipantPeople       = "people.assignment"
	ParticipantOrganization = "org.manager_relationship"
	ParticipantCompensation = "rewards.compensation"
	ParticipantPosition     = "position.occupancy"
	ParticipantBudget       = "rewards.budget_reservation"
)

var (
	ErrInvalidPreparedPlan = errors.New("promotion local commit: prepared plan is invalid")
	ErrParticipantBinding  = errors.New("promotion local commit: participant binding is invalid")
	ErrIdempotencyConflict = errors.New("promotion local commit: idempotency key conflicts")
	ErrCommitAborted       = errors.New("promotion local commit: commit aborted")
)

// Version reports the local Promotion commit contract version.
func Version() int { return contractVersion }

// PreparedPlan is the terminal input. It is intentionally a value containing
// the immutable transaction plan and its consistency-boundary resolution; a
// caller cannot replace the participant set while execution is in progress.
type PreparedPlan struct {
	Plan       transactionplan.TransactionPlan
	Resolution transaction.Resolution

	People            people.PromotionMutation
	Organization      org.ManagerMutation
	Compensation      compensation.PromotionMutation
	InvariantVersions []string
}

func (p PreparedPlan) Validate() error {
	if err := p.Plan.VerifyDigest(); err != nil {
		return fmt.Errorf("%w: transaction plan: %v", ErrInvalidPreparedPlan, err)
	}
	if err := p.Resolution.VerifyDigest(); err != nil {
		return fmt.Errorf("%w: resolution: %v", ErrInvalidPreparedPlan, err)
	}
	if p.Plan.PlanID == "" || p.Resolution.PlanID != p.Plan.PlanID {
		return fmt.Errorf("%w: plan id mismatch", ErrInvalidPreparedPlan)
	}
	if err := p.People.Validate(); err != nil {
		return err
	}
	if err := p.Organization.Validate(); err != nil {
		return err
	}
	if err := p.Compensation.Validate(); err != nil {
		return err
	}
	for name, got := range map[string]string{"people": p.People.ProposalDigest, "organization": p.Organization.ProposalDigest, "compensation": p.Compensation.ProposalDigest} {
		if got != p.Plan.ProposalDigest {
			return fmt.Errorf("%w: %s mutation is bound to proposal %q, plan is %q", ErrInvalidPreparedPlan, name, got, p.Plan.ProposalDigest)
		}
	}
	if err := p.validateParticipants(); err != nil {
		return err
	}
	if err := p.validateWrites(); err != nil {
		return err
	}
	return nil
}

func (p PreparedPlan) validateParticipants() error {
	want := map[string]string{
		ParticipantPeople:       p.People.ExpectedRevision.Stream(),
		ParticipantOrganization: p.Organization.ExpectedRevision.Stream(),
		ParticipantCompensation: p.Compensation.ExpectedRevision.Stream(),
	}
	local := make(map[string]string)
	for _, participant := range p.Resolution.Admitted {
		if !participant.Local {
			return fmt.Errorf("%w: remote participant %s was admitted", ErrParticipantBinding, participant.ParticipantID)
		}
		if previous, exists := local[participant.ParticipantID]; exists {
			return fmt.Errorf("%w: participant %s is admitted twice (%s, %s)", ErrParticipantBinding, participant.ParticipantID, previous, participant.StreamID)
		}
		local[participant.ParticipantID] = participant.StreamID
	}
	for required, stream := range want {
		if got, ok := local[required]; !ok {
			return fmt.Errorf("%w: missing %s", ErrParticipantBinding, required)
		} else if got != stream {
			return fmt.Errorf("%w: %s is bound to stream %s, want %s", ErrParticipantBinding, required, got, stream)
		}
	}
	for _, participant := range p.Resolution.Effects {
		if participant.Local {
			return fmt.Errorf("%w: local participant %s was routed to effects", ErrParticipantBinding, participant.ParticipantID)
		}
	}
	return nil
}

func (p PreparedPlan) validateWrites() error {
	peopleWrites, err := p.People.PlannedWrites()
	if err != nil {
		return err
	}
	orgWrites, err := p.Organization.PlannedWrites()
	if err != nil {
		return err
	}
	compWrites, err := p.Compensation.PlannedWrites()
	if err != nil {
		return err
	}
	want := append(append(peopleWrites, orgWrites...), compWrites...)
	if len(p.Plan.Writes) != len(want) {
		return fmt.Errorf("%w: plan has %d writes, domain set has %d", ErrInvalidPreparedPlan, len(p.Plan.Writes), len(want))
	}
	used := make([]bool, len(p.Plan.Writes))
	for _, write := range want {
		found := false
		for i, actual := range p.Plan.Writes {
			if !used[i] && sameWrite(write, actual) {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: prepared plan drops %s", ErrInvalidPreparedPlan, write.FieldPath)
		}
	}
	return nil
}

func sameWrite(a, b intent.PlannedWrite) bool {
	return a.Subject == b.Subject && a.ResourceKey.Equal(b.ResourceKey) && a.FieldPath == b.FieldPath &&
		a.CurrentCanonicalText == b.CurrentCanonicalText && a.ProposedCanonicalText == b.ProposedCanonicalText &&
		a.SourceAuthorityDecision == b.SourceAuthorityDecision && a.ExpectedRevision.Equal(b.ExpectedRevision)
}

// Receipt is the one local commit fact. Outbox identities are recorded as
// queued obligations; no external destination is called by this package.
type Receipt struct {
	PlanID             string
	PlanDigest         string
	ProposalRevisionID string
	ProposalDigest     string
	ResolutionDigest   string
	ParticipantIDs     []string
	EventCount         int
	OutboxEffectIDs    []string
	InvariantVersions  []string
	Replayed           bool
}

// Failpoint is called before each correctness-bearing boundary. Returning an
// error leaves the in-memory store unchanged, modeling a rolled-back ACID
// transaction without making the domain package depend on a database.
type Failpoint func(stage string) error

// Store is a tiny atomic receipt store used by pure conformance tests and by
// adapters that need a deterministic local coordinator double.
type Store struct {
	mu       sync.Mutex
	commitMu sync.Mutex
	receipts map[string]Receipt
}

func NewStore() *Store { return &Store{receipts: make(map[string]Receipt)} }

func (s *Store) lookup(key string) (Receipt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.receipts[key]
	r.ParticipantIDs = append([]string(nil), r.ParticipantIDs...)
	r.OutboxEffectIDs = append([]string(nil), r.OutboxEffectIDs...)
	r.InvariantVersions = append([]string(nil), r.InvariantVersions...)
	return r, ok
}

func (s *Store) put(key string, r Receipt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receipts[key] = r
}

// Committer stages all plan dimensions in memory and publishes one receipt at
// the end. Its sequencing mirrors the real coordinator's participant, outbox
// and receipt boundaries, making every crash point testable.
//
// Fence is the reservation evidence the commit refuses to proceed without: a
// nil fence fails closed, so a committer that cannot prove fenced capacity
// for the plan's exact proposal digest commits nothing.
type Committer struct {
	Store     *Store
	Failpoint Failpoint
	Fence     ReservationFence
}

func (c *Committer) Commit(ctx context.Context, prepared PreparedPlan) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if c == nil || c.Store == nil {
		return Receipt{}, fmt.Errorf("%w: store is required", ErrInvalidPreparedPlan)
	}
	if err := prepared.Validate(); err != nil {
		return Receipt{}, err
	}
	// The reservation check runs before the commit mutex: a commit with no
	// fenced capacity fails fast instead of serializing behind the winner,
	// and the fence's own stores serialize competing acquisitions.
	if err := c.assertReservations(ctx, prepared); err != nil {
		return Receipt{}, err
	}
	c.Store.commitMu.Lock()
	defer c.Store.commitMu.Unlock()
	key := string(prepared.Plan.Tenant) + "\x00" + prepared.Plan.IdempotencyKey
	if old, ok := c.Store.lookup(key); ok {
		if old.PlanDigest != prepared.Plan.Digest {
			return Receipt{}, ErrIdempotencyConflict
		}
		old.Replayed = true
		return old, nil
	}
	participants := make([]string, 0, len(prepared.Resolution.Admitted))
	for _, participant := range prepared.Resolution.Admitted {
		participants = append(participants, participant.ParticipantID)
	}
	sort.Strings(participants)
	for _, stage := range append([]string{"before-participants"}, participants...) {
		if err := c.fail(stage); err != nil {
			return Receipt{}, fmt.Errorf("%w at %s: %v", ErrCommitAborted, stage, err)
		}
	}
	if err := c.fail("outbox"); err != nil {
		return Receipt{}, fmt.Errorf("%w at outbox: %v", ErrCommitAborted, err)
	}
	effectIDs := make([]string, 0, len(prepared.Plan.OutboxEffects))
	for _, effect := range prepared.Plan.OutboxEffects {
		effectIDs = append(effectIDs, effect.EffectID)
	}
	sort.Strings(effectIDs)
	r := Receipt{PlanID: prepared.Plan.PlanID, PlanDigest: prepared.Plan.Digest, ProposalRevisionID: prepared.Plan.ProposalRevisionID,
		ProposalDigest: prepared.Plan.ProposalDigest, ResolutionDigest: prepared.Resolution.Digest, ParticipantIDs: participants,
		EventCount: len(prepared.Plan.Events), OutboxEffectIDs: effectIDs, InvariantVersions: append([]string(nil), prepared.InvariantVersions...)}
	if err := c.fail("receipt"); err != nil {
		return Receipt{}, fmt.Errorf("%w at receipt: %v", ErrCommitAborted, err)
	}
	c.Store.put(key, r)
	if err := c.fail("after-commit"); err != nil {
		r.Replayed = false
		return r, fmt.Errorf("%w after durable receipt at after-commit: %v", ErrCommitAborted, err)
	}
	return r, nil
}

func (c *Committer) fail(stage string) error {
	if c.Failpoint == nil {
		return nil
	}
	return c.Failpoint(stage)
}

func Explain(p PreparedPlan) string {
	return fmt.Sprintf("promotion local commit v%d plan=%s proposal=%s participants=%d events=%d effects=%d",
		Version(), p.Plan.PlanID, p.Plan.ProposalRevisionID, len(p.Resolution.Admitted), len(p.Plan.Events), len(p.Plan.OutboxEffects))
}

// InvariantStatus is the closed conformance result vocabulary.
type InvariantStatus string

const (
	InvariantPass    InvariantStatus = "PASS"
	InvariantFail    InvariantStatus = "FAIL"
	InvariantUnknown InvariantStatus = "UNKNOWN"
)

type InvariantCode string

const (
	InvariantManagerCycle         InvariantCode = "MANAGER_CYCLE"
	InvariantEmploymentAssignment InvariantCode = "EMPLOYMENT_ASSIGNMENT_OVERLAP"
	InvariantPositionCapacity     InvariantCode = "POSITION_OVER_CAPACITY"
	InvariantCurrencyMismatch     InvariantCode = "COMPENSATION_CURRENCY_MISMATCH"
	InvariantCrossTenantReference InvariantCode = "CROSS_TENANT_REFERENCE"
)

type InvariantFinding struct {
	Code   InvariantCode
	Stream string
	Detail string
}

type TenantReference struct {
	Ref    string
	Tenant values.TenantId
}

// InvariantInput contains only replayable committed-stream facts. Missing
// required coordinates produce UNKNOWN, never a false PASS.
type InvariantInput struct {
	Tenant                               values.TenantId
	WorkerID, ManagerID                  string
	ManagerAncestors                     []string
	EmploymentIntervals                  []values.EffectiveInterval
	AssignmentIntervals                  []values.EffectiveInterval
	PositionCapacity, PositionOccupied   int64
	CompensationCurrency, BudgetCurrency string
	References                           []TenantReference
}

type InvariantEvaluation struct {
	Status   InvariantStatus
	Findings []InvariantFinding
	Versions []string
}

func EvaluateInvariants(in InvariantInput, versions ...string) InvariantEvaluation {
	result := InvariantEvaluation{Status: InvariantPass, Versions: append([]string(nil), versions...)}
	if in.Tenant.Validate() != nil || strings.TrimSpace(in.WorkerID) == "" {
		result.Status = InvariantUnknown
		result.Findings = append(result.Findings, InvariantFinding{Code: InvariantCrossTenantReference, Stream: "transaction", Detail: "tenant and worker are required"})
		return result
	}
	if in.ManagerID == in.WorkerID || contains(in.ManagerAncestors, in.WorkerID) {
		result.add(InvariantFinding{Code: InvariantManagerCycle, Stream: ParticipantOrganization, Detail: "manager chain contains the promoted worker"})
	}
	if len(in.EmploymentIntervals) == 0 || len(in.AssignmentIntervals) == 0 {
		result.Status = InvariantUnknown
		result.Findings = append(result.Findings, InvariantFinding{Code: InvariantEmploymentAssignment, Stream: ParticipantPeople, Detail: "employment and assignment intervals are required"})
	} else if !assignmentsContained(in.EmploymentIntervals, in.AssignmentIntervals) {
		result.add(InvariantFinding{Code: InvariantEmploymentAssignment, Stream: ParticipantPeople, Detail: "assignment interval is not covered by employment"})
	}
	if in.PositionCapacity < 0 || in.PositionOccupied < 0 || in.PositionOccupied > in.PositionCapacity {
		result.add(InvariantFinding{Code: InvariantPositionCapacity, Stream: ParticipantPosition, Detail: "position occupancy exceeds capacity"})
	}
	if in.CompensationCurrency == "" || in.BudgetCurrency == "" {
		result.Status = InvariantUnknown
		result.Findings = append(result.Findings, InvariantFinding{Code: InvariantCurrencyMismatch, Stream: ParticipantCompensation, Detail: "compensation and budget currencies are required"})
	} else if in.CompensationCurrency != in.BudgetCurrency {
		result.add(InvariantFinding{Code: InvariantCurrencyMismatch, Stream: ParticipantCompensation, Detail: "compensation and budget currencies differ"})
	}
	for _, ref := range in.References {
		if ref.Tenant != in.Tenant {
			result.add(InvariantFinding{Code: InvariantCrossTenantReference, Stream: "transaction", Detail: "reference " + ref.Ref + " crosses tenant"})
		}
	}
	sort.Slice(result.Findings, func(i, j int) bool {
		if result.Findings[i].Code != result.Findings[j].Code {
			return result.Findings[i].Code < result.Findings[j].Code
		}
		return result.Findings[i].Stream < result.Findings[j].Stream
	})
	return result
}

func (r *InvariantEvaluation) add(f InvariantFinding) {
	r.Status = InvariantFail
	r.Findings = append(r.Findings, f)
}
func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func assignmentsContained(employment, assignments []values.EffectiveInterval) bool {
	for _, assignment := range assignments {
		covered := false
		for _, job := range employment {
			if intervalContains(job, assignment) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func intervalContains(outer, inner values.EffectiveInterval) bool {
	if outer.Validate() != nil || inner.Validate() != nil || outer.Kind() != inner.Kind() {
		return false
	}
	if outer.Kind() == values.IntervalKindInstant {
		start, _ := outer.StartInstant()
		innerStart, _ := inner.StartInstant()
		if start.Compare(innerStart) > 0 {
			return false
		}
		outerEnd, outerHas := outer.EndInstant()
		innerEnd, innerHas := inner.EndInstant()
		if !outerHas {
			return true
		}
		return innerHas && outerEnd.Compare(innerEnd) >= 0
	}
	start, _ := outer.StartDate()
	innerStart, _ := inner.StartDate()
	if start.Compare(innerStart) > 0 {
		return false
	}
	outerEnd, outerHas := outer.EndDate()
	innerEnd, innerHas := inner.EndDate()
	if !outerHas {
		return true
	}
	return innerHas && outerEnd.Compare(innerEnd) >= 0
}
