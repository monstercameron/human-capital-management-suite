// Package stock lot and serial tracking: SMB-STOCK-008 eligibility,
// quarantine, recall and traceability on top of the STOCK-002 catalog.
package stock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrLotState             = errors.New("stock: lot is not in the required state")
	ErrLotExpired           = errors.New("stock: lot is expired at the pinned cutoff")
	ErrLotBlocked           = errors.New("stock: lot is blocked for issue")
	ErrDuplicateSerial      = errors.New("stock: physical serial is already registered")
	ErrSerialNotFound       = errors.New("stock: serial not found")
	ErrSerialUnavailable    = errors.New("stock: serial is unavailable pending review or lost")
	ErrSerialState          = errors.New("stock: serial command is not valid for the current state")
	ErrAssetBindingRequired = errors.New("stock: serial command requires asset-owner capability binding")
	ErrAssetBindingMismatch = errors.New("stock: asset binding does not match recorded custody or serviceability")
)

// Lot quality states. Only AVAILABLE lots promise or issue stock.
type LotState string

const (
	LotAvailable   LotState = "AVAILABLE"
	LotQuarantined LotState = "QUARANTINED"
	LotRecalled    LotState = "RECALLED"
	LotDisposed    LotState = "DISPOSED"
)

// ExpiryNextLocalDayStart is the single supported cutoff rule: the lot is
// expired from the start of the next local day in the pinned timezone.
const ExpiryNextLocalDayStart = "NEXT_LOCAL_DAY_START"

// ExpiryPolicy is the immutable lot policy snapshot. It never changes when
// the lot moves sites; a stricter site policy is a separate record.
type ExpiryPolicy struct {
	LocalDate string
	Timezone  string
	Rule      string
}

// Lot is one tracked receipt bucket. OnHand is a canonical decimal string at
// Scale; history lives in the immutable event trail, not in revisions.
type Lot struct {
	TenantID       string
	CompanyID      string
	SiteID         string
	LotID          string
	ItemID         string
	ItemSKU        string
	SupplierLot    string
	ReceivedAt     time.Time
	ReceivedFrom   string
	ReceivedDoc    string
	Expiry         ExpiryPolicy
	State          LotState
	PolicyRevision string
	Scale          int32
	OnHand         string
	UpdatedAt      time.Time
}

// RegisterLotInput receives one lot. Origin authentication and scope come
// from the operation context, not the payload.
type RegisterLotInput struct {
	ItemID         string
	SiteID         string
	SupplierLot    string
	ReceivedFrom   string
	ReceivedDoc    string
	ReceivedAt     time.Time
	Expiry         ExpiryPolicy
	PolicyRevision string
	Scale          int32
	OnHand         string
}

// Lot event kinds. Events are append-only facts; nothing edits them.
const (
	LotEventRegistered = "REGISTERED"
	LotEventHeld       = "HELD"
	LotEventReleased   = "RELEASED"
	LotEventRecalled   = "RECALLED"
	LotEventIssued     = "ISSUED"
	LotEventDisposed   = "DISPOSED"
)

// LotEvent is one immutable lot fact. ActorID is an opaque identifier; names
// resolve in the owning domain, never here.
type LotEvent struct {
	Seq         int
	At          time.Time
	Kind        string
	ActorID     string
	Reason      string
	Evidence    string
	Qty         string
	MovementRef string
}

// OpenAllocation is one STOCK-005 demand line against an item. HolderRef is
// opaque to this package and never appears in recall or trace outputs.
type OpenAllocation struct {
	AllocationID string
	LotID        string
	Qty          string
	HolderRef    string
}

// AllocationReader is the STOCK-005 seam: current open demand per item.
type AllocationReader interface {
	OpenAllocations(ctx context.Context, tenantID, companyID, itemID string) ([]OpenAllocation, error)
}

// EligibilityPick is one FEFO take; Excluded names lots left out and why.
type EligibilityPick struct {
	LotID       string
	SupplierLot string
	ExpiryLocal string
	Take        string
}

// EligibilityExclusion names a lot that cannot satisfy this request.
type EligibilityExclusion struct {
	LotID  string
	Reason string
}

// EligibilityPreview is the pre-allocation display: ordered takes plus the
// quantity no eligible lot can cover.
type EligibilityPreview struct {
	Picks     []EligibilityPick
	Excluded  []EligibilityExclusion
	Shortfall string
}

// RecallReport aggregates one recall: every unit counted once across the
// usable, quarantined, demand and shortage buckets.
type RecallReport struct {
	RecallID           string
	TenantID           string
	CompanyID          string
	ItemID             string
	ItemSKU            string
	Reason             string
	Evidence           string
	At                 time.Time
	LotsAffected       []string
	Usable             string
	Quarantined        string
	ActiveDemand       string
	Available          string
	Shortage           string
	BlockedQty         string
	BlockedAllocations []string
}

// LotTrace is the forward trace from receipt: the lot, its pinned cutoff,
// every event in order and every recall touching it.
type LotTrace struct {
	Lot     Lot
	Cutoff  time.Time
	Events  []LotEvent
	Recalls []RecallReport
}

// Serial states. PENDING_REVIEW holds legacy keys until mapped; LOST is
// terminal.
type SerialState string

const (
	SerialAvailable     SerialState = "AVAILABLE"
	SerialAssigned      SerialState = "ASSIGNED"
	SerialOutOfService  SerialState = "OUT_OF_SERVICE"
	SerialLost          SerialState = "LOST"
	SerialPendingReview SerialState = "PENDING_REVIEW"
)

// SerialNamespaceLegacy collects unmapped keys until reviewed.
const SerialNamespaceLegacy = "LEGACY"

// SerialUnit is one physical unit projection. The asset domain owns the
// canonical registry; this row requires its asset_ref and key.
type SerialUnit struct {
	TenantID          string
	CompanyID         string
	Namespace         string
	Serial            string
	AssetRef          string
	ItemID            string
	LotID             string
	SiteID            string
	State             SerialState
	CustodyRev        string
	ServiceabilityRev string
	UpdatedAt         time.Time
}

// RegisterSerialInput registers one physical unit.
type RegisterSerialInput struct {
	Namespace string
	Serial    string
	AssetRef  string
	ItemID    string
	LotID     string
	SiteID    string
}

// SerialCommandKind names the five serialized equipment commands.
type SerialCommandKind string

