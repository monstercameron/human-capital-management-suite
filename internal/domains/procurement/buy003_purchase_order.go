// Package procurement owns SMB purchasing: version-bound purchase orders,
// verified supplier dispatch through durable delivery, amendments fenced on
// receipts, remainder cancellation and least-privilege supplier commitment.
//
// Purchase orders never duplicate the delivery, approval or inventory
// engines: dispatch runs on connectivity/delivery, amendment approval binds
// buyer decisions to revisions for the workflow approval owner, and received
// or invoiced quantities arrive through the BUY-004 and BILL-012 seams.
package procurement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/delivery"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrBuyContext    = errors.New("procurement: operation context is incomplete or unauthenticated")
	ErrBuyScope      = errors.New("procurement: purchase order is outside the authorized scope")
	ErrBuyValidation = errors.New("procurement: purchase order request is invalid")
	ErrBuyState      = errors.New("procurement: purchase order is not in the required state")
	ErrBuyConflict   = errors.New("procurement: idempotency key was reused with a different payload")
	ErrBuyAmend      = errors.New("procurement: amendment violates received or invoiced scope")
	ErrBuyCancel     = errors.New("procurement: cancellation is not valid")
	ErrBuyDispatch   = errors.New("procurement: dispatch cannot proceed")
	ErrBuySupplier   = errors.New("procurement: not your purchase order")
	ErrBuyAck        = errors.New("procurement: acknowledgement is not valid")
)

// Buy origins.
const (
	BuyHuman  = "human"
	BuyApp    = "app"
	BuyDevice = "device"
)

// BuyContext authenticates the buyer caller and scopes the call.
type BuyContext struct {
	TenantID  string
	CompanyID string
	SiteID    string
	ActorID   string
	Origin    string
}

func (op BuyContext) validate() error {
	if strings.TrimSpace(op.TenantID) == "" || strings.TrimSpace(op.CompanyID) == "" || strings.TrimSpace(op.ActorID) == "" {
		return fmt.Errorf("%w: tenant, company and actor are required", ErrBuyContext)
	}
	switch op.Origin {
	case BuyHuman, BuyApp, BuyDevice:
		return nil
	default:
		return fmt.Errorf("%w: origin %q is not declared", ErrBuyContext, op.Origin)
	}
}

// SupplierContext authenticates a supplier caller for one supplier.
type SupplierContext struct {
	TenantID   string
	CompanyID  string
	SupplierID string
	ActorID    string
}

func (sc SupplierContext) validate() error {
	if strings.TrimSpace(sc.TenantID) == "" || strings.TrimSpace(sc.CompanyID) == "" || strings.TrimSpace(sc.SupplierID) == "" || strings.TrimSpace(sc.ActorID) == "" {
		return fmt.Errorf("%w: supplier context is incomplete", ErrBuyContext)
	}
	return nil
}

// BuySupplierSnapshot pins supplier identity at issue. Dispatch never
// updates the bank destination; contact changes need fresh verification.
type BuySupplierSnapshot struct {
	SupplierID string
	Name       string
	ContactRef string
	BankRef    string
}

// BuyLineInput creates or amends one line.
type BuyLineInput struct {
	ItemSKU      string
	Description  string
	UOM          string
	Qty          string
	UnitPrice    string
	TaxRate      string
	DeliveryDate string
}

// BuyLine is one committed line. Received and invoiced quantities arrive
// through the receiving and payables seams; the invoiced unit price pins at
// first invoice and no amendment moves it.
type BuyLine struct {
	LineNo            int
	ItemSKU           string
	Description       string
	UOM               string
	Qty               string
	UnitPrice         string
	TaxRate           string
	DeliveryDate      string
	ReceivedQty       string
	InvoicedQty       string
	InvoicedUnitPrice string
}

// BuyPOStatus is the order lifecycle.
type BuyPOStatus string

const (
	BuyDraft         BuyPOStatus = "DRAFT"
	BuyNeedsApproval BuyPOStatus = "NEEDS_APPROVAL"
	BuyApproved      BuyPOStatus = "APPROVED"
	BuyOpen          BuyPOStatus = "OPEN"
	BuyCancelled     BuyPOStatus = "CANCELLED"
)

