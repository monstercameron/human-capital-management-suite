// Package stock owns the SMB inventory catalog: versioned items, exact
// pack conversions and barcode aliases. Movement quantities and postings
// belong to SMB-STOCK-001; this package exposes the identity and conversion
// contracts postings build on, and freezes them once movements exist.
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

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrStockContext        = errors.New("stock: operation context is incomplete or unauthenticated")
	ErrStockScope          = errors.New("stock: record is outside the authorized scope")
	ErrStockValidation     = errors.New("stock: catalog draft is invalid")
	ErrDuplicateSKU        = errors.New("stock: sku is already active")
	ErrItemNotFound        = errors.New("stock: item not found")
	ErrItemNotEditable     = errors.New("stock: retired items accept no revisions")
	ErrScaleFrozen         = errors.New("stock: base unit, scale or tracking is frozen after movement history")
	ErrAliasConflict       = errors.New("stock: barcode alias is already registered")
	ErrAmbiguousAlias      = errors.New("stock: AMBIGUOUS_ALIAS")
	ErrUnknownCode         = errors.New("stock: UNKNOWN_CODE")
	ErrUnknownUnit         = errors.New("stock: unit is not defined for this item")
	ErrInexactConversion   = errors.New("stock: INEXACT_CONVERSION")
	ErrOpenReservations    = errors.New("stock: open reservations block retirement")
	ErrIdempotencyConflict = errors.New("stock: idempotency key was reused with a different payload")
	ErrSnapshot            = errors.New("stock: snapshot cannot be restored")
)

// Operation origins. Every write names its origin; barcode beeps, toasts and
// websocket pushes never authenticate one.
const (
	OriginHuman  = "human"
	OriginApp    = "app"
	OriginDevice = "device"
)

// OperationContext authenticates the caller and scopes the call to one
// tenant and company. SiteID is an optional read scope: when set, list,
// resolve and preview only see items visible at that site.
type OperationContext struct {
	TenantID  string
	CompanyID string
	SiteID    string
	ActorID   string
	Origin    string
}

func (op OperationContext) validate() error {
	if strings.TrimSpace(op.TenantID) == "" || strings.TrimSpace(op.CompanyID) == "" || strings.TrimSpace(op.ActorID) == "" {
		return fmt.Errorf("%w: tenant, company and actor are required", ErrStockContext)
	}
	switch op.Origin {
	case OriginHuman, OriginApp, OriginDevice:
		return nil
	default:
		return fmt.Errorf("%w: origin %q is not declared", ErrStockContext, op.Origin)
	}
}

// ItemTracking names how base-unit quantities are counted. Each and
// serialized items count whole units only; mass items use the declared scale.
type ItemTracking string

const (
	TrackingEach       ItemTracking = "EACH"
	TrackingSerialized ItemTracking = "SERIALIZED"
	TrackingMass       ItemTracking = "MASS"
)

func (t ItemTracking) valid() bool {
	return t == TrackingEach || t == TrackingSerialized || t == TrackingMass
}

// ItemStatus is the catalog lifecycle. Archived items leave pickers but stay
// readable; retired items are terminal.
type ItemStatus string

const (
	ItemStatusActive   ItemStatus = "ACTIVE"
	ItemStatusArchived ItemStatus = "ARCHIVED"
	ItemStatusRetired  ItemStatus = "RETIRED"
)

// PackConversion names one exact pack unit: one PackUnit is exactly
// Numerator/Denominator base units. Both parts are positive integers from the
// item's fixed base dimension; nothing here rounds.
type PackConversion struct {
	PackUnit    string
	Numerator   int64
	Denominator int64
}

// BarcodeAlias binds one opaque code to an item revision. Codes are opaque
// identifiers, never URLs or scripts: normalization trims whitespace only and
// preserves leading zeros. PackUnit names the unit the code scans as; empty
// means the item base unit.
type BarcodeAlias struct {
	Symbology string
	Code      string
	PackUnit  string
}

// CatalogItem is one immutable revision snapshot. History entries are never
// edited in place: a revision keeps the unit display, scale and tracking the
// business saw when it was current.
type CatalogItem struct {
	TenantID      string
	CompanyID     string
	ItemID        string
	Revision      int
	SKU           string
	Name          string
	BaseUnit      string
	Scale         int32
	Tracking      ItemTracking
	Status        ItemStatus
	Sites         []string
	Conversions   []PackConversion
	Aliases       []BarcodeAlias
	MovementsSeen bool
	UpdatedAt     time.Time
}

