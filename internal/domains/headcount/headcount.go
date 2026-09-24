// Package headcount owns the pure transaction boundary between headcount
// authorization, position creation, and recruiting requisition opening. The
// three lifecycles are intentionally separate; this package does not persist
// rows or call an external HRIS.
package headcount

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidRequest      = errors.New("headcount: invalid request")
	ErrInvalidApproval     = errors.New("headcount: invalid approval")
	ErrCapacityConflict    = errors.New("headcount: capacity conflict")
	ErrBudgetConflict      = errors.New("headcount: budget conflict")
	ErrInvalidRelationship = errors.New("headcount: invalid parent-child relationship")
	ErrExternalNotApplied  = errors.New("headcount: external request is not applied")
	ErrInvalidTransition   = errors.New("headcount: invalid lifecycle transition")
)

type CapacityUnit string

const (
	UnitHead CapacityUnit = "HEAD"
	UnitFTE  CapacityUnit = "FTE"
)

func (u CapacityUnit) Valid() bool { return u == UnitHead || u == UnitFTE }

type HeadcountState string

const (
	HeadcountDraft     HeadcountState = "DRAFT"
	HeadcountSubmitted HeadcountState = "SUBMITTED"
	HeadcountApproved  HeadcountState = "APPROVED"
	HeadcountRejected  HeadcountState = "REJECTED"
)

func (s HeadcountState) Valid() bool {
	return s == HeadcountDraft || s == HeadcountSubmitted || s == HeadcountApproved || s == HeadcountRejected
}

type PositionState string

const (
	PositionProposed               PositionState = "PROPOSED"
	PositionRequested              PositionState = "REQUESTED"
	PositionCreated                PositionState = "CREATED"
	PositionReconciliationRequired PositionState = "RECONCILIATION_REQUIRED"
)

func (s PositionState) Valid() bool {
	return s == PositionProposed || s == PositionRequested || s == PositionCreated || s == PositionReconciliationRequired
}

type RequisitionState string

const (
	RequisitionProposed               RequisitionState = "PROPOSED"
	RequisitionRequested              RequisitionState = "REQUESTED"
	RequisitionOpen                   RequisitionState = "OPEN"
	RequisitionReconciliationRequired RequisitionState = "RECONCILIATION_REQUIRED"
)

func (s RequisitionState) Valid() bool {
	return s == RequisitionProposed || s == RequisitionRequested || s == RequisitionOpen || s == RequisitionReconciliationRequired
}

// HeadcountRequest is the capacity-authorization aggregate. Approval changes
// only this lifecycle; it never creates a Position or Requisition.
type HeadcountRequest struct {
	Tenant, ID, Requester                                                   string
	PlanRef, BudgetRef, OrganizationRef, JobRef, LocationRef, CostCenterRef string
	PositionCount                                                           int64
	Capacity                                                                values.Decimal
	Unit                                                                    CapacityUnit
	State                                                                   HeadcountState
	ProposalRevision                                                        uint64
}

func (r HeadcountRequest) Validate() error {
	if strings.TrimSpace(r.Tenant) == "" || strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Requester) == "" ||
		r.PositionCount <= 0 || !r.Unit.Valid() || r.ProposalRevision == 0 || !r.State.Valid() {
		return fmt.Errorf("%w: tenant, identity, requester, positive count, unit, state and revision are required", ErrInvalidRequest)
	}
	if strings.TrimSpace(r.OrganizationRef) == "" || strings.TrimSpace(r.JobRef) == "" || strings.TrimSpace(r.LocationRef) == "" || strings.TrimSpace(r.CostCenterRef) == "" {
		return fmt.Errorf("%w: canonical organization, job, location and cost-center refs are required", ErrInvalidRequest)
	}
	if err := r.Capacity.Validate(); err != nil || r.Capacity.Sign() <= 0 {
		return fmt.Errorf("%w: capacity must be a positive exact decimal", ErrInvalidRequest)
	}
	return nil
}