// BuyPO is the current revision plus lifecycle state.
type BuyPO struct {
	TenantID              string
	CompanyID             string
	SiteID                string
	POID                  string
	PONumber              string
	Revision              int
	ApprovedRevision      int
	Status                BuyPOStatus
	NeedsReview           bool
	Supplier              BuySupplierSnapshot
	Lines                 []BuyLine
	Currency              string
	CurrencyScale         int32
	Subtotal              string
	TaxTotal              string
	Total                 string
	DeliverySite          string
	DeliveryRecipient     string
	SourceRequestRef      string
	SpendAuthorizationRef string
	CreatedBy             string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// BuyPOInput creates one order. SpendAuthorizationRef records the Finance
// spend authority; observed budget alone never authorizes.
type BuyPOInput struct {
	SiteID                string
	Supplier              BuySupplierSnapshot
	Lines                 []BuyLineInput
	Currency              string
	CurrencyScale         int32
	DeliverySite          string
	DeliveryRecipient     string
	SourceRequestRef      string
	SpendAuthorizationRef string
}

// BuyRevision is one immutable revision snapshot.
type BuyRevision struct {
	Revision   int
	Lines      []BuyLine
	Subtotal   string
	TaxTotal   string
	Total      string
	Reason     string
	ApprovedBy string
	Evidence   string
	At         time.Time
}

// BuyDispatchState tracks one supplier delivery operation.
type BuyDispatchState string

const (
	BuyDispatchAccepted  BuyDispatchState = "ACCEPTED_BY_CHANNEL"
	BuyDispatchDelivered BuyDispatchState = "DELIVERED"
	BuyDispatchFailed    BuyDispatchState = "FAILED"
	BuyDispatchUnknown   BuyDispatchState = "UNKNOWN"
)

// BuyDispatchAttempt is one linked send attempt on the operation.
type BuyDispatchAttempt struct {
	No          int
	At          time.Time
	State       BuyDispatchState
	ProviderRef string
	Reason      string
}

// DispatchInput sends one revision to a verified recipient.
type DispatchInput struct {
	Recipient            string
	Channel              delivery.Channel
	VerificationEvidence string
	At                   time.Time
}

// BuyDispatch is one supplier delivery operation: explicit idempotency per
// business revision, recipient and channel, with linked resend attempts.
type BuyDispatch struct {
	TenantID        string
	CompanyID       string
	OperationID     string
	POID            string
	Revision        int
	Recipient       string
	Channel         delivery.Channel
	State           BuyDispatchState
	Attempts        []BuyDispatchAttempt
	ProviderRef     string
	OutcomeEvidence string
	EnvelopeDigest  string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// BuyAmendmentStatus is the amendment lifecycle.
type BuyAmendmentStatus string

const (
	AmendProposed  BuyAmendmentStatus = "PROPOSED"
	AmendCommitted BuyAmendmentStatus = "COMMITTED"
	AmendRejected  BuyAmendmentStatus = "REJECTED"
)

// BuyAmendment proposes a full new line set for the current revision.
type BuyAmendment struct {
	AmendmentID       string
	POID              string
	BaseRevision      int
	NewLines          []BuyLineInput
	Scope             string
	Reason            string
	ProposedBy        string
	FromProposal      string
	ReturnRef         string
	Status            BuyAmendmentStatus
	CommittedRevision int
	CreatedAt         time.Time
}

// BuyCancelLine is one line remainder.
type BuyCancelLine struct {
	LineNo       int
	OrderedQty   string
	ReceivedQty  string
	CancelledQty string
}

// BuyCancelStatus is the cancellation lifecycle.
type BuyCancelStatus string

const (
	CancelPending   BuyCancelStatus = "PENDING_SUPPLIER"
	CancelEffective BuyCancelStatus = "EFFECTIVE"
	CancelRejected  BuyCancelStatus = "REJECTED"
)

// BuyCancelDecision resolves a cancellation.
type BuyCancelDecision string

const (
	CancelAccept BuyCancelDecision = "ACCEPT"
	CancelReject BuyCancelDecision = "REJECT"
)

// BuyCancellation closes the unreceived remainder after the supplier
// response or policy decision. Received and payable history is untouched.
type BuyCancellation struct {
	CancellationID   string
	POID             string
	Revision         int
	Reason           string
	RequestedBy      string
	Lines            []BuyCancelLine
	InvoicedTotal    string
	Status           BuyCancelStatus
	Decision         string
	DecidedBy        string
	DecisionEvidence string
	CreatedAt        time.Time
}

// BuyAckStatus distinguishes live acknowledgement from outdated evidence.
type BuyAckStatus string

const (
	AckAcknowledged BuyAckStatus = "ACKNOWLEDGED"
	AckSuperseded   BuyAckStatus = "SUPERSEDED_ACK"
)

// BuyAck is one supplier acknowledgement. It carries no line detail, so a
// compromised supplier account cannot enumerate order lines through it.
type BuyAck struct {
	AckID         string
	POID          string
	Revision      int
	AckedRevision int
	SupplierID    string
	Status        BuyAckStatus
	Note          string
	Evidence      string
	At            time.Time
}

// BuyProposalStatus is the delivery-change proposal lifecycle.
type BuyProposalStatus string

const (
	ProposalPending  BuyProposalStatus = "PENDING"
	ProposalAdopted  BuyProposalStatus = "ADOPTED"
	ProposalRejected BuyProposalStatus = "REJECTED"
)

// BuyDeliveryProposal is one append-only supplier delivery change request.
type BuyDeliveryProposal struct {
	ProposalID       string
	POID             string
	BaseRevision     int
	LineNo           int
	SupplierID       string
	ProposedDate     string
	ProposedQty      string
	Reason           string
	Status           BuyProposalStatus
	DecidedBy        string
	DecisionEvidence string
	CreatedAt        time.Time
}

// BuyCommitment is the least-privilege supplier view: totals and status,
// never enumerable line rows.
type BuyCommitment struct {
	POID             string
	Revision         int
	Status           BuyPOStatus
	SupplierID       string
	Total            string
	Currency         string
	LineCount        int
	DeliveryDate     string
	PendingProposals int
	AckStatus        string
}

// BuyEvent is one immutable order fact.
type BuyEvent struct {
	Seq     int
	At      time.Time
	Kind    string
	ActorID string
	Detail  string
}

// Buy event kinds.
const (
	BuyEventCreated     = "PO_CREATED"
	BuyEventSubmitted   = "PO_SUBMITTED"
	BuyEventApproved    = "PO_APPROVED"
	BuyEventDispatched  = "PO_DISPATCHED"
	BuyEventEvidence    = "DISPATCH_EVIDENCE"
	BuyEventResend      = "DISPATCH_RESEND"
	BuyEventAmended     = "PO_AMENDED"
	BuyEventCancelled   = "PO_CANCELLED"
	BuyEventAcked       = "PO_ACKNOWLEDGED"
	BuyEventProposed    = "DELIVERY_PROPOSED"
	BuyEventAdopted     = "DELIVERY_ADOPTED"
	BuyEventReceipt     = "LINE_RECEIVED"
	BuyEventInvoice     = "LINE_INVOICED"
	BuyEventRevalidated = "SUPPLIER_REVALIDATED"
	BuyEventReviewClear = "REVIEW_CLEARED"
)

// BuyCoordinator owns purchase orders, dispatches, amendments,
// cancellations and supplier commitment. Delivery runs on the wired
// delivery.Runner; the composition root owns its store and transport.
type BuyCoordinator struct {
	mu         sync.Mutex
	dispatch   delivery.Runner
	sequence   int
	orders     map[string]*BuyPO
	revisions  map[string][]BuyRevision
	dispatches map[string]*BuyDispatch
	byBusiness map[string]string
	amendments map[string]*BuyAmendment
	cancels    map[string]*BuyCancellation
	acks       map[string]*BuyAck
	proposals  map[string]*BuyDeliveryProposal
	events     map[string][]BuyEvent
	idem       map[string]buyIdem
}

type buyIdem struct {
	Fingerprint string
	POID        string
	RefID       string
}

// NewBuyCoordinator builds an empty coordinator over the delivery runner.
func NewBuyCoordinator(dispatch delivery.Runner) *BuyCoordinator {
	return &BuyCoordinator{
		dispatch: dispatch,
		orders:   make(map[string]*BuyPO), revisions: make(map[string][]BuyRevision),
		dispatches: make(map[string]*BuyDispatch), byBusiness: make(map[string]string),
		amendments: make(map[string]*BuyAmendment), cancels: make(map[string]*BuyCancellation),
		acks: make(map[string]*BuyAck), proposals: make(map[string]*BuyDeliveryProposal),
		events: make(map[string][]BuyEvent), idem: make(map[string]buyIdem),
	}
}

func buyKey(op, tenant, company, key string) string {
	return op + "\x00" + tenant + "\x00" + company + "\x00" + strings.TrimSpace(key)
}

func (c *BuyCoordinator) checkIdem(op, tenant, company, key, fingerprint string) (buyIdem, bool, error) {
	if strings.TrimSpace(key) == "" {
		return buyIdem{}, false, nil
	}
	prior, ok := c.idem[buyKey(op, tenant, company, key)]
	if !ok {
		return buyIdem{}, false, nil
	}
	if prior.Fingerprint != fingerprint {
		return buyIdem{}, false, fmt.Errorf("%w: key %q", ErrBuyConflict, key)
	}
	return prior, true, nil
}

func (c *BuyCoordinator) storeIdem(op, tenant, company, key string, record buyIdem) {
	if strings.TrimSpace(key) == "" {
		return
	}
	c.idem[buyKey(op, tenant, company, key)] = record
}

func (c *BuyCoordinator) appendEvent(poID, kind, actorID, detail string) {
	c.events[poID] = append(c.events[poID], BuyEvent{Seq: len(c.events[poID]) + 1, At: time.Now().UTC(), Kind: kind, ActorID: actorID, Detail: detail})
}

func buyScopeOK(po *BuyPO, tenant, company string) bool {
	return po.TenantID == tenant && po.CompanyID == company
}

func buyMoney(qtyText, priceText, rateText string, scale int32) (line, tax string, err error) {
	qty, err := values.NewDecimal(strings.TrimSpace(qtyText), 18, values.RoundingExactRequired)
	if err != nil {
		return "", "", err
	}
	price, err := values.NewDecimal(strings.TrimSpace(priceText), 18, values.RoundingExactRequired)
	if err != nil {
		return "", "", err
	}
	net, err := qty.Mul(price, scale, values.RoundingHalfUp)
	if err != nil {
		return "", "", err
	}
	rate := strings.TrimSpace(rateText)
	if rate == "" {
		rate = "0"
	}
	rateDec, err := values.NewDecimal(rate, 18, values.RoundingExactRequired)
	if err != nil {
		return "", "", err
	}
	taxDec, err := net.Mul(rateDec, scale, values.RoundingHalfUp)
	if err != nil {
		return "", "", err
	}
	return net.String(), taxDec.String(), nil
}

func buyAdd(a, b string, scale int32) (string, error) {
	first, err := values.NewDecimal(strings.TrimSpace(a), scale, values.RoundingExactRequired)
	if err != nil {
		return "", err
	}
	second, err := values.NewDecimal(strings.TrimSpace(b), scale, values.RoundingExactRequired)
	if err != nil {
		return "", err
	}
	total, err := first.Add(second)
	if err != nil {
		return "", err
	}
	return total.String(), nil
}

// buyCmpQty compares two exact quantity strings.
func buyCmpQty(a, b string) (int, error) {
	first, err := values.NewDecimal(strings.TrimSpace(a), 18, values.RoundingExactRequired)
	if err != nil {
		return 0, err
	}
	second, err := values.NewDecimal(strings.TrimSpace(b), 18, values.RoundingExactRequired)
	if err != nil {
		return 0, err
	}
	return first.Cmp(second), nil
}

func buyQtyDecimals(text string) int {
	trimmed := strings.TrimSpace(text)
	if i := strings.Index(trimmed, "."); i >= 0 {
		return len(trimmed) - i - 1
	}
	return 0
}

// buyCanonical formats unscaled units at scale as an exact string.
func buyCanonical(unscaled *big.Int, scale int32) string {
	neg := unscaled.Sign() < 0
	digits := new(big.Int).Abs(unscaled).String()
	if scale == 0 {
		if neg {
			return "-" + digits
		}
		return digits
	}
	for int32(len(digits)) <= scale {
		digits = "0" + digits
	}
	text := digits[:len(digits)-int(scale)] + "." + digits[len(digits)-int(scale):]
	if neg {
		return "-" + text
	}
	return text
}

// buyRescale renders scale-18 math at a coarser exact display scale.
func buyRescale(unscaled *big.Int, toScale int32) string {
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(18-toScale)), nil)
	quotient, remainder := new(big.Int).QuoRem(new(big.Int).Set(unscaled), divisor, new(big.Int))
	_ = remainder
	return buyCanonical(quotient, toScale)
}

// buyQtySub subtracts b from a, clamped at zero, formatted at the coarsest
// input scale so remainders stay readable exact strings.
func buyQtySub(a, b string) (string, error) {
	scale := buyQtyDecimals(a)
	if decimals := buyQtyDecimals(b); decimals > scale {
		scale = decimals
	}
	first, err := values.NewDecimal(strings.TrimSpace(a), 18, values.RoundingExactRequired)
	if err != nil {
		return "", err
	}
	second, err := values.NewDecimal(strings.TrimSpace(b), 18, values.RoundingExactRequired)
	if err != nil {
		return "", err
	}
	if first.Cmp(second) <= 0 {
		return "0", nil
	}
	diff := new(big.Int).Sub(first.Unscaled(), second.Unscaled())
	return buyRescale(diff, int32(scale)), nil
}

func buyValidateLines(lines []BuyLineInput, scale int32) ([]BuyLine, string, string, string, error) {
	if len(lines) == 0 {
		return nil, "", "", "", fmt.Errorf("%w: at least one line is required", ErrBuyValidation)
	}
	out := make([]BuyLine, 0, len(lines))
	subtotal, taxTotal := "0", "0"
	zero, err := values.NewDecimal("0", scale, values.RoundingExactRequired)
	if err != nil {
		return nil, "", "", "", err
	}
	subtotal, taxTotal = zero.String(), zero.String()
	for i, line := range lines {
		sku := strings.TrimSpace(line.ItemSKU)
		description := strings.TrimSpace(line.Description)
		uom := strings.TrimSpace(line.UOM)
		qty := strings.TrimSpace(line.Qty)
		price := strings.TrimSpace(line.UnitPrice)
		if sku == "" || description == "" || uom == "" || qty == "" || price == "" {
			return nil, "", "", "", fmt.Errorf("%w: line %d needs sku, description, uom, qty and price", ErrBuyValidation, i+1)
		}
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(line.DeliveryDate)); err != nil {
			return nil, "", "", "", fmt.Errorf("%w: line %d delivery date: %v", ErrBuyValidation, i+1, err)
		}
		if cmp, err := buyCmpQty(qty, "0"); err != nil || cmp <= 0 {
			return nil, "", "", "", fmt.Errorf("%w: line %d quantity must be a positive exact decimal", ErrBuyValidation, i+1)
		}
		net, tax, err := buyMoney(qty, price, line.TaxRate, scale)
		if err != nil {
			return nil, "", "", "", fmt.Errorf("%w: line %d totals: %v", ErrBuyValidation, i+1, err)
		}
		subtotal, err = buyAdd(subtotal, net, scale)
		if err != nil {
			return nil, "", "", "", err
		}
		taxTotal, err = buyAdd(taxTotal, tax, scale)
		if err != nil {
			return nil, "", "", "", err
		}
		out = append(out, BuyLine{
			LineNo: i + 1, ItemSKU: sku, Description: description, UOM: uom,
			Qty: qty, UnitPrice: price, TaxRate: strings.TrimSpace(line.TaxRate),
			DeliveryDate: strings.TrimSpace(line.DeliveryDate), ReceivedQty: "0", InvoicedQty: "0",
		})
	}
	total, err := buyAdd(subtotal, taxTotal, scale)
	if err != nil {
		return nil, "", "", "", err
	}
	return out, subtotal, taxTotal, total, nil
}

func cloneBuyLines(in []BuyLine) []BuyLine {
	out := make([]BuyLine, len(in))
	copy(out, in)
	return out
}