const (
	SerialIssue    SerialCommandKind = "ISSUE"
	SerialReturn   SerialCommandKind = "RETURN"
	SerialTransfer SerialCommandKind = "TRANSFER"
	SerialLoss     SerialCommandKind = "LOSS"
	SerialAdjust   SerialCommandKind = "ADJUST"
)

// AssetBinding pins the asset-owner capability result a serial command
// executes under: exact asset, custody and serviceability revisions plus the
// movement operation. Revision reconciliation against the asset owner is the
// ASSET-series seam; this package refuses unbound commands outright.
type AssetBinding struct {
	AssetID           string
	CustodyRev        string
	ServiceabilityRev string
	MovementOp        string
}

// SerialCommand is one equipment transition. MarkOutOfService on a bound
// ADJUST parks the unit; clearing it returns the unit to service.
type SerialCommand struct {
	Kind             SerialCommandKind
	Namespace        string
	Serial           string
	Reason           string
	Evidence         string
	ToSite           string
	MarkOutOfService bool
	Binding          *AssetBinding
}

// SerialEvent is one immutable serial fact.
type SerialEvent struct {
	Seq        int
	At         time.Time
	Kind       string
	ActorID    string
	Reason     string
	MovementOp string
	FromState  SerialState
	ToState    SerialState
	ToSite     string
}

// SerialTrace carries the unit and its transition history.
type SerialTrace struct {
	Unit   SerialUnit
	Events []SerialEvent
}

// LotRegistry is the value-owned lot and serial store. Quantities stay exact
// decimal strings; events and recalls are append-only.
type LotRegistry struct {
	mu           sync.Mutex
	catalog      *Catalog
	allocs       AllocationReader
	lots         map[string]*Lot
	lotEvents    map[string][]LotEvent
	serials      map[string]*SerialUnit
	serialEvents map[string][]SerialEvent
	recalls      map[string]RecallReport
	recallsByLot map[string][]string
	idem         map[string]lotIdemRecord
}

type lotIdemRecord struct {
	Fingerprint string
	Lot         *Lot
	Report      *RecallReport
	Unit        *SerialUnit
}

// NewLotRegistry builds an empty registry. A nil catalog skips item checks;
// a nil AllocationReader reports no open demand.
func NewLotRegistry(catalog *Catalog, allocs AllocationReader) *LotRegistry {
	return &LotRegistry{
		catalog:      catalog,
		allocs:       allocs,
		lots:         make(map[string]*Lot),
		lotEvents:    make(map[string][]LotEvent),
		serials:      make(map[string]*SerialUnit),
		serialEvents: make(map[string][]SerialEvent),
		recalls:      make(map[string]RecallReport),
		recallsByLot: make(map[string][]string),
		idem:         make(map[string]lotIdemRecord),
	}
}

func cloneLot(in Lot) Lot { return in }

func cloneUnit(in SerialUnit) SerialUnit { return in }

func lotVisible(lot *Lot, op OperationContext) bool {
	if lot.TenantID != op.TenantID || lot.CompanyID != op.CompanyID {
		return false
	}
	return op.SiteID == "" || lot.SiteID == "" || lot.SiteID == op.SiteID
}

func serialVisible(unit *SerialUnit, op OperationContext) bool {
	if unit.TenantID != op.TenantID || unit.CompanyID != op.CompanyID {
		return false
	}
	return op.SiteID == "" || unit.SiteID == "" || unit.SiteID == op.SiteID
}

func normalizeSerial(raw string) string {
	return norm.NFC.String(strings.TrimSpace(raw))
}

func serialKey(op OperationContext, namespace, serial string) string {
	return scopeKey(op.TenantID, op.CompanyID, "serial\x00"+namespace+"\x00"+serial)
}

// lotCutoff resolves the pinned expiry instant: midnight starting the day
// after LocalDate in the policy timezone.
func lotCutoff(policy ExpiryPolicy) (time.Time, error) {
	if policy.Rule != ExpiryNextLocalDayStart {
		return time.Time{}, fmt.Errorf("%w: expiry rule %q is not declared", ErrStockValidation, policy.Rule)
	}
	day, err := time.Parse("2006-01-02", strings.TrimSpace(policy.LocalDate))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: expiry date %q: %v", ErrStockValidation, policy.LocalDate, err)
	}
	loc, err := time.LoadLocation(strings.TrimSpace(policy.Timezone))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: expiry timezone %q: %v", ErrStockValidation, policy.Timezone, err)
	}
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	return start.AddDate(0, 0, 1), nil
}

// parseLotQty parses an exact stored quantity into signed unscaled units.
func parseLotQty(text string, scale int32) (*big.Int, error) {
	qty, err := values.NewDecimal(strings.TrimSpace(text), scale, values.RoundingExactRequired)
	if err != nil {
		return nil, fmt.Errorf("%w: quantity %q: %v", ErrStockValidation, text, err)
	}
	magnitude := new(big.Int).Set(qty.Unscaled())
	if qty.Sign() < 0 {
		magnitude.Neg(magnitude)
	}
	return magnitude, nil
}

func (r *LotRegistry) checkLotIdem(opName string, op OperationContext, key, fp string) (lotIdemRecord, bool, error) {
	if strings.TrimSpace(key) == "" {
		return lotIdemRecord{}, false, nil
	}
	mapKey := scopeKey(op.TenantID, op.CompanyID, opName+"\x00"+strings.TrimSpace(key))
	prior, ok := r.idem[mapKey]
	if !ok {
		return lotIdemRecord{}, false, nil
	}
	if prior.Fingerprint != fp {
		return lotIdemRecord{}, false, fmt.Errorf("%w: key %q", ErrIdempotencyConflict, key)
	}
	return prior, true, nil
}

func (r *LotRegistry) storeLotIdem(opName string, op OperationContext, key, fp string, rec lotIdemRecord) {
	if strings.TrimSpace(key) == "" {
		return
	}
	mapKey := scopeKey(op.TenantID, op.CompanyID, opName+"\x00"+strings.TrimSpace(key))
	r.idem[mapKey] = rec
}

func (r *LotRegistry) appendLotEvent(lotID string, event LotEvent) {
	event.Seq = len(r.lotEvents[lotID]) + 1
	event.At = time.Now().UTC()
	r.lotEvents[lotID] = append(r.lotEvents[lotID], event)
}