// ApprovalCertificate proves that approvers came from policy resolution, not
// a requester-selected list, and that requester/approver separation held.
type ApprovalCertificate struct {
	PolicyVersion      string
	DecisionDigest     string
	Requester          string
	ApproverRefs       []string
	RequiredQuorum     int
	ResolvedByPolicy   bool
	SeparationOfDuties bool
}

func (a ApprovalCertificate) ValidateFor(r HeadcountRequest) error {
	if strings.TrimSpace(a.PolicyVersion) == "" || strings.TrimSpace(a.DecisionDigest) == "" || a.Requester == "" || a.RequiredQuorum <= 0 || !a.ResolvedByPolicy || !a.SeparationOfDuties || len(a.ApproverRefs) < a.RequiredQuorum {
		return fmt.Errorf("%w: policy resolution, quorum and separation evidence are required", ErrInvalidApproval)
	}
	seen := map[string]bool{}
	for _, ref := range a.ApproverRefs {
		if strings.TrimSpace(ref) == "" || seen[ref] || ref == r.Requester || ref == a.Requester {
			return fmt.Errorf("%w: requester-selected or duplicate approver", ErrInvalidApproval)
		}
		seen[ref] = true
	}
	if a.Requester != r.Requester {
		return fmt.Errorf("%w: certificate requester differs from proposal requester", ErrInvalidApproval)
	}
	return nil
}

type ExternalState string

const (
	ExternalRequested ExternalState = "REQUESTED"
	ExternalAccepted  ExternalState = "ACCEPTED"
	ExternalApplied   ExternalState = "APPLIED"
	ExternalRejected  ExternalState = "REJECTED"
	ExternalAmbiguous ExternalState = "AMBIGUOUS"
)

func (s ExternalState) Valid() bool {
	return s == ExternalRequested || s == ExternalAccepted || s == ExternalApplied || s == ExternalRejected || s == ExternalAmbiguous
}
func (s ExternalState) Complete() bool { return s == ExternalApplied }

type ExternalObservation struct {
	State         ExternalState
	ExternalID    string
	PayloadDigest string
}

func (o ExternalObservation) Validate() error {
	if !o.State.Valid() || strings.TrimSpace(o.PayloadDigest) == "" {
		return fmt.Errorf("%w: external state and payload digest are required", ErrInvalidRequest)
	}
	if o.State == ExternalApplied && strings.TrimSpace(o.ExternalID) == "" {
		return fmt.Errorf("%w: applied external creation requires an id", ErrInvalidRequest)
	}
	return nil
}

type PositionProposal struct {
	Tenant, ID, ParentRequestID, Requester, BudgetReservationRef, CapacityReservationRef string
	State                                                                                PositionState
	ApprovalDigest                                                                       string
	External                                                                             ExternalObservation
}

func (p PositionProposal) ValidateFor(r HeadcountRequest) error {
	if strings.TrimSpace(p.Tenant) == "" || strings.TrimSpace(p.ID) == "" || p.ParentRequestID != r.ID || p.Requester != r.Requester || !p.State.Valid() {
		return fmt.Errorf("%w: position must reference the exact request and requester", ErrInvalidRelationship)
	}
	if strings.TrimSpace(p.BudgetReservationRef) == "" || strings.TrimSpace(p.CapacityReservationRef) == "" {
		return fmt.Errorf("%w: budget and capacity reservation references are required", ErrInvalidRelationship)
	}
	if p.Tenant != r.Tenant {
		return fmt.Errorf("%w: tenant mismatch", ErrInvalidRelationship)
	}
	return nil
}

type RequisitionProposal struct {
	Tenant, ID, ParentRequestID, PositionID, Requester string
	State                                              RequisitionState
	ApprovalDigest                                     string
	External                                           ExternalObservation
}