// ItemDraft is the create/revise payload. SKU is identity and immutable;
// revisions carry the full conversion and alias tables.
type ItemDraft struct {
	SKU         string
	Name        string
	BaseUnit    string
	Scale       int32
	Tracking    ItemTracking
	Sites       []string
	Conversions []PackConversion
	Aliases     []BarcodeAlias
}

func (d ItemDraft) validate() (ItemDraft, error) {
	out := ItemDraft{
		SKU: strings.TrimSpace(d.SKU), Name: strings.TrimSpace(d.Name), BaseUnit: strings.TrimSpace(d.BaseUnit),
		Scale: d.Scale, Tracking: d.Tracking,
	}
	if out.SKU == "" || out.Name == "" || out.BaseUnit == "" {
		return ItemDraft{}, fmt.Errorf("%w: sku, name and base unit are required", ErrStockValidation)
	}
	if out.Scale < 0 || out.Scale > values.MaxScale {
		return ItemDraft{}, fmt.Errorf("%w: scale %d is outside [0,%d]", ErrStockValidation, out.Scale, values.MaxScale)
	}
	if !out.Tracking.valid() {
		return ItemDraft{}, fmt.Errorf("%w: tracking %q is not declared", ErrStockValidation, out.Tracking)
	}
	seenPacks := map[string]bool{out.BaseUnit: true}
	for _, c := range d.Conversions {
		unit := strings.TrimSpace(c.PackUnit)
		if unit == "" || c.Numerator <= 0 || c.Denominator <= 0 {
			return ItemDraft{}, fmt.Errorf("%w: conversion %q needs a unit and positive integer numerator/denominator", ErrStockValidation, c.PackUnit)
		}
		if seenPacks[unit] {
			return ItemDraft{}, fmt.Errorf("%w: unit %q is defined twice", ErrStockValidation, unit)
		}
		seenPacks[unit] = true
		out.Conversions = append(out.Conversions, PackConversion{PackUnit: unit, Numerator: c.Numerator, Denominator: c.Denominator})
	}
	seenCodes := map[string]bool{}
	for _, a := range d.Aliases {
		symbology := strings.TrimSpace(a.Symbology)
		code := strings.TrimSpace(a.Code)
		pack := strings.TrimSpace(a.PackUnit)
		if symbology == "" || code == "" {
			return ItemDraft{}, fmt.Errorf("%w: alias needs symbology and code", ErrStockValidation)
		}
		if pack != "" && !seenPacks[pack] {
			return ItemDraft{}, fmt.Errorf("%w: alias unit %q is not defined for this item", ErrStockValidation, pack)
		}
		key := symbology + "\x00" + code
		if seenCodes[key] {
			return ItemDraft{}, fmt.Errorf("%w: alias %s %q is listed twice", ErrStockValidation, symbology, code)
		}
		seenCodes[key] = true
		out.Aliases = append(out.Aliases, BarcodeAlias{Symbology: symbology, Code: code, PackUnit: pack})
	}
	for _, site := range d.Sites {
		if s := strings.TrimSpace(site); s != "" {
			out.Sites = append(out.Sites, s)
		}
	}
	return out, nil
}

// ReservationReader reports open reservations blocking retirement. It is the
// seam SMB-STOCK-005 owns; a nil reader reports none.
type ReservationReader interface {
	OpenReservations(ctx context.Context, tenantID, companyID, itemID string) (int, error)
}

// ItemFilter scopes ListItems. PickersOnly selects ACTIVE items only;
// Search matches SKU, name and alias codes case-insensitively.
type ItemFilter struct {
	Search      string
	Tracking    ItemTracking
	Status      ItemStatus
	PickersOnly bool
}

// BarcodeResolution is the single exact item+unit a scan resolves to. Status
// travels with it so command layers refuse retired items without guessing.
type BarcodeResolution struct {
	ItemID   string
	SKU      string
	Name     string
	Unit     string
	Revision int
	Status   ItemStatus
}

