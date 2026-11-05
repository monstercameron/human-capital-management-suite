// Package inventory owns SMB consumable-stock truth: one domain-owned
// movement journal and guarded balance projections. Item identity,
// conversions, locations, lots, and imports stay with their sibling
// owners in internal/domains/stock; this package posts exact base-unit
// quantities, derives every balance from committed movements, and never
// edits a balance in place.
//
// The journal is hermetic: no storage, clocks, or network. Restarts
// restore through Export/Import snapshots; durable tables, RLS, and
// served transport stay with the owning store and application layers.
package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/stock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrMovementInvalid      = errors.New("inventory: movement request is invalid")
	ErrMovementNotFound     = errors.New("inventory: stock record is not visible")
	ErrItemNotPostable      = errors.New("inventory: item does not accept postings")
	ErrInsufficientStock    = errors.New("inventory: insufficient stock")
	ErrStaleItemRevision    = errors.New("inventory: item revision moved under this writer")
	ErrOperationConflict    = errors.New("inventory: operation key was reused for another movement")
	ErrInvalidUnit          = errors.New("inventory: quantity is not exact in the item base unit")
	ErrSerialNotReady       = errors.New("inventory: FEATURE_DEPENDENCY_NOT_READY")
	ErrSerialRefused        = errors.New("inventory: serialized unit is not available")
	ErrSerialConflict       = errors.New("inventory: serial is already recorded")
	ErrCrossCompany         = errors.New("inventory: cross-company movements are refused")
	ErrAllocationInvalid    = errors.New("inventory: allocation fence is not eligible")
	ErrMovementResponseLost = errors.New("inventory: movement recorded but response lost")
	ErrSnapshotInvalid      = errors.New("inventory: snapshot cannot be restored")
	ErrRebuildDiverged      = errors.New("inventory: rebuild diverged from live balances")
)

// CapabilitySerialized gates serialized-equipment posting until the asset
// owner supplies canonical namespace/catalog links (SMB-ASSET-001).
const CapabilitySerialized = "stock.serialized"

// MovementKind declares one journal event type.
type MovementKind string

const (
	MovementReceive  MovementKind = "RECEIVE"
	MovementIssue    MovementKind = "ISSUE"
	MovementAdjust   MovementKind = "ADJUST"
	MovementTransfer MovementKind = "TRANSFER"
	MovementReturn   MovementKind = "RETURN"
	MovementReversal MovementKind = "REVERSAL"
)

func (k MovementKind) valid() bool {
	switch k {
	case MovementReceive, MovementIssue, MovementAdjust, MovementTransfer, MovementReturn, MovementReversal:
		return true
	default:
		return false
	}
}

// Stock buckets. Usable and quarantine sum to on-hand; transit, lost, and
// disposed are disjoint outside it.
type Bucket string

const (
	BucketUsable     Bucket = "usable"
	BucketQuarantine Bucket = "quarantine"
	BucketInTransit  Bucket = "in_transit"
	BucketLost       Bucket = "lost"
	BucketDisposed   Bucket = "disposed"
)

func (b Bucket) valid() bool {
	switch b {
	case BucketUsable, BucketQuarantine, BucketInTransit, BucketLost, BucketDisposed:
		return true
	default:
		return false
	}
}

// SerialLine names one physical serialized unit in a movement.
type SerialLine struct {
	Namespace string
	Serial    string
}

// SerialStanding is the asset owner's current word on one serial: whether
// it maps to a registered asset, whether it is assigned out, and whether
// it is serviceable.
type SerialStanding struct {
	Mapped      bool
	Assigned    bool
	Serviceable bool
}

// AssetBinder is the SMB-ASSET-001 seam: serialized posting binds exact
// asset standing per serial. A nil binder refuses serialized posting;
// generic stock commands never alter equipment availability directly.
type AssetBinder interface {
	InspectSerial(ctx context.Context, op stock.OperationContext, namespace, serial string) (SerialStanding, error)
}

// LocationResolver validates posting locations against the STOCK-003
// owner. A nil resolver skips location validation.
type LocationResolver interface {
	GetLocation(op stock.OperationContext, id string) (stock.StockLocation, error)
}

// AllocationReader is the SMB-STOCK-005 seam: unmet committed demand for
// ATP math plus per-allocation fences for reserved issues. A nil reader
// reports no demand.
type AllocationReader interface {
	ActiveQuantity(ctx context.Context, op stock.OperationContext, itemID, siteID string) (string, error)
	Cover(ctx context.Context, op stock.OperationContext, ref, itemID, siteID string, qty values.Decimal) (bool, error)
}

// MovementRequest posts one journal event. QuantityText is a positive
// base-unit decimal exact at the item scale; serials are required iff the
// item tracks serialized units.
type MovementRequest struct {
	ItemID               string
	ExpectedItemRevision int
	Kind                 MovementKind
	QuantityText         string
	SiteID               string
	LocationID           string
	FromBucket           Bucket
	ToBucket             Bucket
	FromSite             string
	ToSite               string
	ToCompany            string
	Reason               string
	SourceRef            string
	OriginalMovementRef  string
	AllocationRef        string
	AllocationQtyText    string
	EffectiveAt          time.Time
	Serials              []SerialLine
	At                   time.Time
}