func (r *LotRegistry) appendSerialEvent(key string, event SerialEvent) {
	event.Seq = len(r.serialEvents[key]) + 1
	event.At = time.Now().UTC()
	r.serialEvents[key] = append(r.serialEvents[key], event)
}

func (r *LotRegistry) itemSKU(op OperationContext, itemID string) (string, error) {
	if r.catalog == nil {
		return "", nil
	}
	history, err := r.catalog.History(op, itemID)
	if err != nil {
		return "", err
	}
	return history[0].SKU, nil
}

// RegisterLot receives one lot into AVAILABLE with its immutable expiry
// snapshot. The expiry policy, scale and provenance freeze at registration.
func (r *LotRegistry) RegisterLot(_ context.Context, op OperationContext, input RegisterLotInput, key string) (Lot, error) {
	if err := op.validate(); err != nil {
		return Lot{}, err
	}
	clean := RegisterLotInput{
		ItemID: strings.TrimSpace(input.ItemID), SiteID: strings.TrimSpace(input.SiteID),
		SupplierLot: strings.TrimSpace(input.SupplierLot), ReceivedFrom: strings.TrimSpace(input.ReceivedFrom),
		ReceivedDoc: strings.TrimSpace(input.ReceivedDoc), ReceivedAt: input.ReceivedAt,
		Expiry: ExpiryPolicy{
			LocalDate: strings.TrimSpace(input.Expiry.LocalDate),
			Timezone:  strings.TrimSpace(input.Expiry.Timezone),
			Rule:      strings.TrimSpace(input.Expiry.Rule),
		},
		PolicyRevision: strings.TrimSpace(input.PolicyRevision), Scale: input.Scale,
		OnHand: strings.TrimSpace(input.OnHand),
	}
	if clean.ItemID == "" || clean.SiteID == "" || clean.SupplierLot == "" || clean.PolicyRevision == "" || clean.ReceivedAt.IsZero() {
		return Lot{}, fmt.Errorf("%w: lot needs item, site, supplier lot, policy revision and receipt time", ErrStockValidation)
	}
	if clean.Scale < 0 || clean.Scale > values.MaxScale {
		return Lot{}, fmt.Errorf("%w: scale %d is outside [0,%d]", ErrStockValidation, clean.Scale, values.MaxScale)
	}
	if _, err := lotCutoff(clean.Expiry); err != nil {
		return Lot{}, err
	}
	onHand, err := parseLotQty(clean.OnHand, clean.Scale)
	if err != nil {
		return Lot{}, err
	}
	if onHand.Sign() < 0 {
		return Lot{}, fmt.Errorf("%w: on-hand quantity cannot be negative", ErrStockValidation)
	}
	sku, err := r.itemSKU(op, clean.ItemID)
	if err != nil {
		return Lot{}, err
	}
	onHandText, err := canonicalQuantity(onHand, clean.Scale)
	if err != nil {
		return Lot{}, err
	}
	fp := fingerprint("lot-register", op, clean)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok, err := r.checkLotIdem("lot-register", op, key, fp); err != nil || ok {
		if err != nil {
			return Lot{}, err
		}
		return cloneLot(*prior.Lot), nil
	}
	now := time.Now().UTC()
	lot := &Lot{
		TenantID: op.TenantID, CompanyID: op.CompanyID, SiteID: clean.SiteID, LotID: uuid.NewString(),
		ItemID: clean.ItemID, ItemSKU: sku, SupplierLot: clean.SupplierLot,
		ReceivedAt: clean.ReceivedAt, ReceivedFrom: clean.ReceivedFrom, ReceivedDoc: clean.ReceivedDoc,
		Expiry: clean.Expiry, State: LotAvailable, PolicyRevision: clean.PolicyRevision,
		Scale: clean.Scale, OnHand: onHandText, UpdatedAt: now,
	}
	r.lots[lot.LotID] = lot
	r.appendLotEvent(lot.LotID, LotEvent{Kind: LotEventRegistered, ActorID: op.ActorID, Qty: onHandText})
	r.storeLotIdem("lot-register", op, key, fp, lotIdemRecord{Fingerprint: fp, Lot: lot})
	return cloneLot(*lot), nil
}

// HoldLot quarantines an available lot with a reason. The hold and racing
// issues serialize on the registry mutex: no issue posts after the hold.
func (r *LotRegistry) HoldLot(_ context.Context, op OperationContext, lotID, reason, evidence, key string) (Lot, error) {
	if err := op.validate(); err != nil {
		return Lot{}, err
	}
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return Lot{}, fmt.Errorf("%w: hold needs a reason", ErrStockValidation)
	}
	fp := fingerprint("lot-hold", op, lotID, trimmed, strings.TrimSpace(evidence))
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok, err := r.checkLotIdem("lot-hold", op, key, fp); err != nil || ok {
		if err != nil {
			return Lot{}, err
		}
		return cloneLot(*prior.Lot), nil
	}
	lot, ok := r.lots[lotID]
	if !ok || !lotVisible(lot, op) {
		return Lot{}, fmt.Errorf("%w: %s", ErrItemNotFound, lotID)
	}
	if lot.State != LotAvailable {
		return Lot{}, fmt.Errorf("%w: lot %s is %s", ErrLotState, lotID, lot.State)
	}
	lot.State = LotQuarantined
	lot.UpdatedAt = time.Now().UTC()
	r.appendLotEvent(lotID, LotEvent{Kind: LotEventHeld, ActorID: op.ActorID, Reason: trimmed, Evidence: strings.TrimSpace(evidence)})
	r.storeLotIdem("lot-hold", op, key, fp, lotIdemRecord{Fingerprint: fp, Lot: lot})
	return cloneLot(*lot), nil
}

