// Material returns and inspected disposition for SMB-STOCK-010.
//
// Jobs and workers return leftovers; returned goods must not become usable
// before condition is checked. Physical receipts land quarantine-held by
// default, the returnable allowance is consumed atomically at receipt
// (before inspection), and serial/lot identity must match the recorded
// original issue. Inspection splits each receipt into accepted,
// quarantine-held, disposed and rejected buckets; resolution emits restock
// intents for accepted goods and links at most one credit request per
// return. Stock disposition never executes a financial remedy: resolutions
// carry no refund state, and unavailable finance reads as pending outside
// inventory, never completed.
//
// Original-issue facts are recorded through RecordIssueLine, the seam the
// fulfillment owner (SMB-STOCK-004) feeds; returns fence against recorded
// lines only. Serialized equipment receives require a complete asset-owner
// binding (the AssetBinding shape owned by SMB-STOCK-008); revision
// reconciliation against the asset owner stays an ASSET-series seam.
package stock

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrReturnValidation = errors.New("stock: material return request is invalid")
	ErrReturnConflict   = errors.New("stock: material return revision conflict")
)

// ReturnSourceKind names the original-fulfillment lineage a return traces to.
type ReturnSourceKind string

const (
	ReturnSourceJobIssue         ReturnSourceKind = "JOB_ISSUE"
	ReturnSourceWorkerIssue      ReturnSourceKind = "WORKER_ISSUE"
	ReturnSourceCustomerShipment ReturnSourceKind = "CUSTOMER_SHIPMENT"
)

func (k ReturnSourceKind) valid() bool {
	switch k {
	case ReturnSourceJobIssue, ReturnSourceWorkerIssue, ReturnSourceCustomerShipment:
		return true
	default:
		return false
	}
}

// ReturnCondition is the observed condition at physical receipt.
type ReturnCondition string

const (
	ReturnConditionUnopened ReturnCondition = "UNOPENED"
	ReturnConditionUsed     ReturnCondition = "USED"
	ReturnConditionDamaged  ReturnCondition = "DAMAGED"
)

func (c ReturnCondition) valid() bool {
	switch c {
	case ReturnConditionUnopened, ReturnConditionUsed, ReturnConditionDamaged:
		return true
	default:
		return false
	}
}

// ReturnRemedy names the requested commercial remedy. Replacement ships
// through SALE and credit decides in BILL; this package only records the
// request and its evidence.
type ReturnRemedy string

const (
	ReturnRemedyRestock       ReturnRemedy = "RESTOCK"
	ReturnRemedyReplacement   ReturnRemedy = "REPLACEMENT"
	ReturnRemedyCreditRequest ReturnRemedy = "CREDIT_REQUEST"
)

func (r ReturnRemedy) valid() bool {
	switch r {
	case ReturnRemedyRestock, ReturnRemedyReplacement, ReturnRemedyCreditRequest:
		return true
	default:
		return false
	}
}

// ReturnStatus is the return lifecycle. Discrepancy blocks receiving until
// an explicit review decision; closed is terminal.
type ReturnStatus string

const (
	ReturnStatusAuthorized  ReturnStatus = "AUTHORIZED"
	ReturnStatusDiscrepancy ReturnStatus = "DISCREPANCY"
	ReturnStatusClosed      ReturnStatus = "CLOSED"
)

// IssueLine is the recorded original-fulfillment fact returns fence
// against: physically fulfilled quantity plus the lot/serial snapshot and
// typed source refs.
type IssueLine struct {
	LineID       string           `json:"line_id"`
	TenantID     string           `json:"tenant_id"`
	CompanyID    string           `json:"company_id"`
	SiteID       string           `json:"site_id"`
	ItemID       string           `json:"item_id"`
	Quantity     values.Decimal   `json:"quantity"`
	LotID        string           `json:"lot_id,omitempty"`
	Serials      []string         `json:"serials,omitempty"`
	SourceKind   ReturnSourceKind `json:"source_kind"`
	SourceRef    string           `json:"source_ref"`
	OrderReceipt string           `json:"order_receipt,omitempty"`
}

// RecordIssueInput records one original-fulfillment line. Recording the
// same line ID with identical content is idempotent; a changed payload
// conflicts.
type RecordIssueInput struct {
	LineID       string
	SiteID       string
	ItemID       string
	Quantity     values.Decimal
	LotID        string
	Serials      []string
	SourceKind   ReturnSourceKind
	SourceRef    string
	OrderReceipt string
}

// MaterialReturn is one bounded return authorization with its receipts,
// inspections, discrepancy log and terminal resolution.
type MaterialReturn struct {
	ReturnID       string              `json:"return_id"`
	TenantID       string              `json:"tenant_id"`
	CompanyID      string              `json:"company_id"`
	SiteID         string              `json:"site_id"`
	ItemID         string              `json:"item_id"`
	LineID         string              `json:"line_id,omitempty"`
	Requested      values.Decimal      `json:"requested"`
	Received       values.Decimal      `json:"received"`
	ReturningParty string              `json:"returning_party"`
	Status         ReturnStatus        `json:"status"`
	Revision       uint64              `json:"revision"`
	Unlinked       bool                `json:"unlinked"`
	PermissionRef  string              `json:"permission_ref,omitempty"`
	LinkEvidence   string              `json:"link_evidence,omitempty"`
	SubstituteLot  string              `json:"substitute_lot,omitempty"`
	SubstituteSer  []string            `json:"substitute_serials,omitempty"`
	Receipts       []MaterialReceipt   `json:"receipts,omitempty"`
	Discrepancies  []ReturnDiscrepancy `json:"discrepancies,omitempty"`
	Resolution     *ReturnResolution   `json:"resolution,omitempty"`
	Cancelled      bool                `json:"cancelled"`
	CancelReason   string              `json:"cancel_reason,omitempty"`
	OpenedAt       time.Time           `json:"opened_at"`
}

// MaterialReceipt is one physical receipt. Everything received lands
// quarantine-held; accepted stays zero until inspection dispositions it.
type MaterialReceipt struct {
	ReceiptID      string            `json:"receipt_id"`
	ReturnID       string            `json:"return_id"`
	Quantity       values.Decimal    `json:"quantity"`
	LotID          string            `json:"lot_id,omitempty"`
	Serials        []string          `json:"serials,omitempty"`
	Condition      ReturnCondition   `json:"condition"`
	Evidence       string            `json:"evidence,omitempty"`
	Actor          string            `json:"actor"`
	At             time.Time         `json:"at"`
	Binding        *AssetBinding     `json:"binding,omitempty"`
	QuarantineHeld values.Decimal    `json:"quarantine_held"`
	Accepted       values.Decimal    `json:"accepted"`
	Inspected      bool              `json:"inspected"`
	Inspection     *ReturnInspection `json:"inspection,omitempty"`
}

// ReturnInspection splits one receipt across the four dispositions. At most
// one inspection exists per receipt.
type ReturnInspection struct {
	ReceiptID         string         `json:"receipt_id"`
	Inspector         string         `json:"inspector"`
	Accepted          values.Decimal `json:"accepted"`
	Quarantined       values.Decimal `json:"quarantined"`
	Disposed          values.Decimal `json:"disposed"`
	Rejected          values.Decimal `json:"rejected"`
	AcceptedSerials   []string       `json:"accepted_serials,omitempty"`
	DisposeReason     string         `json:"dispose_reason,omitempty"`
	ReturnShipmentRef string         `json:"return_shipment_ref,omitempty"`
	At                time.Time      `json:"at"`
	ReturnRevision    uint64         `json:"return_revision"`
}

// ReturnDiscrepancy records presented-but-unmatched identity plus its
// review outcome. A rejected receive changes zero stock; the discrepancy
// log carries the mismatch until a reviewer decides.
type ReturnDiscrepancy struct {
	Key              string    `json:"key"`
	PresentedLot     string    `json:"presented_lot,omitempty"`
	PresentedSerials []string  `json:"presented_serials,omitempty"`
	Reason           string    `json:"reason"`
	Evidence         string    `json:"evidence"`
	Actor            string    `json:"actor"`
	At               time.Time `json:"at"`
	Resolved         bool      `json:"resolved"`
	Decision         string    `json:"decision,omitempty"`
	Reviewer         string    `json:"reviewer,omitempty"`
	ResolveEvidence  string    `json:"resolve_evidence,omitempty"`
}

// RestockIntent asks the movement owner to restore inspected-good units to
// usable stock. It is data, not a posting.
type RestockIntent struct {
	ReturnID  string         `json:"return_id"`
	ReceiptID string         `json:"receipt_id"`
	ItemID    string         `json:"item_id"`
	LotID     string         `json:"lot_id,omitempty"`
	Serials   []string       `json:"serials,omitempty"`
	Quantity  values.Decimal `json:"quantity"`
	Evidence  string         `json:"evidence,omitempty"`
}

// QuarantineHold keeps inspected units quarantine-held with a reason.
type QuarantineHold struct {
	ReturnID  string         `json:"return_id"`
	ReceiptID string         `json:"receipt_id"`
	ItemID    string         `json:"item_id"`
	Quantity  values.Decimal `json:"quantity"`
	Reason    string         `json:"reason"`
}

// DisposalRecord removes inspected units through a named adjustment.
type DisposalRecord struct {
	ReturnID  string         `json:"return_id"`
	ReceiptID string         `json:"receipt_id"`
	ItemID    string         `json:"item_id"`
	Quantity  values.Decimal `json:"quantity"`
	Reason    string         `json:"reason"`
}

// RejectionRecord keeps a physically received rejected return in
// attributable custody: quarantine-held or a return-to-sender shipment. A
// rejection never restores returnable allowance.
type RejectionRecord struct {
	ReturnID       string         `json:"return_id"`
	ReceiptID      string         `json:"receipt_id"`
	ItemID         string         `json:"item_id"`
	Quantity       values.Decimal `json:"quantity"`
	ShipmentRef    string         `json:"shipment_ref,omitempty"`
	HoldQuarantine bool           `json:"hold_quarantine"`
}

// CreditRequest links one return to its original order receipt for the
// BILL owner. Finance decides credit; stock only links, exactly once.
type CreditRequest struct {
	ReturnID     string         `json:"return_id"`
	OrderReceipt string         `json:"order_receipt"`
	ItemID       string         `json:"item_id"`
	Quantity     values.Decimal `json:"quantity"`
}

// ReturnBuckets totals one return across its inspections.
type ReturnBuckets struct {
	Requested   values.Decimal `json:"requested"`
	Received    values.Decimal `json:"received"`
	Accepted    values.Decimal `json:"accepted"`
	Quarantined values.Decimal `json:"quarantined"`
	Disposed    values.Decimal `json:"disposed"`
	Rejected    values.Decimal `json:"rejected"`
}

// ReturnResolution closes a return with its buckets and emitted records.
type ReturnResolution struct {
	ReturnID          string            `json:"return_id"`
	Buckets           ReturnBuckets     `json:"buckets"`
	Restock           []RestockIntent   `json:"restock,omitempty"`
	Holds             []QuarantineHold  `json:"holds,omitempty"`
	Disposals         []DisposalRecord  `json:"disposals,omitempty"`
	Rejections        []RejectionRecord `json:"rejections,omitempty"`
	Credit            *CreditRequest    `json:"credit,omitempty"`
	Remedy            ReturnRemedy      `json:"remedy"`
	AdditionalRemedy  ReturnRemedy      `json:"additional_remedy,omitempty"`
	SplitExceptionRef string            `json:"split_exception_ref,omitempty"`
	Actor             string            `json:"actor"`
	At                time.Time         `json:"at"`
}