// CreatePurchaseOrder drafts one order with exact totals. Creation records
// the Finance spend authority reference; nothing here spends it.
func (c *BuyCoordinator) CreatePurchaseOrder(_ context.Context, op BuyContext, input BuyPOInput, key string) (BuyPO, error) {
	if err := op.validate(); err != nil {
		return BuyPO{}, err
	}
	supplier := BuySupplierSnapshot{
		SupplierID: strings.TrimSpace(input.Supplier.SupplierID), Name: strings.TrimSpace(input.Supplier.Name),
		ContactRef: strings.TrimSpace(input.Supplier.ContactRef), BankRef: strings.TrimSpace(input.Supplier.BankRef),
	}
	if supplier.SupplierID == "" || supplier.Name == "" || supplier.ContactRef == "" {
		return BuyPO{}, fmt.Errorf("%w: supplier id, name and contact are required", ErrBuyValidation)
	}
	if strings.TrimSpace(input.SiteID) == "" || strings.TrimSpace(input.Currency) == "" {
		return BuyPO{}, fmt.Errorf("%w: site and currency are required", ErrBuyValidation)
	}
	if input.CurrencyScale < 0 || input.CurrencyScale > values.MaxScale {
		return BuyPO{}, fmt.Errorf("%w: currency scale is outside [0,38]", ErrBuyValidation)
	}
	if strings.TrimSpace(input.SpendAuthorizationRef) == "" {
		return BuyPO{}, fmt.Errorf("%w: spend authorization reference is required", ErrBuyValidation)
	}
	lines, subtotal, taxTotal, total, err := buyValidateLines(input.Lines, input.CurrencyScale)
	if err != nil {
		return BuyPO{}, err
	}
	fp := fmt.Sprintf("po-create\x00%#v", input)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("po-create", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyPO{}, err
		}
		return *c.orders[prior.POID], nil
	}
	now := time.Now().UTC()
	c.sequence++
	po := &BuyPO{
		TenantID: op.TenantID, CompanyID: op.CompanyID, SiteID: strings.TrimSpace(input.SiteID),
		POID: uuid.NewString(), PONumber: fmt.Sprintf("PO-%06d", c.sequence),
		Revision: 1, Status: BuyDraft, Supplier: supplier, Lines: lines,
		Currency: strings.TrimSpace(input.Currency), CurrencyScale: input.CurrencyScale,
		Subtotal: subtotal, TaxTotal: taxTotal, Total: total,
		DeliverySite: strings.TrimSpace(input.DeliverySite), DeliveryRecipient: strings.TrimSpace(input.DeliveryRecipient),
		SourceRequestRef: strings.TrimSpace(input.SourceRequestRef), SpendAuthorizationRef: strings.TrimSpace(input.SpendAuthorizationRef),
		CreatedBy: op.ActorID, CreatedAt: now, UpdatedAt: now,
	}
	c.orders[po.POID] = po
	c.revisions[po.POID] = []BuyRevision{{Revision: 1, Lines: cloneBuyLines(lines), Subtotal: subtotal, TaxTotal: taxTotal, Total: total, Reason: "created", ApprovedBy: op.ActorID, At: now}}
	c.appendEvent(po.POID, BuyEventCreated, op.ActorID, po.PONumber+" rev 1 "+total+" "+po.Currency)
	c.storeIdem("po-create", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID})
	return *po, nil
}

// SubmitPurchaseOrder moves a draft to buyer approval.
func (c *BuyCoordinator) SubmitPurchaseOrder(_ context.Context, op BuyContext, poID, key string) (BuyPO, error) {
	if err := op.validate(); err != nil {
		return BuyPO{}, err
	}
	fp := fmt.Sprintf("po-submit\x00%s", poID)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("po-submit", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyPO{}, err
		}
		return *c.orders[prior.POID], nil
	}
	po, ok := c.orders[strings.TrimSpace(poID)]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyPO{}, ErrBuyScope
	}
	if po.Status != BuyDraft {
		return BuyPO{}, fmt.Errorf("%w: order is %s", ErrBuyState, po.Status)
	}
	po.Status = BuyNeedsApproval
	po.UpdatedAt = time.Now().UTC()
	c.appendEvent(po.POID, BuyEventSubmitted, op.ActorID, fmt.Sprintf("rev %d", po.Revision))
	c.storeIdem("po-submit", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID})
	return *po, nil
}

// ApprovePurchaseOrder binds buyer approval to the current revision. Later
// revisions need their own approval before dispatch.
func (c *BuyCoordinator) ApprovePurchaseOrder(_ context.Context, op BuyContext, poID, approvedBy, evidence, key string) (BuyPO, error) {
	if err := op.validate(); err != nil {
		return BuyPO{}, err
	}
	if strings.TrimSpace(approvedBy) == "" || strings.TrimSpace(evidence) == "" {
		return BuyPO{}, fmt.Errorf("%w: approval needs an approver and evidence", ErrBuyValidation)
	}
	fp := fmt.Sprintf("po-approve\x00%s\x00%s\x00%s", poID, approvedBy, evidence)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("po-approve", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyPO{}, err
		}
		return *c.orders[prior.POID], nil
	}
	po, ok := c.orders[strings.TrimSpace(poID)]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyPO{}, ErrBuyScope
	}
	if po.Status != BuyNeedsApproval {
		return BuyPO{}, fmt.Errorf("%w: order is %s", ErrBuyState, po.Status)
	}
	po.Status = BuyApproved
	po.ApprovedRevision = po.Revision
	po.UpdatedAt = time.Now().UTC()
	c.appendEvent(po.POID, BuyEventApproved, op.ActorID, fmt.Sprintf("rev %d by %s", po.Revision, strings.TrimSpace(approvedBy)))
	c.storeIdem("po-approve", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID})
	return *po, nil
}

// POLookup returns the current revision within scope.
func (c *BuyCoordinator) POLookup(op BuyContext, poID string) (BuyPO, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	po, ok := c.orders[strings.TrimSpace(poID)]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyPO{}, ErrBuyScope
	}
	dup := *po
	dup.Lines = cloneBuyLines(po.Lines)
	return dup, nil
}

// PORevisions returns every revision snapshot oldest-first.
func (c *BuyCoordinator) PORevisions(op BuyContext, poID string) ([]BuyRevision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	po, ok := c.orders[strings.TrimSpace(poID)]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return nil, ErrBuyScope
	}
	history := c.revisions[po.POID]
	out := make([]BuyRevision, len(history))
	for i, rev := range history {
		out[i] = rev
		out[i].Lines = cloneBuyLines(rev.Lines)
	}
	return out, nil
}

// POTrace returns the order event history oldest-first.
func (c *BuyCoordinator) POTrace(op BuyContext, poID string) ([]BuyEvent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	po, ok := c.orders[strings.TrimSpace(poID)]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return nil, ErrBuyScope
	}
	return append([]BuyEvent(nil), c.events[po.POID]...), nil
}

// DispatchPurchaseOrder sends the approved revision to a verified recipient
// through durable delivery. Idempotency is explicit per business revision,
// recipient and channel; send outcomes are data, so ambiguous and failed
// sends return their operation instead of an error.
func (c *BuyCoordinator) DispatchPurchaseOrder(ctx context.Context, op BuyContext, poID string, input DispatchInput, key string) (BuyDispatch, error) {
	if err := op.validate(); err != nil {
		return BuyDispatch{}, err
	}
	recipient := strings.TrimSpace(input.Recipient)
	if recipient == "" || strings.TrimSpace(input.VerificationEvidence) == "" || input.At.IsZero() {
		return BuyDispatch{}, fmt.Errorf("%w: dispatch needs a recipient, verification evidence and time", ErrBuyValidation)
	}
	switch input.Channel {
	case delivery.ChannelEmail, delivery.ChannelSMS, delivery.ChannelPush, delivery.ChannelInbox:
	default:
		return BuyDispatch{}, fmt.Errorf("%w: unsupported channel %q", ErrBuyValidation, input.Channel)
	}
	trimmed := strings.TrimSpace(poID)
	c.mu.Lock()
	po, ok := c.orders[trimmed]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		c.mu.Unlock()
		return BuyDispatch{}, ErrBuyScope
	}
	fp := fmt.Sprintf("dispatch\x00%s\x00%d\x00%s\x00%s\x00%s", trimmed, po.Revision, recipient, input.Channel, strings.TrimSpace(input.VerificationEvidence))
	if prior, ok, err := c.checkIdem("dispatch", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			c.mu.Unlock()
			return BuyDispatch{}, err
		}
		c.mu.Unlock()
		return c.GetDispatchOutcome(op, prior.RefID)
	}
	if po.Status != BuyApproved && po.Status != BuyOpen {
		c.mu.Unlock()
		return BuyDispatch{}, fmt.Errorf("%w: order is %s", ErrBuyState, po.Status)
	}
	if po.ApprovedRevision != po.Revision {
		c.mu.Unlock()
		return BuyDispatch{}, fmt.Errorf("%w: revision %d is not approved", ErrBuyState, po.Revision)
	}
	business := strings.Join([]string{op.TenantID, op.CompanyID, trimmed, fmt.Sprintf("%d", po.Revision), recipient, string(input.Channel)}, "\x00")
	if existing, ok := c.byBusiness[business]; ok {
		c.storeIdem("dispatch", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: trimmed, RefID: existing})
		c.mu.Unlock()
		return c.GetDispatchOutcome(op, existing)
	}
	operation := &BuyDispatch{
		TenantID: op.TenantID, CompanyID: op.CompanyID, OperationID: uuid.NewString(),
		POID: trimmed, Revision: po.Revision, Recipient: recipient, Channel: input.Channel,
		State: BuyDispatchUnknown, CreatedAt: input.At.UTC(), UpdatedAt: input.At.UTC(),
	}
	operation.EnvelopeDigest = buyRevisionDigest(po)
	c.dispatches[operation.OperationID] = operation
	c.byBusiness[business] = operation.OperationID
	po.Status = BuyOpen
	po.UpdatedAt = time.Now().UTC()
	envelope := delivery.Envelope{
		TenantID: op.TenantID, IntentID: "po-dispatch:" + trimmed + ":rev" + fmt.Sprintf("%d", po.Revision),
		RecipientRef: recipient, EndpointRef: string(input.Channel) + ":" + recipient,
		ContentRef:  "po-artifact:" + trimmed + ":rev" + fmt.Sprintf("%d", po.Revision),
		TemplateRef: "po-dispatch-v1", Purpose: "purchase-order-dispatch", Classification: "restricted",
		Channel: input.Channel, IdempotencyKey: business, CorrelationID: operation.OperationID,
		ContentDigest: operation.EnvelopeDigest, AvailableAt: input.At.UTC(), ExpiresAt: input.At.UTC().Add(24 * time.Hour),
	}
	c.mu.Unlock()

	observation, sendErr := c.dispatch.Deliver(ctx, envelope)
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.dispatches[operation.OperationID]
	if !ok {
		return BuyDispatch{}, fmt.Errorf("%w: dispatch is gone", ErrBuyState)
	}
	current.Attempts = append(current.Attempts, BuyDispatchAttempt{No: len(current.Attempts) + 1, At: input.At.UTC(), State: BuyDispatchAccepted, ProviderRef: observation.ProviderRef, Reason: observation.Reason})
	switch {
	case sendErr == nil && observation.State == delivery.StateSubmitted:
		current.State = BuyDispatchAccepted
		current.Attempts[len(current.Attempts)-1].State = BuyDispatchAccepted
	case sendErr != nil && isBuyAmbiguous(sendErr):
		current.State = BuyDispatchUnknown
		current.Attempts[len(current.Attempts)-1].State = BuyDispatchUnknown
		current.Attempts[len(current.Attempts)-1].Reason = sendErr.Error()
	default:
		current.State = BuyDispatchFailed
		current.Attempts[len(current.Attempts)-1].State = BuyDispatchFailed
		if sendErr != nil {
			current.Attempts[len(current.Attempts)-1].Reason = sendErr.Error()
		}
	}
	current.ProviderRef = observation.ProviderRef
	current.UpdatedAt = time.Now().UTC()
	c.appendEvent(trimmed, BuyEventDispatched, op.ActorID, fmt.Sprintf("%s rev %d %s", current.OperationID, current.Revision, current.State))
	c.storeIdem("dispatch", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: trimmed, RefID: current.OperationID})
	dup := *current
	dup.Attempts = append([]BuyDispatchAttempt(nil), current.Attempts...)
	return dup, nil
}