// ReleaseLot returns a quarantined lot to available under reviewer evidence.
// Expired lots stay quarantined: release never revives dead proposals.
func (r *LotRegistry) ReleaseLot(_ context.Context, op OperationContext, lotID, reviewer, evidence string, asOf time.Time, key string) (Lot, error) {
	if err := op.validate(); err != nil {
		return Lot{}, err
	}
	trimmedReviewer := strings.TrimSpace(reviewer)
	trimmedEvidence := strings.TrimSpace(evidence)
	if trimmedReviewer == "" || trimmedEvidence == "" {
		return Lot{}, fmt.Errorf("%w: release needs a reviewer and evidence", ErrStockValidation)
	}
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	fp := fingerprint("lot-release", op, lotID, trimmedReviewer, trimmedEvidence)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok, err := r.checkLotIdem("lot-release", op, key, fp); err != nil || ok {
		if err != nil {
			return Lot{}, err
		}
		return cloneLot(*prior.Lot), nil
	}
	lot, ok := r.lots[lotID]
	if !ok || !lotVisible(lot, op) {
		return Lot{}, fmt.Errorf("%w: %s", ErrItemNotFound, lotID)
	}
	if lot.State != LotQuarantined {
		return Lot{}, fmt.Errorf("%w: lot %s is %s", ErrLotState, lotID, lot.State)
	}
	cutoff, err := lotCutoff(lot.Expiry)
	if err != nil {
		return Lot{}, err
	}
	if !asOf.Before(cutoff) {
		return Lot{}, fmt.Errorf("%w: lot %s expired at %v", ErrLotExpired, lotID, cutoff)
	}
	lot.State = LotAvailable
	lot.UpdatedAt = time.Now().UTC()
	r.appendLotEvent(lotID, LotEvent{Kind: LotEventReleased, ActorID: op.ActorID, Reason: "released by " + trimmedReviewer, Evidence: trimmedEvidence})
	r.storeLotIdem("lot-release", op, key, fp, lotIdemRecord{Fingerprint: fp, Lot: lot})
	return cloneLot(*lot), nil
}

// LotExpired reports whether the lot is expired at asOf in its pinned zone.
func (r *LotRegistry) LotExpired(op OperationContext, lotID string, asOf time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lot, ok := r.lots[lotID]
	if !ok || !lotVisible(lot, op) {
		return false, fmt.Errorf("%w: %s", ErrItemNotFound, lotID)
	}
	cutoff, err := lotCutoff(lot.Expiry)
	if err != nil {
		return false, err
	}
	return !asOf.Before(cutoff), nil
}

// IssueFromLot posts one movement-bound issue after rechecking state and
// expiry at issue time. Every check and the decrement hold the same mutex.
func (r *LotRegistry) IssueFromLot(_ context.Context, op OperationContext, lotID, qtyText, movementRef string, asOf time.Time, key string) (Lot, error) {
	if err := op.validate(); err != nil {
		return Lot{}, err
	}
	trimmedRef := strings.TrimSpace(movementRef)
	if trimmedRef == "" {
		return Lot{}, fmt.Errorf("%w: issue needs a movement reference", ErrStockValidation)
	}
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	fp := fingerprint("lot-issue", op, lotID, strings.TrimSpace(qtyText), trimmedRef)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok, err := r.checkLotIdem("lot-issue", op, key, fp); err != nil || ok {
		if err != nil {
			return Lot{}, err
		}
		return cloneLot(*prior.Lot), nil
	}
	lot, ok := r.lots[lotID]
	if !ok || !lotVisible(lot, op) {
		return Lot{}, fmt.Errorf("%w: %s", ErrItemNotFound, lotID)
	}
	if lot.State != LotAvailable {
		return Lot{}, fmt.Errorf("%w: lot %s is %s", ErrLotBlocked, lotID, lot.State)
	}
	cutoff, err := lotCutoff(lot.Expiry)
	if err != nil {
		return Lot{}, err
	}
	if !asOf.Before(cutoff) {
		return Lot{}, fmt.Errorf("%w: lot %s expired at %v", ErrLotExpired, lotID, cutoff)
	}
	qty, err := parseLotQty(qtyText, lot.Scale)
	if err != nil {
		return Lot{}, err
	}
	if qty.Sign() <= 0 {
		return Lot{}, fmt.Errorf("%w: issue quantity must be positive", ErrStockValidation)
	}
	onHand, err := parseLotQty(lot.OnHand, lot.Scale)
	if err != nil {
		return Lot{}, err
	}
	if qty.Cmp(onHand) > 0 {
		return Lot{}, fmt.Errorf("%w: issue %q exceeds on hand %q", ErrLotState, qtyText, lot.OnHand)
	}
	remaining := new(big.Int).Sub(onHand, qty)
	remainingText, err := canonicalQuantity(remaining, lot.Scale)
	if err != nil {
		return Lot{}, err
	}
	qtyText2, err := canonicalQuantity(qty, lot.Scale)
	if err != nil {
		return Lot{}, err
	}
	lot.OnHand = remainingText
	lot.UpdatedAt = time.Now().UTC()
	r.appendLotEvent(lotID, LotEvent{Kind: LotEventIssued, ActorID: op.ActorID, Qty: qtyText2, MovementRef: trimmedRef})
	r.storeLotIdem("lot-issue", op, key, fp, lotIdemRecord{Fingerprint: fp, Lot: lot})
	return cloneLot(*lot), nil
}

// DisposeLot ends a lot through a movement reference. Disposal moves the
// remaining on-hand out; it never deletes the trail.
func (r *LotRegistry) DisposeLot(_ context.Context, op OperationContext, lotID, movementRef, reason, key string) (Lot, error) {
	if err := op.validate(); err != nil {
		return Lot{}, err
	}
	trimmedRef := strings.TrimSpace(movementRef)
	trimmedReason := strings.TrimSpace(reason)
	if trimmedRef == "" || trimmedReason == "" {
		return Lot{}, fmt.Errorf("%w: disposal needs a movement reference and reason", ErrStockValidation)
	}
	fp := fingerprint("lot-dispose", op, lotID, trimmedRef, trimmedReason)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok, err := r.checkLotIdem("lot-dispose", op, key, fp); err != nil || ok {
		if err != nil {
			return Lot{}, err
		}
		return cloneLot(*prior.Lot), nil
	}
	lot, ok := r.lots[lotID]
	if !ok || !lotVisible(lot, op) {
		return Lot{}, fmt.Errorf("%w: %s", ErrItemNotFound, lotID)
	}
	if lot.State == LotDisposed {
		return Lot{}, fmt.Errorf("%w: lot %s is already disposed", ErrLotState, lotID)
	}
	r.appendLotEvent(lotID, LotEvent{Kind: LotEventDisposed, ActorID: op.ActorID, Reason: trimmedReason, Qty: lot.OnHand, MovementRef: trimmedRef})
	zero, err := canonicalQuantity(new(big.Int), lot.Scale)
	if err != nil {
		return Lot{}, err
	}
	lot.OnHand = zero
	lot.State = LotDisposed
	lot.UpdatedAt = time.Now().UTC()
	r.storeLotIdem("lot-dispose", op, key, fp, lotIdemRecord{Fingerprint: fp, Lot: lot})
	return cloneLot(*lot), nil
}