// ConversionPreview is the exact pre-posting display: the canonical base-unit
// quantity plus the human sentence the UI shows before Save.
type ConversionPreview struct {
	BaseQtyText string
	BaseUnit    string
	Aggregate   bool
	Display     string
}

// Catalog is the value-owned item registry. All state lives on the value;
// snapshots carry it across restarts.
type Catalog struct {
	mu           sync.Mutex
	revisions    map[string][]CatalogItem
	seen         map[string]bool
	bySKU        map[string]string
	byAlias      map[string]string
	idem         map[string]idempotencyRecord
	reservations ReservationReader
}

type idempotencyRecord struct {
	Fingerprint string
	Item        CatalogItem
}

// NewCatalog builds an empty catalog. A nil ReservationReader reports no
// open reservations.
func NewCatalog(rr ReservationReader) *Catalog {
	return &Catalog{
		revisions:    make(map[string][]CatalogItem),
		seen:         make(map[string]bool),
		bySKU:        make(map[string]string),
		byAlias:      make(map[string]string),
		idem:         make(map[string]idempotencyRecord),
		reservations: rr,
	}
}

func scopeKey(tenantID, companyID, rest string) string {
	return tenantID + "\x00" + companyID + "\x00" + rest
}

func cloneItem(in CatalogItem) CatalogItem {
	in.Sites = append([]string(nil), in.Sites...)
	in.Conversions = append([]PackConversion(nil), in.Conversions...)
	in.Aliases = append([]BarcodeAlias(nil), in.Aliases...)
	return in
}

func (c *Catalog) current(itemID string) (CatalogItem, bool) {
	history := c.revisions[itemID]
	if len(history) == 0 {
		return CatalogItem{}, false
	}
	return history[len(history)-1], true
}

func (c *Catalog) visible(item CatalogItem, op OperationContext) bool {
	if item.TenantID != op.TenantID || item.CompanyID != op.CompanyID {
		return false
	}
	if op.SiteID == "" || len(item.Sites) == 0 {
		return true
	}
	for _, site := range item.Sites {
		if site == op.SiteID {
			return true
		}
	}
	return false
}

func (c *Catalog) checkIdem(opName string, op OperationContext, key, fingerprint string) (CatalogItem, bool, error) {
	if strings.TrimSpace(key) == "" {
		return CatalogItem{}, false, nil
	}
	mapKey := scopeKey(op.TenantID, op.CompanyID, opName+"\x00"+strings.TrimSpace(key))
	prior, ok := c.idem[mapKey]
	if !ok {
		return CatalogItem{}, false, nil
	}
	if prior.Fingerprint != fingerprint {
		return CatalogItem{}, false, fmt.Errorf("%w: key %q", ErrIdempotencyConflict, key)
	}
	return cloneItem(prior.Item), true, nil
}

func (c *Catalog) storeIdem(opName string, op OperationContext, key, fingerprint string, item CatalogItem) {
	if strings.TrimSpace(key) == "" {
		return
	}
	mapKey := scopeKey(op.TenantID, op.CompanyID, opName+"\x00"+strings.TrimSpace(key))
	c.idem[mapKey] = idempotencyRecord{Fingerprint: fingerprint, Item: cloneItem(item)}
}

func fingerprint(opName string, op OperationContext, parts ...any) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%#v", opName, op.TenantID, op.CompanyID, parts)
}