// BeginReturnInput opens a bounded linked return authorization.
type BeginReturnInput struct {
	LineID         string
	Requested      values.Decimal
	ReturningParty string
	At             time.Time
}

// BeginUnlinkedInput opens a return with no original line. It requires a
// separately authorized permission plus evidence, and can never draw on a
// linked line allowance.
type BeginUnlinkedInput struct {
	SiteID         string
	ItemID         string
	Quantity       values.Decimal
	PermissionRef  string
	Evidence       string
	ReturningParty string
	At             time.Time
}

// ReceiveReturnInput records one physical receipt. Everything received
// lands quarantine-held; the lot/serial identity must match the recorded
// line (or an accepted substitute), and serialized lines require a
// complete asset-owner binding.
type ReceiveReturnInput struct {
	ReturnID         string
	ExpectedRevision uint64
	Key              string
	Quantity         values.Decimal
	LotID            string
	Serials          []string
	Condition        ReturnCondition
	Evidence         string
	Actor            string
	At               time.Time
	Binding          *AssetBinding
}

// InspectReturnInput splits one receipt across dispositions. Unset
// quantities read as zero; the four must sum exactly to the receipt.
// Inspector must differ from the receiving actor (maker-checker).
type InspectReturnInput struct {
	ReturnID          string
	ReceiptID         string
	Key               string
	Accepted          values.Decimal
	Quarantined       values.Decimal
	Disposed          values.Decimal
	Rejected          values.Decimal
	AcceptedSerials   []string
	DisposeReason     string
	ReturnShipmentRef string
	Inspector         string
	At                time.Time
}

// ResolveReturnInput closes a fully inspected return. Replacement plus
// full credit together require an explicit split exception; a credit
// request requires the original order receipt.
type ResolveReturnInput struct {
	ReturnID          string
	ExpectedRevision  uint64
	Key               string
	Remedy            ReturnRemedy
	AdditionalRemedy  ReturnRemedy
	SplitExceptionRef string
	Actor             string
	At                time.Time
}

// CancelReturnInput closes an unreceived return, releasing its held
// authorization. Anything received must resolve through disposition.
type CancelReturnInput struct {
	ReturnID         string
	ExpectedRevision uint64
	Key              string
	Reason           string
	Actor            string
	At               time.Time
}

// EscalateDiscrepancyInput parks a return after presented identity failed
// to match. The failed receive changed zero stock; this log carries the
// mismatch until review.
type EscalateDiscrepancyInput struct {
	ReturnID         string
	ExpectedRevision uint64
	Key              string
	PresentedLot     string
	PresentedSerials []string
	Reason           string
	Evidence         string
	Actor            string
	At               time.Time
}

// Discrepancy decisions: accept the presented identity as an authorized
// substitute, or reject it so the return can retry with correct identity.
const (
	DiscrepancyAcceptSubstitute = "ACCEPT_SUBSTITUTE"
	DiscrepancyReject           = "REJECT"
)

// ResolveDiscrepancyInput decides one open discrepancy.
type ResolveDiscrepancyInput struct {
	ReturnID         string
	ExpectedRevision uint64
	Key              string
	DiscrepancyKey   string
	Decision         string
	Reviewer         string
	Evidence         string
	At               time.Time
}

// LineStatement shows issued, returned and net quantities separately.
type LineStatement struct {
	LineID   string
	ItemID   string
	Issued   values.Decimal
	Returned values.Decimal
	Net      values.Decimal
}

// UnlinkedAuthorizer approves unlinked-return permission refs. A nil
// authorizer denies every unlinked return (fail closed).
type UnlinkedAuthorizer func(op OperationContext, permissionRef string) error

type returnIdem struct {
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
	Kind        string `json:"kind"`
	ReturnID    string `json:"return_id"`
	ReceiptID   string `json:"receipt_id,omitempty"`
}

// ReturnRegistry is the concurrency-safe returns port. Results replay from
// validated state (looked up by operation key), never from stored
// payloads, so idempotency records cannot smuggle forged outcomes.
type ReturnRegistry struct {
	mu      sync.Mutex
	lines   map[string]*IssueLine
	returns map[string]*MaterialReturn
	idem    map[string]returnIdem
	auth    UnlinkedAuthorizer
	seq     uint64
}

// NewReturnRegistry builds an empty registry. A nil authorizer denies all
// unlinked returns.
func NewReturnRegistry(auth UnlinkedAuthorizer) *ReturnRegistry {
	return &ReturnRegistry{
		lines:   make(map[string]*IssueLine),
		returns: make(map[string]*MaterialReturn),
		idem:    make(map[string]returnIdem),
		auth:    auth,
	}
}

func returnLineKey(op OperationContext, lineID string) string {
	return scopeKey(op.TenantID, op.CompanyID, "line\x00"+lineID)
}

func returnKey(op OperationContext, returnID string) string {
	return scopeKey(op.TenantID, op.CompanyID, "return\x00"+returnID)
}

func returnVisible(tenantID, companyID, siteID string, op OperationContext) bool {
	if tenantID != op.TenantID || companyID != op.CompanyID {
		return false
	}
	return op.SiteID == "" || siteID == op.SiteID
}

func zeroReturnDecimal(scale int32) (values.Decimal, error) {
	return values.NewDecimal("0", scale, values.RoundingExactRequired)
}

func returnDecOrZero(d values.Decimal, scale int32) (values.Decimal, error) {
	if err := d.Validate(); err != nil {
		return zeroReturnDecimal(scale)
	}
	if d.Scale() != scale {
		return values.Decimal{}, fmt.Errorf("%w: quantity scale must match", ErrReturnValidation)
	}
	if d.Sign() < 0 {
		return values.Decimal{}, fmt.Errorf("%w: quantities cannot be negative", ErrReturnValidation)
	}
	return d, nil
}

func normalizeReturnSerials(serials []string) ([]string, error) {
	seen := make(map[string]bool, len(serials))
	out := make([]string, 0, len(serials))
	for _, raw := range serials {
		serial := normalizeSerial(raw)
		if serial == "" {
			return nil, fmt.Errorf("%w: serials must be nonempty", ErrReturnValidation)
		}
		if seen[serial] {
			return nil, fmt.Errorf("%w: duplicate serial %q", ErrReturnValidation, serial)
		}
		seen[serial] = true
		out = append(out, serial)
	}
	sort.Strings(out)
	return out, nil
}

func validReturnBinding(binding *AssetBinding) bool {
	return binding != nil && strings.TrimSpace(binding.AssetID) != "" &&
		strings.TrimSpace(binding.CustodyRev) != "" && strings.TrimSpace(binding.ServiceabilityRev) != "" &&
		strings.TrimSpace(binding.MovementOp) != ""
}

func (r *ReturnRegistry) checkReturnIdem(kind string, op OperationContext, key, fp string) (returnIdem, bool, error) {
	if strings.TrimSpace(key) == "" {
		return returnIdem{}, false, nil
	}
	mapKey := scopeKey(op.TenantID, op.CompanyID, kind+"\x00"+strings.TrimSpace(key))
	prior, ok := r.idem[mapKey]
	if !ok {
		return returnIdem{}, false, nil
	}
	if prior.Fingerprint != fp {
		return returnIdem{}, false, fmt.Errorf("%w: key %q", ErrIdempotencyConflict, key)
	}
	return prior, true, nil
}

func (r *ReturnRegistry) storeReturnIdem(kind string, op OperationContext, key, fp, returnID, receiptID string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	mapKey := scopeKey(op.TenantID, op.CompanyID, kind+"\x00"+strings.TrimSpace(key))
	r.idem[mapKey] = returnIdem{Key: strings.TrimSpace(key), Fingerprint: fp, Kind: kind, ReturnID: returnID, ReceiptID: receiptID}
}

// RecordIssueLine records one original-fulfillment fact. Repeating the
// same line ID with identical content returns the recorded line; a changed
// payload conflicts.
func (r *ReturnRegistry) RecordIssueLine(op OperationContext, input RecordIssueInput) (IssueLine, error) {
	if err := op.validate(); err != nil {
		return IssueLine{}, err
	}
	if strings.TrimSpace(input.LineID) == "" || strings.TrimSpace(input.SiteID) == "" || strings.TrimSpace(input.ItemID) == "" {
		return IssueLine{}, fmt.Errorf("%w: line, site and item are required", ErrReturnValidation)
	}
	if err := input.Quantity.Validate(); err != nil || input.Quantity.Sign() <= 0 {
		return IssueLine{}, fmt.Errorf("%w: issued quantity must be positive", ErrReturnValidation)
	}
	if !input.SourceKind.valid() {
		return IssueLine{}, fmt.Errorf("%w: source kind %q is unknown", ErrReturnValidation, input.SourceKind)
	}
	if strings.TrimSpace(input.SourceRef) == "" {
		return IssueLine{}, fmt.Errorf("%w: source ref is required", ErrReturnValidation)
	}
	if input.SourceKind == ReturnSourceCustomerShipment && strings.TrimSpace(input.OrderReceipt) == "" {
		return IssueLine{}, fmt.Errorf("%w: customer shipments require the original order receipt", ErrReturnValidation)
	}
	if input.SourceKind != ReturnSourceCustomerShipment && input.OrderReceipt != "" {
		return IssueLine{}, fmt.Errorf("%w: order receipts belong to customer shipments only", ErrReturnValidation)
	}
	serials, err := normalizeReturnSerials(input.Serials)
	if err != nil {
		return IssueLine{}, err
	}
	if len(serials) > 0 && input.Quantity.Scale() != 0 {
		return IssueLine{}, fmt.Errorf("%w: serialized lines count whole units", ErrReturnValidation)
	}
	line := IssueLine{
		LineID: input.LineID, TenantID: op.TenantID, CompanyID: op.CompanyID, SiteID: input.SiteID,
		ItemID: input.ItemID, Quantity: input.Quantity, LotID: input.LotID, Serials: serials,
		SourceKind: input.SourceKind, SourceRef: input.SourceRef, OrderReceipt: input.OrderReceipt,
	}
	fp := fingerprint("stock-return-issue", op, line.LineID, line.SiteID, line.ItemID,
		line.Quantity.String(), line.LotID, strings.Join(line.Serials, ","), string(line.SourceKind), line.SourceRef, line.OrderReceipt)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok := r.lines[returnLineKey(op, input.LineID)]; ok {
		priorFP := fingerprint("stock-return-issue", op, prior.LineID, prior.SiteID, prior.ItemID,
			prior.Quantity.String(), prior.LotID, strings.Join(prior.Serials, ","), string(prior.SourceKind), prior.SourceRef, prior.OrderReceipt)
		if priorFP != fp {
			return IssueLine{}, fmt.Errorf("%w: line %q already recorded with different content", ErrReturnConflict, input.LineID)
		}
		return *prior, nil
	}
	stored := line
	r.lines[returnLineKey(op, input.LineID)] = &stored
	return line, nil
}

// lineReceivedTotal sums physically received quantities for one line across
// every return. Callers hold r.mu.
func (r *ReturnRegistry) lineReceivedTotal(op OperationContext, lineID string) values.Decimal {
	var total values.Decimal
	initialized := false
	for key, ret := range r.returns {
		if !strings.HasPrefix(key, op.TenantID+"\x00"+op.CompanyID+"\x00") || ret.LineID != lineID {
			continue
		}
		if !initialized {
			total, initialized = ret.Received, true
			continue
		}
		sum, err := total.Add(ret.Received)
		if err == nil {
			total = sum
		}
	}
	return total
}