// RecallItem atomically reclassifies every usable lot of the item to
// recalled and blocks matching demand. Active demand stays counted once:
// available clamps at zero while shortage names the gap.
func (r *LotRegistry) RecallItem(ctx context.Context, op OperationContext, itemID, reason, evidence, key string) (RecallReport, error) {
	if err := op.validate(); err != nil {
		return RecallReport{}, err
	}
	trimmedReason := strings.TrimSpace(reason)
	trimmedEvidence := strings.TrimSpace(evidence)
	if strings.TrimSpace(itemID) == "" || trimmedReason == "" || trimmedEvidence == "" {
		return RecallReport{}, fmt.Errorf("%w: recall needs an item, reason and evidence", ErrStockValidation)
	}
	if _, err := r.itemSKU(op, strings.TrimSpace(itemID)); err != nil {
		return RecallReport{}, err
	}
	var demand []OpenAllocation
	if r.allocs != nil {
		allocs, err := r.allocs.OpenAllocations(ctx, op.TenantID, op.CompanyID, strings.TrimSpace(itemID))
		if err != nil {
			return RecallReport{}, err
		}
		demand = allocs
	}
	fp := fingerprint("lot-recall", op, strings.TrimSpace(itemID), trimmedReason, trimmedEvidence)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok, err := r.checkLotIdem("lot-recall", op, key, fp); err != nil || ok {
		if err != nil {
			return RecallReport{}, err
		}
		return cloneRecall(*prior.Report), nil
	}
	var scoped []*Lot
	for _, lot := range r.lots {
		if lot.ItemID == strings.TrimSpace(itemID) && lotVisible(lot, op) && lot.State != LotDisposed {
			scoped = append(scoped, lot)
		}
	}
	if len(scoped) == 0 {
		return RecallReport{}, fmt.Errorf("%w: no lots for item %s", ErrItemNotFound, itemID)
	}
	scale := scoped[0].Scale
	for _, lot := range scoped[1:] {
		if lot.Scale != scale {
			return RecallReport{}, fmt.Errorf("%w: item %s spans mixed lot scales", ErrStockValidation, itemID)
		}
	}
	var transitioned []string
	for _, lot := range scoped {
		if lot.State == LotAvailable || lot.State == LotQuarantined {
			lot.State = LotRecalled
			lot.UpdatedAt = time.Now().UTC()
			r.appendLotEvent(lot.LotID, LotEvent{Kind: LotEventRecalled, ActorID: op.ActorID, Reason: trimmedReason, Evidence: trimmedEvidence})
			transitioned = append(transitioned, lot.LotID)
		}
	}
	sort.Strings(transitioned)
	recalled := map[string]bool{}
	for _, lot := range scoped {
		if lot.State == LotRecalled {
			recalled[lot.LotID] = true
		}
	}
	usable := new(big.Int)
	quarantined := new(big.Int)
	for _, lot := range scoped {
		onHand, err := parseLotQty(lot.OnHand, scale)
		if err != nil {
			return RecallReport{}, err
		}
		switch lot.State {
		case LotAvailable:
			usable.Add(usable, onHand)
		case LotQuarantined, LotRecalled:
			quarantined.Add(quarantined, onHand)
		}
	}
	activeDemand := new(big.Int)
	blockedQty := new(big.Int)
	var blocked []string
	for _, alloc := range demand {
		qty, err := parseLotQty(alloc.Qty, scale)
		if err != nil {
			return RecallReport{}, fmt.Errorf("%w: allocation %q: %v", ErrStockValidation, alloc.AllocationID, err)
		}
		activeDemand.Add(activeDemand, qty)
		if alloc.LotID == "" || recalled[alloc.LotID] {
			blockedQty.Add(blockedQty, qty)
			blocked = append(blocked, alloc.AllocationID)
		}
	}
	sort.Strings(blocked)
	available := new(big.Int).Sub(usable, activeDemand)
	if available.Sign() < 0 {
		available = new(big.Int)
	}
	shortage := new(big.Int).Sub(activeDemand, usable)
	if shortage.Sign() < 0 {
		shortage = new(big.Int)
	}
	report := RecallReport{
		RecallID: uuid.NewString(), TenantID: op.TenantID, CompanyID: op.CompanyID,
		ItemID: strings.TrimSpace(itemID), ItemSKU: scoped[0].ItemSKU,
		Reason: trimmedReason, Evidence: trimmedEvidence, At: time.Now().UTC(),
		LotsAffected:       transitioned,
		BlockedAllocations: blocked,
	}
	var err error
	if report.Usable, err = canonicalQuantity(usable, scale); err != nil {
		return RecallReport{}, err
	}
	if report.Quarantined, err = canonicalQuantity(quarantined, scale); err != nil {
		return RecallReport{}, err
	}
	if report.ActiveDemand, err = canonicalQuantity(activeDemand, scale); err != nil {
		return RecallReport{}, err
	}
	if report.Available, err = canonicalQuantity(available, scale); err != nil {
		return RecallReport{}, err
	}
	if report.Shortage, err = canonicalQuantity(shortage, scale); err != nil {
		return RecallReport{}, err
	}
	if report.BlockedQty, err = canonicalQuantity(blockedQty, scale); err != nil {
		return RecallReport{}, err
	}
	stored := cloneRecall(report)
	r.recalls[report.RecallID] = stored
	for _, lotID := range transitioned {
		r.recallsByLot[lotID] = append(r.recallsByLot[lotID], report.RecallID)
	}
	r.storeLotIdem("lot-recall", op, key, fp, lotIdemRecord{Fingerprint: fp, Report: &stored})
	return report, nil
}

func cloneRecall(in RecallReport) RecallReport {
	in.LotsAffected = append([]string(nil), in.LotsAffected...)
	in.BlockedAllocations = append([]string(nil), in.BlockedAllocations...)
	return in
}