// Movement is one immutable journal event. Debit/Credit legs are the
// persisted posting outcome replay re-derives balances from; serial
// outcomes persist the same way so holdings never need a second source.
type Movement struct {
	MovementID          string
	OperationID         string
	Sequence            uint64
	TenantID            string
	CompanyID           string
	ItemID              string
	ItemRevision        int
	Kind                MovementKind
	QuantityText        string
	SiteID              string
	FromSite            string
	ToSite              string
	LocationID          string
	FromBucket          Bucket
	ToBucket            Bucket
	DebitSite           string
	DebitBucket         Bucket
	CreditSite          string
	CreditBucket        Bucket
	Reason              string
	SourceRef           string
	OriginalMovementRef string
	AllocationRef       string
	RecordedAt          time.Time
	EffectiveAt         time.Time
	ActorID             string
	Origin              string
	SerialKeys          []string
	SerialSite          string
	SerialBucket        Bucket
	SerialDrop          bool
}

// BalanceReport is the derived per-site position of one item.
type BalanceReport struct {
	ItemID       string
	SiteID       string
	Usable       values.Decimal
	Quarantine   values.Decimal
	InTransit    values.Decimal
	Lost         values.Decimal
	Disposed     values.Decimal
	OnHandTotal  values.Decimal
	Allocated    values.Decimal
	Available    values.Decimal
	Shortage     values.Decimal
	AsOfSequence uint64
}

// Total sums every physical bucket.
func (b BalanceReport) Total() values.Decimal {
	total, err := b.Usable.Add(b.Quarantine)
	if err != nil {
		return b.Usable
	}
	for _, bucket := range []values.Decimal{b.InTransit, b.Lost, b.Disposed} {
		total, err = total.Add(bucket)
		if err != nil {
			return total
		}
	}
	return total
}

// JournalConfig wires the movement owner to its siblings. Catalog is
// required; Locations, Allocations, and Assets are seams whose absence
// skips (locations, allocations) or gates (serialized assets) the paths
// that need them.
type JournalConfig struct {
	Locations    LocationResolver
	Allocations  AllocationReader
	Assets       AssetBinder
	Capabilities map[string]bool
}

type movementEffect struct {
	Fingerprint string `json:"fingerprint"`
	MovementID  string `json:"movement_id"`
}

type serialHolding struct {
	ItemID string
	SiteID string
	Bucket Bucket
}

// Journal is the concurrency-safe in-memory movement port. AfterCommit is
// a fault-injection seam: when non-nil it runs after a movement commits
// but before the response, so tests prove a lost response still replays
// without a duplicate posting.
type Journal struct {
	mu          sync.Mutex
	catalog     *stock.Catalog
	locations   LocationResolver
	allocations AllocationReader
	assets      AssetBinder
	caps        map[string]bool
	sequence    uint64
	movements   []Movement
	byID        map[string]int
	byOp        map[string]int
	effects     map[string]movementEffect
	balances    map[string]values.Decimal
	serials     map[string]serialHolding
	reversed    map[string]values.Decimal
	AfterCommit func() error
}

// NewJournal builds the movement port over the sibling item catalog.
func NewJournal(catalog *stock.Catalog, config JournalConfig) *Journal {
	return &Journal{
		catalog:     catalog,
		locations:   config.Locations,
		allocations: config.Allocations,
		assets:      config.Assets,
		caps:        config.Capabilities,
		byID:        make(map[string]int),
		byOp:        make(map[string]int),
		effects:     make(map[string]movementEffect),
		balances:    make(map[string]values.Decimal),
		serials:     make(map[string]serialHolding),
		reversed:    make(map[string]values.Decimal),
	}
}

// checkOp mirrors the stock OperationContext contract: every write names
// an authenticated tenant, company, actor, and origin.
func checkOp(op stock.OperationContext) error {
	if strings.TrimSpace(op.TenantID) == "" || strings.TrimSpace(op.CompanyID) == "" || strings.TrimSpace(op.ActorID) == "" {
		return fmt.Errorf("%w: tenant, company and actor are required", ErrMovementInvalid)
	}
	switch op.Origin {
	case stock.OriginHuman, stock.OriginApp, stock.OriginDevice:
		return nil
	default:
		return fmt.Errorf("%w: origin %q is not declared", ErrMovementInvalid, op.Origin)
	}
}

func balanceKey(tenant, company, site, item string, bucket Bucket) string {
	return tenant + "\x00" + company + "\x00" + site + "\x00" + item + "\x00" + string(bucket)
}

// normalizeSerial applies the default immutable serial policy: exact
// Unicode NFC with surrounding whitespace removed; case and leading zeros
// preserved.
func normalizeSerial(raw string) string {
	return norm.NFC.String(strings.TrimSpace(raw))
}

func serialKey(company, namespace, serial string) string {
	return company + "\x00" + namespace + "\x00" + serial
}

func zeroAt(scale int32) values.Decimal {
	d, err := values.NewDecimal("0", scale, values.RoundingExactRequired)
	if err != nil {
		panic(err)
	}
	return d
}

func (j *Journal) balanceLocked(tenant, company, site, item string, bucket Bucket, scale int32) values.Decimal {
	if qty, ok := j.balances[balanceKey(tenant, company, site, item, bucket)]; ok {
		return qty
	}
	return zeroAt(scale)
}

func (j *Journal) creditLocked(tenant, company, site, item string, bucket Bucket, qty values.Decimal) {
	key := balanceKey(tenant, company, site, item, bucket)
	current, ok := j.balances[key]
	if !ok {
		current = zeroAt(qty.Scale())
	}
	next, err := current.Add(qty)
	if err != nil {
		panic(err)
	}
	j.balances[key] = next
}

func (j *Journal) debitLocked(tenant, company, site, item string, bucket Bucket, qty values.Decimal) {
	key := balanceKey(tenant, company, site, item, bucket)
	current, ok := j.balances[key]
	if !ok {
		current = zeroAt(qty.Scale())
	}
	next, err := current.Sub(qty)
	if err != nil {
		panic(err)
	}
	j.balances[key] = next
}

