package garnishment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const orderSchemaVersion = 1

var (
	// ErrOrderRejected is the GARN-001 seeded-defect sentinel. Intake or
	// activation without verified authority/jurisdiction/person/order/dates/
	// balance/priority/remittance evidence must fail with this error carrying
	// the offending field, state and version.
	ErrOrderRejected = errors.New("GARN_001_REJECTED")
	// ErrOrderInvalid identifies a malformed order record.
	ErrOrderInvalid = errors.New("garnishment: invalid attachment order")
)

// OrderType is the closed GARN-001 wage-attachment vocabulary.
type OrderType string

const (
	OrderChildSupport OrderType = "CHILD_SUPPORT"
	OrderTaxLevy      OrderType = "TAX_LEVY"
	OrderCreditor     OrderType = "CREDITOR"
	OrderStudentLoan  OrderType = "STUDENT_LOAN"
)

func (t OrderType) Valid() bool {
	switch t {
	case OrderChildSupport, OrderTaxLevy, OrderCreditor, OrderStudentLoan:
		return true
	default:
		return false
	}
}

// OrderStatus is the closed order lifecycle vocabulary.
type OrderStatus string

const (
	OrderPendingVerification OrderStatus = "PENDING_VERIFICATION"
	OrderActive              OrderStatus = "ACTIVE"
	OrderSuperseded          OrderStatus = "SUPERSEDED"
	OrderReleased            OrderStatus = "RELEASED"
)

// Evidence keys: every one must be bound before activation.
const (
	EvidenceAuthority    = "authority"
	EvidenceJurisdiction = "jurisdiction"
	EvidencePerson       = "person"
	EvidenceDates        = "dates"
	EvidenceBalance      = "balance"
	EvidencePriority     = "priority"
	EvidenceRemittance   = "remittance"
)

// RequiredEvidenceKeys is the ordered evidence contract.
var RequiredEvidenceKeys = []string{
	EvidenceAuthority, EvidenceJurisdiction, EvidencePerson, EvidenceDates,
	EvidenceBalance, EvidencePriority, EvidenceRemittance,
}

// IntakeRequest is the wage-attachment order as received, with the original
// document digest that anchors lineage for every later version.
type IntakeRequest struct {
	TenantID         string
	OrderID          string
	OrderType        OrderType
	Jurisdiction     string
	PersonRef        string
	IssuingAuthority string
	EffectiveFrom    time.Time
	EffectiveTo      time.Time
	ReceivedAt       time.Time
	ClaimedBalance   values.Decimal
	Priority         int
	RemittanceRef    string
	DocumentDigest   string
}

// AmendRequest changes the claimed balance with a reason. The original
// document digest is immutable: amendments never rewrite intake lineage.
type AmendRequest struct {
	ClaimedBalance values.Decimal
	Reason         string
	AmendedBy      string
}

// AttachmentOrder is one versioned wage-attachment order. Original document
// and extracted fields retain lineage through Digest and Supersedes.
type AttachmentOrder struct {
	TenantID         string
	OrderID          string
	Version          int
	OrderType        OrderType
	Jurisdiction     string
	PersonRef        string
	IssuingAuthority string
	EffectiveFrom    time.Time
	EffectiveTo      time.Time
	ReceivedAt       time.Time
	ClaimedBalance   values.Decimal
	Priority         int
	RemittanceRef    string
	DocumentDigest   string
	Evidence         map[string]string
	Status           OrderStatus
	Supersedes       string
	Digest           string
}

// OrderRejection is the stable GARN-001 failure shape.
type OrderRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *OrderRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrOrderRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the GARN_001_REJECTED sentinel to errors.Is.
func (r *OrderRejection) Unwrap() error { return ErrOrderRejected }

func orderReject(field, state, version, reason string) error {
	return &OrderRejection{Field: field, State: state, Version: version, Reason: reason}
}