// PreviewEligibility suggests FEFO takes for qtyText as of the given instant:
// earliest pinned cutoff first, lot ID breaking ties. Quarantined, recalled,
// disposed, expired and depleted lots are excluded with reasons, never takes.
func (r *LotRegistry) PreviewEligibility(op OperationContext, itemID, qtyText string, asOf time.Time) (EligibilityPreview, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	trimmed := strings.TrimSpace(itemID)
	var scoped []*Lot
	for _, lot := range r.lots {
		if lot.ItemID == trimmed && lotVisible(lot, op) {
			scoped = append(scoped, lot)
		}
	}
	if r.catalog != nil {
		if _, err := r.catalog.History(op, trimmed); err != nil {
			return EligibilityPreview{}, err
		}
	} else if len(scoped) == 0 {
		return EligibilityPreview{}, fmt.Errorf("%w: %s", ErrItemNotFound, trimmed)
	}
	scale, err := r.eligibilityScale(op, trimmed, scoped)
	if err != nil {
		return EligibilityPreview{}, err
	}
	need, err := parseLotQty(qtyText, scale)
	if err != nil {
		return EligibilityPreview{}, err
	}
	if need.Sign() <= 0 {
		return EligibilityPreview{}, fmt.Errorf("%w: requested quantity must be positive", ErrStockValidation)
	}
	type candidate struct {
		lot    *Lot
		cutoff time.Time
		onHand *big.Int
	}
	var candidates []candidate
	preview := EligibilityPreview{}
	for _, lot := range scoped {
		if lot.State != LotAvailable {
			preview.Excluded = append(preview.Excluded, EligibilityExclusion{LotID: lot.LotID, Reason: string(lot.State)})
			continue
		}
		cutoff, err := lotCutoff(lot.Expiry)
		if err != nil {
			return EligibilityPreview{}, err
		}
		if !asOf.Before(cutoff) {
			preview.Excluded = append(preview.Excluded, EligibilityExclusion{LotID: lot.LotID, Reason: "EXPIRED"})
			continue
		}
		onHand, err := parseLotQty(lot.OnHand, scale)
		if err != nil {
			return EligibilityPreview{}, err
		}
		if onHand.Sign() <= 0 {
			preview.Excluded = append(preview.Excluded, EligibilityExclusion{LotID: lot.LotID, Reason: "DEPLETED"})
			continue
		}
		candidates = append(candidates, candidate{lot: lot, cutoff: cutoff, onHand: onHand})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].cutoff.Equal(candidates[j].cutoff) {
			return candidates[i].lot.LotID < candidates[j].lot.LotID
		}
		return candidates[i].cutoff.Before(candidates[j].cutoff)
	})
	remaining := new(big.Int).Set(need)
	for _, c := range candidates {
		if remaining.Sign() <= 0 {
			break
		}
		take := new(big.Int).Set(c.onHand)
		if take.Cmp(remaining) > 0 {
			take.Set(remaining)
		}
		takeText, err := canonicalQuantity(take, scale)
		if err != nil {
			return EligibilityPreview{}, err
		}
		preview.Picks = append(preview.Picks, EligibilityPick{LotID: c.lot.LotID, SupplierLot: c.lot.SupplierLot, ExpiryLocal: c.lot.Expiry.LocalDate, Take: takeText})
		remaining.Sub(remaining, take)
	}
	sort.Slice(preview.Excluded, func(i, j int) bool { return preview.Excluded[i].LotID < preview.Excluded[j].LotID })
	if preview.Shortfall, err = canonicalQuantity(remaining, scale); err != nil {
		return EligibilityPreview{}, err
	}
	return preview, nil
}

func (r *LotRegistry) eligibilityScale(op OperationContext, itemID string, scoped []*Lot) (int32, error) {
	if r.catalog != nil {
		history, err := r.catalog.History(op, itemID)
		if err != nil {
			return 0, err
		}
		return history[len(history)-1].Scale, nil
	}
	if len(scoped) == 0 {
		return 0, fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
	}
	for _, lot := range scoped[1:] {
		if lot.Scale != scoped[0].Scale {
			return 0, fmt.Errorf("%w: item %s spans mixed lot scales", ErrStockValidation, itemID)
		}
	}
	return scoped[0].Scale, nil
}

// TraceLot returns the forward trace from receipt: the lot, its pinned
// cutoff, every event in order and every recall touching it. Outputs carry
// opaque IDs and counts; holder and worker names never appear.
func (r *LotRegistry) TraceLot(op OperationContext, lotID string) (LotTrace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lot, ok := r.lots[lotID]
	if !ok || !lotVisible(lot, op) {
		return LotTrace{}, fmt.Errorf("%w: %s", ErrItemNotFound, lotID)
	}
	cutoff, err := lotCutoff(lot.Expiry)
	if err != nil {
		return LotTrace{}, err
	}
	trace := LotTrace{Lot: cloneLot(*lot), Cutoff: cutoff, Events: append([]LotEvent(nil), r.lotEvents[lotID]...)}
	for _, recallID := range r.recallsByLot[lotID] {
		if report, ok := r.recalls[recallID]; ok {
			trace.Recalls = append(trace.Recalls, cloneRecall(report))
		}
	}
	return trace, nil
}

