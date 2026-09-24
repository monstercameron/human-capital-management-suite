package payinput

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrInvalidCheckoff = errors.New("payinput: invalid dues checkoff")

type CheckoffAuthorizationState string

const CheckoffAuthorizationActive CheckoffAuthorizationState = "AUTHORIZED"

// CheckoffDeductionSource is the evidence required to project one CBA
// authorization into a payroll assignment. State is a closed CBA resolution
// token; policy law remains an evidence-bearing input owned by CBA.
type CheckoffDeductionSource struct {
	State                                                  CheckoffAuthorizationState
	TenantID, WorkerRef, MembershipID, AuthorizationDigest string
	UnionRef, PeriodID                                     string
	Amount                                                 values.Decimal
	Effective                                              values.EffectiveInterval
}

// CheckoffDeduction pairs the single bounded pay-input assignment with its
// authorization lineage so the payroll line remains auditable.
type CheckoffDeduction struct {
	Assignment                                                      WorkerAssignment
	TenantID, MembershipID, AuthorizationDigest, UnionRef, PeriodID string
	Currency                                                        string
}

// NewCheckoffDeduction constructs exactly one fixed amount assignment for an
// active authorization. Definition limits bound the amount before projection.
func NewCheckoffDeduction(definition Definition, source CheckoffDeductionSource) (CheckoffDeduction, error) {
	if source.State != CheckoffAuthorizationActive || strings.TrimSpace(source.TenantID) == "" || strings.TrimSpace(source.WorkerRef) == "" ||
		strings.TrimSpace(source.MembershipID) == "" || strings.TrimSpace(source.AuthorizationDigest) == "" ||
		strings.TrimSpace(source.UnionRef) == "" || strings.TrimSpace(source.PeriodID) == "" ||
		source.Amount.Validate() != nil || source.Amount.Sign() <= 0 || source.Effective.Validate() != nil {
		return CheckoffDeduction{}, ErrInvalidCheckoff
	}
	if definition.kind() != KindDeduction {
		return CheckoffDeduction{}, fmt.Errorf("%w: definition must be a deduction", ErrInvalidCheckoff)
	}
	limits := definition.limits()
	if limits.Minimum.Validate() == nil && source.Amount.Cmp(limits.Minimum) < 0 ||
		limits.Maximum.Validate() == nil && source.Amount.Cmp(limits.Maximum) > 0 {
		return CheckoffDeduction{}, fmt.Errorf("%w: amount outside definition limits", ErrInvalidCheckoff)
	}
	assignmentID := "checkoff:" + canonicalbytes.Digest([]byte(source.TenantID+"\x00"+source.AuthorizationDigest+"\x00"+source.PeriodID))
	a, err := NewWorkerAssignment(definition, WorkerAssignment{
		AssignmentID: assignmentID, WorkerRef: source.WorkerRef, Effective: source.Effective,
		Amount: source.Amount, Recurrence: RecurrenceOneTime,
	})
	if err != nil {
		return CheckoffDeduction{}, err
	}
	return CheckoffDeduction{Assignment: a, TenantID: source.TenantID, MembershipID: source.MembershipID, AuthorizationDigest: source.AuthorizationDigest, UnionRef: source.UnionRef, PeriodID: source.PeriodID, Currency: definition.Currency}, nil
}

// Digest binds the deduction assignment to its CBA authorization, membership,
// union and exact period.
func (d CheckoffDeduction) Digest() (string, error) {
	if err := d.Assignment.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(d.TenantID) == "" || strings.TrimSpace(d.MembershipID) == "" || strings.TrimSpace(d.AuthorizationDigest) == "" || strings.TrimSpace(d.UnionRef) == "" || strings.TrimSpace(d.PeriodID) == "" || strings.TrimSpace(d.Currency) == "" {
		return "", ErrInvalidCheckoff
	}
	assignmentDigest := d.Assignment.computedDigest()
	w := canonicalbytes.New("hcmnext.domains.payinput.CheckoffDeduction", schemaVersion).
		String("assignment_digest", assignmentDigest).String("membership", d.MembershipID).
		String("tenant", d.TenantID).String("authorization_digest", d.AuthorizationDigest).String("union", d.UnionRef).String("period", d.PeriodID).String("currency", d.Currency)
	b, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(b), nil
}

type RemittanceState string

const (
	RemittanceOpen       RemittanceState = "OPEN"
	RemittanceReconciled RemittanceState = "RECONCILED"
)

// RemittanceObligation is a separate union/period liability. Reconciliation
// records remittance evidence and does not depend on payroll settlement.
type RemittanceObligation struct {
	ID, TenantID, UnionRef, PeriodID, Currency string
	Amount                                     values.Decimal
	State                                      RemittanceState
	RemittanceRef, EvidenceRef                 string
	ReconciledAmount                           values.Decimal
	DeductionDigests                           []string
}