func orderDigest(o AttachmentOrder) string {
	w := canonicalbytes.New("hcmnext.domains.garnishment.AttachmentOrder", orderSchemaVersion).
		String("tenant", o.TenantID).
		String("order", o.OrderID).
		Int("version", int64(o.Version)).
		String("type", string(o.OrderType)).
		String("jurisdiction", o.Jurisdiction).
		String("person", o.PersonRef).
		String("authority", o.IssuingAuthority).
		String("effective_from", o.EffectiveFrom.UTC().Format(time.RFC3339Nano)).
		String("effective_to", o.EffectiveTo.UTC().Format(time.RFC3339Nano)).
		String("received_at", o.ReceivedAt.UTC().Format(time.RFC3339Nano)).
		String("balance", o.ClaimedBalance.String()).
		Int("priority", int64(o.Priority)).
		String("remittance", o.RemittanceRef).
		String("document", o.DocumentDigest).
		String("status", string(o.Status)).
		String("supersedes", o.Supersedes)
	for _, key := range RequiredEvidenceKeys {
		w.String("evidence."+key, o.Evidence[key])
	}
	digest, err := w.Digest()
	if err != nil {
		sum := sha256.Sum256([]byte(strings.Join([]string{o.TenantID, o.OrderID, fmt.Sprintf("%d", o.Version)}, "\x00")))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	return digest
}

// IntakeOrder records version 1 of a wage-attachment order as
// PENDING_VERIFICATION. Nothing is active on intake.
func IntakeOrder(req IntakeRequest) (AttachmentOrder, error) {
	const version = "garnishment-order/v1"
	if strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.OrderID) == "" {
		return AttachmentOrder{}, orderReject("order.identity", "MISSING", version, "tenant and order id are required")
	}
	if !req.OrderType.Valid() {
		return AttachmentOrder{}, orderReject("order.type", "UNDECLARED", version, fmt.Sprintf("order type %q is not declared", req.OrderType))
	}
	if strings.TrimSpace(req.Jurisdiction) == "" || strings.TrimSpace(req.PersonRef) == "" || strings.TrimSpace(req.IssuingAuthority) == "" {
		return AttachmentOrder{}, orderReject("order.parties", "MISSING", version, "jurisdiction, person and issuing authority are required")
	}
	if req.EffectiveFrom.IsZero() || req.ReceivedAt.IsZero() {
		return AttachmentOrder{}, orderReject("order.dates", "MISSING", version, "effective-from and received-at are required")
	}
	if !req.EffectiveTo.IsZero() && !req.EffectiveFrom.Before(req.EffectiveTo) {
		return AttachmentOrder{}, orderReject("order.dates", "INVALID", version, "effective window is inverted")
	}
	if err := req.ClaimedBalance.Validate(); err != nil {
		return AttachmentOrder{}, orderReject("order.balance", "INVALID", version, "claimed balance is invalid")
	}
	if req.Priority < 0 || strings.TrimSpace(req.RemittanceRef) == "" || strings.TrimSpace(req.DocumentDigest) == "" {
		return AttachmentOrder{}, orderReject("order.remittance", "MISSING", version, "priority, remittance and document digest are required")
	}
	order := AttachmentOrder{
		TenantID: req.TenantID, OrderID: req.OrderID, Version: 1,
		OrderType: req.OrderType, Jurisdiction: req.Jurisdiction, PersonRef: req.PersonRef,
		IssuingAuthority: req.IssuingAuthority, EffectiveFrom: req.EffectiveFrom.UTC(),
		EffectiveTo: req.EffectiveTo.UTC(), ReceivedAt: req.ReceivedAt.UTC(),
		ClaimedBalance: req.ClaimedBalance, Priority: req.Priority,
		RemittanceRef: req.RemittanceRef, DocumentDigest: req.DocumentDigest,
		Evidence: map[string]string{}, Status: OrderPendingVerification,
	}
	order.Digest = orderDigest(order)
	return order, nil
}