func (p RequisitionProposal) ValidateFor(r HeadcountRequest, pos PositionProposal) error {
	if strings.TrimSpace(p.Tenant) == "" || strings.TrimSpace(p.ID) == "" || p.ParentRequestID != r.ID || p.PositionID != pos.ID || p.Requester != r.Requester || !p.State.Valid() {
		return fmt.Errorf("%w: requisition must reference the exact request, position and requester", ErrInvalidRelationship)
	}
	if p.Tenant != r.Tenant || pos.Tenant != r.Tenant {
		return fmt.Errorf("%w: tenant mismatch", ErrInvalidRelationship)
	}
	return nil
}

// CapacitySnapshot carries the position-domain and budget-domain checks that
// must both pass. The domains are separate: neither quantity implies the
// other, and the baseline/fence are part of the caller's revalidation proof.
type CapacitySnapshot struct {
	Capacity, Consumed, Reserved values.Decimal
	BudgetAvailable              values.Decimal
	BaselineVersion              string
	ReservationFence             uint64
}

// RequisitionCapacity is the headcount-owned proof recruiting must present
// before opening a requisition. Available is the capacity remaining after
// position reservations and prior requisition allocations.
type RequisitionCapacity struct {
	Tenant, RequestID string
	Revision          uint64
	State             HeadcountState
	Available         values.Decimal
	BaselineVersion   string
	ReservationFence  uint64
}

// RequisitionCapacityReference is the tenant-scoped lookup key accepted by
// recruiting when it resolves headcount authorization through its port.
type RequisitionCapacityReference struct {
	Tenant, RequestID string
}

func (r RequisitionCapacityReference) Validate() error {
	if strings.TrimSpace(r.Tenant) == "" || strings.TrimSpace(r.RequestID) == "" {
		return fmt.Errorf("%w: tenant and request identity are required", ErrInvalidRelationship)
	}
	return nil
}

// RequisitionCapacityReservation is the durable allocation receipt returned
// after capacity is atomically reserved for one ATS requisition.
type RequisitionCapacityReservation struct {
	ID, Tenant, RequestID, RequisitionID string
	Amount                               values.Decimal
	CapacityRevision                     uint64
	ReservationFence                     uint64
}

func (r RequisitionCapacityReservation) Validate() error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Tenant) == "" || strings.TrimSpace(r.RequestID) == "" ||
		strings.TrimSpace(r.RequisitionID) == "" || r.CapacityRevision == 0 || r.ReservationFence == 0 {
		return fmt.Errorf("%w: capacity reservation identity and fence are required", ErrInvalidRelationship)
	}
	if err := r.Amount.Validate(); err != nil || r.Amount.Sign() <= 0 {
		return fmt.Errorf("%w: capacity reservation amount must be positive", ErrInvalidRelationship)
	}
	return nil
}

// RequisitionCapacityAllocator is the headcount-owned write port for ATS
// allocation. Implementations must atomically check current approval and
// remaining capacity, then durably reserve amount. RequisitionID is an
// idempotency key so retries return the same reservation.
type RequisitionCapacityAllocator interface {
	ReserveRequisitionCapacity(RequisitionCapacityReference, string, values.Decimal) (RequisitionCapacityReservation, error)
}