func isBuyAmbiguous(err error) bool {
	var providerErr *delivery.ProviderError
	return errors.As(err, &providerErr) && providerErr.Ambiguous
}

func buyRevisionDigest(po *BuyPO) string {
	var parts []string
	parts = append(parts, po.POID, fmt.Sprintf("%d", po.Revision), po.Supplier.SupplierID, po.Currency, po.Total)
	for _, line := range po.Lines {
		parts = append(parts, fmt.Sprintf("%d", line.LineNo), line.ItemSKU, line.Qty, line.UnitPrice, line.DeliveryDate)
	}
	sum := sha256.Sum256([]byte("smb-buy-003-revision/v1\x00" + strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// GetDispatchOutcome returns the current known outcome without sending.
func (c *BuyCoordinator) GetDispatchOutcome(op BuyContext, operationID string) (BuyDispatch, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	dispatch, ok := c.dispatches[strings.TrimSpace(operationID)]
	if !ok || dispatch.TenantID != op.TenantID || dispatch.CompanyID != op.CompanyID {
		return BuyDispatch{}, ErrBuyScope
	}
	dup := *dispatch
	dup.Attempts = append([]BuyDispatchAttempt(nil), dispatch.Attempts...)
	return dup, nil
}

// SupplierDispatchView lists the owning supplier delivery operations with
// current known outcomes. Anything else answers as foreign.
func (c *BuyCoordinator) SupplierDispatchView(supplier SupplierContext, poID string) ([]BuyDispatch, error) {
	if err := supplier.validate(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	po, ok := c.orders[strings.TrimSpace(poID)]
	if !ok || po.TenantID != supplier.TenantID || po.CompanyID != supplier.CompanyID {
		return nil, ErrBuySupplier
	}
	if po.Supplier.SupplierID != supplier.SupplierID {
		return nil, ErrBuySupplier
	}
	var out []BuyDispatch
	for _, dispatch := range c.dispatches {
		if dispatch.POID == po.POID {
			dup := *dispatch
			dup.Attempts = append([]BuyDispatchAttempt(nil), dispatch.Attempts...)
			out = append(out, dup)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// RecordDispatchEvidence lands the terminal channel outcome. Terminal
// operations converge: further evidence returns the current state.
func (c *BuyCoordinator) RecordDispatchEvidence(_ context.Context, op BuyContext, operationID string, delivered bool, providerRef, evidence, key string) (BuyDispatch, error) {
	if err := op.validate(); err != nil {
		return BuyDispatch{}, err
	}
	if strings.TrimSpace(evidence) == "" {
		return BuyDispatch{}, fmt.Errorf("%w: dispatch evidence is required", ErrBuyValidation)
	}
	fp := fmt.Sprintf("evidence\x00%s\x00%v\x00%s\x00%s", operationID, delivered, providerRef, evidence)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("evidence", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyDispatch{}, err
		}
		dup := *c.dispatches[prior.RefID]
		dup.Attempts = append([]BuyDispatchAttempt(nil), dup.Attempts...)
		return dup, nil
	}
	dispatch, ok := c.dispatches[strings.TrimSpace(operationID)]
	if !ok || dispatch.TenantID != op.TenantID || dispatch.CompanyID != op.CompanyID {
		return BuyDispatch{}, ErrBuyScope
	}
	if dispatch.State == BuyDispatchDelivered || dispatch.State == BuyDispatchFailed {
		c.storeIdem("evidence", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: dispatch.POID, RefID: dispatch.OperationID})
		dup := *dispatch
		dup.Attempts = append([]BuyDispatchAttempt(nil), dispatch.Attempts...)
		return dup, nil
	}
	if delivered {
		dispatch.State = BuyDispatchDelivered
	} else {
		dispatch.State = BuyDispatchFailed
	}
	if strings.TrimSpace(providerRef) != "" {
		dispatch.ProviderRef = strings.TrimSpace(providerRef)
	}
	dispatch.OutcomeEvidence = strings.TrimSpace(evidence)
	dispatch.UpdatedAt = time.Now().UTC()
	c.appendEvent(dispatch.POID, BuyEventEvidence, op.ActorID, fmt.Sprintf("%s %s", dispatch.OperationID, dispatch.State))
	c.storeIdem("evidence", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: dispatch.POID, RefID: dispatch.OperationID})
	dup := *dispatch
	dup.Attempts = append([]BuyDispatchAttempt(nil), dispatch.Attempts...)
	return dup, nil
}

// ResendDispatch appends a linked attempt to one operation. Unknown external
// outcomes need recorded reconciliation first; delivered operations refuse.
func (c *BuyCoordinator) ResendDispatch(ctx context.Context, op BuyContext, operationID string, reconciled bool, evidence, key string) (BuyDispatch, error) {
	if err := op.validate(); err != nil {
		return BuyDispatch{}, err
	}
	trimmed := strings.TrimSpace(operationID)
	c.mu.Lock()
	dispatch, ok := c.dispatches[trimmed]
	if !ok || dispatch.TenantID != op.TenantID || dispatch.CompanyID != op.CompanyID {
		c.mu.Unlock()
		return BuyDispatch{}, ErrBuyScope
	}
	po, ok := c.orders[dispatch.POID]
	if !ok {
		c.mu.Unlock()
		return BuyDispatch{}, ErrBuyScope
	}
	fp := fmt.Sprintf("resend\x00%s\x00%v\x00%s", trimmed, reconciled, evidence)
	if prior, ok, err := c.checkIdem("resend", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			c.mu.Unlock()
			return BuyDispatch{}, err
		}
		c.mu.Unlock()
		return c.GetDispatchOutcome(op, prior.RefID)
	}
	if dispatch.State == BuyDispatchDelivered {
		c.mu.Unlock()
		return BuyDispatch{}, fmt.Errorf("%w: operation is already delivered", ErrBuyDispatch)
	}
	if dispatch.State == BuyDispatchUnknown && (!reconciled || strings.TrimSpace(evidence) == "") {
		c.mu.Unlock()
		return BuyDispatch{}, fmt.Errorf("%w: unknown outcome needs recorded reconciliation before resend", ErrBuyDispatch)
	}
	envelope := delivery.Envelope{
		TenantID: op.TenantID, IntentID: "po-dispatch:" + dispatch.POID + ":rev" + fmt.Sprintf("%d", dispatch.Revision),
		RecipientRef: dispatch.Recipient, EndpointRef: string(dispatch.Channel) + ":" + dispatch.Recipient,
		ContentRef:  "po-artifact:" + dispatch.POID + ":rev" + fmt.Sprintf("%d", dispatch.Revision),
		TemplateRef: "po-dispatch-v1", Purpose: "purchase-order-dispatch", Classification: "restricted",
		Channel: dispatch.Channel, IdempotencyKey: strings.Join([]string{op.TenantID, op.CompanyID, dispatch.POID, fmt.Sprintf("%d", dispatch.Revision), dispatch.Recipient, string(dispatch.Channel)}, "\x00"),
		CorrelationID: dispatch.OperationID, ContentDigest: buyRevisionDigest(po),
		AvailableAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	c.mu.Unlock()

	observation, sendErr := c.dispatch.Deliver(ctx, envelope)
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.dispatches[trimmed]
	if !ok {
		return BuyDispatch{}, fmt.Errorf("%w: dispatch is gone", ErrBuyState)
	}
	attempt := BuyDispatchAttempt{No: len(current.Attempts) + 1, At: time.Now().UTC(), State: BuyDispatchAccepted, ProviderRef: observation.ProviderRef, Reason: observation.Reason}
	switch {
	case sendErr == nil && observation.State == delivery.StateSubmitted:
		current.State = BuyDispatchAccepted
	case sendErr != nil && isBuyAmbiguous(sendErr):
		attempt.State = BuyDispatchUnknown
		attempt.Reason = sendErr.Error()
		current.State = BuyDispatchUnknown
	default:
		attempt.State = BuyDispatchFailed
		if sendErr != nil {
			attempt.Reason = sendErr.Error()
		}
		current.State = BuyDispatchFailed
	}
	current.Attempts = append(current.Attempts, attempt)
	if observation.ProviderRef != "" {
		current.ProviderRef = observation.ProviderRef
	}
	current.UpdatedAt = time.Now().UTC()
	c.appendEvent(current.POID, BuyEventResend, op.ActorID, fmt.Sprintf("%s attempt %d %s", current.OperationID, attempt.No, current.State))
	c.storeIdem("resend", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: current.POID, RefID: current.OperationID})
	dup := *current
	dup.Attempts = append([]BuyDispatchAttempt(nil), current.Attempts...)
	return dup, nil
}

// ProposePOAmendment stages a full new line set against the current
// revision. Nothing commits until the buyer approves the new version.
func (c *BuyCoordinator) ProposePOAmendment(_ context.Context, op BuyContext, poID string, newLines []BuyLineInput, scope, reason, key, returnRef string) (BuyAmendment, error) {
	if err := op.validate(); err != nil {
		return BuyAmendment{}, err
	}
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(reason) == "" {
		return BuyAmendment{}, fmt.Errorf("%w: amendment needs scope and reason", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(poID)
	fp := fmt.Sprintf("amend-propose\x00%s\x00%#v\x00%s\x00%s\x00%s", trimmed, newLines, scope, reason, returnRef)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("amend-propose", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyAmendment{}, err
		}
		return *c.amendments[prior.RefID], nil
	}
	po, ok := c.orders[trimmed]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyAmendment{}, ErrBuyScope
	}
	if po.Status == BuyCancelled {
		return BuyAmendment{}, fmt.Errorf("%w: order is cancelled", ErrBuyState)
	}
	if len(newLines) != len(po.Lines) {
		return BuyAmendment{}, fmt.Errorf("%w: amendment carries the full line set, got %d rows for %d lines", ErrBuyValidation, len(newLines), len(po.Lines))
	}
	amendment := &BuyAmendment{
		AmendmentID: uuid.NewString(), POID: trimmed, BaseRevision: po.Revision,
		NewLines: append([]BuyLineInput(nil), newLines...), Scope: strings.TrimSpace(scope), Reason: strings.TrimSpace(reason),
		ProposedBy: op.ActorID, ReturnRef: strings.TrimSpace(returnRef), Status: AmendProposed, CreatedAt: time.Now().UTC(),
	}
	c.amendments[amendment.AmendmentID] = amendment
	c.storeIdem("amend-propose", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: trimmed, RefID: amendment.AmendmentID})
	return *amendment, nil
}

// CommitPOAmendment approves and commits one amendment to a new revision.
// Quantity may not drop below accepted receipts without the explicit
// return path; invoiced prices stay pinned to their snapshots.
func (c *BuyCoordinator) CommitPOAmendment(_ context.Context, op BuyContext, amendmentID, approvedBy, evidence, key string) (BuyPO, error) {
	if err := op.validate(); err != nil {
		return BuyPO{}, err
	}
	if strings.TrimSpace(approvedBy) == "" || strings.TrimSpace(evidence) == "" {
		return BuyPO{}, fmt.Errorf("%w: amendment approval needs an approver and evidence", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(amendmentID)
	fp := fmt.Sprintf("amend-commit\x00%s\x00%s\x00%s", trimmed, approvedBy, evidence)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("amend-commit", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyPO{}, err
		}
		return *c.orders[prior.POID], nil
	}
	amendment, ok := c.amendments[trimmed]
	if !ok {
		return BuyPO{}, fmt.Errorf("%w: amendment %s", ErrBuyState, amendmentID)
	}
	po, ok := c.orders[amendment.POID]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyPO{}, ErrBuyScope
	}
	if amendment.Status == AmendCommitted {
		return *po, nil
	}
	if amendment.Status != AmendProposed {
		return BuyPO{}, fmt.Errorf("%w: amendment is %s", ErrBuyState, amendment.Status)
	}
	if amendment.BaseRevision != po.Revision {
		return BuyPO{}, fmt.Errorf("%w: amendment targets revision %d, current is %d", ErrBuyState, amendment.BaseRevision, po.Revision)
	}
	if len(amendment.NewLines) != len(po.Lines) {
		return BuyPO{}, fmt.Errorf("%w: amendment line set no longer matches", ErrBuyState)
	}
	next := make([]BuyLine, 0, len(po.Lines))
	for i, proposed := range amendment.NewLines {
		current := po.Lines[i]
		sku := strings.TrimSpace(proposed.ItemSKU)
		if sku == "" {
			sku = current.ItemSKU
		}
		if sku != current.ItemSKU || strings.TrimSpace(proposed.UOM) != "" && strings.TrimSpace(proposed.UOM) != current.UOM {
			return BuyPO{}, fmt.Errorf("%w: line %d identity is immutable", ErrBuyAmend, i+1)
		}
		qty := strings.TrimSpace(proposed.Qty)
		if qty == "" {
			qty = current.Qty
		}
		if cmp, err := buyCmpQty(qty, current.ReceivedQty); err != nil {
			return BuyPO{}, fmt.Errorf("%w: line %d quantity: %v", ErrBuyValidation, i+1, err)
		} else if cmp < 0 && amendment.ReturnRef == "" {
			if curCmp, err := buyCmpQty(qty, current.Qty); err != nil {
				return BuyPO{}, fmt.Errorf("%w: line %d quantity: %v", ErrBuyValidation, i+1, err)
			} else if curCmp < 0 {
				return BuyPO{}, fmt.Errorf("%w: line %d quantity %q drops below received %q without a return reference", ErrBuyAmend, i+1, qty, current.ReceivedQty)
			}
		}
		price := strings.TrimSpace(proposed.UnitPrice)
		if price == "" {
			price = current.UnitPrice
		}
		rate := strings.TrimSpace(proposed.TaxRate)
		if rate == "" && current.TaxRate != "" {
			rate = current.TaxRate
		}
		if _, _, err := buyMoney(qty, price, rate, po.CurrencyScale); err != nil {
			return BuyPO{}, fmt.Errorf("%w: line %d totals: %v", ErrBuyValidation, i+1, err)
		}
		date := strings.TrimSpace(proposed.DeliveryDate)
		if date == "" {
			date = current.DeliveryDate
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return BuyPO{}, fmt.Errorf("%w: line %d delivery date: %v", ErrBuyValidation, i+1, err)
		}
		description := strings.TrimSpace(proposed.Description)
		if description == "" {
			description = current.Description
		}
		next = append(next, BuyLine{
			LineNo: i + 1, ItemSKU: current.ItemSKU, Description: description, UOM: current.UOM,
			Qty: qty, UnitPrice: price, TaxRate: rate, DeliveryDate: date,
			ReceivedQty: current.ReceivedQty, InvoicedQty: current.InvoicedQty, InvoicedUnitPrice: current.InvoicedUnitPrice,
		})
	}
	subtotal, taxTotal, total, err := buyTotals(next, po.CurrencyScale)
	if err != nil {
		return BuyPO{}, err
	}
	po.Revision++
	po.Lines = next
	po.Subtotal, po.TaxTotal, po.Total = subtotal, taxTotal, total
	po.Status = BuyNeedsApproval
	po.UpdatedAt = time.Now().UTC()
	c.revisions[po.POID] = append(c.revisions[po.POID], BuyRevision{
		Revision: po.Revision, Lines: cloneBuyLines(next), Subtotal: subtotal, TaxTotal: taxTotal, Total: total,
		Reason: amendment.Reason, ApprovedBy: strings.TrimSpace(approvedBy), Evidence: strings.TrimSpace(evidence), At: time.Now().UTC(),
	})
	amendment.Status = AmendCommitted
	amendment.CommittedRevision = po.Revision
	c.appendEvent(po.POID, BuyEventAmended, op.ActorID, fmt.Sprintf("rev %d by %s: %s", po.Revision, strings.TrimSpace(approvedBy), amendment.Reason))
	c.storeIdem("amend-commit", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID, RefID: amendment.AmendmentID})
	dup := *po
	dup.Lines = cloneBuyLines(po.Lines)
	return dup, nil
}

func buyTotals(lines []BuyLine, scale int32) (string, string, string, error) {
	zero, err := values.NewDecimal("0", scale, values.RoundingExactRequired)
	if err != nil {
		return "", "", "", err
	}
	subtotal, taxTotal := zero.String(), zero.String()
	for _, line := range lines {
		net, tax, err := buyMoney(line.Qty, line.UnitPrice, line.TaxRate, scale)
		if err != nil {
			return "", "", "", err
		}
		subtotal, err = buyAdd(subtotal, net, scale)
		if err != nil {
			return "", "", "", err
		}
		taxTotal, err = buyAdd(taxTotal, tax, scale)
		if err != nil {
			return "", "", "", err
		}
	}
	total, err := buyAdd(subtotal, taxTotal, scale)
	if err != nil {
		return "", "", "", err
	}
	return subtotal, taxTotal, total, nil
}

// RejectPOAmendment records a buyer rejection with a reason.
func (c *BuyCoordinator) RejectPOAmendment(_ context.Context, op BuyContext, amendmentID, reason, key string) (BuyAmendment, error) {
	if err := op.validate(); err != nil {
		return BuyAmendment{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return BuyAmendment{}, fmt.Errorf("%w: rejection needs a reason", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(amendmentID)
	fp := fmt.Sprintf("amend-reject\x00%s\x00%s", trimmed, reason)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("amend-reject", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyAmendment{}, err
		}
		return *c.amendments[prior.RefID], nil
	}
	amendment, ok := c.amendments[trimmed]
	if !ok {
		return BuyAmendment{}, fmt.Errorf("%w: amendment %s", ErrBuyState, amendmentID)
	}
	po, ok := c.orders[amendment.POID]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyAmendment{}, ErrBuyScope
	}
	if amendment.Status != AmendProposed {
		return BuyAmendment{}, fmt.Errorf("%w: amendment is %s", ErrBuyState, amendment.Status)
	}
	amendment.Status = AmendRejected
	c.storeIdem("amend-reject", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID, RefID: amendment.AmendmentID})
	return *amendment, nil
}

// RequestPOCancellation opens the one cancellation record for the
// unreceived remainder. Duplicates return the pending record.
func (c *BuyCoordinator) RequestPOCancellation(_ context.Context, op BuyContext, poID, reason, key string) (BuyCancellation, error) {
	if err := op.validate(); err != nil {
		return BuyCancellation{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return BuyCancellation{}, fmt.Errorf("%w: cancellation needs a reason", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(poID)
	fp := fmt.Sprintf("cancel-request\x00%s\x00%s", trimmed, reason)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("cancel-request", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyCancellation{}, err
		}
		return *c.cancels[prior.RefID], nil
	}
	po, ok := c.orders[trimmed]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyCancellation{}, ErrBuyScope
	}
	for _, existing := range c.cancels {
		if existing.POID == trimmed && existing.Status == CancelPending {
			c.storeIdem("cancel-request", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: trimmed, RefID: existing.CancellationID})
			return *existing, nil
		}
	}
	if po.Status == BuyCancelled {
		return BuyCancellation{}, fmt.Errorf("%w: order is already cancelled", ErrBuyCancel)
	}
	lines := make([]BuyCancelLine, 0, len(po.Lines))
	for _, line := range po.Lines {
		remainder, err := buyQtySub(line.Qty, line.ReceivedQty)
		if err != nil {
			return BuyCancellation{}, err
		}
		lines = append(lines, BuyCancelLine{LineNo: line.LineNo, OrderedQty: line.Qty, ReceivedQty: line.ReceivedQty, CancelledQty: remainder})
	}
	invoiced, err := buyInvoicedTotal(po)
	if err != nil {
		return BuyCancellation{}, err
	}
	cancel := &BuyCancellation{
		CancellationID: uuid.NewString(), POID: trimmed, Revision: po.Revision,
		Reason: strings.TrimSpace(reason), RequestedBy: op.ActorID,
		Lines: lines, InvoicedTotal: invoiced, Status: CancelPending, CreatedAt: time.Now().UTC(),
	}
	c.cancels[cancel.CancellationID] = cancel
	c.storeIdem("cancel-request", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: trimmed, RefID: cancel.CancellationID})
	return *cancel, nil
}