// RegisterSerial records one physical unit under its canonical key
// (company, namespace, normalized serial). The key excludes catalog and item
// revision: reclassifying an item cannot duplicate one physical unit.
// Namespace defaults to LEGACY, which stays pending review until mapped.
func (r *LotRegistry) RegisterSerial(_ context.Context, op OperationContext, input RegisterSerialInput, key string) (SerialUnit, error) {
	if err := op.validate(); err != nil {
		return SerialUnit{}, err
	}
	namespace := normalizeSerial(input.Namespace)
	if namespace == "" {
		namespace = SerialNamespaceLegacy
	}
	serial := normalizeSerial(input.Serial)
	asset := strings.TrimSpace(input.AssetRef)
	itemID := strings.TrimSpace(input.ItemID)
	lotID := strings.TrimSpace(input.LotID)
	siteID := strings.TrimSpace(input.SiteID)
	if serial == "" || itemID == "" {
		return SerialUnit{}, fmt.Errorf("%w: serial needs a serial and item", ErrStockValidation)
	}
	if namespace != SerialNamespaceLegacy && asset == "" {
		return SerialUnit{}, fmt.Errorf("%w: serial requires asset_ref", ErrStockValidation)
	}
	if _, err := r.itemSKU(op, itemID); err != nil {
		return SerialUnit{}, err
	}
	fp := fingerprint("serial-register", op, namespace, serial, asset, itemID, lotID, siteID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok, err := r.checkLotIdem("serial-register", op, key, fp); err != nil || ok {
		if err != nil {
			return SerialUnit{}, err
		}
		return cloneUnit(*prior.Unit), nil
	}
	if lotID != "" {
		lot, ok := r.lots[lotID]
		if !ok || !lotVisible(lot, op) {
			return SerialUnit{}, fmt.Errorf("%w: %s", ErrItemNotFound, lotID)
		}
	}
	mapKey := serialKey(op, namespace, serial)
	if _, exists := r.serials[mapKey]; exists {
		return SerialUnit{}, fmt.Errorf("%w: %s %q", ErrDuplicateSerial, namespace, serial)
	}
	state := SerialAvailable
	if namespace == SerialNamespaceLegacy && asset == "" {
		state = SerialPendingReview
	}
	now := time.Now().UTC()
	unit := &SerialUnit{
		TenantID: op.TenantID, CompanyID: op.CompanyID, Namespace: namespace, Serial: serial,
		AssetRef: asset, ItemID: itemID, LotID: lotID, SiteID: siteID, State: state, UpdatedAt: now,
	}
	r.serials[mapKey] = unit
	r.appendSerialEvent(mapKey, SerialEvent{Kind: LotEventRegistered, ActorID: op.ActorID, FromState: state, ToState: state})
	r.storeLotIdem("serial-register", op, key, fp, lotIdemRecord{Fingerprint: fp, Unit: unit})
	return cloneUnit(*unit), nil
}

// MapLegacySerial binds a pending legacy key to its asset and releases it.
func (r *LotRegistry) MapLegacySerial(_ context.Context, op OperationContext, namespace, serial, assetRef, key string) (SerialUnit, error) {
	if err := op.validate(); err != nil {
		return SerialUnit{}, err
	}
	cleanNS := normalizeSerial(namespace)
	cleanSerial := normalizeSerial(serial)
	cleanAsset := strings.TrimSpace(assetRef)
	if cleanSerial == "" || cleanAsset == "" {
		return SerialUnit{}, fmt.Errorf("%w: legacy mapping needs a serial and asset_ref", ErrStockValidation)
	}
	fp := fingerprint("serial-map", op, cleanNS, cleanSerial, cleanAsset)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok, err := r.checkLotIdem("serial-map", op, key, fp); err != nil || ok {
		if err != nil {
			return SerialUnit{}, err
		}
		return cloneUnit(*prior.Unit), nil
	}
	mapKey := serialKey(op, cleanNS, cleanSerial)
	unit, ok := r.serials[mapKey]
	if !ok || !serialVisible(unit, op) {
		return SerialUnit{}, fmt.Errorf("%w: %s %q", ErrSerialNotFound, cleanNS, cleanSerial)
	}
	if unit.Namespace != SerialNamespaceLegacy || unit.State != SerialPendingReview {
		return SerialUnit{}, fmt.Errorf("%w: %s %q is not pending legacy review", ErrSerialState, cleanNS, cleanSerial)
	}
	unit.AssetRef = cleanAsset
	unit.State = SerialAvailable
	unit.UpdatedAt = time.Now().UTC()
	r.appendSerialEvent(mapKey, SerialEvent{Kind: "MAPPED", ActorID: op.ActorID, FromState: SerialPendingReview, ToState: SerialAvailable})
	r.storeLotIdem("serial-map", op, key, fp, lotIdemRecord{Fingerprint: fp, Unit: unit})
	return cloneUnit(*unit), nil
}

// PostSerialCommand executes one bound equipment transition. Generic
// (unbound) commands are refused with zero movement; bound replays return
// the durable unit without projecting a second transition.
func (r *LotRegistry) PostSerialCommand(_ context.Context, op OperationContext, cmd SerialCommand, key string) (SerialUnit, error) {
	if err := op.validate(); err != nil {
		return SerialUnit{}, err
	}
	switch cmd.Kind {
	case SerialIssue, SerialReturn, SerialTransfer, SerialLoss, SerialAdjust:
	default:
		return SerialUnit{}, fmt.Errorf("%w: serial command %q is not declared", ErrStockValidation, cmd.Kind)
	}
	namespace := normalizeSerial(cmd.Namespace)
	serial := normalizeSerial(cmd.Serial)
	reason := strings.TrimSpace(cmd.Reason)
	if namespace == "" || serial == "" || reason == "" {
		return SerialUnit{}, fmt.Errorf("%w: serial command needs namespace, serial and reason", ErrStockValidation)
	}
	toSite := strings.TrimSpace(cmd.ToSite)
	if cmd.Kind == SerialTransfer && toSite == "" {
		return SerialUnit{}, fmt.Errorf("%w: transfer needs a destination site", ErrStockValidation)
	}
	var binding AssetBinding
	if cmd.Binding != nil {
		binding = *cmd.Binding
	}
	fp := fingerprint("serial-cmd", op, string(cmd.Kind), namespace, serial, reason, strings.TrimSpace(cmd.Evidence), toSite, cmd.MarkOutOfService, binding)
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok, err := r.checkLotIdem("serial-cmd", op, key, fp); err != nil || ok {
		if err != nil {
			return SerialUnit{}, err
		}
		return cloneUnit(*prior.Unit), nil
	}
	mapKey := serialKey(op, namespace, serial)
	unit, ok := r.serials[mapKey]
	if !ok || !serialVisible(unit, op) {
		return SerialUnit{}, fmt.Errorf("%w: %s %q", ErrSerialNotFound, namespace, serial)
	}
	if unit.State == SerialPendingReview || unit.State == SerialLost {
		return SerialUnit{}, fmt.Errorf("%w: %s %q", ErrSerialUnavailable, namespace, serial)
	}
	if cmd.Binding == nil {
		return SerialUnit{}, fmt.Errorf("%w: %s on %s %q", ErrAssetBindingRequired, cmd.Kind, namespace, serial)
	}
	if strings.TrimSpace(binding.AssetID) != unit.AssetRef {
		return SerialUnit{}, fmt.Errorf("%w: binding names %q, unit is %q", ErrAssetBindingMismatch, binding.AssetID, unit.AssetRef)
	}
	if strings.TrimSpace(binding.CustodyRev) == "" || strings.TrimSpace(binding.ServiceabilityRev) == "" || strings.TrimSpace(binding.MovementOp) == "" {
		return SerialUnit{}, fmt.Errorf("%w: binding needs custody, serviceability and movement revisions", ErrStockValidation)
	}
	from := unit.State
	to := from
	switch cmd.Kind {
	case SerialIssue:
		if from != SerialAvailable {
			return SerialUnit{}, fmt.Errorf("%w: issue from %s", ErrSerialState, from)
		}
		to = SerialAssigned
	case SerialReturn:
		if from != SerialAssigned {
			return SerialUnit{}, fmt.Errorf("%w: return from %s", ErrSerialState, from)
		}
		to = SerialAvailable
	case SerialTransfer:
		unit.SiteID = toSite
	case SerialLoss:
		to = SerialLost
	case SerialAdjust:
		if cmd.MarkOutOfService {
			if from != SerialAvailable && from != SerialAssigned {
				return SerialUnit{}, fmt.Errorf("%w: park from %s", ErrSerialState, from)
			}
			to = SerialOutOfService
		} else if from == SerialOutOfService {
			to = SerialAvailable
		}
	}
	unit.CustodyRev = strings.TrimSpace(binding.CustodyRev)
	unit.ServiceabilityRev = strings.TrimSpace(binding.ServiceabilityRev)
	unit.State = to
	unit.UpdatedAt = time.Now().UTC()
	r.appendSerialEvent(mapKey, SerialEvent{
		Kind: string(cmd.Kind), ActorID: op.ActorID, Reason: reason,
		MovementOp: strings.TrimSpace(binding.MovementOp), FromState: from, ToState: to, ToSite: toSite,
	})
	r.storeLotIdem("serial-cmd", op, key, fp, lotIdemRecord{Fingerprint: fp, Unit: unit})
	return cloneUnit(*unit), nil
}

// SerialTrace returns the unit and its transition history.
func (r *LotRegistry) SerialTrace(op OperationContext, namespace, serial string) (SerialTrace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mapKey := serialKey(op, normalizeSerial(namespace), normalizeSerial(serial))
	unit, ok := r.serials[mapKey]
	if !ok || !serialVisible(unit, op) {
		return SerialTrace{}, fmt.Errorf("%w: %s %q", ErrSerialNotFound, namespace, serial)
	}
	return SerialTrace{Unit: cloneUnit(*unit), Events: append([]SerialEvent(nil), r.serialEvents[mapKey]...)}, nil
}

// Snapshot exports lots, serials, events, recalls and durable idempotency
// results so a restart reopens the same obligations.
func (r *LotRegistry) Snapshot() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	export := struct {
		Lots         map[string]Lot
		LotEvents    map[string][]LotEvent
		Serials      map[string]SerialUnit
		SerialEvents map[string][]SerialEvent
		Recalls      map[string]RecallReport
		RecallsByLot map[string][]string
		Idem         map[string]lotIdemRecord
	}{
		Lots: make(map[string]Lot, len(r.lots)), LotEvents: r.lotEvents,
		Serials: make(map[string]SerialUnit, len(r.serials)), SerialEvents: r.serialEvents,
		Recalls: r.recalls, RecallsByLot: r.recallsByLot, Idem: r.idem,
	}
	for id, lot := range r.lots {
		export.Lots[id] = cloneLot(*lot)
	}
	for key, unit := range r.serials {
		export.Serials[key] = cloneUnit(*unit)
	}
	return json.Marshal(export)
}