// CreateItem registers the first revision. The SKU must be unique among
// active (non-retired) items of the company; aliases must be free within
// company and symbology.
func (c *Catalog) CreateItem(_ context.Context, op OperationContext, draft ItemDraft, key string) (CatalogItem, error) {
	if err := op.validate(); err != nil {
		return CatalogItem{}, err
	}
	clean, err := draft.validate()
	if err != nil {
		return CatalogItem{}, err
	}
	fp := fingerprint("create", op, clean)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("create", op, key, fp); err != nil || ok {
		return prior, err
	}
	skuKey := scopeKey(op.TenantID, op.CompanyID, "sku\x00"+clean.SKU)
	if _, exists := c.bySKU[skuKey]; exists {
		return CatalogItem{}, fmt.Errorf("%w: %q", ErrDuplicateSKU, clean.SKU)
	}
	for _, a := range clean.Aliases {
		aliasKey := scopeKey(op.TenantID, op.CompanyID, "alias\x00"+a.Symbology+"\x00"+a.Code)
		if _, exists := c.byAlias[aliasKey]; exists {
			return CatalogItem{}, fmt.Errorf("%w: %s %q", ErrAliasConflict, a.Symbology, a.Code)
		}
	}
	now := time.Now().UTC()
	item := CatalogItem{
		TenantID: op.TenantID, CompanyID: op.CompanyID, ItemID: uuid.NewString(), Revision: 1,
		SKU: clean.SKU, Name: clean.Name, BaseUnit: clean.BaseUnit, Scale: clean.Scale, Tracking: clean.Tracking,
		Status: ItemStatusActive, Sites: clean.Sites, Conversions: clean.Conversions, Aliases: clean.Aliases,
		UpdatedAt: now,
	}
	c.bySKU[skuKey] = item.ItemID
	for _, a := range clean.Aliases {
		aliasKey := scopeKey(op.TenantID, op.CompanyID, "alias\x00"+a.Symbology+"\x00"+a.Code)
		c.byAlias[aliasKey] = item.ItemID
	}
	c.revisions[item.ItemID] = []CatalogItem{cloneItem(item)}
	c.storeIdem("create", op, key, fp, item)
	return cloneItem(item), nil
}

// ReviseItem appends a revision carrying the full conversion and alias
// tables. After movement history exists, base unit, scale and tracking are
// frozen: successors are new items, never edits.
func (c *Catalog) ReviseItem(_ context.Context, op OperationContext, itemID string, draft ItemDraft, key string) (CatalogItem, error) {
	if err := op.validate(); err != nil {
		return CatalogItem{}, err
	}
	clean, err := draft.validate()
	if err != nil {
		return CatalogItem{}, err
	}
	fp := fingerprint("revise", op, itemID, clean)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("revise", op, key, fp); err != nil || ok {
		return prior, err
	}
	current, ok := c.current(itemID)
	if !ok || current.TenantID != op.TenantID || current.CompanyID != op.CompanyID {
		return CatalogItem{}, fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
	}
	if current.Status == ItemStatusRetired {
		return CatalogItem{}, fmt.Errorf("%w: %s", ErrItemNotEditable, itemID)
	}
	if clean.SKU != current.SKU {
		return CatalogItem{}, fmt.Errorf("%w: sku is immutable, want successor item", ErrStockValidation)
	}
	if c.seen[itemID] && (clean.BaseUnit != current.BaseUnit || clean.Scale != current.Scale || clean.Tracking != current.Tracking) {
		return CatalogItem{}, fmt.Errorf("%w: %s", ErrScaleFrozen, itemID)
	}
	for _, a := range clean.Aliases {
		aliasKey := scopeKey(op.TenantID, op.CompanyID, "alias\x00"+a.Symbology+"\x00"+a.Code)
		if owner, exists := c.byAlias[aliasKey]; exists && owner != itemID {
			return CatalogItem{}, fmt.Errorf("%w: %s %q", ErrAliasConflict, a.Symbology, a.Code)
		}
	}
	for _, a := range current.Aliases {
		aliasKey := scopeKey(op.TenantID, op.CompanyID, "alias\x00"+a.Symbology+"\x00"+a.Code)
		delete(c.byAlias, aliasKey)
	}
	next := cloneItem(current)
	next.Revision++
	next.Name = clean.Name
	next.BaseUnit = clean.BaseUnit
	next.Scale = clean.Scale
	next.Tracking = clean.Tracking
	next.Sites = clean.Sites
	next.Conversions = clean.Conversions
	next.Aliases = clean.Aliases
	next.MovementsSeen = c.seen[itemID]
	next.UpdatedAt = time.Now().UTC()
	for _, a := range clean.Aliases {
		aliasKey := scopeKey(op.TenantID, op.CompanyID, "alias\x00"+a.Symbology+"\x00"+a.Code)
		c.byAlias[aliasKey] = itemID
	}
	c.revisions[itemID] = append(c.revisions[itemID], cloneItem(next))
	c.storeIdem("revise", op, key, fp, next)
	return cloneItem(next), nil
}