// lineHeldTotal sums open (non-closed) requested-but-unreceived quantities
// for one line. Callers hold r.mu.
func (r *ReturnRegistry) lineHeldTotal(op OperationContext, lineID string) values.Decimal {
	var total values.Decimal
	initialized := false
	for key, ret := range r.returns {
		if !strings.HasPrefix(key, op.TenantID+"\x00"+op.CompanyID+"\x00") || ret.LineID != lineID || ret.Status == ReturnStatusClosed {
			continue
		}
		open, err := ret.Requested.Sub(ret.Received)
		if err != nil {
			continue
		}
		if !initialized {
			total, initialized = open, true
			continue
		}
		if sum, err := total.Add(open); err == nil {
			total = sum
		}
	}
	return total
}

// BeginMaterialReturn opens a bounded linked authorization. Requested must
// fit the issued quantity less physically received returns less open
// held authorizations.
func (r *ReturnRegistry) BeginMaterialReturn(op OperationContext, key string, input BeginReturnInput) (MaterialReturn, error) {
	if err := op.validate(); err != nil {
		return MaterialReturn{}, err
	}
	if strings.TrimSpace(input.LineID) == "" || strings.TrimSpace(input.ReturningParty) == "" {
		return MaterialReturn{}, fmt.Errorf("%w: line and returning party are required", ErrReturnValidation)
	}
	if input.At.IsZero() {
		return MaterialReturn{}, fmt.Errorf("%w: authorization instant is required", ErrReturnValidation)
	}
	if err := input.Requested.Validate(); err != nil || input.Requested.Sign() <= 0 {
		return MaterialReturn{}, fmt.Errorf("%w: requested quantity must be positive", ErrReturnValidation)
	}
	fp := fingerprint("stock-return-begin", op, key, input.LineID, input.Requested.String(), input.ReturningParty, input.At.UTC().String())
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, done, err := r.checkReturnIdem("stock-return-begin", op, key, fp); done || err != nil {
		if err != nil {
			return MaterialReturn{}, err
		}
		if ret, ok := r.returns[returnKey(op, prior.ReturnID)]; ok {
			return *ret, nil
		}
		return MaterialReturn{}, fmt.Errorf("%w: authorized return is missing", ErrReturnValidation)
	}
	line, ok := r.lines[returnLineKey(op, input.LineID)]
	if !ok || !returnVisible(line.TenantID, line.CompanyID, line.SiteID, op) {
		return MaterialReturn{}, ErrStockScope
	}
	if input.Requested.Scale() != line.Quantity.Scale() {
		return MaterialReturn{}, fmt.Errorf("%w: requested scale must match the line scale", ErrReturnValidation)
	}
	if len(line.Serials) > 0 && input.Requested.Scale() != 0 {
		return MaterialReturn{}, fmt.Errorf("%w: serialized returns count whole units", ErrReturnValidation)
	}
	received := r.lineReceivedTotal(op, input.LineID)
	held := r.lineHeldTotal(op, input.LineID)
	available := line.Quantity
	if initialized := received.Validate() == nil; initialized {
		remaining, err := available.Sub(received)
		if err != nil {
			return MaterialReturn{}, fmt.Errorf("%w: derive allowance: %v", ErrReturnValidation, err)
		}
		available = remaining
	}
	if initialized := held.Validate() == nil; initialized {
		remaining, err := available.Sub(held)
		if err != nil {
			return MaterialReturn{}, fmt.Errorf("%w: derive held allowance: %v", ErrReturnValidation, err)
		}
		available = remaining
	}
	if input.Requested.Cmp(available) > 0 {
		return MaterialReturn{}, fmt.Errorf("%w: requested %s exceeds available %s", ErrReturnValidation, input.Requested, available)
	}
	zero, err := zeroReturnDecimal(line.Quantity.Scale())
	if err != nil {
		return MaterialReturn{}, fmt.Errorf("%w: zero received: %v", ErrReturnValidation, err)
	}
	r.seq++
	ret := MaterialReturn{
		ReturnID: fmt.Sprintf("ret-%06d", r.seq), TenantID: op.TenantID, CompanyID: op.CompanyID,
		SiteID: line.SiteID, ItemID: line.ItemID, LineID: line.LineID, Requested: input.Requested,
		Received: zero, ReturningParty: input.ReturningParty, Status: ReturnStatusAuthorized,
		Revision: 1, OpenedAt: input.At,
	}
	stored := ret
	r.returns[returnKey(op, ret.ReturnID)] = &stored
	r.storeReturnIdem("stock-return-begin", op, key, fp, ret.ReturnID, "")
	return ret, nil
}

// BeginUnlinkedReturn opens a lineless return under a separately
// authorized permission plus evidence. A nil authorizer denies everything.
func (r *ReturnRegistry) BeginUnlinkedReturn(op OperationContext, key string, input BeginUnlinkedInput) (MaterialReturn, error) {
	if err := op.validate(); err != nil {
		return MaterialReturn{}, err
	}
	if strings.TrimSpace(input.SiteID) == "" || strings.TrimSpace(input.ItemID) == "" {
		return MaterialReturn{}, fmt.Errorf("%w: site and item are required", ErrReturnValidation)
	}
	if err := input.Quantity.Validate(); err != nil || input.Quantity.Sign() <= 0 {
		return MaterialReturn{}, fmt.Errorf("%w: quantity must be positive", ErrReturnValidation)
	}
	if strings.TrimSpace(input.PermissionRef) == "" || strings.TrimSpace(input.Evidence) == "" {
		return MaterialReturn{}, fmt.Errorf("%w: unlinked returns require a permission ref and evidence", ErrReturnValidation)
	}
	if strings.TrimSpace(input.ReturningParty) == "" || input.At.IsZero() {
		return MaterialReturn{}, fmt.Errorf("%w: returning party and instant are required", ErrReturnValidation)
	}
	if r.auth == nil {
		return MaterialReturn{}, fmt.Errorf("%w: unlinked returns are not authorized", ErrReturnValidation)
	}
	if err := r.auth(op, input.PermissionRef); err != nil {
		return MaterialReturn{}, fmt.Errorf("%w: unlinked permission refused", ErrReturnValidation)
	}
	fp := fingerprint("stock-return-begin-unlinked", op, key, input.SiteID, input.ItemID,
		input.Quantity.String(), input.PermissionRef, input.Evidence, input.ReturningParty, input.At.UTC().String())
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, done, err := r.checkReturnIdem("stock-return-begin-unlinked", op, key, fp); done || err != nil {
		if err != nil {
			return MaterialReturn{}, err
		}
		if ret, ok := r.returns[returnKey(op, prior.ReturnID)]; ok {
			return *ret, nil
		}
		return MaterialReturn{}, fmt.Errorf("%w: authorized return is missing", ErrReturnValidation)
	}
	zero, err := zeroReturnDecimal(input.Quantity.Scale())
	if err != nil {
		return MaterialReturn{}, fmt.Errorf("%w: zero received: %v", ErrReturnValidation, err)
	}
	r.seq++
	ret := MaterialReturn{
		ReturnID: fmt.Sprintf("ret-%06d", r.seq), TenantID: op.TenantID, CompanyID: op.CompanyID,
		SiteID: input.SiteID, ItemID: input.ItemID, Requested: input.Quantity,
		Received: zero, ReturningParty: input.ReturningParty, Status: ReturnStatusAuthorized,
		Revision: 1, Unlinked: true, PermissionRef: input.PermissionRef, LinkEvidence: input.Evidence,
		OpenedAt: input.At,
	}
	stored := ret
	r.returns[returnKey(op, ret.ReturnID)] = &stored
	r.storeReturnIdem("stock-return-begin-unlinked", op, key, fp, ret.ReturnID, "")
	return ret, nil
}

// receivedSerials collects normalized presented serials across a return's
// receipts. Callers hold r.mu.
func receivedSerials(ret *MaterialReturn) map[string]bool {
	out := make(map[string]bool)
	for _, receipt := range ret.Receipts {
		for _, serial := range receipt.Serials {
			out[serial] = true
		}
	}
	return out
}

// lineReceivedSerials collects received serials for one line across every
// return in scope. Callers hold r.mu.
func (r *ReturnRegistry) lineReceivedSerials(op OperationContext, lineID string) map[string]bool {
	out := make(map[string]bool)
	for key, ret := range r.returns {
		if !strings.HasPrefix(key, op.TenantID+"\x00"+op.CompanyID+"\x00") || ret.LineID != lineID {
			continue
		}
		for serial := range receivedSerials(ret) {
			out[serial] = true
		}
	}
	return out
}