// Restore loads a Snapshot into an empty registry.
func (r *LotRegistry) Restore(data []byte) error {
	var export struct {
		Lots         map[string]Lot
		LotEvents    map[string][]LotEvent
		Serials      map[string]SerialUnit
		SerialEvents map[string][]SerialEvent
		Recalls      map[string]RecallReport
		RecallsByLot map[string][]string
		Idem         map[string]lotIdemRecord
	}
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("%w: %v", ErrSnapshot, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lots) > 0 || len(r.serials) > 0 || len(r.recalls) > 0 {
		return fmt.Errorf("%w: registry is already populated", ErrSnapshot)
	}
	for id, lot := range export.Lots {
		if lot.LotID != id || lot.TenantID == "" || lot.CompanyID == "" || lot.ItemID == "" {
			return fmt.Errorf("%w: lot %s is corrupt", ErrSnapshot, id)
		}
	}
	for key, unit := range export.Serials {
		if unit.TenantID == "" || unit.CompanyID == "" || unit.Namespace == "" || unit.Serial == "" {
			return fmt.Errorf("%w: serial %s is corrupt", ErrSnapshot, key)
		}
	}
	r.lots = make(map[string]*Lot, len(export.Lots))
	for id, lot := range export.Lots {
		stored := cloneLot(lot)
		r.lots[id] = &stored
	}
	r.lotEvents = export.LotEvents
	if r.lotEvents == nil {
		r.lotEvents = make(map[string][]LotEvent)
	}
	r.serials = make(map[string]*SerialUnit, len(export.Serials))
	for key, unit := range export.Serials {
		stored := cloneUnit(unit)
		r.serials[key] = &stored
	}
	r.serialEvents = export.SerialEvents
	if r.serialEvents == nil {
		r.serialEvents = make(map[string][]SerialEvent)
	}
	r.recalls = export.Recalls
	if r.recalls == nil {
		r.recalls = make(map[string]RecallReport)
	}
	r.recallsByLot = export.RecallsByLot
	if r.recallsByLot == nil {
		r.recallsByLot = make(map[string][]string)
	}
	r.idem = export.Idem
	if r.idem == nil {
		r.idem = make(map[string]lotIdemRecord)
	}
	return nil
}

// LotEventAmended records an opening-correction amendment of on-hand stock.
const LotEventAmended = "AMENDED"

// AmendLotOnHand sets a lot on-hand quantity for opening corrections. The
// amendment is an explicit event, never a silent rewrite.
func (r *LotRegistry) AmendLotOnHand(op OperationContext, lotID, newQtyText, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	lot, ok := r.lots[lotID]
	if !ok || !lotVisible(lot, op) {
		return fmt.Errorf("%w: %s", ErrItemNotFound, lotID)
	}
	qty, err := parseLotQty(strings.TrimSpace(newQtyText), lot.Scale)
	if err != nil || qty.Sign() < 0 {
		return fmt.Errorf("%w: amendment quantity must be a non-negative exact decimal", ErrStockValidation)
	}
	text, err := canonicalQuantity(qty, lot.Scale)
	if err != nil {
		return err
	}
	lot.OnHand = text
	lot.UpdatedAt = time.Now().UTC()
	r.appendLotEvent(lotID, LotEvent{Kind: LotEventAmended, ActorID: op.ActorID, Reason: strings.TrimSpace(reason), Qty: text})
	return nil
}