// RegisterAlias adds one alias atomically under its own revision so parallel
// writers race on the registry instead of silently sharing a code.
func (c *Catalog) RegisterAlias(_ context.Context, op OperationContext, itemID string, alias BarcodeAlias, key string) (CatalogItem, error) {
	if err := op.validate(); err != nil {
		return CatalogItem{}, err
	}
	symbology := strings.TrimSpace(alias.Symbology)
	code := strings.TrimSpace(alias.Code)
	pack := strings.TrimSpace(alias.PackUnit)
	if symbology == "" || code == "" {
		return CatalogItem{}, fmt.Errorf("%w: alias needs symbology and code", ErrStockValidation)
	}
	clean := BarcodeAlias{Symbology: symbology, Code: code, PackUnit: pack}
	fp := fingerprint("alias", op, itemID, clean)
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem("alias", op, key, fp); err != nil || ok {
		return prior, err
	}
	current, ok := c.current(itemID)
	if !ok || current.TenantID != op.TenantID || current.CompanyID != op.CompanyID {
		return CatalogItem{}, fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
	}
	if current.Status == ItemStatusRetired {
		return CatalogItem{}, fmt.Errorf("%w: %s", ErrItemNotEditable, itemID)
	}
	if pack != "" && pack != current.BaseUnit {
		known := false
		for _, conv := range current.Conversions {
			if conv.PackUnit == pack {
				known = true
				break
			}
		}
		if !known {
			return CatalogItem{}, fmt.Errorf("%w: alias unit %q is not defined for this item", ErrStockValidation, pack)
		}
	}
	for _, a := range current.Aliases {
		if a.Symbology == symbology && a.Code == code {
			return CatalogItem{}, fmt.Errorf("%w: %s %q", ErrAliasConflict, symbology, code)
		}
	}
	aliasKey := scopeKey(op.TenantID, op.CompanyID, "alias\x00"+symbology+"\x00"+code)
	if _, exists := c.byAlias[aliasKey]; exists {
		return CatalogItem{}, fmt.Errorf("%w: %s %q", ErrAliasConflict, symbology, code)
	}
	next := cloneItem(current)
	next.Revision++
	next.Aliases = append(next.Aliases, clean)
	next.MovementsSeen = c.seen[itemID]
	next.UpdatedAt = time.Now().UTC()
	c.byAlias[aliasKey] = itemID
	c.revisions[itemID] = append(c.revisions[itemID], cloneItem(next))
	c.storeIdem("alias", op, key, fp, next)
	return cloneItem(next), nil
}

// ArchiveItem hides the item from pickers; history stays readable and the
// item stays revisable until retirement.
func (c *Catalog) ArchiveItem(_ context.Context, op OperationContext, itemID string, key string) (CatalogItem, error) {
	return c.restatus(context.Background(), op, itemID, key, "archive", ItemStatusArchived)
}

// RetireItem ends the item once no open reservations remain. Retirement
// releases the SKU for a successor but never deletes history.
func (c *Catalog) RetireItem(ctx context.Context, op OperationContext, itemID string, key string) (CatalogItem, error) {
	if err := op.validate(); err != nil {
		return CatalogItem{}, err
	}
	c.mu.Lock()
	current, ok := c.current(itemID)
	inScope := ok && current.TenantID == op.TenantID && current.CompanyID == op.CompanyID
	c.mu.Unlock()
	if !inScope {
		return CatalogItem{}, fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
	}
	if c.reservations != nil {
		open, err := c.reservations.OpenReservations(ctx, op.TenantID, op.CompanyID, itemID)
		if err != nil {
			return CatalogItem{}, err
		}
		if open > 0 {
			return CatalogItem{}, fmt.Errorf("%w: %d open on %s", ErrOpenReservations, open, itemID)
		}
	}
	return c.restatus(ctx, op, itemID, key, "retire", ItemStatusRetired)
}