// ReceiveMaterialReturn records one physical receipt. The returnable
// allowance is consumed atomically here, before inspection: the receipt
// quantity must fit both this return's requested bound and the line fence
// (issued less physically received). Everything received lands
// quarantine-held. Identity, binding, condition and scale refusals change
// zero stock.
func (r *ReturnRegistry) ReceiveMaterialReturn(op OperationContext, input ReceiveReturnInput) (MaterialReceipt, error) {
	if err := op.validate(); err != nil {
		return MaterialReceipt{}, err
	}
	if strings.TrimSpace(input.ReturnID) == "" || strings.TrimSpace(input.Actor) == "" {
		return MaterialReceipt{}, fmt.Errorf("%w: return and actor are required", ErrReturnValidation)
	}
	if input.At.IsZero() {
		return MaterialReceipt{}, fmt.Errorf("%w: receipt instant is required", ErrReturnValidation)
	}
	if err := input.Quantity.Validate(); err != nil || input.Quantity.Sign() <= 0 {
		return MaterialReceipt{}, fmt.Errorf("%w: receipt quantity must be positive", ErrReturnValidation)
	}
	if !input.Condition.valid() {
		return MaterialReceipt{}, fmt.Errorf("%w: condition %q is unknown", ErrReturnValidation, input.Condition)
	}
	if input.Condition == ReturnConditionDamaged && strings.TrimSpace(input.Evidence) == "" {
		return MaterialReceipt{}, fmt.Errorf("%w: damaged receipts require evidence", ErrReturnValidation)
	}
	serials, err := normalizeReturnSerials(input.Serials)
	if err != nil {
		return MaterialReceipt{}, err
	}
	fp := fingerprint("stock-return-receive", op, input.Key, input.ReturnID, input.ExpectedRevision,
		input.Quantity.String(), input.LotID, strings.Join(serials, ","), string(input.Condition),
		input.Evidence, input.Actor, input.At.UTC().String())
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, done, err := r.checkReturnIdem("stock-return-receive", op, input.Key, fp); done || err != nil {
		if err != nil {
			return MaterialReceipt{}, err
		}
		if ret, ok := r.returns[returnKey(op, prior.ReturnID)]; ok {
			for _, receipt := range ret.Receipts {
				if receipt.ReceiptID == prior.ReceiptID {
					return receipt, nil
				}
			}
		}
		return MaterialReceipt{}, fmt.Errorf("%w: recorded receipt is missing", ErrReturnValidation)
	}
	ret, ok := r.returns[returnKey(op, input.ReturnID)]
	if !ok || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
		return MaterialReceipt{}, ErrStockScope
	}
	if ret.Status != ReturnStatusAuthorized {
		return MaterialReceipt{}, fmt.Errorf("%w: return is %s", ErrReturnValidation, ret.Status)
	}
	if input.ExpectedRevision != ret.Revision {
		return MaterialReceipt{}, fmt.Errorf("%w: expected revision %d, current is %d", ErrReturnConflict, input.ExpectedRevision, ret.Revision)
	}
	if input.Quantity.Scale() != ret.Requested.Scale() {
		return MaterialReceipt{}, fmt.Errorf("%w: receipt scale must match the authorized scale", ErrReturnValidation)
	}
	open, err := ret.Requested.Sub(ret.Received)
	if err != nil {
		return MaterialReceipt{}, fmt.Errorf("%w: derive open authorization: %v", ErrReturnValidation, err)
	}
	if input.Quantity.Cmp(open) > 0 {
		return MaterialReceipt{}, fmt.Errorf("%w: receipt %s exceeds open authorization %s", ErrReturnValidation, input.Quantity, open)
	}
	if ret.Unlinked {
		if len(serials) > 0 && !validReturnBinding(input.Binding) {
			return MaterialReceipt{}, ErrAssetBindingRequired
		}
	} else {
		line, ok := r.lines[returnLineKey(op, ret.LineID)]
		if !ok {
			return MaterialReceipt{}, fmt.Errorf("%w: original line is missing", ErrReturnValidation)
		}
		lineTotal := r.lineReceivedTotal(op, ret.LineID)
		if lineTotal.Validate() == nil {
			after, err := lineTotal.Add(input.Quantity)
			if err != nil {
				return MaterialReceipt{}, fmt.Errorf("%w: fence arithmetic: %v", ErrReturnValidation, err)
			}
			if after.Cmp(line.Quantity) > 0 {
				remaining, _ := line.Quantity.Sub(lineTotal)
				return MaterialReceipt{}, fmt.Errorf("%w: receipt exceeds the %s returnable allowance", ErrReturnValidation, remaining)
			}
		} else if input.Quantity.Cmp(line.Quantity) > 0 {
			return MaterialReceipt{}, fmt.Errorf("%w: receipt exceeds the issued quantity", ErrReturnValidation)
		}
		effectiveLot := line.LotID
		effectiveSerials := make(map[string]bool, len(line.Serials))
		for _, serial := range line.Serials {
			effectiveSerials[serial] = true
		}
		if ret.SubstituteLot != "" || len(ret.SubstituteSer) > 0 {
			effectiveLot = ret.SubstituteLot
			effectiveSerials = make(map[string]bool, len(ret.SubstituteSer))
			for _, serial := range ret.SubstituteSer {
				effectiveSerials[serial] = true
			}
		}
		if input.LotID != effectiveLot {
			return MaterialReceipt{}, fmt.Errorf("%w: presented lot does not match the recorded issue", ErrReturnValidation)
		}
		if len(effectiveSerials) > 0 {
			if input.Quantity.Scale() != 0 {
				return MaterialReceipt{}, fmt.Errorf("%w: serialized returns count whole units", ErrReturnValidation)
			}
			count, err := strconv.Atoi(input.Quantity.String())
			if err != nil || count != len(serials) {
				return MaterialReceipt{}, fmt.Errorf("%w: serialized receipt must present one identity per unit", ErrReturnValidation)
			}
			if !validReturnBinding(input.Binding) {
				return MaterialReceipt{}, ErrAssetBindingRequired
			}
			already := r.lineReceivedSerials(op, ret.LineID)
			for _, serial := range serials {
				if !effectiveSerials[serial] {
					return MaterialReceipt{}, fmt.Errorf("%w: serial %q was not issued on this line", ErrReturnValidation, serial)
				}
				if already[serial] {
					return MaterialReceipt{}, fmt.Errorf("%w: serial %q is already returned", ErrReturnValidation, serial)
				}
			}
		} else if len(serials) > 0 {
			return MaterialReceipt{}, fmt.Errorf("%w: untracked line cannot receive serialized units", ErrReturnValidation)
		}
	}
	zero, err := zeroReturnDecimal(ret.Requested.Scale())
	if err != nil {
		return MaterialReceipt{}, fmt.Errorf("%w: zero accepted: %v", ErrReturnValidation, err)
	}
	received, err := ret.Received.Add(input.Quantity)
	if err != nil {
		return MaterialReceipt{}, fmt.Errorf("%w: advance received: %v", ErrReturnValidation, err)
	}
	r.seq++
	receipt := MaterialReceipt{
		ReceiptID: fmt.Sprintf("rcp-%06d", r.seq), ReturnID: ret.ReturnID, Quantity: input.Quantity,
		LotID: input.LotID, Serials: serials, Condition: input.Condition, Evidence: input.Evidence,
		Actor: input.Actor, At: input.At, Binding: input.Binding,
		QuarantineHeld: input.Quantity, Accepted: zero,
	}
	ret.Receipts = append(ret.Receipts, receipt)
	ret.Received = received
	ret.Revision++
	r.storeReturnIdem("stock-return-receive", op, input.Key, fp, ret.ReturnID, receipt.ReceiptID)
	return receipt, nil
}

// InspectMaterialReturn splits one receipt across accepted, quarantined,
// disposed and rejected. Unset quantities read as zero; the four must sum
// exactly to the receipt. The inspector must differ from the receiver.
func (r *ReturnRegistry) InspectMaterialReturn(op OperationContext, input InspectReturnInput) (ReturnInspection, error) {
	if err := op.validate(); err != nil {
		return ReturnInspection{}, err
	}
	if strings.TrimSpace(input.ReturnID) == "" || strings.TrimSpace(input.ReceiptID) == "" || strings.TrimSpace(input.Inspector) == "" {
		return ReturnInspection{}, fmt.Errorf("%w: return, receipt and inspector are required", ErrReturnValidation)
	}
	if input.At.IsZero() {
		return ReturnInspection{}, fmt.Errorf("%w: inspection instant is required", ErrReturnValidation)
	}
	serials, err := normalizeReturnSerials(input.AcceptedSerials)
	if err != nil {
		return ReturnInspection{}, err
	}
	fp := fingerprint("stock-return-inspect", op, input.Key, input.ReturnID, input.ReceiptID,
		input.Accepted.String(), input.Quarantined.String(), input.Disposed.String(), input.Rejected.String(),
		strings.Join(serials, ","), input.DisposeReason, input.ReturnShipmentRef, input.Inspector, input.At.UTC().String())
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, done, err := r.checkReturnIdem("stock-return-inspect", op, input.Key, fp); done || err != nil {
		if err != nil {
			return ReturnInspection{}, err
		}
		if ret, ok := r.returns[returnKey(op, prior.ReturnID)]; ok {
			for _, receipt := range ret.Receipts {
				if receipt.ReceiptID == prior.ReceiptID && receipt.Inspection != nil {
					return *receipt.Inspection, nil
				}
			}
		}
		return ReturnInspection{}, fmt.Errorf("%w: recorded inspection is missing", ErrReturnValidation)
	}
	ret, ok := r.returns[returnKey(op, input.ReturnID)]
	if !ok || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
		return ReturnInspection{}, ErrStockScope
	}
	if ret.Status != ReturnStatusAuthorized {
		return ReturnInspection{}, fmt.Errorf("%w: return is %s", ErrReturnValidation, ret.Status)
	}
	slot := -1
	for i := range ret.Receipts {
		if ret.Receipts[i].ReceiptID == input.ReceiptID {
			slot = i
			break
		}
	}
	if slot < 0 {
		return ReturnInspection{}, fmt.Errorf("%w: receipt is unknown", ErrReturnValidation)
	}
	receipt := &ret.Receipts[slot]
	if receipt.Inspected {
		return ReturnInspection{}, fmt.Errorf("%w: receipt is already inspected", ErrReturnValidation)
	}
	if input.Inspector == receipt.Actor {
		return ReturnInspection{}, fmt.Errorf("%w: inspector must differ from the receiver", ErrReturnValidation)
	}
	scale := receipt.Quantity.Scale()
	accepted, err := returnDecOrZero(input.Accepted, scale)
	if err != nil {
		return ReturnInspection{}, err
	}
	quarantined, err := returnDecOrZero(input.Quarantined, scale)
	if err != nil {
		return ReturnInspection{}, err
	}
	disposed, err := returnDecOrZero(input.Disposed, scale)
	if err != nil {
		return ReturnInspection{}, err
	}
	rejected, err := returnDecOrZero(input.Rejected, scale)
	if err != nil {
		return ReturnInspection{}, err
	}
	split, err := accepted.Add(quarantined)
	if err != nil {
		return ReturnInspection{}, fmt.Errorf("%w: split arithmetic: %v", ErrReturnValidation, err)
	}
	if split, err = split.Add(disposed); err != nil {
		return ReturnInspection{}, fmt.Errorf("%w: split arithmetic: %v", ErrReturnValidation, err)
	}
	if split, err = split.Add(rejected); err != nil {
		return ReturnInspection{}, fmt.Errorf("%w: split arithmetic: %v", ErrReturnValidation, err)
	}
	if !split.Equal(receipt.Quantity) {
		return ReturnInspection{}, fmt.Errorf("%w: dispositions sum %s, receipt is %s", ErrReturnValidation, split, receipt.Quantity)
	}
	if len(receipt.Serials) > 0 {
		count, err := strconv.Atoi(accepted.String())
		if err != nil || count != len(serials) {
			return ReturnInspection{}, fmt.Errorf("%w: accepted serialized units need one identity each", ErrReturnValidation)
		}
		presented := make(map[string]bool, len(receipt.Serials))
		for _, serial := range receipt.Serials {
			presented[serial] = true
		}
		for _, serial := range serials {
			if !presented[serial] {
				return ReturnInspection{}, fmt.Errorf("%w: serial %q was not presented on this receipt", ErrReturnValidation, serial)
			}
		}
	} else if len(serials) > 0 {
		return ReturnInspection{}, fmt.Errorf("%w: untracked receipt cannot accept serials", ErrReturnValidation)
	}
	if disposed.Sign() > 0 && strings.TrimSpace(input.DisposeReason) == "" {
		return ReturnInspection{}, fmt.Errorf("%w: disposal requires a reason", ErrReturnValidation)
	}
	ret.Revision++
	inspection := ReturnInspection{
		ReceiptID: receipt.ReceiptID, Inspector: input.Inspector, Accepted: accepted,
		Quarantined: quarantined, Disposed: disposed, Rejected: rejected, AcceptedSerials: serials,
		DisposeReason: input.DisposeReason, ReturnShipmentRef: input.ReturnShipmentRef,
		At: input.At, ReturnRevision: ret.Revision,
	}
	receipt.Inspected = true
	receipt.Inspection = &inspection
	stored := inspection
	r.storeReturnIdem("stock-return-inspect", op, input.Key, fp, ret.ReturnID, receipt.ReceiptID)
	return stored, nil
}