func buyInvoicedTotal(po *BuyPO) (string, error) {
	zero, err := values.NewDecimal("0", po.CurrencyScale, values.RoundingExactRequired)
	if err != nil {
		return "", err
	}
	total := zero.String()
	for _, line := range po.Lines {
		if line.InvoicedQty == "" || line.InvoicedQty == "0" || line.InvoicedUnitPrice == "" {
			continue
		}
		net, _, err := buyMoney(line.InvoicedQty, line.InvoicedUnitPrice, "", po.CurrencyScale)
		if err != nil {
			return "", err
		}
		total, err = buyAdd(total, net, po.CurrencyScale)
		if err != nil {
			return "", err
		}
	}
	return total, nil
}

// ResolvePOCancellation applies the supplier response or policy decision.
// Acceptance closes future fulfillment and leaves received and payable
// history unchanged; rejection leaves the order untouched.
func (c *BuyCoordinator) ResolvePOCancellation(_ context.Context, op BuyContext, cancellationID string, decision BuyCancelDecision, responder, evidence, key string) (BuyCancellation, error) {
	if err := op.validate(); err != nil {
		return BuyCancellation{}, err
	}
	if decision != CancelAccept && decision != CancelReject {
		return BuyCancellation{}, fmt.Errorf("%w: decision must be ACCEPT or REJECT", ErrBuyValidation)
	}
	if strings.TrimSpace(responder) == "" || strings.TrimSpace(evidence) == "" {
		return BuyCancellation{}, fmt.Errorf("%w: resolution needs a responder and evidence", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(cancellationID)
	fp := fmt.Sprintf("cancel-resolve\x00%s\x00%s\x00%s\x00%s", trimmed, decision, responder, evidence)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("cancel-resolve", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyCancellation{}, err
		}
		return *c.cancels[prior.RefID], nil
	}
	cancel, ok := c.cancels[trimmed]
	if !ok {
		return BuyCancellation{}, fmt.Errorf("%w: cancellation %s", ErrBuyState, cancellationID)
	}
	po, ok := c.orders[cancel.POID]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyCancellation{}, ErrBuyScope
	}
	if cancel.Status != CancelPending {
		return *cancel, nil
	}
	cancel.Decision = string(decision)
	cancel.DecidedBy = strings.TrimSpace(responder)
	cancel.DecisionEvidence = strings.TrimSpace(evidence)
	if decision == CancelAccept {
		cancel.Status = CancelEffective
		po.Status = BuyCancelled
		po.UpdatedAt = time.Now().UTC()
		c.appendEvent(po.POID, BuyEventCancelled, op.ActorID, fmt.Sprintf("%s accepted by %s", cancel.CancellationID, cancel.DecidedBy))
	} else {
		cancel.Status = CancelRejected
	}
	c.storeIdem("cancel-resolve", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID, RefID: cancel.CancellationID})
	return *cancel, nil
}