func (c *Catalog) restatus(_ context.Context, op OperationContext, itemID, key, opName string, status ItemStatus) (CatalogItem, error) {
	if err := op.validate(); err != nil {
		return CatalogItem{}, err
	}
	fp := fingerprint(opName, op, itemID, string(status))
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, ok, err := c.checkIdem(opName, op, key, fp); err != nil || ok {
		return prior, err
	}
	current, ok := c.current(itemID)
	if !ok || current.TenantID != op.TenantID || current.CompanyID != op.CompanyID {
		return CatalogItem{}, fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
	}
	if current.Status == ItemStatusRetired {
		c.storeIdem(opName, op, key, fp, current)
		return cloneItem(current), nil
	}
	if current.Status == status {
		c.storeIdem(opName, op, key, fp, current)
		return cloneItem(current), nil
	}
	next := cloneItem(current)
	next.Revision++
	next.Status = status
	next.MovementsSeen = c.seen[itemID]
	next.UpdatedAt = time.Now().UTC()
	if status == ItemStatusRetired {
		delete(c.bySKU, scopeKey(op.TenantID, op.CompanyID, "sku\x00"+current.SKU))
	}
	c.revisions[itemID] = append(c.revisions[itemID], cloneItem(next))
	c.storeIdem(opName, op, key, fp, next)
	return cloneItem(next), nil
}

// MarkMovementsSeen is the SMB-STOCK-001 seam: once the movement owner
// records history for an item, its base unit, scale and tracking freeze.
func (c *Catalog) MarkMovementsSeen(tenantID, companyID, itemID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.current(itemID)
	if !ok || current.TenantID != tenantID || current.CompanyID != companyID {
		return fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
	}
	c.seen[itemID] = true
	return nil
}

// ListItems returns current revisions in SKU order. Search and site
// visibility apply before anything is projected.
func (c *Catalog) ListItems(op OperationContext, filter ItemFilter) []CatalogItem {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]CatalogItem, 0)
	want := strings.ToLower(strings.TrimSpace(filter.Search))
	for _, history := range c.revisions {
		if len(history) == 0 {
			continue
		}
		item := history[len(history)-1]
		if !c.visible(item, op) {
			continue
		}
		if filter.PickersOnly && item.Status != ItemStatusActive {
			continue
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if filter.Tracking != "" && item.Tracking != filter.Tracking {
			continue
		}
		if want != "" && !matchesSearch(item, want) {
			continue
		}
		result = append(result, cloneItem(item))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SKU < result[j].SKU })
	return result
}

func matchesSearch(item CatalogItem, want string) bool {
	if strings.Contains(strings.ToLower(item.SKU), want) || strings.Contains(strings.ToLower(item.Name), want) {
		return true
	}
	for _, a := range item.Aliases {
		if strings.Contains(strings.ToLower(a.Code), want) {
			return true
		}
	}
	return false
}

// History returns every revision oldest-first. Retired and archived items
// stay readable; out-of-scope callers get not-found, never a redirect.
func (c *Catalog) History(op OperationContext, itemID string) ([]CatalogItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	history := c.revisions[itemID]
	if len(history) == 0 || history[0].TenantID != op.TenantID || history[0].CompanyID != op.CompanyID {
		return nil, fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
	}
	out := make([]CatalogItem, len(history))
	for i, rev := range history {
		out[i] = cloneItem(rev)
	}
	return out, nil
}

// ResolveBarcode returns the one exact item+unit for a scan. A bare code
// seen under several symbologies needs explicit selection; unknown, denied
// and out-of-scope codes all report UNKNOWN_CODE alike.
func (c *Catalog) ResolveBarcode(op OperationContext, symbology, code string) (BarcodeResolution, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	trimmedSymbology := strings.TrimSpace(symbology)
	trimmedCode := strings.TrimSpace(code)
	if trimmedCode == "" {
		return BarcodeResolution{}, ErrUnknownCode
	}
	if trimmedSymbology != "" {
		owner, ok := c.byAlias[scopeKey(op.TenantID, op.CompanyID, "alias\x00"+trimmedSymbology+"\x00"+trimmedCode)]
		if !ok {
			return BarcodeResolution{}, ErrUnknownCode
		}
		return c.resolution(op, owner, trimmedSymbology, trimmedCode)
	}
	var matches []BarcodeResolution
	seen := map[string]bool{}
	for key, owner := range c.byAlias {
		parts := strings.Split(key, "\x00")
		if len(parts) != 5 || parts[0] != op.TenantID || parts[1] != op.CompanyID || parts[2] != "alias" || parts[4] != trimmedCode {
			continue
		}
		res, err := c.resolution(op, owner, parts[3], trimmedCode)
		if err != nil {
			continue
		}
		dedupe := res.ItemID + "\x00" + res.Unit
		if !seen[dedupe] {
			seen[dedupe] = true
			matches = append(matches, res)
		}
	}
	if len(matches) == 0 {
		return BarcodeResolution{}, ErrUnknownCode
	}
	if len(matches) > 1 {
		return BarcodeResolution{}, ErrAmbiguousAlias
	}
	return matches[0], nil
}