func (j *Journal) activeAllocation(ctx context.Context, op stock.OperationContext, itemID, siteID string, scale int32) (values.Decimal, error) {
	if j.allocations == nil {
		return zeroAt(scale), nil
	}
	text, err := j.allocations.ActiveQuantity(ctx, op, itemID, siteID)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("%w: allocation demand: %v", ErrMovementInvalid, err)
	}
	qty, err := values.NewDecimal(strings.TrimSpace(text), scale, values.RoundingExactRequired)
	if err != nil {
		return values.Decimal{}, fmt.Errorf("%w: allocation demand %q: %v", ErrMovementInvalid, text, err)
	}
	if qty.Sign() < 0 {
		return values.Decimal{}, fmt.Errorf("%w: allocation demand is negative", ErrMovementInvalid)
	}
	return qty, nil
}

func fingerprintOf(op stock.OperationContext, req MovementRequest, key string) string {
	parts := []string{key, op.TenantID, op.CompanyID, req.ItemID, string(req.Kind), req.QuantityText,
		req.SiteID, req.LocationID, string(req.FromBucket), string(req.ToBucket),
		req.FromSite, req.ToSite, req.ToCompany, req.Reason, req.SourceRef,
		req.OriginalMovementRef, req.AllocationRef, req.AllocationQtyText,
		req.EffectiveAt.UTC().Format(time.RFC3339Nano), req.At.UTC().Format(time.RFC3339Nano)}
	for _, line := range req.Serials {
		parts = append(parts, line.Namespace+"\x00"+normalizeSerial(line.Serial))
	}
	return strings.Join(parts, "\x00")
}

// resolveItem returns the current catalog revision or a hidden not-found:
// foreign items and missing items read identically.
func (j *Journal) resolveItem(op stock.OperationContext, itemID string) (stock.CatalogItem, error) {
	if j.catalog == nil {
		return stock.CatalogItem{}, fmt.Errorf("%w: no item catalog is configured", ErrMovementInvalid)
	}
	history, err := j.catalog.History(op, itemID)
	if err != nil || len(history) == 0 {
		return stock.CatalogItem{}, ErrMovementNotFound
	}
	return history[len(history)-1], nil
}