// AcknowledgePurchaseOrder records one supplier acknowledgement. Current
// revisions acknowledge; outdated ones stay as superseded evidence with a
// buyer review flag and never rewrite anything.
func (c *BuyCoordinator) AcknowledgePurchaseOrder(_ context.Context, supplier SupplierContext, poID string, revision int, note, evidence, key string) (BuyAck, error) {
	if err := supplier.validate(); err != nil {
		return BuyAck{}, err
	}
	trimmed := strings.TrimSpace(poID)
	fp := fmt.Sprintf("ack\x00%s\x00%d\x00%s\x00%s\x00%s", trimmed, revision, supplier.SupplierID, note, evidence)
	c.mu.Lock()
	defer c.mu.Unlock()
	idemKey := buyKey("ack", supplier.TenantID, supplier.CompanyID, key)
	if strings.TrimSpace(key) != "" {
		if prior, ok := c.idem[idemKey]; ok {
			if prior.Fingerprint != fp {
				return BuyAck{}, fmt.Errorf("%w: key %q", ErrBuyConflict, key)
			}
			return *c.acks[prior.RefID], nil
		}
	}
	po, ok := c.orders[trimmed]
	if !ok || po.TenantID != supplier.TenantID || po.CompanyID != supplier.CompanyID {
		return BuyAck{}, ErrBuyScope
	}
	if po.Supplier.SupplierID != supplier.SupplierID {
		return BuyAck{}, ErrBuySupplier
	}
	if revision > po.Revision {
		return BuyAck{}, fmt.Errorf("%w: revision %d is not issued", ErrBuyAck, revision)
	}
	ack := &BuyAck{
		AckID: uuid.NewString(), POID: trimmed, Revision: po.Revision, AckedRevision: revision,
		SupplierID: supplier.SupplierID, Status: AckAcknowledged,
		Note: strings.TrimSpace(note), Evidence: strings.TrimSpace(evidence), At: time.Now().UTC(),
	}
	if revision < po.Revision {
		ack.Status = AckSuperseded
		po.NeedsReview = true
	}
	c.acks[ack.AckID] = ack
	c.appendEvent(trimmed, BuyEventAcked, supplier.ActorID, fmt.Sprintf("rev %d %s", revision, ack.Status))
	if strings.TrimSpace(key) != "" {
		c.idem[idemKey] = buyIdem{Fingerprint: fp, POID: trimmed, RefID: ack.AckID}
	}
	return *ack, nil
}