// NewRemittanceObligation derives the union/period liability from the exact
// checkoff deductions. Callers cannot enter an unrelated amount as the source.
func NewRemittanceObligation(id string, deductions []CheckoffDeduction) (RemittanceObligation, error) {
	if strings.TrimSpace(id) == "" || len(deductions) == 0 {
		return RemittanceObligation{}, ErrInvalidCheckoff
	}
	first := deductions[0]
	amount := first.Assignment.Amount
	if _, err := first.Digest(); err != nil || first.Assignment.Rate.Validate() == nil {
		return RemittanceObligation{}, ErrInvalidCheckoff
	}
	digests := make([]string, 0, len(deductions))
	seen := make(map[string]struct{}, len(deductions))
	for i, d := range deductions {
		if d.TenantID != first.TenantID || d.UnionRef != first.UnionRef || d.PeriodID != first.PeriodID || d.Currency != first.Currency || d.Assignment.Rate.Validate() == nil {
			return RemittanceObligation{}, ErrInvalidCheckoff
		}
		digest, err := d.Digest()
		if err != nil {
			return RemittanceObligation{}, err
		}
		if _, exists := seen[digest]; exists {
			return RemittanceObligation{}, fmt.Errorf("%w: duplicate deduction", ErrInvalidCheckoff)
		}
		seen[digest] = struct{}{}
		if i > 0 {
			amount, err = amount.Add(d.Assignment.Amount)
			if err != nil {
				return RemittanceObligation{}, err
			}
		}
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	o := RemittanceObligation{ID: id, TenantID: first.TenantID, UnionRef: first.UnionRef, PeriodID: first.PeriodID, Currency: first.Currency, Amount: amount, State: RemittanceOpen, DeductionDigests: digests}
	if err := o.Validate(); err != nil {
		return RemittanceObligation{}, err
	}
	return o, nil
}

func (o RemittanceObligation) Validate() error {
	if strings.TrimSpace(o.ID) == "" || strings.TrimSpace(o.TenantID) == "" || strings.TrimSpace(o.UnionRef) == "" || strings.TrimSpace(o.PeriodID) == "" || strings.TrimSpace(o.Currency) == "" || o.Amount.Validate() != nil || o.Amount.Sign() <= 0 || len(o.DeductionDigests) == 0 {
		return ErrInvalidCheckoff
	}
	for i, d := range o.DeductionDigests {
		if strings.TrimSpace(d) == "" || (i > 0 && o.DeductionDigests[i-1] >= d) {
			return ErrInvalidCheckoff
		}
	}
	if o.State != RemittanceOpen && o.State != RemittanceReconciled {
		return ErrInvalidCheckoff
	}
	if o.State == RemittanceOpen && (o.RemittanceRef != "" || o.EvidenceRef != "" || o.ReconciledAmount.Validate() == nil) {
		return ErrInvalidCheckoff
	}
	if o.State == RemittanceReconciled && (strings.TrimSpace(o.RemittanceRef) == "" || strings.TrimSpace(o.EvidenceRef) == "" || o.ReconciledAmount.Validate() != nil || !o.Amount.Equal(o.ReconciledAmount)) {
		return ErrInvalidCheckoff
	}
	return nil
}

// Reconcile records exact remittance evidence. Payroll settlement has no input
// to this operation and cannot satisfy the obligation implicitly.
func (o RemittanceObligation) Reconcile(amount values.Decimal, remittanceRef, evidenceRef string) (RemittanceObligation, error) {
	if err := o.Validate(); err != nil {
		return RemittanceObligation{}, err
	}
	if o.State != RemittanceOpen || amount.Validate() != nil || !amount.Equal(o.Amount) || strings.TrimSpace(remittanceRef) == "" || strings.TrimSpace(evidenceRef) == "" {
		return RemittanceObligation{}, fmt.Errorf("%w: reconciliation must match the open obligation and carry evidence", ErrInvalidCheckoff)
	}
	o.State, o.RemittanceRef, o.EvidenceRef, o.ReconciledAmount = RemittanceReconciled, remittanceRef, evidenceRef, amount
	return o, nil
}

func (o RemittanceObligation) Digest() (string, error) {
	if err := o.Validate(); err != nil {
		return "", err
	}
	w := canonicalbytes.New("hcmnext.domains.payinput.RemittanceObligation", schemaVersion).
		String("id", o.ID).String("tenant", o.TenantID).String("union", o.UnionRef).String("period", o.PeriodID).String("currency", o.Currency).
		Value("amount", o.Amount).String("state", string(o.State)).String("remittance_ref", o.RemittanceRef).
		String("evidence_ref", o.EvidenceRef).Bool("reconciled_amount_present", o.ReconciledAmount.Validate() == nil).Count("deduction_digests", len(o.DeductionDigests))
	for _, digest := range o.DeductionDigests {
		w.String("deduction_digest", digest)
	}
	if o.ReconciledAmount.Validate() == nil {
		w.Value("reconciled_amount", o.ReconciledAmount)
	}
	b, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(b), nil
}