// verified reports whether every required evidence key is bound.
func (o AttachmentOrder) verified() (string, bool) {
	for _, key := range RequiredEvidenceKeys {
		if strings.TrimSpace(o.Evidence[key]) == "" {
			return key, false
		}
	}
	return "", true
}

// Verify binds evidence refs to the order. Partial evidence is refused:
// there is no partially-verified state.
func (o AttachmentOrder) Verify(evidence map[string]string) (AttachmentOrder, error) {
	const version = "garnishment-order/v1"
	if o.Status != OrderPendingVerification {
		return AttachmentOrder{}, orderReject("order.status", "IMMUTABLE", version, "only pending orders accept evidence")
	}
	for _, key := range RequiredEvidenceKeys {
		if strings.TrimSpace(evidence[key]) == "" {
			return AttachmentOrder{}, orderReject("evidence."+key, "UNVERIFIED", version, fmt.Sprintf("evidence %q is required", key))
		}
	}
	next := o
	next.Evidence = map[string]string{}
	for k, v := range evidence {
		next.Evidence[k] = v
	}
	next.Digest = orderDigest(next)
	return next, nil
}

// Activate moves a verified order to ACTIVE.
func (o AttachmentOrder) Activate(by string) (AttachmentOrder, error) {
	return o.ActivateFor(o.TenantID, o.PersonRef, by)
}

// ActivateFor activates only when tenant and person bindings match exactly.
func (o AttachmentOrder) ActivateFor(tenant, person, by string) (AttachmentOrder, error) {
	const version = "garnishment-order/v1"
	if tenant != o.TenantID {
		return AttachmentOrder{}, orderReject("order.tenant", "MISMATCH", version, "cross-tenant activation is refused")
	}
	if person != o.PersonRef {
		return AttachmentOrder{}, orderReject("order.person", "MISMATCH", version, "activation is bound to the verified worker")
	}
	if strings.TrimSpace(by) == "" {
		return AttachmentOrder{}, orderReject("order.activated_by", "MISSING", version, "activation needs an operator")
	}
	if o.Status != OrderPendingVerification {
		return AttachmentOrder{}, orderReject("order.status", "IMMUTABLE", version, "only pending orders activate")
	}
	if key, ok := o.verified(); !ok {
		return AttachmentOrder{}, orderReject("evidence."+key, "UNVERIFIED", version, "unverified orders cannot activate")
	}
	next := o
	next.Status = OrderActive
	next.Digest = orderDigest(next)
	return next, nil
}

// Amend issues the next version with a corrected balance. The prior version
// is superseded by digest link, and the original document digest is
// immutable across every version.
func (o AttachmentOrder) Amend(req AmendRequest) (AttachmentOrder, error) {
	const version = "garnishment-order/v1"
	if err := req.ClaimedBalance.Validate(); err != nil {
		return AttachmentOrder{}, orderReject("order.balance", "INVALID", version, "amended balance is invalid")
	}
	if strings.TrimSpace(req.Reason) == "" || strings.TrimSpace(req.AmendedBy) == "" {
		return AttachmentOrder{}, orderReject("order.amendment", "MISSING", version, "amendment needs a reason and an author")
	}
	next := o
	next.Version = o.Version + 1
	next.ClaimedBalance = req.ClaimedBalance
	next.Evidence = map[string]string{}
	next.Status = OrderPendingVerification
	next.Supersedes = o.Digest
	next.Digest = orderDigest(next)
	return next, nil
}

// Release closes an active order; released orders never reactivate.
func (o AttachmentOrder) Release(by, reason string) (AttachmentOrder, error) {
	const version = "garnishment-order/v1"
	if o.Status != OrderActive {
		return AttachmentOrder{}, orderReject("order.status", "IMMUTABLE", version, "only active orders release")
	}
	if strings.TrimSpace(by) == "" || strings.TrimSpace(reason) == "" {
		return AttachmentOrder{}, orderReject("order.release", "MISSING", version, "release needs an operator and a reason")
	}
	next := o
	next.Status = OrderReleased
	next.Digest = orderDigest(next)
	return next, nil
}