func (c *Catalog) resolution(op OperationContext, itemID, symbology, code string) (BarcodeResolution, error) {
	current, ok := c.current(itemID)
	if !ok || !c.visible(current, op) {
		return BarcodeResolution{}, ErrUnknownCode
	}
	unit := current.BaseUnit
	for _, a := range current.Aliases {
		if a.Symbology == symbology && a.Code == code && a.PackUnit != "" {
			unit = a.PackUnit
			break
		}
	}
	return BarcodeResolution{ItemID: current.ItemID, SKU: current.SKU, Name: current.Name, Unit: unit, Revision: current.Revision, Status: current.Status}, nil
}

// ValidateConversion previews an exact base-unit quantity for qtyText counted
// in fromUnit. Single-unit fractions that the item scale cannot represent are
// refused; callers converting a summed quantity pass aggregate approval and
// the total must still divide exactly. Nothing here posts or rounds.
func (c *Catalog) ValidateConversion(op OperationContext, itemID, qtyText, fromUnit string, aggregate bool) (ConversionPreview, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.current(itemID)
	if !ok || !c.visible(current, op) {
		return ConversionPreview{}, fmt.Errorf("%w: %s", ErrItemNotFound, itemID)
	}
	unit := strings.TrimSpace(fromUnit)
	num, den := int64(1), int64(1)
	if unit != "" && unit != current.BaseUnit {
		found := false
		for _, conv := range current.Conversions {
			if conv.PackUnit == unit {
				num, den, found = conv.Numerator, conv.Denominator, true
				break
			}
		}
		if !found {
			return ConversionPreview{}, fmt.Errorf("%w: %q on %s", ErrUnknownUnit, unit, current.SKU)
		}
	}
	qty, err := values.NewDecimal(strings.TrimSpace(qtyText), current.Scale, values.RoundingExactRequired)
	if err != nil {
		if errors.Is(err, values.ErrExcessScale) {
			return ConversionPreview{}, fmt.Errorf("%w: %q exceeds scale %d on %s: %v", ErrInexactConversion, qtyText, current.Scale, current.SKU, err)
		}
		return ConversionPreview{}, fmt.Errorf("%w: quantity %q: %v", ErrStockValidation, qtyText, err)
	}
	magnitude := new(big.Int).Set(qty.Unscaled())
	if qty.Sign() < 0 {
		magnitude.Neg(magnitude)
	}
	scalePow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(current.Scale)), nil)
	if num != 1 || den != 1 {
		scaledNum := new(big.Int).Mul(scalePow, big.NewInt(num))
		if new(big.Int).Rem(scaledNum, big.NewInt(den)).Sign() != 0 {
			if !aggregate {
				return ConversionPreview{}, fmt.Errorf("%w: one %s is not representable at scale %d without aggregate approval", ErrInexactConversion, unit, current.Scale)
			}
		} else if current.Tracking != TrackingMass {
			whole := new(big.Int).Quo(new(big.Int).Mul(scalePow, big.NewInt(num)), big.NewInt(den))
			if new(big.Int).Rem(whole, scalePow).Sign() != 0 && !aggregate {
				return ConversionPreview{}, fmt.Errorf("%w: one %s is a fractional each without aggregate approval", ErrInexactConversion, unit)
			}
		}
	}
	total := new(big.Int).Mul(magnitude, big.NewInt(num))
	quotient, remainder := new(big.Int).QuoRem(total, big.NewInt(den), new(big.Int))
	if remainder.Sign() != 0 {
		return ConversionPreview{}, fmt.Errorf("%w: %s %s does not divide into scale %d exactly", ErrInexactConversion, qtyText, unit, current.Scale)
	}
	if current.Tracking != TrackingMass && new(big.Int).Rem(quotient, scalePow).Sign() != 0 {
		return ConversionPreview{}, fmt.Errorf("%w: fractional %s is not countable at scale %d", ErrInexactConversion, current.BaseUnit, current.Scale)
	}
	text, err := canonicalQuantity(quotient, current.Scale)
	if err != nil {
		return ConversionPreview{}, err
	}
	if _, err := values.NewDecimal(text, current.Scale, values.RoundingExactRequired); err != nil {
		return ConversionPreview{}, fmt.Errorf("%w: preview %q: %v", ErrStockValidation, text, err)
	}
	return ConversionPreview{BaseQtyText: text, BaseUnit: current.BaseUnit, Aggregate: aggregate && (num != 1 || den != 1), Display: previewDisplay(qtyText, unit, current.BaseUnit, num, den, text, aggregate)}, nil
}