// ResolveReturn closes a fully inspected return, emitting restock intents
// for accepted goods, quarantine holds, disposal and rejection custody
// records, and at most one credit request. Replacement plus full credit
// together require an explicit split exception; a credit request requires
// the original order receipt.
func (r *ReturnRegistry) ResolveReturn(op OperationContext, input ResolveReturnInput) (ReturnResolution, error) {
	if err := op.validate(); err != nil {
		return ReturnResolution{}, err
	}
	if strings.TrimSpace(input.ReturnID) == "" || strings.TrimSpace(input.Actor) == "" {
		return ReturnResolution{}, fmt.Errorf("%w: return and actor are required", ErrReturnValidation)
	}
	if input.At.IsZero() {
		return ReturnResolution{}, fmt.Errorf("%w: resolution instant is required", ErrReturnValidation)
	}
	if !input.Remedy.valid() {
		return ReturnResolution{}, fmt.Errorf("%w: remedy %q is unknown", ErrReturnValidation, input.Remedy)
	}
	if input.AdditionalRemedy != "" && !input.AdditionalRemedy.valid() {
		return ReturnResolution{}, fmt.Errorf("%w: remedy %q is unknown", ErrReturnValidation, input.AdditionalRemedy)
	}
	if input.AdditionalRemedy != "" && input.AdditionalRemedy == input.Remedy {
		return ReturnResolution{}, fmt.Errorf("%w: duplicate remedy", ErrReturnValidation)
	}
	remedies := map[ReturnRemedy]bool{input.Remedy: true}
	if input.AdditionalRemedy != "" {
		remedies[input.AdditionalRemedy] = true
	}
	if remedies[ReturnRemedyReplacement] && remedies[ReturnRemedyCreditRequest] && strings.TrimSpace(input.SplitExceptionRef) == "" {
		return ReturnResolution{}, fmt.Errorf("%w: full replacement plus full credit requires an explicit split exception", ErrReturnValidation)
	}
	fp := fingerprint("stock-return-resolve", op, input.Key, input.ReturnID, input.ExpectedRevision,
		string(input.Remedy), string(input.AdditionalRemedy), input.SplitExceptionRef, input.Actor, input.At.UTC().String())
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, done, err := r.checkReturnIdem("stock-return-resolve", op, input.Key, fp); done || err != nil {
		if err != nil {
			return ReturnResolution{}, err
		}
		if ret, ok := r.returns[returnKey(op, prior.ReturnID)]; ok && ret.Resolution != nil {
			return *ret.Resolution, nil
		}
		return ReturnResolution{}, fmt.Errorf("%w: recorded resolution is missing", ErrReturnValidation)
	}
	ret, ok := r.returns[returnKey(op, input.ReturnID)]
	if !ok || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
		return ReturnResolution{}, ErrStockScope
	}
	if ret.Status != ReturnStatusAuthorized {
		return ReturnResolution{}, fmt.Errorf("%w: return is %s", ErrReturnValidation, ret.Status)
	}
	if input.ExpectedRevision != ret.Revision {
		return ReturnResolution{}, fmt.Errorf("%w: expected revision %d, current is %d", ErrReturnConflict, input.ExpectedRevision, ret.Revision)
	}
	if len(ret.Receipts) == 0 {
		return ReturnResolution{}, fmt.Errorf("%w: nothing received; cancel instead", ErrReturnValidation)
	}
	for _, receipt := range ret.Receipts {
		if !receipt.Inspected || receipt.Inspection == nil {
			return ReturnResolution{}, fmt.Errorf("%w: receipt %q is not inspected", ErrReturnValidation, receipt.ReceiptID)
		}
	}
	scale := ret.Requested.Scale()
	zero, err := zeroReturnDecimal(scale)
	if err != nil {
		return ReturnResolution{}, fmt.Errorf("%w: zero buckets: %v", ErrReturnValidation, err)
	}
	buckets := ReturnBuckets{Requested: ret.Requested, Received: ret.Received, Accepted: zero, Quarantined: zero, Disposed: zero, Rejected: zero}
	resolution := ReturnResolution{
		ReturnID: ret.ReturnID, Remedy: input.Remedy, AdditionalRemedy: input.AdditionalRemedy,
		SplitExceptionRef: input.SplitExceptionRef, Actor: input.Actor, At: input.At,
	}
	add := func(into values.Decimal, more values.Decimal) values.Decimal {
		sum, err := into.Add(more)
		if err != nil {
			return into
		}
		return sum
	}
	for _, receipt := range ret.Receipts {
		inspection := receipt.Inspection
		buckets.Accepted = add(buckets.Accepted, inspection.Accepted)
		buckets.Quarantined = add(buckets.Quarantined, inspection.Quarantined)
		buckets.Disposed = add(buckets.Disposed, inspection.Disposed)
		buckets.Rejected = add(buckets.Rejected, inspection.Rejected)
		if inspection.Accepted.Sign() > 0 {
			resolution.Restock = append(resolution.Restock, RestockIntent{
				ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, ItemID: ret.ItemID,
				LotID: receipt.LotID, Serials: append([]string(nil), inspection.AcceptedSerials...),
				Quantity: inspection.Accepted, Evidence: receipt.Evidence,
			})
		}
		if inspection.Quarantined.Sign() > 0 {
			resolution.Holds = append(resolution.Holds, QuarantineHold{
				ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, ItemID: ret.ItemID,
				Quantity: inspection.Quarantined, Reason: "inspection hold",
			})
		}
		if inspection.Disposed.Sign() > 0 {
			resolution.Disposals = append(resolution.Disposals, DisposalRecord{
				ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, ItemID: ret.ItemID,
				Quantity: inspection.Disposed, Reason: inspection.DisposeReason,
			})
		}
		if inspection.Rejected.Sign() > 0 {
			resolution.Rejections = append(resolution.Rejections, RejectionRecord{
				ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, ItemID: ret.ItemID,
				Quantity: inspection.Rejected, ShipmentRef: inspection.ReturnShipmentRef,
				HoldQuarantine: inspection.ReturnShipmentRef == "",
			})
		}
	}
	if remedies[ReturnRemedyCreditRequest] {
		if ret.Unlinked {
			return ReturnResolution{}, fmt.Errorf("%w: unlinked returns have no order receipt to link", ErrReturnValidation)
		}
		line, ok := r.lines[returnLineKey(op, ret.LineID)]
		if !ok || line.OrderReceipt == "" {
			return ReturnResolution{}, fmt.Errorf("%w: credit requests require the original order receipt", ErrReturnValidation)
		}
		resolution.Credit = &CreditRequest{
			ReturnID: ret.ReturnID, OrderReceipt: line.OrderReceipt, ItemID: ret.ItemID, Quantity: ret.Received,
		}
	}
	resolution.Buckets = buckets
	ret.Status = ReturnStatusClosed
	ret.Revision++
	ret.Resolution = &resolution
	stored := resolution
	r.storeReturnIdem("stock-return-resolve", op, input.Key, fp, ret.ReturnID, "")
	return stored, nil
}

// CancelReturn closes an unreceived return, releasing its held
// authorization. Anything physically received must resolve through
// disposition; stock is never erased.
func (r *ReturnRegistry) CancelReturn(op OperationContext, input CancelReturnInput) (MaterialReturn, error) {
	if err := op.validate(); err != nil {
		return MaterialReturn{}, err
	}
	if strings.TrimSpace(input.ReturnID) == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.Actor) == "" {
		return MaterialReturn{}, fmt.Errorf("%w: return, reason and actor are required", ErrReturnValidation)
	}
	if input.At.IsZero() {
		return MaterialReturn{}, fmt.Errorf("%w: cancellation instant is required", ErrReturnValidation)
	}
	fp := fingerprint("stock-return-cancel", op, input.Key, input.ReturnID, input.ExpectedRevision, input.Reason, input.Actor, input.At.UTC().String())
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, done, err := r.checkReturnIdem("stock-return-cancel", op, input.Key, fp); done || err != nil {
		if err != nil {
			return MaterialReturn{}, err
		}
		if ret, ok := r.returns[returnKey(op, prior.ReturnID)]; ok {
			return *ret, nil
		}
		return MaterialReturn{}, fmt.Errorf("%w: cancelled return is missing", ErrReturnValidation)
	}
	ret, ok := r.returns[returnKey(op, input.ReturnID)]
	if !ok || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
		return MaterialReturn{}, ErrStockScope
	}
	if ret.Status != ReturnStatusAuthorized {
		return MaterialReturn{}, fmt.Errorf("%w: return is %s", ErrReturnValidation, ret.Status)
	}
	if input.ExpectedRevision != ret.Revision {
		return MaterialReturn{}, fmt.Errorf("%w: expected revision %d, current is %d", ErrReturnConflict, input.ExpectedRevision, ret.Revision)
	}
	if ret.Received.Sign() > 0 {
		return MaterialReturn{}, fmt.Errorf("%w: received returns resolve through disposition, never cancel", ErrReturnValidation)
	}
	ret.Status = ReturnStatusClosed
	ret.Revision++
	ret.Cancelled = true
	ret.CancelReason = input.Reason
	r.storeReturnIdem("stock-return-cancel", op, input.Key, fp, ret.ReturnID, "")
	return *ret, nil
}

// EscalateDiscrepancy parks an authorized return after presented identity
// failed to match. Receiving stays blocked until review decides.
func (r *ReturnRegistry) EscalateDiscrepancy(op OperationContext, input EscalateDiscrepancyInput) (MaterialReturn, error) {
	if err := op.validate(); err != nil {
		return MaterialReturn{}, err
	}
	if strings.TrimSpace(input.ReturnID) == "" || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.Evidence) == "" || strings.TrimSpace(input.Actor) == "" {
		return MaterialReturn{}, fmt.Errorf("%w: return, reason, evidence and actor are required", ErrReturnValidation)
	}
	if input.At.IsZero() {
		return MaterialReturn{}, fmt.Errorf("%w: escalation instant is required", ErrReturnValidation)
	}
	if input.PresentedLot == "" && len(input.PresentedSerials) == 0 {
		return MaterialReturn{}, fmt.Errorf("%w: presented identity is required", ErrReturnValidation)
	}
	serials, err := normalizeReturnSerials(input.PresentedSerials)
	if err != nil {
		return MaterialReturn{}, err
	}
	fp := fingerprint("stock-return-escalate", op, input.Key, input.ReturnID, input.ExpectedRevision,
		input.PresentedLot, strings.Join(serials, ","), input.Reason, input.Evidence, input.Actor, input.At.UTC().String())
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, done, err := r.checkReturnIdem("stock-return-escalate", op, input.Key, fp); done || err != nil {
		if err != nil {
			return MaterialReturn{}, err
		}
		if ret, ok := r.returns[returnKey(op, prior.ReturnID)]; ok {
			return *ret, nil
		}
		return MaterialReturn{}, fmt.Errorf("%w: escalated return is missing", ErrReturnValidation)
	}
	ret, ok := r.returns[returnKey(op, input.ReturnID)]
	if !ok || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
		return MaterialReturn{}, ErrStockScope
	}
	if ret.Status != ReturnStatusAuthorized {
		return MaterialReturn{}, fmt.Errorf("%w: return is %s", ErrReturnValidation, ret.Status)
	}
	if input.ExpectedRevision != ret.Revision {
		return MaterialReturn{}, fmt.Errorf("%w: expected revision %d, current is %d", ErrReturnConflict, input.ExpectedRevision, ret.Revision)
	}
	ret.Discrepancies = append(ret.Discrepancies, ReturnDiscrepancy{
		Key: input.Key, PresentedLot: input.PresentedLot, PresentedSerials: serials,
		Reason: input.Reason, Evidence: input.Evidence, Actor: input.Actor, At: input.At,
	})
	ret.Status = ReturnStatusDiscrepancy
	ret.Revision++
	r.storeReturnIdem("stock-return-escalate", op, input.Key, fp, ret.ReturnID, "")
	return *ret, nil
}