// ApprovedRequisitionCapacity derives the recruiting-facing capacity proof
// from an approved request and the current capacity reservation snapshot.
func ApprovedRequisitionCapacity(r HeadcountRequest, snapshot CapacitySnapshot) (RequisitionCapacity, error) {
	if err := r.Validate(); err != nil {
		return RequisitionCapacity{}, err
	}
	if r.State != HeadcountApproved {
		return RequisitionCapacity{}, fmt.Errorf("%w: headcount approval is required", ErrInvalidTransition)
	}
	if err := snapshot.Validate(); err != nil {
		return RequisitionCapacity{}, err
	}
	if snapshot.Capacity.Scale() != r.Capacity.Scale() || snapshot.Consumed.Scale() != r.Capacity.Scale() || snapshot.Reserved.Scale() != r.Capacity.Scale() {
		return RequisitionCapacity{}, ErrCapacityConflict
	}
	used, err := snapshot.Consumed.Add(snapshot.Reserved)
	if err != nil {
		return RequisitionCapacity{}, err
	}
	available, err := r.Capacity.Sub(used)
	if err != nil {
		return RequisitionCapacity{}, err
	}
	if available.Sign() < 0 {
		available = values.MustDecimal("0", r.Capacity.Scale(), values.RoundingExactRequired)
	}
	capacity := RequisitionCapacity{Tenant: r.Tenant, RequestID: r.ID, Revision: r.ProposalRevision,
		State: r.State, Available: available, BaselineVersion: snapshot.BaselineVersion, ReservationFence: snapshot.ReservationFence}
	if err := capacity.Validate(); err != nil {
		return RequisitionCapacity{}, err
	}
	return capacity, nil
}

func (c RequisitionCapacity) Validate() error {
	if strings.TrimSpace(c.Tenant) == "" || strings.TrimSpace(c.RequestID) == "" || c.Revision == 0 ||
		c.State != HeadcountApproved || strings.TrimSpace(c.BaselineVersion) == "" || c.ReservationFence == 0 {
		return fmt.Errorf("%w: approved capacity identity, revision and fence are required", ErrInvalidRelationship)
	}
	if err := c.Available.Validate(); err != nil || c.Available.Sign() < 0 {
		return fmt.Errorf("%w: available capacity must be a non-negative exact decimal", ErrInvalidRelationship)
	}
	return nil
}

func (s CapacitySnapshot) Validate() error {
	if strings.TrimSpace(s.BaselineVersion) == "" || s.ReservationFence == 0 {
		return fmt.Errorf("%w: capacity baseline and reservation fence are required", ErrInvalidRequest)
	}
	for name, d := range map[string]values.Decimal{"capacity": s.Capacity, "consumed": s.Consumed, "reserved": s.Reserved, "budget": s.BudgetAvailable} {
		if err := d.Validate(); err != nil || d.Sign() < 0 {
			return fmt.Errorf("%w: %s must be a non-negative exact decimal", ErrInvalidRequest, name)
		}
	}
	return nil
}

func (s CapacitySnapshot) Available() (values.Decimal, error) {
	if err := s.Validate(); err != nil {
		return values.Decimal{}, err
	}
	used, err := s.Consumed.Add(s.Reserved)
	if err != nil {
		return values.Decimal{}, err
	}
	return s.Capacity.Sub(used)
}

func CheckCapacity(s CapacitySnapshot, requested values.Decimal) error {
	available, err := s.Available()
	if err != nil {
		return err
	}
	if err := requested.Validate(); err != nil || requested.Sign() <= 0 || requested.Scale() != available.Scale() {
		return fmt.Errorf("%w: requested capacity has wrong scale or sign", ErrCapacityConflict)
	}
	if requested.Cmp(available) > 0 {
		return ErrCapacityConflict
	}
	if requested.Cmp(s.BudgetAvailable) > 0 {
		return ErrBudgetConflict
	}
	return nil
}

// ApproveHeadcount is intentionally the only operation needed to authorize
// capacity. It cannot manufacture child aggregates.
func ApproveHeadcount(r HeadcountRequest, a ApprovalCertificate) (HeadcountRequest, error) {
	if err := r.Validate(); err != nil {
		return HeadcountRequest{}, err
	}
	if r.State != HeadcountSubmitted {
		return HeadcountRequest{}, fmt.Errorf("%w: request is not submitted", ErrInvalidTransition)
	}
	if err := a.ValidateFor(r); err != nil {
		return HeadcountRequest{}, err
	}
	r.State = HeadcountApproved
	return r, nil
}