// ProposeDeliveryChange appends one supplier delivery change request. The
// committed order is untouched until the buyer adopts it.
func (c *BuyCoordinator) ProposeDeliveryChange(_ context.Context, supplier SupplierContext, poID string, revision, lineNo int, newDate, newQty, reason, key string) (BuyDeliveryProposal, error) {
	if err := supplier.validate(); err != nil {
		return BuyDeliveryProposal{}, err
	}
	if strings.TrimSpace(newDate) == "" && strings.TrimSpace(newQty) == "" {
		return BuyDeliveryProposal{}, fmt.Errorf("%w: proposal needs a date or quantity", ErrBuyValidation)
	}
	if strings.TrimSpace(reason) == "" {
		return BuyDeliveryProposal{}, fmt.Errorf("%w: proposal needs a reason", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(poID)
	fp := fmt.Sprintf("propose\x00%s\x00%d\x00%d\x00%s\x00%s\x00%s\x00%s", trimmed, revision, lineNo, supplier.SupplierID, newDate, newQty, reason)
	c.mu.Lock()
	defer c.mu.Unlock()
	idemKey := buyKey("propose", supplier.TenantID, supplier.CompanyID, key)
	if strings.TrimSpace(key) != "" {
		if prior, ok := c.idem[idemKey]; ok {
			if prior.Fingerprint != fp {
				return BuyDeliveryProposal{}, fmt.Errorf("%w: key %q", ErrBuyConflict, key)
			}
			return *c.proposals[prior.RefID], nil
		}
	}
	po, ok := c.orders[trimmed]
	if !ok || po.TenantID != supplier.TenantID || po.CompanyID != supplier.CompanyID {
		return BuyDeliveryProposal{}, ErrBuyScope
	}
	if po.Supplier.SupplierID != supplier.SupplierID {
		return BuyDeliveryProposal{}, ErrBuySupplier
	}
	if po.Status == BuyCancelled {
		return BuyDeliveryProposal{}, fmt.Errorf("%w: order is cancelled", ErrBuyState)
	}
	if revision != po.Revision {
		return BuyDeliveryProposal{}, fmt.Errorf("%w: proposal targets revision %d, current is %d", ErrBuyState, revision, po.Revision)
	}
	if lineNo < 1 || lineNo > len(po.Lines) {
		return BuyDeliveryProposal{}, fmt.Errorf("%w: line %d is out of range", ErrBuyValidation, lineNo)
	}
	if strings.TrimSpace(newDate) != "" {
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(newDate)); err != nil {
			return BuyDeliveryProposal{}, fmt.Errorf("%w: proposed date: %v", ErrBuyValidation, err)
		}
	}
	proposal := &BuyDeliveryProposal{
		ProposalID: uuid.NewString(), POID: trimmed, BaseRevision: revision, LineNo: lineNo,
		SupplierID: supplier.SupplierID, ProposedDate: strings.TrimSpace(newDate), ProposedQty: strings.TrimSpace(newQty),
		Reason: strings.TrimSpace(reason), Status: ProposalPending, CreatedAt: time.Now().UTC(),
	}
	c.proposals[proposal.ProposalID] = proposal
	c.appendEvent(trimmed, BuyEventProposed, supplier.ActorID, fmt.Sprintf("line %d: %s", lineNo, proposal.Reason))
	if strings.TrimSpace(key) != "" {
		c.idem[idemKey] = buyIdem{Fingerprint: fp, POID: trimmed, RefID: proposal.ProposalID}
	}
	return *proposal, nil
}

// AdoptDeliveryChange approves one supplier proposal into a committed new
// revision. No buyer approval, no adoption.
func (c *BuyCoordinator) AdoptDeliveryChange(_ context.Context, op BuyContext, proposalID, approvedBy, evidence, key string) (BuyPO, error) {
	if err := op.validate(); err != nil {
		return BuyPO{}, err
	}
	if strings.TrimSpace(approvedBy) == "" || strings.TrimSpace(evidence) == "" {
		return BuyPO{}, fmt.Errorf("%w: adoption needs an approver and evidence", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(proposalID)
	fp := fmt.Sprintf("adopt\x00%s\x00%s\x00%s", trimmed, approvedBy, evidence)

	c.mu.Lock()
	if prior, ok, err := c.checkIdem("adopt", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			c.mu.Unlock()
			return BuyPO{}, err
		}
		po, ok := c.orders[prior.POID]
		if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
			c.mu.Unlock()
			return BuyPO{}, ErrBuyScope
		}
		dup := *po
		dup.Lines = cloneBuyLines(po.Lines)
		c.mu.Unlock()
		return dup, nil
	}
	proposal, ok := c.proposals[trimmed]
	if !ok {
		c.mu.Unlock()
		return BuyPO{}, fmt.Errorf("%w: proposal %s", ErrBuyState, proposalID)
	}
	if proposal.Status == ProposalAdopted {
		po, ok := c.orders[proposal.POID]
		if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
			c.mu.Unlock()
			return BuyPO{}, ErrBuyScope
		}
		dup := *po
		dup.Lines = cloneBuyLines(po.Lines)
		c.storeIdem("adopt", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID, RefID: proposal.ProposalID})
		c.mu.Unlock()
		return dup, nil
	}
	if proposal.Status != ProposalPending {
		c.mu.Unlock()
		return BuyPO{}, fmt.Errorf("%w: proposal is %s", ErrBuyState, proposal.Status)
	}
	snap := *proposal
	c.mu.Unlock()

	current, err := c.POLookup(op, snap.POID)
	if err != nil {
		return BuyPO{}, err
	}
	if snap.BaseRevision != current.Revision {
		return BuyPO{}, fmt.Errorf("%w: proposal targets revision %d, current is %d", ErrBuyState, snap.BaseRevision, current.Revision)
	}
	inputs := make([]BuyLineInput, 0, len(current.Lines))
	for _, line := range current.Lines {
		inputs = append(inputs, BuyLineInput{ItemSKU: line.ItemSKU, Description: line.Description, UOM: line.UOM, Qty: line.Qty, UnitPrice: line.UnitPrice, TaxRate: line.TaxRate, DeliveryDate: line.DeliveryDate})
	}
	target := snap.LineNo - 1
	if snap.ProposedDate != "" {
		inputs[target].DeliveryDate = snap.ProposedDate
	}
	if snap.ProposedQty != "" {
		inputs[target].Qty = snap.ProposedQty
	}
	amendment, err := c.adoptAmendment(op, &snap, inputs, evidence)
	if err != nil {
		return BuyPO{}, err
	}
	revised, commitErr := c.CommitPOAmendment(context.Background(), op, amendment.AmendmentID, approvedBy, evidence, "adopt-commit:"+strings.TrimSpace(key)+":"+snap.ProposalID)
	c.mu.Lock()
	defer c.mu.Unlock()
	live, ok := c.proposals[trimmed]
	if !ok {
		return BuyPO{}, ErrBuyScope
	}
	if live.Status == ProposalAdopted {
		// A concurrent adoption won; converge on the current order.
		po, ok := c.orders[live.POID]
		if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
			return BuyPO{}, ErrBuyScope
		}
		dup := *po
		dup.Lines = cloneBuyLines(po.Lines)
		c.storeIdem("adopt", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID, RefID: live.ProposalID})
		return dup, nil
	}
	if commitErr != nil {
		return BuyPO{}, commitErr
	}
	live.Status = ProposalAdopted
	live.DecidedBy = strings.TrimSpace(approvedBy)
	live.DecisionEvidence = strings.TrimSpace(evidence)
	c.appendEvent(live.POID, BuyEventAdopted, op.ActorID, live.ProposalID)
	c.storeIdem("adopt", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: revised.POID, RefID: live.ProposalID})
	return revised, nil
}

func (c *BuyCoordinator) adoptAmendment(op BuyContext, proposal *BuyDeliveryProposal, inputs []BuyLineInput, evidence string) (BuyAmendment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	po, ok := c.orders[proposal.POID]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyAmendment{}, ErrBuyScope
	}
	amendment := &BuyAmendment{
		AmendmentID: uuid.NewString(), POID: po.POID, BaseRevision: po.Revision,
		NewLines: inputs, Scope: "adopt supplier proposal", Reason: proposal.Reason,
		ProposedBy: proposal.SupplierID, FromProposal: proposal.ProposalID, Status: AmendProposed, CreatedAt: time.Now().UTC(),
	}
	_ = evidence
	c.amendments[amendment.AmendmentID] = amendment
	return *amendment, nil
}

// RejectDeliveryChange records a buyer rejection with a reason.
func (c *BuyCoordinator) RejectDeliveryChange(_ context.Context, op BuyContext, proposalID, reason, key string) (BuyDeliveryProposal, error) {
	if err := op.validate(); err != nil {
		return BuyDeliveryProposal{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return BuyDeliveryProposal{}, fmt.Errorf("%w: rejection needs a reason", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(proposalID)
	fp := fmt.Sprintf("proposal-reject\x00%s\x00%s", trimmed, reason)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("proposal-reject", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyDeliveryProposal{}, err
		}
		return *c.proposals[prior.RefID], nil
	}
	proposal, ok := c.proposals[trimmed]
	if !ok {
		return BuyDeliveryProposal{}, fmt.Errorf("%w: proposal %s", ErrBuyState, proposalID)
	}
	po, ok := c.orders[proposal.POID]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyDeliveryProposal{}, ErrBuyScope
	}
	if proposal.Status != ProposalPending {
		return BuyDeliveryProposal{}, fmt.Errorf("%w: proposal is %s", ErrBuyState, proposal.Status)
	}
	proposal.Status = ProposalRejected
	proposal.DecidedBy = op.ActorID
	proposal.DecisionEvidence = strings.TrimSpace(reason)
	c.storeIdem("proposal-reject", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID, RefID: proposal.ProposalID})
	return *proposal, nil
}

// GetSupplierCommitment returns the least-privilege current terms for the
// owning supplier: totals and status, never enumerable line rows.
func (c *BuyCoordinator) GetSupplierCommitment(supplier SupplierContext, poID string) (BuyCommitment, error) {
	if err := supplier.validate(); err != nil {
		return BuyCommitment{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	po, ok := c.orders[strings.TrimSpace(poID)]
	if !ok || po.TenantID != supplier.TenantID || po.CompanyID != supplier.CompanyID {
		return BuyCommitment{}, ErrBuyScope
	}
	if po.Supplier.SupplierID != supplier.SupplierID {
		return BuyCommitment{}, ErrBuySupplier
	}
	commitment := BuyCommitment{
		POID: po.POID, Revision: po.Revision, Status: po.Status, SupplierID: po.Supplier.SupplierID,
		Total: po.Total, Currency: po.Currency, LineCount: len(po.Lines),
	}
	earliest := ""
	for _, line := range po.Lines {
		if earliest == "" || line.DeliveryDate < earliest {
			earliest = line.DeliveryDate
		}
	}
	commitment.DeliveryDate = earliest
	for _, proposal := range c.proposals {
		if proposal.POID == po.POID && proposal.Status == ProposalPending {
			commitment.PendingProposals++
		}
	}
	for _, ack := range c.acks {
		if ack.POID == po.POID && ack.AckedRevision == po.Revision {
			commitment.AckStatus = string(ack.Status)
		}
	}
	return commitment, nil
}

// ApplyReceiptLine records one BUY-004 goods receipt against a line.
// Acknowledgements never receive; only open orders accept receipts.
func (c *BuyCoordinator) ApplyReceiptLine(_ context.Context, op BuyContext, poID string, lineNo int, qty, receiptRef, key string) (BuyLine, error) {
	if err := op.validate(); err != nil {
		return BuyLine{}, err
	}
	if strings.TrimSpace(receiptRef) == "" {
		return BuyLine{}, fmt.Errorf("%w: receipt reference is required", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(poID)
	fp := fmt.Sprintf("receipt\x00%s\x00%d\x00%s\x00%s", trimmed, lineNo, qty, receiptRef)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("receipt", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyLine{}, err
		}
		po := c.orders[prior.POID]
		for _, line := range po.Lines {
			if line.LineNo == lineNo {
				return line, nil
			}
		}
		return BuyLine{}, fmt.Errorf("%w: line %d", ErrBuyState, lineNo)
	}
	po, ok := c.orders[trimmed]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyLine{}, ErrBuyScope
	}
	if po.Status != BuyOpen {
		return BuyLine{}, fmt.Errorf("%w: order is %s", ErrBuyState, po.Status)
	}
	if lineNo < 1 || lineNo > len(po.Lines) {
		return BuyLine{}, fmt.Errorf("%w: line %d is out of range", ErrBuyValidation, lineNo)
	}
	line := &po.Lines[lineNo-1]
	add, err := values.NewDecimal(strings.TrimSpace(qty), 18, values.RoundingExactRequired)
	if err != nil || add.Sign() <= 0 {
		return BuyLine{}, fmt.Errorf("%w: receipt quantity must be a positive exact decimal", ErrBuyValidation)
	}
	received, err := values.NewDecimal(strings.TrimSpace(line.ReceivedQty), 18, values.RoundingExactRequired)
	if err != nil {
		return BuyLine{}, err
	}
	ordered, err := values.NewDecimal(strings.TrimSpace(line.Qty), 18, values.RoundingExactRequired)
	if err != nil {
		return BuyLine{}, err
	}
	updated, err := received.Add(add)
	if err != nil {
		return BuyLine{}, err
	}
	if updated.Cmp(ordered) > 0 {
		return BuyLine{}, fmt.Errorf("%w: receipt exceeds ordered quantity", ErrBuyState)
	}
	scale := buyQtyDecimals(line.Qty)
	if decimals := buyQtyDecimals(qty); decimals > scale {
		scale = decimals
	}
	if decimals := buyQtyDecimals(line.ReceivedQty); decimals > scale {
		scale = decimals
	}
	line.ReceivedQty = buyRescale(updated.Unscaled(), int32(scale))
	po.UpdatedAt = time.Now().UTC()
	c.appendEvent(po.POID, BuyEventReceipt, op.ActorID, fmt.Sprintf("line %d %s", lineNo, strings.TrimSpace(receiptRef)))
	c.storeIdem("receipt", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID})
	return *line, nil
}

// ApplyInvoiceLine records one BILL-012 payable line. The invoiced unit
// price pins at first invoice and amendments preserve it.
func (c *BuyCoordinator) ApplyInvoiceLine(_ context.Context, op BuyContext, poID string, lineNo int, qty, invoiceRef, key string) (BuyLine, error) {
	if err := op.validate(); err != nil {
		return BuyLine{}, err
	}
	if strings.TrimSpace(invoiceRef) == "" {
		return BuyLine{}, fmt.Errorf("%w: invoice reference is required", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(poID)
	fp := fmt.Sprintf("invoice\x00%s\x00%d\x00%s\x00%s", trimmed, lineNo, qty, invoiceRef)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("invoice", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyLine{}, err
		}
		po := c.orders[prior.POID]
		for _, line := range po.Lines {
			if line.LineNo == lineNo {
				return line, nil
			}
		}
		return BuyLine{}, fmt.Errorf("%w: line %d", ErrBuyState, lineNo)
	}
	po, ok := c.orders[trimmed]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyLine{}, ErrBuyScope
	}
	if po.Status != BuyOpen && po.Status != BuyCancelled {
		return BuyLine{}, fmt.Errorf("%w: order is %s", ErrBuyState, po.Status)
	}
	if lineNo < 1 || lineNo > len(po.Lines) {
		return BuyLine{}, fmt.Errorf("%w: line %d is out of range", ErrBuyValidation, lineNo)
	}
	line := &po.Lines[lineNo-1]
	add, err := values.NewDecimal(strings.TrimSpace(qty), 18, values.RoundingExactRequired)
	if err != nil || add.Sign() <= 0 {
		return BuyLine{}, fmt.Errorf("%w: invoice quantity must be a positive exact decimal", ErrBuyValidation)
	}
	invoiced, err := values.NewDecimal(strings.TrimSpace(line.InvoicedQty), 18, values.RoundingExactRequired)
	if err != nil {
		return BuyLine{}, err
	}
	received, err := values.NewDecimal(strings.TrimSpace(line.ReceivedQty), 18, values.RoundingExactRequired)
	if err != nil {
		return BuyLine{}, err
	}
	updated, err := invoiced.Add(add)
	if err != nil {
		return BuyLine{}, err
	}
	if updated.Cmp(received) > 0 {
		return BuyLine{}, fmt.Errorf("%w: invoice exceeds received quantity", ErrBuyState)
	}
	scale := buyQtyDecimals(line.Qty)
	if decimals := buyQtyDecimals(qty); decimals > scale {
		scale = decimals
	}
	if decimals := buyQtyDecimals(line.InvoicedQty); decimals > scale {
		scale = decimals
	}
	line.InvoicedQty = buyRescale(updated.Unscaled(), int32(scale))
	if line.InvoicedUnitPrice == "" {
		line.InvoicedUnitPrice = line.UnitPrice
	}
	po.UpdatedAt = time.Now().UTC()
	c.appendEvent(po.POID, BuyEventInvoice, op.ActorID, fmt.Sprintf("line %d %s", lineNo, strings.TrimSpace(invoiceRef)))
	c.storeIdem("invoice", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID})
	return *line, nil
}