// Post validates, capacity-checks, and appends one movement. Every check
// runs before anything is appended: a refused post writes zero movements.
func (j *Journal) Post(ctx context.Context, op stock.OperationContext, req MovementRequest, key string) (Movement, error) {
	if err := checkOp(op); err != nil {
		return Movement{}, err
	}
	if !req.Kind.valid() {
		return Movement{}, fmt.Errorf("%w: kind %q is not declared", ErrMovementInvalid, req.Kind)
	}
	if strings.TrimSpace(key) == "" || req.At.IsZero() {
		return Movement{}, fmt.Errorf("%w: operation key and time are required", ErrMovementInvalid)
	}
	item, err := j.resolveItem(op, req.ItemID)
	if err != nil {
		return Movement{}, err
	}
	if item.Status != stock.ItemStatusActive {
		return Movement{}, fmt.Errorf("%w: item %s is %s", ErrItemNotPostable, req.ItemID, item.Status)
	}
	if item.Revision != req.ExpectedItemRevision {
		return Movement{}, fmt.Errorf("%w: writer holds %d, item is at %d", ErrStaleItemRevision, req.ExpectedItemRevision, item.Revision)
	}
	qty, err := values.NewDecimal(strings.TrimSpace(req.QuantityText), item.Scale, values.RoundingExactRequired)
	if err != nil || qty.Sign() <= 0 {
		return Movement{}, fmt.Errorf("%w: quantity %q is not a positive base-unit amount", ErrInvalidUnit, req.QuantityText)
	}
	serialized := item.Tracking == stock.TrackingSerialized
	if serialized {
		if !j.caps[CapabilitySerialized] || j.assets == nil {
			return Movement{}, fmt.Errorf("%w: serialized posting needs asset namespace links", ErrSerialNotReady)
		}
		if _, err := values.NewDecimal(strings.TrimSpace(req.QuantityText), 0, values.RoundingExactRequired); err != nil {
			return Movement{}, fmt.Errorf("%w: serialized quantity %q is not whole units", ErrInvalidUnit, req.QuantityText)
		}
	}
	effectiveAt := req.EffectiveAt
	if effectiveAt.IsZero() {
		effectiveAt = req.At
	}
	if err := j.validateRoute(op, req, serialized); err != nil {
		return Movement{}, err
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	fingerprint := fingerprintOf(op, req, key)
	if prior, ok := j.effects[key]; ok {
		if prior.Fingerprint != fingerprint {
			return Movement{}, fmt.Errorf("%w: key %q moved different goods", ErrOperationConflict, key)
		}
		return j.movements[j.byOp[key]], nil
	}
	planned, err := j.planLocked(ctx, op, req, item, qty)
	if err != nil {
		return Movement{}, err
	}
	j.sequence++
	movement := Movement{
		MovementID: uuid.NewString(), OperationID: key, Sequence: j.sequence,
		TenantID: op.TenantID, CompanyID: op.CompanyID,
		ItemID: req.ItemID, ItemRevision: item.Revision, Kind: req.Kind,
		QuantityText: qty.String(), SiteID: req.SiteID,
		FromSite: req.FromSite, ToSite: req.ToSite, LocationID: req.LocationID,
		FromBucket: req.FromBucket, ToBucket: req.ToBucket,
		DebitSite: planned.debitSite, DebitBucket: planned.debitBucket,
		CreditSite: planned.creditSite, CreditBucket: planned.creditBucket,
		Reason: req.Reason, SourceRef: req.SourceRef,
		OriginalMovementRef: req.OriginalMovementRef, AllocationRef: req.AllocationRef,
		RecordedAt: req.At, EffectiveAt: effectiveAt, ActorID: op.ActorID, Origin: op.Origin,
		SerialKeys: planned.serialKeys, SerialSite: planned.serialSite,
		SerialBucket: planned.serialBucket, SerialDrop: planned.serialDrop,
	}
	j.applyLocked(op, req, item, qty, planned)
	j.movements = append(j.movements, movement)
	j.byID[movement.MovementID] = len(j.movements) - 1
	j.byOp[key] = len(j.movements) - 1
	j.effects[key] = movementEffect{Fingerprint: fingerprint, MovementID: movement.MovementID}
	_ = j.catalog.MarkMovementsSeen(op.TenantID, op.CompanyID, req.ItemID)
	if j.AfterCommit != nil {
		if crash := j.AfterCommit(); crash != nil {
			return Movement{}, fmt.Errorf("%w: %w", ErrMovementResponseLost, crash)
		}
	}
	return movement, nil
}

func (j *Journal) validateRoute(op stock.OperationContext, req MovementRequest, serialized bool) error {
	switch req.Kind {
	case MovementReceive, MovementReturn, MovementIssue:
		if strings.TrimSpace(req.SiteID) == "" {
			return fmt.Errorf("%w: site is required", ErrMovementInvalid)
		}
	case MovementAdjust:
		if strings.TrimSpace(req.SiteID) == "" {
			return fmt.Errorf("%w: site is required", ErrMovementInvalid)
		}
		if !req.FromBucket.valid() || !req.ToBucket.valid() || req.FromBucket == req.ToBucket {
			return fmt.Errorf("%w: adjust needs two distinct valid buckets", ErrMovementInvalid)
		}
		if strings.TrimSpace(req.Reason) == "" {
			return fmt.Errorf("%w: adjust needs a reason", ErrMovementInvalid)
		}
	case MovementTransfer:
		if strings.TrimSpace(req.FromSite) == "" || strings.TrimSpace(req.ToSite) == "" || req.FromSite == req.ToSite {
			return fmt.Errorf("%w: transfer needs two distinct sites", ErrMovementInvalid)
		}
		if strings.TrimSpace(req.SiteID) != "" {
			return fmt.Errorf("%w: transfer uses from/to sites, not a posting site", ErrMovementInvalid)
		}
		if req.ToCompany != "" && req.ToCompany != op.CompanyID {
			return fmt.Errorf("%w: %s is outside company %s", ErrCrossCompany, req.ToCompany, op.CompanyID)
		}
	case MovementReversal:
		if strings.TrimSpace(req.OriginalMovementRef) == "" {
			return fmt.Errorf("%w: reversal needs its original movement", ErrMovementInvalid)
		}
		if strings.TrimSpace(req.SiteID) == "" && strings.TrimSpace(req.FromSite) == "" {
			return fmt.Errorf("%w: site is required", ErrMovementInvalid)
		}
	}
	if req.LocationID != "" && j.locations != nil {
		if _, err := j.locations.GetLocation(op, req.LocationID); err != nil {
			return ErrMovementNotFound
		}
	}
	if serialized && len(req.Serials) == 0 {
		return fmt.Errorf("%w: serialized posting needs its unit lines", ErrMovementInvalid)
	}
	if !serialized && len(req.Serials) > 0 {
		return fmt.Errorf("%w: consumable posting takes no serials", ErrMovementInvalid)
	}
	return nil
}

type movementPlan struct {
	debitSite    string
	debitBucket  Bucket
	debitGateATP bool
	creditSite   string
	creditBucket Bucket
	serialKeys   []string
	serialSite   string
	serialBucket Bucket
	serialDrop   bool
	reverseOf    string
}

func (j *Journal) planLocked(ctx context.Context, op stock.OperationContext, req MovementRequest, item stock.CatalogItem, qty values.Decimal) (movementPlan, error) {
	var plan movementPlan
	switch req.Kind {
	case MovementReceive, MovementReturn:
		plan.creditSite, plan.creditBucket = req.SiteID, BucketUsable
	case MovementIssue:
		plan.debitSite, plan.debitBucket, plan.debitGateATP = req.SiteID, BucketUsable, true
		if req.AllocationRef != "" {
			if err := j.checkAllocationFence(ctx, op, req, item, qty); err != nil {
				return movementPlan{}, err
			}
			plan.debitGateATP = false
		}
	case MovementAdjust:
		plan.debitSite, plan.debitBucket = req.SiteID, req.FromBucket
		plan.creditSite, plan.creditBucket = req.SiteID, req.ToBucket
	case MovementTransfer:
		plan.debitSite, plan.debitBucket, plan.debitGateATP = req.FromSite, BucketUsable, true
		plan.creditSite, plan.creditBucket = req.ToSite, BucketUsable
	case MovementReversal:
		legs, reverseOf, err := j.reversalLegsLocked(op, req, item, qty)
		if err != nil {
			return movementPlan{}, err
		}
		plan.debitSite, plan.debitBucket = legs.debitSite, legs.debitBucket
		plan.creditSite, plan.creditBucket = legs.creditSite, legs.creditBucket
		plan.reverseOf = reverseOf
	}
	if item.Tracking == stock.TrackingSerialized {
		if err := j.planSerialsLocked(ctx, op, req, item, qty, &plan); err != nil {
			return movementPlan{}, err
		}
	}
	if err := j.checkCapacityLocked(ctx, op, req, item, qty, plan); err != nil {
		return movementPlan{}, err
	}
	return plan, nil
}

type reversalLegs struct {
	debitSite    string
	debitBucket  Bucket
	creditSite   string
	creditBucket Bucket
}

// reversalLegsLocked resolves the compensating legs for a reversal. Chains
// flatten to the first non-reversal movement so a reversal of a reversal
// replays the nested direction; quantity accounting always binds the
// directly referenced movement.
func (j *Journal) reversalLegsLocked(op stock.OperationContext, req MovementRequest, item stock.CatalogItem, qty values.Decimal) (reversalLegs, string, error) {
	idx, ok := j.byID[req.OriginalMovementRef]
	if !ok {
		return reversalLegs{}, "", ErrMovementNotFound
	}
	original := j.movements[idx]
	if original.TenantID != op.TenantID || original.CompanyID != op.CompanyID || original.ItemID != req.ItemID {
		return reversalLegs{}, "", ErrMovementNotFound
	}
	originalQty, err := values.NewDecimal(original.QuantityText, item.Scale, values.RoundingExactRequired)
	if err != nil {
		return reversalLegs{}, "", fmt.Errorf("%w: original quantity: %v", ErrMovementInvalid, err)
	}
	already := j.reversed[original.MovementID]
	if _, ok := j.reversed[original.MovementID]; !ok {
		already = zeroAt(item.Scale)
	}
	remaining, err := originalQty.Sub(already)
	if err != nil {
		return reversalLegs{}, "", fmt.Errorf("%w: reversal accounting: %v", ErrMovementInvalid, err)
	}
	if qty.Cmp(remaining) > 0 {
		return reversalLegs{}, "", fmt.Errorf("%w: only %s of %s remains reversible", ErrInsufficientStock, remaining, originalQty)
	}
	base := original
	for base.Kind == MovementReversal {
		nested, ok := j.byID[base.OriginalMovementRef]
		if !ok {
			return reversalLegs{}, "", fmt.Errorf("%w: reversal chain is broken", ErrMovementInvalid)
		}
		base = j.movements[nested]
	}
	var legs reversalLegs
	switch base.Kind {
	case MovementReceive, MovementReturn:
		legs.debitSite, legs.debitBucket = base.CreditSite, base.CreditBucket
	case MovementIssue:
		legs.creditSite, legs.creditBucket = base.DebitSite, base.DebitBucket
	case MovementAdjust:
		legs.debitSite, legs.debitBucket = base.CreditSite, base.CreditBucket
		legs.creditSite, legs.creditBucket = base.DebitSite, base.DebitBucket
	case MovementTransfer:
		legs.debitSite, legs.debitBucket = base.CreditSite, base.CreditBucket
		legs.creditSite, legs.creditBucket = base.DebitSite, base.DebitBucket
	}
	return legs, original.MovementID, nil
}

func (j *Journal) checkAllocationFence(ctx context.Context, op stock.OperationContext, req MovementRequest, item stock.CatalogItem, qty values.Decimal) error {
	if j.allocations == nil {
		return fmt.Errorf("%w: no allocation owner is configured", ErrAllocationInvalid)
	}
	allocQty, err := values.NewDecimal(strings.TrimSpace(req.AllocationQtyText), item.Scale, values.RoundingExactRequired)
	if err != nil || allocQty.Sign() <= 0 {
		return fmt.Errorf("%w: allocation quantity %q is not positive", ErrAllocationInvalid, req.AllocationQtyText)
	}
	if qty.Cmp(allocQty) > 0 {
		return fmt.Errorf("%w: issue exceeds its allocation fence", ErrAllocationInvalid)
	}
	cover, err := j.allocations.Cover(ctx, op, req.AllocationRef, req.ItemID, req.SiteID, qty)
	if err != nil {
		return fmt.Errorf("%w: allocation fence: %v", ErrAllocationInvalid, err)
	}
	if !cover {
		return fmt.Errorf("%w: allocation %q is not eligible", ErrAllocationInvalid, req.AllocationRef)
	}
	return nil
}

func (j *Journal) planSerialsLocked(ctx context.Context, op stock.OperationContext, req MovementRequest, item stock.CatalogItem, qty values.Decimal, plan *movementPlan) error {
	count, err := strconv.Atoi(strings.TrimSpace(req.QuantityText))
	if err != nil || count <= 0 || fmt.Sprintf("%d", count) != strings.TrimSpace(req.QuantityText) {
		return fmt.Errorf("%w: serialized quantity %q is not whole units", ErrInvalidUnit, req.QuantityText)
	}
	if len(req.Serials) != count {
		return fmt.Errorf("%w: %d serials for quantity %d", ErrMovementInvalid, len(req.Serials), count)
	}
	seen := make(map[string]bool, len(req.Serials))
	keys := make([]string, 0, len(req.Serials))
	for _, line := range req.Serials {
		namespace := strings.TrimSpace(line.Namespace)
		serial := normalizeSerial(line.Serial)
		if namespace == "" || serial == "" {
			return fmt.Errorf("%w: serial namespace and code are required", ErrMovementInvalid)
		}
		key := serialKey(op.CompanyID, namespace, serial)
		if seen[key] {
			return fmt.Errorf("%w: %s/%s is listed twice", ErrSerialConflict, namespace, serial)
		}
		seen[key] = true
		standing, err := j.assets.InspectSerial(ctx, op, namespace, serial)
		if err != nil {
			return fmt.Errorf("%w: asset standing: %v", ErrSerialRefused, err)
		}
		if err := checkSerialStanding(req.Kind, namespace, serial, standing); err != nil {
			return err
		}
		holding, held := j.serials[key]
		switch req.Kind {
		case MovementReceive, MovementReturn:
			if held {
				return fmt.Errorf("%w: %s/%s is already in stock", ErrSerialConflict, namespace, serial)
			}
			plan.serialSite, plan.serialBucket = req.SiteID, BucketUsable
		case MovementIssue:
			if !held || holding.ItemID != req.ItemID || holding.SiteID != req.SiteID || holding.Bucket != BucketUsable {
				return fmt.Errorf("%w: %s/%s is not usable here", ErrSerialRefused, namespace, serial)
			}
			plan.serialDrop = true
		case MovementAdjust:
			if !held || holding.ItemID != req.ItemID || holding.SiteID != req.SiteID || holding.Bucket != req.FromBucket {
				return fmt.Errorf("%w: %s/%s is not in %s", ErrSerialRefused, namespace, serial, req.FromBucket)
			}
			plan.serialSite, plan.serialBucket = req.SiteID, req.ToBucket
		case MovementTransfer:
			if !held || holding.ItemID != req.ItemID || holding.SiteID != req.FromSite || holding.Bucket != BucketUsable {
				return fmt.Errorf("%w: %s/%s is not usable at %s", ErrSerialRefused, namespace, serial, req.FromSite)
			}
			plan.serialSite, plan.serialBucket = req.ToSite, BucketUsable
		case MovementReversal:
			if err := j.planSerialReversalLocked(req, key, namespace, serial, held, holding, plan); err != nil {
				return err
			}
		}
		keys = append(keys, key)
	}
	plan.serialKeys = keys
	_ = qty
	_ = item
	return nil
}

// planSerialReversalLocked mirrors the original serial transition: credit
// reversals drop the units, debit reversals restore them.
func (j *Journal) planSerialReversalLocked(req MovementRequest, key, namespace, serial string, held bool, holding serialHolding, plan *movementPlan) error {
	original := j.movements[j.byID[req.OriginalMovementRef]]
	base := original
	for base.Kind == MovementReversal {
		nested, ok := j.byID[base.OriginalMovementRef]
		if !ok {
			return fmt.Errorf("%w: reversal chain is broken", ErrMovementInvalid)
		}
		base = j.movements[nested]
	}
	switch base.Kind {
	case MovementReceive, MovementReturn:
		if !held || holding.ItemID != req.ItemID || holding.Bucket != BucketUsable {
			return fmt.Errorf("%w: %s/%s is not available to take back", ErrSerialRefused, namespace, serial)
		}
		plan.serialDrop = true
	case MovementIssue:
		if held {
			return fmt.Errorf("%w: %s/%s is already in stock", ErrSerialConflict, namespace, serial)
		}
		plan.serialSite, plan.serialBucket = base.DebitSite, base.DebitBucket
	case MovementAdjust:
		if !held || holding.Bucket != base.CreditBucket {
			return fmt.Errorf("%w: %s/%s is not in %s", ErrSerialRefused, namespace, serial, base.CreditBucket)
		}
		plan.serialSite, plan.serialBucket = base.DebitSite, base.DebitBucket
	case MovementTransfer:
		if !held || holding.SiteID != base.CreditSite {
			return fmt.Errorf("%w: %s/%s is not at %s", ErrSerialRefused, namespace, serial, base.CreditSite)
		}
		plan.serialSite, plan.serialBucket = base.DebitSite, base.DebitBucket
	}
	return nil
}

func checkSerialStanding(kind MovementKind, namespace, serial string, standing SerialStanding) error {
	if !standing.Mapped {
		return fmt.Errorf("%w: %s/%s is not mapped to an asset", ErrSerialRefused, namespace, serial)
	}
	switch kind {
	case MovementReceive, MovementReturn, MovementIssue:
		if standing.Assigned {
			return fmt.Errorf("%w: %s/%s is assigned out", ErrSerialRefused, namespace, serial)
		}
		if !standing.Serviceable {
			return fmt.Errorf("%w: %s/%s is out of service", ErrSerialRefused, namespace, serial)
		}
	}
	return nil
}

func (j *Journal) checkCapacityLocked(ctx context.Context, op stock.OperationContext, req MovementRequest, item stock.CatalogItem, qty values.Decimal, plan movementPlan) error {
	if plan.debitBucket == "" {
		return nil
	}
	balance := j.balanceLocked(op.TenantID, op.CompanyID, plan.debitSite, req.ItemID, plan.debitBucket, item.Scale)
	if qty.Cmp(balance) > 0 {
		return fmt.Errorf("%w: %s holds %s, %s requested", ErrInsufficientStock, plan.debitBucket, balance, qty)
	}
	if plan.debitGateATP && plan.debitBucket == BucketUsable {
		allocated, err := j.activeAllocation(ctx, op, req.ItemID, plan.debitSite, item.Scale)
		if err != nil {
			return err
		}
		free, err := balance.Sub(allocated)
		if err != nil {
			return fmt.Errorf("%w: ATP math: %v", ErrMovementInvalid, err)
		}
		if qty.Cmp(free) > 0 {
			return fmt.Errorf("%w: available to promise is %s, %s requested", ErrInsufficientStock, free, qty)
		}
	}
	return nil
}

func (j *Journal) applyLocked(op stock.OperationContext, req MovementRequest, item stock.CatalogItem, qty values.Decimal, plan movementPlan) {
	if plan.debitBucket != "" {
		j.debitLocked(op.TenantID, op.CompanyID, plan.debitSite, req.ItemID, plan.debitBucket, qty)
	}
	if plan.creditBucket != "" {
		j.creditLocked(op.TenantID, op.CompanyID, plan.creditSite, req.ItemID, plan.creditBucket, qty)
	}
	for _, key := range plan.serialKeys {
		if plan.serialDrop {
			delete(j.serials, key)
		} else {
			j.serials[key] = serialHolding{ItemID: req.ItemID, SiteID: plan.serialSite, Bucket: plan.serialBucket}
		}
	}
	if plan.reverseOf != "" {
		already, ok := j.reversed[plan.reverseOf]
		if !ok {
			already = zeroAt(item.Scale)
		}
		next, err := already.Add(qty)
		if err != nil {
			panic(err)
		}
		j.reversed[plan.reverseOf] = next
	}
}

// ReadBalance derives the per-site position from committed movements plus
// live allocation demand. Missing buckets read as exact zero.
func (j *Journal) ReadBalance(ctx context.Context, op stock.OperationContext, itemID, siteID string) (BalanceReport, error) {
	if err := checkOp(op); err != nil {
		return BalanceReport{}, err
	}
	item, err := j.resolveItem(op, itemID)
	if err != nil {
		return BalanceReport{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	report := BalanceReport{
		ItemID:       itemID,
		SiteID:       siteID,
		Usable:       j.balanceLocked(op.TenantID, op.CompanyID, siteID, itemID, BucketUsable, item.Scale),
		Quarantine:   j.balanceLocked(op.TenantID, op.CompanyID, siteID, itemID, BucketQuarantine, item.Scale),
		InTransit:    j.balanceLocked(op.TenantID, op.CompanyID, siteID, itemID, BucketInTransit, item.Scale),
		Lost:         j.balanceLocked(op.TenantID, op.CompanyID, siteID, itemID, BucketLost, item.Scale),
		Disposed:     j.balanceLocked(op.TenantID, op.CompanyID, siteID, itemID, BucketDisposed, item.Scale),
		AsOfSequence: j.sequence,
	}
	total, err := report.Usable.Add(report.Quarantine)
	if err != nil {
		return BalanceReport{}, err
	}
	report.OnHandTotal = total
	allocated, err := j.activeAllocation(ctx, op, itemID, siteID, item.Scale)
	if err != nil {
		return BalanceReport{}, err
	}
	report.Allocated = allocated
	free, err := report.Usable.Sub(allocated)
	if err != nil {
		return BalanceReport{}, err
	}
	if free.Sign() < 0 {
		report.Available = zeroAt(item.Scale)
		shortage, err := allocated.Sub(report.Usable)
		if err != nil {
			return BalanceReport{}, err
		}
		report.Shortage = shortage
	} else {
		report.Available = free
		report.Shortage = zeroAt(item.Scale)
	}
	return report, nil
}

// GetMovement returns one committed movement. Foreign records read as
// not-found.
func (j *Journal) GetMovement(op stock.OperationContext, movementID string) (Movement, error) {
	if err := checkOp(op); err != nil {
		return Movement{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	idx, ok := j.byID[movementID]
	if !ok {
		return Movement{}, ErrMovementNotFound
	}
	movement := j.movements[idx]
	if movement.TenantID != op.TenantID || movement.CompanyID != op.CompanyID {
		return Movement{}, ErrMovementNotFound
	}
	return movement, nil
}

// GetOperation resolves an idempotency key to its recorded movement: the
// recovery path after a lost response.
func (j *Journal) GetOperation(_ context.Context, op stock.OperationContext, operationID string) (Movement, error) {
	if err := checkOp(op); err != nil {
		return Movement{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	idx, ok := j.byOp[operationID]
	if !ok {
		return Movement{}, ErrMovementNotFound
	}
	movement := j.movements[idx]
	if movement.TenantID != op.TenantID || movement.CompanyID != op.CompanyID {
		return Movement{}, ErrMovementNotFound
	}
	return movement, nil
}

// Rebuild replays the journal into a shadow projection, verifies it
// against the live one, and swaps only on an exact match. Balances are
// never edited; they are always re-derived.
func (j *Journal) Rebuild() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	shadow := NewJournal(j.catalog, JournalConfig{
		Locations: j.locations, Allocations: j.allocations, Assets: j.assets, Capabilities: j.caps,
	})
	for _, movement := range j.movements {
		if err := shadow.replay(movement); err != nil {
			return err
		}
	}
	if shadow.sequence != j.sequence {
		return fmt.Errorf("%w: shadow sequence %d, live %d", ErrRebuildDiverged, shadow.sequence, j.sequence)
	}
	if len(shadow.balances) != len(j.balances) {
		return fmt.Errorf("%w: shadow holds %d buckets, live %d", ErrRebuildDiverged, len(shadow.balances), len(j.balances))
	}
	for key, want := range shadow.balances {
		got, ok := j.balances[key]
		if !ok || got.Cmp(want) != 0 {
			return fmt.Errorf("%w: bucket %q", ErrRebuildDiverged, key)
		}
	}
	if len(shadow.serials) != len(j.serials) {
		return fmt.Errorf("%w: shadow holds %d serials, live %d", ErrRebuildDiverged, len(shadow.serials), len(j.serials))
	}
	for key, want := range shadow.serials {
		if got, ok := j.serials[key]; !ok || got != want {
			return fmt.Errorf("%w: serial %q", ErrRebuildDiverged, key)
		}
	}
	j.balances = shadow.balances
	j.serials = shadow.serials
	j.reversed = shadow.reversed
	return nil
}

type journalSnapshot struct {
	Sequence  uint64                    `json:"sequence"`
	Movements []Movement                `json:"movements"`
	Effects   map[string]movementEffect `json:"effects"`
}

// Export captures the journal for restart recovery. Balances are
// deliberately excluded: Import re-derives them by replay, proving the
// journal alone is the truth.
func (j *Journal) Export() ([]byte, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	snapshot := journalSnapshot{
		Sequence:  j.sequence,
		Movements: append([]Movement(nil), j.movements...),
		Effects:   make(map[string]movementEffect, len(j.effects)),
	}
	for key, effect := range j.effects {
		snapshot.Effects[key] = effect
	}
	return json.Marshal(snapshot)
}

// Import restores a snapshot previously captured by Export. The sequence
// chain is revalidated and every balance re-derived by replay, so corrupt
// snapshots fail closed.
func (j *Journal) Import(data []byte) error {
	var snapshot journalSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("%w: snapshot is not valid journal state: %v", ErrSnapshotInvalid, err)
	}
	if uint64(len(snapshot.Movements)) != snapshot.Sequence {
		return fmt.Errorf("%w: sequence head does not match its chain", ErrSnapshotInvalid)
	}
	for i, movement := range snapshot.Movements {
		if movement.Sequence != uint64(i+1) || movement.MovementID == "" || movement.OperationID == "" {
			return fmt.Errorf("%w: movement chain is broken", ErrSnapshotInvalid)
		}
	}
	for key, effect := range snapshot.Effects {
		if key == "" || effect.Fingerprint == "" || effect.MovementID == "" {
			return fmt.Errorf("%w: operation record is incomplete", ErrSnapshotInvalid)
		}
	}
	rebuilt := NewJournal(j.catalog, JournalConfig{
		Locations: j.locations, Allocations: j.allocations, Assets: j.assets, Capabilities: j.caps,
	})
	for _, movement := range snapshot.Movements {
		if err := rebuilt.replay(movement); err != nil {
			return fmt.Errorf("%w: %v", ErrSnapshotInvalid, err)
		}
	}
	for key, effect := range snapshot.Effects {
		rebuilt.effects[key] = effect
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.sequence = rebuilt.sequence
	j.movements = rebuilt.movements
	j.byID = rebuilt.byID
	j.byOp = rebuilt.byOp
	j.effects = rebuilt.effects
	j.balances = rebuilt.balances
	j.serials = rebuilt.serials
	j.reversed = rebuilt.reversed
	return nil
}

// replay appends one exported movement by re-deriving its persisted legs.
// The snapshot was valid when captured, and Import revalidates the chain
// around it.
func (j *Journal) replay(movement Movement) error {
	op := stock.OperationContext{TenantID: movement.TenantID, CompanyID: movement.CompanyID, ActorID: movement.ActorID, Origin: movement.Origin}
	item, err := j.resolveItem(op, movement.ItemID)
	if err != nil {
		return err
	}
	qty, err := values.NewDecimal(movement.QuantityText, item.Scale, values.RoundingExactRequired)
	if err != nil {
		return err
	}
	j.sequence++
	if j.sequence != movement.Sequence {
		return fmt.Errorf("sequence %d follows %d", movement.Sequence, j.sequence)
	}
	if movement.DebitBucket != "" {
		j.debitLocked(op.TenantID, op.CompanyID, movement.DebitSite, movement.ItemID, movement.DebitBucket, qty)
	}
	if movement.CreditBucket != "" {
		j.creditLocked(op.TenantID, op.CompanyID, movement.CreditSite, movement.ItemID, movement.CreditBucket, qty)
	}
	for _, key := range movement.SerialKeys {
		if movement.SerialDrop {
			delete(j.serials, key)
		} else {
			j.serials[key] = serialHolding{ItemID: movement.ItemID, SiteID: movement.SerialSite, Bucket: movement.SerialBucket}
		}
	}
	if movement.OriginalMovementRef != "" {
		already, ok := j.reversed[movement.OriginalMovementRef]
		if !ok {
			already = zeroAt(item.Scale)
		}
		next, err := already.Add(qty)
		if err != nil {
			return err
		}
		j.reversed[movement.OriginalMovementRef] = next
	}
	j.movements = append(j.movements, movement)
	j.byID[movement.MovementID] = len(j.movements) - 1
	j.byOp[movement.OperationID] = len(j.movements) - 1
	return nil
}

// MovementRefusalTitle maps a posting refusal to UI-safe guidance.
func MovementRefusalTitle(err error) string {
	switch {
	case errors.Is(err, ErrInsufficientStock):
		return "Not enough stock is available."
	case errors.Is(err, ErrStaleItemRevision):
		return "Item changed under you; refetch and retry."
	case errors.Is(err, ErrOperationConflict):
		return "Operation key was reused for another movement."
	case errors.Is(err, ErrInvalidUnit):
		return "Quantity is not exact in the item unit."
	case errors.Is(err, ErrSerialNotReady):
		return "Serialized stock is not enabled yet."
	case errors.Is(err, ErrSerialRefused):
		return "Serialized unit is not available."
	case errors.Is(err, ErrSerialConflict):
		return "Serial is already recorded."
	case errors.Is(err, ErrItemNotPostable):
		return "Item does not accept postings."
	case errors.Is(err, ErrCrossCompany):
		return "Stock never moves across companies."
	case errors.Is(err, ErrAllocationInvalid):
		return "Allocation does not cover this issue."
	case errors.Is(err, ErrMovementNotFound):
		return "Stock record is not available."
	default:
		return "Movement was refused."
	}
}