// ResolveDiscrepancy decides one open discrepancy. Accepting adopts the
// presented identity as an authorized substitute for later receives;
// rejecting clears the park so the return can retry with correct identity.
func (r *ReturnRegistry) ResolveDiscrepancy(op OperationContext, input ResolveDiscrepancyInput) (MaterialReturn, error) {
	if err := op.validate(); err != nil {
		return MaterialReturn{}, err
	}
	if strings.TrimSpace(input.ReturnID) == "" || strings.TrimSpace(input.DiscrepancyKey) == "" || strings.TrimSpace(input.Reviewer) == "" || strings.TrimSpace(input.Evidence) == "" {
		return MaterialReturn{}, fmt.Errorf("%w: return, discrepancy, reviewer and evidence are required", ErrReturnValidation)
	}
	if input.Decision != DiscrepancyAcceptSubstitute && input.Decision != DiscrepancyReject {
		return MaterialReturn{}, fmt.Errorf("%w: decision %q is unknown", ErrReturnValidation, input.Decision)
	}
	if input.At.IsZero() {
		return MaterialReturn{}, fmt.Errorf("%w: decision instant is required", ErrReturnValidation)
	}
	fp := fingerprint("stock-return-decide", op, input.Key, input.ReturnID, input.ExpectedRevision,
		input.DiscrepancyKey, input.Decision, input.Reviewer, input.Evidence, input.At.UTC().String())
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, done, err := r.checkReturnIdem("stock-return-decide", op, input.Key, fp); done || err != nil {
		if err != nil {
			return MaterialReturn{}, err
		}
		if ret, ok := r.returns[returnKey(op, prior.ReturnID)]; ok {
			return *ret, nil
		}
		return MaterialReturn{}, fmt.Errorf("%w: decided return is missing", ErrReturnValidation)
	}
	ret, ok := r.returns[returnKey(op, input.ReturnID)]
	if !ok || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
		return MaterialReturn{}, ErrStockScope
	}
	if ret.Status != ReturnStatusDiscrepancy {
		return MaterialReturn{}, fmt.Errorf("%w: return is %s", ErrReturnValidation, ret.Status)
	}
	if input.ExpectedRevision != ret.Revision {
		return MaterialReturn{}, fmt.Errorf("%w: expected revision %d, current is %d", ErrReturnConflict, input.ExpectedRevision, ret.Revision)
	}
	slot := -1
	for i := range ret.Discrepancies {
		if ret.Discrepancies[i].Key == input.DiscrepancyKey && !ret.Discrepancies[i].Resolved {
			slot = i
			break
		}
	}
	if slot < 0 {
		return MaterialReturn{}, fmt.Errorf("%w: open discrepancy is unknown", ErrReturnValidation)
	}
	open := false
	for i := range ret.Discrepancies {
		if i != slot && !ret.Discrepancies[i].Resolved {
			open = true
			break
		}
	}
	entry := &ret.Discrepancies[slot]
	entry.Resolved = true
	entry.Decision = input.Decision
	entry.Reviewer = input.Reviewer
	entry.ResolveEvidence = input.Evidence
	if input.Decision == DiscrepancyAcceptSubstitute {
		ret.SubstituteLot = entry.PresentedLot
		ret.SubstituteSer = append([]string(nil), entry.PresentedSerials...)
	}
	if !open {
		ret.Status = ReturnStatusAuthorized
	}
	ret.Revision++
	r.storeReturnIdem("stock-return-decide", op, input.Key, fp, ret.ReturnID, "")
	return *ret, nil
}

// GetReturn returns one return or the neutral scope refusal.
func (r *ReturnRegistry) GetReturn(op OperationContext, returnID string) (MaterialReturn, error) {
	if err := op.validate(); err != nil {
		return MaterialReturn{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ret, ok := r.returns[returnKey(op, returnID)]
	if !ok || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
		return MaterialReturn{}, ErrStockScope
	}
	return *ret, nil
}

// ListReturns lists visible returns, optionally for one line, in ID order.
func (r *ReturnRegistry) ListReturns(op OperationContext, lineID string) []MaterialReturn {
	if err := op.validate(); err != nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []MaterialReturn
	for key, ret := range r.returns {
		if !strings.HasPrefix(key, op.TenantID+"\x00"+op.CompanyID+"\x00") || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
			continue
		}
		if lineID != "" && ret.LineID != lineID {
			continue
		}
		out = append(out, *ret)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReturnID < out[j].ReturnID })
	return out
}