func canonicalQuantity(unscaled *big.Int, scale int32) (string, error) {
	neg := unscaled.Sign() < 0
	digits := new(big.Int).Abs(unscaled).String()
	if scale == 0 {
		if neg {
			return "-" + digits, nil
		}
		return digits, nil
	}
	for int32(len(digits)) <= scale {
		digits = "0" + digits
	}
	text := digits[:len(digits)-int(scale)] + "." + digits[len(digits)-int(scale):]
	if neg {
		return "-" + text, nil
	}
	return text, nil
}

func previewDisplay(qtyText, unit, baseUnit string, num, den int64, result string, aggregate bool) string {
	if unit == "" || unit == baseUnit {
		return qtyText + " " + baseUnit + " = " + result + " " + baseUnit
	}
	ratio := fmt.Sprintf("%d %s", num, baseUnit)
	if den != 1 {
		ratio = fmt.Sprintf("%d/%d %s", num, den, baseUnit)
	}
	display := qtyText + " × " + unit + " (" + ratio + ") = " + result + " " + baseUnit
	if aggregate {
		display += " (aggregate exact)"
	}
	return display
}

// Snapshot exports every revision, movement freeze and durable idempotency
// result so a restart reopens the same catalog.
func (c *Catalog) Snapshot() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	export := struct {
		Revisions map[string][]CatalogItem
		Seen      map[string]bool
		Idem      map[string]idempotencyRecord
	}{Revisions: c.revisions, Seen: c.seen, Idem: c.idem}
	return json.Marshal(export)
}

// Restore loads a Snapshot into an empty catalog and rebuilds the SKU and
// alias registries from current revisions.
func (c *Catalog) Restore(data []byte) error {
	var export struct {
		Revisions map[string][]CatalogItem
		Seen      map[string]bool
		Idem      map[string]idempotencyRecord
	}
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("%w: %v", ErrSnapshot, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.revisions) > 0 {
		return fmt.Errorf("%w: catalog is already populated", ErrSnapshot)
	}
	bySKU := make(map[string]string)
	byAlias := make(map[string]string)
	for itemID, history := range export.Revisions {
		if len(history) == 0 {
			return fmt.Errorf("%w: item %s has no revisions", ErrSnapshot, itemID)
		}
		for i, rev := range history {
			if rev.ItemID != itemID || rev.TenantID == "" || rev.CompanyID == "" || rev.SKU == "" || rev.Revision != i+1 {
				return fmt.Errorf("%w: item %s revision %d is corrupt", ErrSnapshot, itemID, i+1)
			}
		}
		current := history[len(history)-1]
		if current.Status != ItemStatusRetired {
			skuKey := scopeKey(current.TenantID, current.CompanyID, "sku\x00"+current.SKU)
			if _, dup := bySKU[skuKey]; dup {
				return fmt.Errorf("%w: duplicate active sku %q", ErrSnapshot, current.SKU)
			}
			bySKU[skuKey] = itemID
		}
		for _, a := range current.Aliases {
			aliasKey := scopeKey(current.TenantID, current.CompanyID, "alias\x00"+a.Symbology+"\x00"+a.Code)
			if _, dup := byAlias[aliasKey]; dup {
				return fmt.Errorf("%w: duplicate alias %s %q", ErrSnapshot, a.Symbology, a.Code)
			}
			byAlias[aliasKey] = itemID
		}
	}
	c.revisions = export.Revisions
	c.seen = export.Seen
	if c.seen == nil {
		c.seen = make(map[string]bool)
	}
	c.bySKU = bySKU
	c.byAlias = byAlias
	c.idem = export.Idem
	if c.idem == nil {
		c.idem = make(map[string]idempotencyRecord)
	}
	return nil
}