// RevalidateSupplier pins a fresh supplier snapshot with evidence after
// contact or bank detail changes.
func (c *BuyCoordinator) RevalidateSupplier(_ context.Context, op BuyContext, poID, contactRef, bankRef, evidence, key string) (BuyPO, error) {
	if err := op.validate(); err != nil {
		return BuyPO{}, err
	}
	if strings.TrimSpace(contactRef) == "" || strings.TrimSpace(evidence) == "" {
		return BuyPO{}, fmt.Errorf("%w: revalidation needs a contact and evidence", ErrBuyValidation)
	}
	trimmed := strings.TrimSpace(poID)
	fp := fmt.Sprintf("revalidate\x00%s\x00%s\x00%s\x00%s", trimmed, contactRef, bankRef, evidence)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("revalidate", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyPO{}, err
		}
		return *c.orders[prior.POID], nil
	}
	po, ok := c.orders[trimmed]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyPO{}, ErrBuyScope
	}
	po.Supplier.ContactRef = strings.TrimSpace(contactRef)
	if strings.TrimSpace(bankRef) != "" {
		po.Supplier.BankRef = strings.TrimSpace(bankRef)
	}
	po.UpdatedAt = time.Now().UTC()
	c.appendEvent(po.POID, BuyEventRevalidated, op.ActorID, po.Supplier.ContactRef)
	c.storeIdem("revalidate", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID})
	dup := *po
	dup.Lines = cloneBuyLines(po.Lines)
	return dup, nil
}

// ClearAckReview clears the outdated-acknowledgement review flag after the
// buyer has reviewed the superseded evidence.
func (c *BuyCoordinator) ClearAckReview(_ context.Context, op BuyContext, poID, key string) (BuyPO, error) {
	if err := op.validate(); err != nil {
		return BuyPO{}, err
	}
	trimmed := strings.TrimSpace(poID)
	fp := fmt.Sprintf("review-clear\x00%s", trimmed)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("review-clear", op.TenantID, op.CompanyID, key, fp); err != nil || ok {
		if err != nil {
			return BuyPO{}, err
		}
		return *c.orders[prior.POID], nil
	}
	po, ok := c.orders[trimmed]
	if !ok || !buyScopeOK(po, op.TenantID, op.CompanyID) {
		return BuyPO{}, ErrBuyScope
	}
	po.NeedsReview = false
	po.UpdatedAt = time.Now().UTC()
	c.appendEvent(po.POID, BuyEventReviewClear, op.ActorID, "reviewed")
	c.storeIdem("review-clear", op.TenantID, op.CompanyID, key, buyIdem{Fingerprint: fp, POID: po.POID})
	dup := *po
	dup.Lines = cloneBuyLines(po.Lines)
	return dup, nil
}

// buySnapshot is the durable coordinator state.
type buySnapshot struct {
	Sequence   int
	Orders     map[string]BuyPO
	Revisions  map[string][]BuyRevision
	Dispatches map[string]BuyDispatch
	ByBusiness map[string]string
	Amendments map[string]BuyAmendment
	Cancels    map[string]BuyCancellation
	Acks       map[string]BuyAck
	Proposals  map[string]BuyDeliveryProposal
	Events     map[string][]BuyEvent
	Idem       map[string]buyIdem
}

// Snapshot exports orders, revisions, dispatches, decisions and evidence.
func (c *BuyCoordinator) Snapshot() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	export := buySnapshot{
		Sequence: c.sequence, Orders: make(map[string]BuyPO, len(c.orders)),
		Revisions: c.revisions, Dispatches: make(map[string]BuyDispatch, len(c.dispatches)),
		ByBusiness: c.byBusiness, Amendments: make(map[string]BuyAmendment, len(c.amendments)),
		Cancels: make(map[string]BuyCancellation, len(c.cancels)),
		Acks:    make(map[string]BuyAck, len(c.acks)), Proposals: make(map[string]BuyDeliveryProposal, len(c.proposals)),
		Events: c.events, Idem: c.idem,
	}
	for id, po := range c.orders {
		dup := *po
		dup.Lines = cloneBuyLines(po.Lines)
		export.Orders[id] = dup
	}
	for id, dispatch := range c.dispatches {
		dup := *dispatch
		dup.Attempts = append([]BuyDispatchAttempt(nil), dispatch.Attempts...)
		export.Dispatches[id] = dup
	}
	for id, amendment := range c.amendments {
		export.Amendments[id] = *amendment
	}
	for id, cancel := range c.cancels {
		export.Cancels[id] = *cancel
	}
	for id, ack := range c.acks {
		export.Acks[id] = *ack
	}
	for id, proposal := range c.proposals {
		export.Proposals[id] = *proposal
	}
	return json.Marshal(export)
}

// Restore loads a Snapshot into an empty coordinator.
func (c *BuyCoordinator) Restore(data []byte) error {
	var export buySnapshot
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("procurement: snapshot cannot be restored: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.orders) > 0 {
		return fmt.Errorf("procurement: snapshot cannot be restored into a populated coordinator")
	}
	for id, po := range export.Orders {
		if po.POID != id || po.TenantID == "" {
			return fmt.Errorf("procurement: snapshot order %s is corrupt", id)
		}
	}
	c.sequence = export.Sequence
	c.orders = make(map[string]*BuyPO, len(export.Orders))
	for id, po := range export.Orders {
		stored := po
		stored.Lines = cloneBuyLines(po.Lines)
		c.orders[id] = &stored
	}
	c.revisions = export.Revisions
	if c.revisions == nil {
		c.revisions = make(map[string][]BuyRevision)
	}
	c.dispatches = make(map[string]*BuyDispatch, len(export.Dispatches))
	for id, dispatch := range export.Dispatches {
		stored := dispatch
		stored.Attempts = append([]BuyDispatchAttempt(nil), dispatch.Attempts...)
		c.dispatches[id] = &stored
	}
	c.byBusiness = export.ByBusiness
	if c.byBusiness == nil {
		c.byBusiness = make(map[string]string)
	}
	c.amendments = make(map[string]*BuyAmendment, len(export.Amendments))
	for id, amendment := range export.Amendments {
		stored := amendment
		c.amendments[id] = &stored
	}
	c.cancels = make(map[string]*BuyCancellation, len(export.Cancels))
	for id, cancel := range export.Cancels {
		stored := cancel
		c.cancels[id] = &stored
	}
	c.acks = make(map[string]*BuyAck, len(export.Acks))
	for id, ack := range export.Acks {
		stored := ack
		c.acks[id] = &stored
	}
	c.proposals = make(map[string]*BuyDeliveryProposal, len(export.Proposals))
	for id, proposal := range export.Proposals {
		stored := proposal
		c.proposals[id] = &stored
	}
	c.events = export.Events
	if c.events == nil {
		c.events = make(map[string][]BuyEvent)
	}
	c.idem = export.Idem
	if c.idem == nil {
		c.idem = make(map[string]buyIdem)
	}
	return nil
}