// ReturnableAllowance reports issued less physically received for one
// line. Inspection never changes it: allowance is consumed at receipt.
func (r *ReturnRegistry) ReturnableAllowance(op OperationContext, lineID string) (values.Decimal, error) {
	if err := op.validate(); err != nil {
		return values.Decimal{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	line, ok := r.lines[returnLineKey(op, lineID)]
	if !ok || !returnVisible(line.TenantID, line.CompanyID, line.SiteID, op) {
		return values.Decimal{}, ErrStockScope
	}
	received := r.lineReceivedTotal(op, lineID)
	if received.Validate() != nil {
		return line.Quantity, nil
	}
	allowance, err := line.Quantity.Sub(received)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("%w: derive allowance: %v", ErrReturnValidation, err)
	}
	return allowance, nil
}

// LineStatement shows issued, returned and net quantities separately for
// one line.
func (r *ReturnRegistry) LineStatement(op OperationContext, lineID string) (LineStatement, error) {
	if err := op.validate(); err != nil {
		return LineStatement{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	line, ok := r.lines[returnLineKey(op, lineID)]
	if !ok || !returnVisible(line.TenantID, line.CompanyID, line.SiteID, op) {
		return LineStatement{}, ErrStockScope
	}
	received := r.lineReceivedTotal(op, lineID)
	if received.Validate() != nil {
		zero, err := zeroReturnDecimal(line.Quantity.Scale())
		if err != nil {
			return LineStatement{}, fmt.Errorf("%w: zero returned: %v", ErrReturnValidation, err)
		}
		received = zero
	}
	net, err := line.Quantity.Sub(received)
	if err != nil {
		return LineStatement{}, fmt.Errorf("%w: derive net: %v", ErrReturnValidation, err)
	}
	return LineStatement{LineID: line.LineID, ItemID: line.ItemID, Issued: line.Quantity, Returned: received, Net: net}, nil
}

// ReturnQueueRow is one return-queue row: identity, derived status and the
// remaining returnable quantity against the original line.
type ReturnQueueRow struct {
	ReturnID            string `json:"return_id"`
	LineID              string `json:"line_id"`
	ItemID              string `json:"item_id"`
	Status              string `json:"status"`
	Requested           string `json:"requested"`
	Received            string `json:"received"`
	RemainingReturnable string `json:"remaining_returnable"`
}

// ProjectReturnQueue builds the work-queue rows visible to the caller.
// Denied scopes yield no rows.
func (r *ReturnRegistry) ProjectReturnQueue(op OperationContext, lineID string) []ReturnQueueRow {
	if err := op.validate(); err != nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var rows []ReturnQueueRow
	for key, ret := range r.returns {
		if !strings.HasPrefix(key, op.TenantID+"\x00"+op.CompanyID+"\x00") || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
			continue
		}
		if lineID != "" && ret.LineID != lineID {
			continue
		}
		status := "CLOSED"
		switch ret.Status {
		case ReturnStatusDiscrepancy:
			status = "DISCREPANCY"
		case ReturnStatusAuthorized:
			status = "AWAITING_RECEIPT"
			for _, receipt := range ret.Receipts {
				if !receipt.Inspected {
					status = "AWAITING_INSPECTION"
					break
				}
			}
			if len(ret.Receipts) > 0 && status == "AWAITING_RECEIPT" {
				status = "READY_TO_RESOLVE"
			}
		}
		remaining := ""
		if !ret.Unlinked {
			if line, ok := r.lines[returnLineKey(op, ret.LineID)]; ok {
				received := r.lineReceivedTotal(op, ret.LineID)
				if received.Validate() != nil {
					remaining = line.Quantity.String()
				} else if allowance, err := line.Quantity.Sub(received); err == nil {
					remaining = allowance.String()
				}
			}
		} else if open, err := ret.Requested.Sub(ret.Received); err == nil {
			remaining = open.String()
		}
		rows = append(rows, ReturnQueueRow{
			ReturnID: ret.ReturnID, LineID: ret.LineID, ItemID: ret.ItemID, Status: status,
			Requested: ret.Requested.String(), Received: ret.Received.String(), RemainingReturnable: remaining,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ReturnID < rows[j].ReturnID })
	return rows
}

// InspectChoice previews one inspection split. Unset quantities read as
// zero, exactly like the command.
type InspectChoice struct {
	Accepted    values.Decimal
	Quarantined values.Decimal
	Disposed    values.Decimal
	Rejected    values.Decimal
}

// InspectionPreview shows the resulting buckets for one choice, or an
// explicit error when the split cannot apply.
type InspectionPreview struct {
	ResultingUsable     string `json:"resulting_usable"`
	ResultingQuarantine string `json:"resulting_quarantine"`
	ResultingDisposed   string `json:"resulting_disposed"`
	ResultingRejected   string `json:"resulting_rejected"`
	Error               string `json:"error,omitempty"`
}

// ProjectInspectionPreview validates one split against the uninspected
// receipt without recording anything.
func (r *ReturnRegistry) ProjectInspectionPreview(op OperationContext, returnID, receiptID string, choice InspectChoice) InspectionPreview {
	if err := op.validate(); err != nil {
		return InspectionPreview{Error: "not available"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ret, ok := r.returns[returnKey(op, returnID)]
	if !ok || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
		return InspectionPreview{Error: "not found or denied"}
	}
	var receipt *MaterialReceipt
	for i := range ret.Receipts {
		if ret.Receipts[i].ReceiptID == receiptID {
			receipt = &ret.Receipts[i]
			break
		}
	}
	if receipt == nil {
		return InspectionPreview{Error: "receipt is unknown"}
	}
	if receipt.Inspected {
		return InspectionPreview{Error: "receipt is already inspected"}
	}
	scale := receipt.Quantity.Scale()
	accepted, err := returnDecOrZero(choice.Accepted, scale)
	if err != nil {
		return InspectionPreview{Error: err.Error()}
	}
	quarantined, err := returnDecOrZero(choice.Quarantined, scale)
	if err != nil {
		return InspectionPreview{Error: err.Error()}
	}
	disposed, err := returnDecOrZero(choice.Disposed, scale)
	if err != nil {
		return InspectionPreview{Error: err.Error()}
	}
	rejected, err := returnDecOrZero(choice.Rejected, scale)
	if err != nil {
		return InspectionPreview{Error: err.Error()}
	}
	split, err := accepted.Add(quarantined)
	if err != nil {
		return InspectionPreview{Error: err.Error()}
	}
	if split, err = split.Add(disposed); err != nil {
		return InspectionPreview{Error: err.Error()}
	}
	if split, err = split.Add(rejected); err != nil {
		return InspectionPreview{Error: err.Error()}
	}
	if !split.Equal(receipt.Quantity) {
		return InspectionPreview{Error: "dispositions sum " + split.String() + ", receipt is " + receipt.Quantity.String()}
	}
	return InspectionPreview{
		ResultingUsable: accepted.String(), ResultingQuarantine: quarantined.String(),
		ResultingDisposed: disposed.String(), ResultingRejected: rejected.String(),
	}
}

// RemedyProjection reports the requested remedy state. Completed is never
// true from stock: finance decides outside inventory, and an unavailable
// finance capability reads as pending outside inventory.
type RemedyProjection struct {
	ReturnID  string `json:"return_id"`
	Requested string `json:"requested"`
	State     string `json:"state"`
	Completed bool   `json:"completed"`
}

// ProjectRemedyStatus builds the remedy projection for one return.
func (r *ReturnRegistry) ProjectRemedyStatus(op OperationContext, returnID, remedy string, billCapable bool) RemedyProjection {
	if err := op.validate(); err != nil {
		return RemedyProjection{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ret, ok := r.returns[returnKey(op, returnID)]
	if !ok || !returnVisible(ret.TenantID, ret.CompanyID, ret.SiteID, op) {
		return RemedyProjection{}
	}
	view := RemedyProjection{ReturnID: ret.ReturnID, Requested: remedy}
	switch {
	case strings.TrimSpace(remedy) == "":
		view.State = "no-remedy-requested"
	case !billCapable:
		view.State = "remedy-pending-outside-inventory"
	default:
		view.State = "recorded-in-inventory"
	}
	return view
}

type returnSnapshot struct {
	Seq     uint64                `json:"seq"`
	Lines   []IssueLine           `json:"lines"`
	Returns []MaterialReturn      `json:"returns"`
	Idem    map[string]returnIdem `json:"idem"`
}

// Export captures the registry for restart recovery with deterministic
// order.
func (r *ReturnRegistry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := returnSnapshot{
		Seq:   r.seq,
		Lines: make([]IssueLine, 0, len(r.lines)),
		Idem:  make(map[string]returnIdem, len(r.idem)),
	}
	for _, line := range r.lines {
		stored := *line
		stored.Serials = append([]string(nil), line.Serials...)
		snapshot.Lines = append(snapshot.Lines, stored)
	}
	sort.Slice(snapshot.Lines, func(i, j int) bool { return snapshot.Lines[i].LineID < snapshot.Lines[j].LineID })
	for _, ret := range r.returns {
		stored := *ret
		stored.Receipts = append([]MaterialReceipt(nil), ret.Receipts...)
		stored.Discrepancies = append([]ReturnDiscrepancy(nil), ret.Discrepancies...)
		stored.SubstituteSer = append([]string(nil), ret.SubstituteSer...)
		if ret.Resolution != nil {
			resolved := *ret.Resolution
			resolved.Restock = append([]RestockIntent(nil), ret.Resolution.Restock...)
			resolved.Holds = append([]QuarantineHold(nil), ret.Resolution.Holds...)
			resolved.Disposals = append([]DisposalRecord(nil), ret.Resolution.Disposals...)
			resolved.Rejections = append([]RejectionRecord(nil), ret.Resolution.Rejections...)
			if ret.Resolution.Credit != nil {
				credit := *ret.Resolution.Credit
				resolved.Credit = &credit
			}
			stored.Resolution = &resolved
		}
		snapshot.Returns = append(snapshot.Returns, stored)
	}
	sort.Slice(snapshot.Returns, func(i, j int) bool { return snapshot.Returns[i].ReturnID < snapshot.Returns[j].ReturnID })
	for key, record := range r.idem {
		snapshot.Idem[key] = record
	}
	return json.Marshal(snapshot)
}

// Import restores a snapshot, revalidating every field fail-closed: line
// content and identity rules, return bindings to lines, received totals
// recomputed from receipts, inspection splits and serial rules,
// maker-checker, resolution buckets and emissions regenerated from
// inspections, discrepancy shape, status consistency, idempotency key
// bindings, and the ID sequence floor.
func (r *ReturnRegistry) Import(data []byte) error {
	var snapshot returnSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("%w: return snapshot is not valid", ErrSnapshot)
	}
	lines := make(map[string]*IssueLine, len(snapshot.Lines))
	for i := range snapshot.Lines {
		line := &snapshot.Lines[i]
		if strings.TrimSpace(line.LineID) == "" || strings.TrimSpace(line.SiteID) == "" || strings.TrimSpace(line.ItemID) == "" {
			return fmt.Errorf("%w: snapshot line identity is incomplete", ErrSnapshot)
		}
		if line.TenantID == "" || line.CompanyID == "" {
			return fmt.Errorf("%w: snapshot line scope is incomplete", ErrSnapshot)
		}
		if err := line.Quantity.Validate(); err != nil || line.Quantity.Sign() <= 0 {
			return fmt.Errorf("%w: snapshot line quantity is invalid", ErrSnapshot)
		}
		if !line.SourceKind.valid() || strings.TrimSpace(line.SourceRef) == "" {
			return fmt.Errorf("%w: snapshot line source is invalid", ErrSnapshot)
		}
		if line.SourceKind == ReturnSourceCustomerShipment && line.OrderReceipt == "" {
			return fmt.Errorf("%w: snapshot shipment line lacks its order receipt", ErrSnapshot)
		}
		if line.SourceKind != ReturnSourceCustomerShipment && line.OrderReceipt != "" {
			return fmt.Errorf("%w: snapshot line mixes order receipts", ErrSnapshot)
		}
		seen := make(map[string]bool, len(line.Serials))
		for _, serial := range line.Serials {
			if serial == "" || serial != normalizeSerial(serial) || seen[serial] {
				return fmt.Errorf("%w: snapshot line serials are malformed", ErrSnapshot)
			}
			seen[serial] = true
		}
		if len(line.Serials) > 0 && line.Quantity.Scale() != 0 {
			return fmt.Errorf("%w: snapshot serialized line must count whole units", ErrSnapshot)
		}
		mapKey := scopeKey(line.TenantID, line.CompanyID, "line\x00"+line.LineID)
		if _, dup := lines[mapKey]; dup {
			return fmt.Errorf("%w: snapshot line %q repeats", ErrSnapshot, line.LineID)
		}
		lines[mapKey] = line
	}
	returns := make(map[string]*MaterialReturn, len(snapshot.Returns))
	receiptIDs := make(map[string]bool)
	var maxSeq uint64
	claimID := func(prefix, id string) error {
		rest, ok := strings.CutPrefix(id, prefix+"-")
		if !ok || len(rest) != 6 {
			return fmt.Errorf("%w: snapshot id %q is malformed", ErrSnapshot, id)
		}
		seq, err := strconv.ParseUint(rest, 10, 64)
		if err != nil || seq == 0 || fmt.Sprintf("%s-%06d", prefix, seq) != id {
			return fmt.Errorf("%w: snapshot id %q is malformed", ErrSnapshot, id)
		}
		if _, dup := receiptIDs[id]; dup {
			return fmt.Errorf("%w: snapshot id %q repeats", ErrSnapshot, id)
		}
		receiptIDs[id] = true
		if seq > maxSeq {
			maxSeq = seq
		}
		return nil
	}
	for i := range snapshot.Returns {
		ret := &snapshot.Returns[i]
		if err := claimID("ret", ret.ReturnID); err != nil {
			return err
		}
		if ret.TenantID == "" || ret.CompanyID == "" || ret.SiteID == "" || ret.ItemID == "" {
			return fmt.Errorf("%w: snapshot return scope is incomplete", ErrSnapshot)
		}
		if ret.Revision == 0 || ret.OpenedAt.IsZero() || ret.ReturningParty == "" {
			return fmt.Errorf("%w: snapshot return %q is incomplete", ErrSnapshot, ret.ReturnID)
		}
		var line *IssueLine
		if ret.Unlinked {
			if ret.LineID != "" || ret.PermissionRef == "" || ret.LinkEvidence == "" {
				return fmt.Errorf("%w: snapshot unlinked return %q is malformed", ErrSnapshot, ret.ReturnID)
			}
		} else {
			lineKey := scopeKey(ret.TenantID, ret.CompanyID, "line\x00"+ret.LineID)
			var ok bool
			if line, ok = lines[lineKey]; !ok {
				return fmt.Errorf("%w: snapshot return %q references an unknown line", ErrSnapshot, ret.ReturnID)
			}
			if line.SiteID != ret.SiteID || line.ItemID != ret.ItemID {
				return fmt.Errorf("%w: snapshot return %q disagrees with its line", ErrSnapshot, ret.ReturnID)
			}
		}
		scale := ret.Requested.Scale()
		if err := ret.Requested.Validate(); err != nil || ret.Requested.Sign() <= 0 {
			return fmt.Errorf("%w: snapshot return %q requested is invalid", ErrSnapshot, ret.ReturnID)
		}
		if line != nil && scale != line.Quantity.Scale() {
			return fmt.Errorf("%w: snapshot return %q scale disagrees with its line", ErrSnapshot, ret.ReturnID)
		}
		received, err := zeroReturnDecimal(scale)
		if err != nil {
			return fmt.Errorf("%w: zero received: %v", ErrSnapshot, err)
		}
		for j := range ret.Receipts {
			receipt := &ret.Receipts[j]
			if err := claimID("rcp", receipt.ReceiptID); err != nil {
				return err
			}
			if receipt.ReturnID != ret.ReturnID {
				return fmt.Errorf("%w: snapshot receipt is filed under the wrong return", ErrSnapshot)
			}
			if err := receipt.Quantity.Validate(); err != nil || receipt.Quantity.Sign() <= 0 || receipt.Quantity.Scale() != scale {
				return fmt.Errorf("%w: snapshot receipt quantity is invalid", ErrSnapshot)
			}
			if !receipt.Condition.valid() || receipt.Actor == "" || receipt.At.IsZero() {
				return fmt.Errorf("%w: snapshot receipt %q is incomplete", ErrSnapshot, receipt.ReceiptID)
			}
			if receipt.Condition == ReturnConditionDamaged && receipt.Evidence == "" {
				return fmt.Errorf("%w: snapshot damaged receipt lacks evidence", ErrSnapshot)
			}
			serials, dup := normalizeReturnSerials(receipt.Serials)
			_ = serials
			if dup != nil {
				return fmt.Errorf("%w: snapshot receipt serials are malformed: %v", ErrSnapshot, dup)
			}
			if !receipt.QuarantineHeld.Equal(receipt.Quantity) || receipt.Accepted.Sign() != 0 {
				return fmt.Errorf("%w: snapshot receipt buckets disagree with their quantity", ErrSnapshot)
			}
			if len(receipt.Serials) > 0 && !validReturnBinding(receipt.Binding) {
				return fmt.Errorf("%w: snapshot serialized receipt lacks its binding", ErrSnapshot)
			}
			if receipt.Inspection == nil && receipt.Inspected {
				return fmt.Errorf("%w: snapshot receipt inspection flag disagrees", ErrSnapshot)
			}
			if receipt.Inspection != nil {
				inspection := receipt.Inspection
				if !receipt.Inspected || inspection.ReceiptID != receipt.ReceiptID || inspection.Inspector == "" || inspection.Inspector == receipt.Actor || inspection.At.IsZero() {
					return fmt.Errorf("%w: snapshot inspection is malformed", ErrSnapshot)
				}
				for _, part := range []values.Decimal{inspection.Accepted, inspection.Quarantined, inspection.Disposed, inspection.Rejected} {
					if err := part.Validate(); err != nil || part.Scale() != scale || part.Sign() < 0 {
						return fmt.Errorf("%w: snapshot inspection split is invalid", ErrSnapshot)
					}
				}
				split, err := inspection.Accepted.Add(inspection.Quarantined)
				if err != nil {
					return fmt.Errorf("%w: snapshot split arithmetic: %v", ErrSnapshot, err)
				}
				if split, err = split.Add(inspection.Disposed); err != nil {
					return fmt.Errorf("%w: snapshot split arithmetic: %v", ErrSnapshot, err)
				}
				if split, err = split.Add(inspection.Rejected); err != nil {
					return fmt.Errorf("%w: snapshot split arithmetic: %v", ErrSnapshot, err)
				}
				if !split.Equal(receipt.Quantity) {
					return fmt.Errorf("%w: snapshot inspection split disagrees with its receipt", ErrSnapshot)
				}
				if inspection.Disposed.Sign() > 0 && inspection.DisposeReason == "" {
					return fmt.Errorf("%w: snapshot disposal lacks its reason", ErrSnapshot)
				}
				if inspection.ReturnRevision == 0 || inspection.ReturnRevision > ret.Revision {
					return fmt.Errorf("%w: snapshot inspection revision disagrees", ErrSnapshot)
				}
				if len(receipt.Serials) > 0 {
					count, err := strconv.Atoi(inspection.Accepted.String())
					if err != nil || count != len(inspection.AcceptedSerials) {
						return fmt.Errorf("%w: snapshot accepted serials disagree", ErrSnapshot)
					}
					presented := make(map[string]bool, len(receipt.Serials))
					for _, serial := range receipt.Serials {
						presented[serial] = true
					}
					for _, serial := range inspection.AcceptedSerials {
						if !presented[serial] {
							return fmt.Errorf("%w: snapshot accepted serial was not presented", ErrSnapshot)
						}
					}
				} else if len(inspection.AcceptedSerials) > 0 {
					return fmt.Errorf("%w: snapshot untracked receipt accepts serials", ErrSnapshot)
				}
			}
			if received, err = received.Add(receipt.Quantity); err != nil {
				return fmt.Errorf("%w: snapshot received arithmetic: %v", ErrSnapshot, err)
			}
		}
		if !received.Equal(ret.Received) {
			return fmt.Errorf("%w: snapshot return %q received total disagrees with its receipts", ErrSnapshot, ret.ReturnID)
		}
		if ret.Received.Cmp(ret.Requested) > 0 {
			return fmt.Errorf("%w: snapshot received exceeds requested", ErrSnapshot)
		}
		open := false
		for _, entry := range ret.Discrepancies {
			if entry.Key == "" || entry.Reason == "" || entry.Evidence == "" || entry.Actor == "" || entry.At.IsZero() {
				return fmt.Errorf("%w: snapshot discrepancy is incomplete", ErrSnapshot)
			}
			if entry.PresentedLot == "" && len(entry.PresentedSerials) == 0 {
				return fmt.Errorf("%w: snapshot discrepancy lacks presented identity", ErrSnapshot)
			}
			if entry.Resolved && (entry.Decision != DiscrepancyAcceptSubstitute && entry.Decision != DiscrepancyReject || entry.Reviewer == "" || entry.ResolveEvidence == "") {
				return fmt.Errorf("%w: snapshot discrepancy decision is malformed", ErrSnapshot)
			}
			if !entry.Resolved {
				open = true
			}
		}
		wantStatus := ReturnStatusAuthorized
		switch {
		case ret.Cancelled || ret.Resolution != nil:
			wantStatus = ReturnStatusClosed
		case open:
			wantStatus = ReturnStatusDiscrepancy
		}
		if ret.Status != wantStatus {
			return fmt.Errorf("%w: snapshot status disagrees with its records", ErrSnapshot)
		}
		if ret.Cancelled && (ret.CancelReason == "" || ret.Resolution != nil || ret.Received.Sign() > 0) {
			return fmt.Errorf("%w: snapshot cancellation is malformed", ErrSnapshot)
		}
		if ret.Resolution != nil {
			if err := r.validateSnapshotResolution(ret, line); err != nil {
				return err
			}
		}
		mapKey := scopeKey(ret.TenantID, ret.CompanyID, "return\x00"+ret.ReturnID)
		if _, dup := returns[mapKey]; dup {
			return fmt.Errorf("%w: snapshot return %q repeats", ErrSnapshot, ret.ReturnID)
		}
		returns[mapKey] = ret
	}
	if snapshot.Seq < maxSeq {
		return fmt.Errorf("%w: snapshot sequence regressed below issued ids", ErrSnapshot)
	}
	for key, record := range snapshot.Idem {
		parts := strings.SplitN(key, "\x00", 4)
		if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
			return fmt.Errorf("%w: snapshot idempotency key is malformed", ErrSnapshot)
		}
		if record.Key == "" || record.Key != parts[3] || record.Fingerprint == "" {
			return fmt.Errorf("%w: snapshot idempotency record disagrees with its key", ErrSnapshot)
		}
		switch record.Kind {
		case "stock-return-begin", "stock-return-begin-unlinked", "stock-return-receive", "stock-return-inspect", "stock-return-resolve", "stock-return-cancel", "stock-return-escalate", "stock-return-decide":
		default:
			return fmt.Errorf("%w: snapshot idempotency kind %q is unknown", ErrSnapshot, record.Kind)
		}
		retKey := scopeKey(parts[0], parts[1], "return\x00"+record.ReturnID)
		ret, ok := returns[retKey]
		if !ok {
			return fmt.Errorf("%w: snapshot idempotency record references an unknown return", ErrSnapshot)
		}
		if record.ReceiptID != "" {
			found := false
			for _, receipt := range ret.Receipts {
				if receipt.ReceiptID == record.ReceiptID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%w: snapshot idempotency record references an unknown receipt", ErrSnapshot)
			}
		} else if record.Kind == "stock-return-receive" || record.Kind == "stock-return-inspect" {
			return fmt.Errorf("%w: snapshot receipt idempotency record lacks its receipt", ErrSnapshot)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq = snapshot.Seq
	r.lines = make(map[string]*IssueLine, len(lines))
	for key, line := range lines {
		stored := *line
		stored.Serials = append([]string(nil), line.Serials...)
		r.lines[key] = &stored
	}
	r.returns = make(map[string]*MaterialReturn, len(returns))
	for key, ret := range returns {
		stored := *ret
		stored.Receipts = append([]MaterialReceipt(nil), ret.Receipts...)
		stored.Discrepancies = append([]ReturnDiscrepancy(nil), ret.Discrepancies...)
		stored.SubstituteSer = append([]string(nil), ret.SubstituteSer...)
		if ret.Resolution != nil {
			resolved := *ret.Resolution
			resolved.Restock = append([]RestockIntent(nil), ret.Resolution.Restock...)
			resolved.Holds = append([]QuarantineHold(nil), ret.Resolution.Holds...)
			resolved.Disposals = append([]DisposalRecord(nil), ret.Resolution.Disposals...)
			resolved.Rejections = append([]RejectionRecord(nil), ret.Resolution.Rejections...)
			if ret.Resolution.Credit != nil {
				credit := *ret.Resolution.Credit
				resolved.Credit = &credit
			}
			stored.Resolution = &resolved
		}
		r.returns[key] = &stored
	}
	r.idem = make(map[string]returnIdem, len(snapshot.Idem))
	for key, record := range snapshot.Idem {
		r.idem[key] = record
	}
	return nil
}

// validateSnapshotResolution regenerates the expected resolution from the
// return's inspections and proves the snapshot copy matches it exactly.
func (r *ReturnRegistry) validateSnapshotResolution(ret *MaterialReturn, line *IssueLine) error {
	resolution := ret.Resolution
	if resolution.ReturnID != ret.ReturnID || !resolution.Remedy.valid() {
		return fmt.Errorf("%w: snapshot resolution identity is malformed", ErrSnapshot)
	}
	if resolution.AdditionalRemedy != "" && (!resolution.AdditionalRemedy.valid() || resolution.AdditionalRemedy == resolution.Remedy) {
		return fmt.Errorf("%w: snapshot resolution remedies are malformed", ErrSnapshot)
	}
	remedies := map[ReturnRemedy]bool{resolution.Remedy: true, resolution.AdditionalRemedy: true}
	if remedies[ReturnRemedyReplacement] && remedies[ReturnRemedyCreditRequest] && resolution.SplitExceptionRef == "" {
		return fmt.Errorf("%w: snapshot resolution lacks its split exception", ErrSnapshot)
	}
	if resolution.Actor == "" || resolution.At.IsZero() {
		return fmt.Errorf("%w: snapshot resolution is incomplete", ErrSnapshot)
	}
	scale := ret.Requested.Scale()
	zero, err := zeroReturnDecimal(scale)
	if err != nil {
		return fmt.Errorf("%w: zero buckets: %v", ErrSnapshot, err)
	}
	buckets := ReturnBuckets{Requested: ret.Requested, Received: ret.Received, Accepted: zero, Quarantined: zero, Disposed: zero, Rejected: zero}
	var restock []RestockIntent
	var holds []QuarantineHold
	var disposals []DisposalRecord
	var rejections []RejectionRecord
	for _, receipt := range ret.Receipts {
		inspection := receipt.Inspection
		if inspection == nil {
			return fmt.Errorf("%w: snapshot resolved return has an uninspected receipt", ErrSnapshot)
		}
		for _, part := range []struct {
			amount *values.Decimal
			more   values.Decimal
		}{
			{&buckets.Accepted, inspection.Accepted},
			{&buckets.Quarantined, inspection.Quarantined},
			{&buckets.Disposed, inspection.Disposed},
			{&buckets.Rejected, inspection.Rejected},
		} {
			sum, err := part.amount.Add(part.more)
			if err != nil {
				return fmt.Errorf("%w: snapshot bucket arithmetic: %v", ErrSnapshot, err)
			}
			*part.amount = sum
		}
		if inspection.Accepted.Sign() > 0 {
			restock = append(restock, RestockIntent{
				ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, ItemID: ret.ItemID,
				LotID: receipt.LotID, Serials: inspection.AcceptedSerials,
				Quantity: inspection.Accepted, Evidence: receipt.Evidence,
			})
		}
		if inspection.Quarantined.Sign() > 0 {
			holds = append(holds, QuarantineHold{
				ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, ItemID: ret.ItemID,
				Quantity: inspection.Quarantined, Reason: "inspection hold",
			})
		}
		if inspection.Disposed.Sign() > 0 {
			disposals = append(disposals, DisposalRecord{
				ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, ItemID: ret.ItemID,
				Quantity: inspection.Disposed, Reason: inspection.DisposeReason,
			})
		}
		if inspection.Rejected.Sign() > 0 {
			rejections = append(rejections, RejectionRecord{
				ReturnID: ret.ReturnID, ReceiptID: receipt.ReceiptID, ItemID: ret.ItemID,
				Quantity: inspection.Rejected, ShipmentRef: inspection.ReturnShipmentRef,
				HoldQuarantine: inspection.ReturnShipmentRef == "",
			})
		}
	}
	if !buckets.Accepted.Equal(resolution.Buckets.Accepted) || !buckets.Quarantined.Equal(resolution.Buckets.Quarantined) ||
		!buckets.Disposed.Equal(resolution.Buckets.Disposed) || !buckets.Rejected.Equal(resolution.Buckets.Rejected) ||
		!buckets.Requested.Equal(resolution.Buckets.Requested) || !buckets.Received.Equal(resolution.Buckets.Received) {
		return fmt.Errorf("%w: snapshot resolution buckets disagree with inspections", ErrSnapshot)
	}
	encoded := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(raw)
	}
	if encoded(restock) != encoded(resolution.Restock) || encoded(holds) != encoded(resolution.Holds) ||
		encoded(disposals) != encoded(resolution.Disposals) || encoded(rejections) != encoded(resolution.Rejections) {
		return fmt.Errorf("%w: snapshot resolution emissions disagree with inspections", ErrSnapshot)
	}
	if remedies[ReturnRemedyCreditRequest] {
		if ret.Unlinked || line == nil || line.OrderReceipt == "" {
			return fmt.Errorf("%w: snapshot credit lacks its order receipt", ErrSnapshot)
		}
		if resolution.Credit == nil || resolution.Credit.ReturnID != ret.ReturnID || resolution.Credit.OrderReceipt != line.OrderReceipt ||
			resolution.Credit.ItemID != ret.ItemID || !resolution.Credit.Quantity.Equal(ret.Received) {
			return fmt.Errorf("%w: snapshot credit linkage disagrees", ErrSnapshot)
		}
	} else if resolution.Credit != nil {
		return fmt.Errorf("%w: snapshot credit without a credit remedy", ErrSnapshot)
	}
	return nil
}