// RequestPositionCreation advances only the Position lifecycle. External
// REQUESTED or ACCEPTED is never reported as created.
func RequestPositionCreation(r HeadcountRequest, p PositionProposal, a ApprovalCertificate, capacity CapacitySnapshot) (PositionProposal, error) {
	if err := r.Validate(); err != nil {
		return PositionProposal{}, err
	}
	if r.State != HeadcountApproved {
		return PositionProposal{}, fmt.Errorf("%w: headcount approval is required", ErrInvalidTransition)
	}
	if err := p.ValidateFor(r); err != nil {
		return PositionProposal{}, err
	}
	if p.State != PositionProposed {
		return PositionProposal{}, fmt.Errorf("%w: position is not proposed", ErrInvalidTransition)
	}
	if err := a.ValidateFor(r); err != nil {
		return PositionProposal{}, err
	}
	if err := CheckCapacity(capacity, r.Capacity); err != nil {
		return PositionProposal{}, err
	}
	p.State, p.ApprovalDigest = PositionRequested, approvalDigest(a)
	return p, nil
}

func ObservePositionCreation(p PositionProposal, observation ExternalObservation) (PositionProposal, error) {
	if p.State != PositionRequested {
		return PositionProposal{}, fmt.Errorf("%w: position creation was not requested", ErrInvalidTransition)
	}
	if err := observation.Validate(); err != nil {
		return PositionProposal{}, err
	}
	p.External = observation
	if !observation.State.Complete() {
		if observation.State == ExternalAmbiguous {
			p.State = PositionReconciliationRequired
		}
		return p, ErrExternalNotApplied
	}
	p.State = PositionCreated
	return p, nil
}

func RequestRequisitionOpen(r HeadcountRequest, p PositionProposal, q RequisitionProposal, a ApprovalCertificate) (RequisitionProposal, error) {
	if err := r.Validate(); err != nil {
		return RequisitionProposal{}, err
	}
	if r.State != HeadcountApproved || p.State != PositionCreated {
		return RequisitionProposal{}, fmt.Errorf("%w: approved headcount and created position are required", ErrInvalidTransition)
	}
	if err := p.External.Validate(); err != nil || !p.External.State.Complete() {
		return RequisitionProposal{}, ErrExternalNotApplied
	}
	if err := q.ValidateFor(r, p); err != nil {
		return RequisitionProposal{}, err
	}
	if q.State != RequisitionProposed {
		return RequisitionProposal{}, fmt.Errorf("%w: requisition is not proposed", ErrInvalidTransition)
	}
	if err := a.ValidateFor(r); err != nil {
		return RequisitionProposal{}, err
	}
	q.State, q.ApprovalDigest = RequisitionRequested, approvalDigest(a)
	return q, nil
}

func ObserveRequisition(q RequisitionProposal, observation ExternalObservation) (RequisitionProposal, error) {
	if q.State != RequisitionRequested {
		return RequisitionProposal{}, fmt.Errorf("%w: requisition was not requested", ErrInvalidTransition)
	}
	if err := observation.Validate(); err != nil {
		return RequisitionProposal{}, err
	}
	q.External = observation
	if !observation.State.Complete() {
		if observation.State == ExternalAmbiguous {
			q.State = RequisitionReconciliationRequired
		}
		return q, ErrExternalNotApplied
	}
	q.State = RequisitionOpen
	return q, nil
}

func approvalDigest(a ApprovalCertificate) string {
	b := a.PolicyVersion + "|" + a.DecisionDigest + "|" + a.Requester + "|" + strings.Join(a.ApproverRefs, "|")
	sum := sha256.Sum256([]byte(b))
	return hex.EncodeToString(sum[:])
}

// Explain is reference-only and never includes compensation or budget values.
func (r HeadcountRequest) Explain() string {
	return fmt.Sprintf("headcount request %s state=%s revision=%d; position and requisition are separate transactions", r.ID, r.State, r.ProposalRevision)
}
