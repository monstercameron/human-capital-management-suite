// Stock locations and availability views for SMB-STOCK-003.
//
// A business distinguishes warehouse, van, site and quarantine stock on
// canonical site refs with a parent/bin topology; no second location or
// address hierarchy is invented. Quarantine is one quantity classification
// independent of bin purpose: quarantined stock inside a normal bin and
// stock inside a quarantine-purpose bin both exclude from usable
// availability exactly once, never double-deducted.
//
// Balances are reported per location and item by the movement owner
// (SMB-STOCK-001); this slice owns topology admission, bucket validation
// and scoped availability math. Item IDs are opaque refs: the catalog is
// never cross-queried here, so no foreign-table reads exist.
package stock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrLocationValidation = errors.New("stock: stock location request is invalid")
	ErrLocationCycle      = errors.New("stock: stock location topology cycle")
	ErrLocationNotEmpty   = errors.New("stock: stock location still holds stock, reservations or transfers")
	ErrLocationConflict   = errors.New("stock: stock location revision or code conflict")
)

// LocationKind names the stock-location purpose. Purpose never changes
// bucket math: quarantine stays one quantity classification.
type LocationKind string

const (
	LocationStore      LocationKind = "STORE"
	LocationVan        LocationKind = "VAN"
	LocationBin        LocationKind = "BIN"
	LocationQuarantine LocationKind = "QUARANTINE"
	LocationTransit    LocationKind = "TRANSIT"
)

func (k LocationKind) valid() bool {
	switch k {
	case LocationStore, LocationVan, LocationBin, LocationQuarantine, LocationTransit:
		return true
	default:
		return false
	}
}

// StockLocation is one node of the site topology. Bins are leaves; other
// kinds may nest (warehouse zones) but never across sites and never in a
// cycle. AssetRef optionally links a van to its asset; it is opaque here.
type StockLocation struct {
	ID        string       `json:"id"`
	TenantID  string       `json:"tenant_id"`
	CompanyID string       `json:"company_id"`
	SiteRef   string       `json:"site_ref"`
	ParentID  string       `json:"parent_id,omitempty"`
	Kind      LocationKind `json:"kind"`
	Code      string       `json:"code"`
	Label     string       `json:"label"`
	AssetRef  string       `json:"asset_ref,omitempty"`
	Active    bool         `json:"active"`
	Revision  uint64       `json:"revision"`
}

// LocationRequest creates one location. The tenant and company come from
// the operation context, never from the request body.
type LocationRequest struct {
	SiteRef  string
	ParentID string
	Kind     LocationKind
	Code     string
	Label    string
	AssetRef string
}

// ReviseLocationRequest updates label, code, asset link or parent. Empty
// fields keep their current values except ParentID, which only changes
// when Reparent is set. A nonempty bin reparent requires an explicit
// reorganization key and never moves stock physically.
type ReviseLocationRequest struct {
	Label             string
	Code              string
	AssetRef          string
	ParentID          string
	Reparent          bool
	ReorganizationKey string
}

// ArchiveLocationRequest retires a location. Locations that ever held
// stock must cite the authorized relocation that emptied them.
type ArchiveLocationRequest struct {
	RelocationRef string
}

// BalanceInput reports the observed buckets for one location and item.
// OnHand covers usable plus quarantined physical stock; quarantine is a
// subset classification of it, and in-transit is a separate bucket outside
// on-hand. All four quantities must share one scale.
type BalanceInput struct {
	OnHand     values.Decimal
	Allocated  values.Decimal
	Quarantine values.Decimal
	InTransit  values.Decimal
}

// LocationBalance is the derived availability view: usable and available
// are computed, never reported.
type LocationBalance struct {
	LocationID string         `json:"location_id"`
	ItemID     string         `json:"item_id"`
	OnHand     values.Decimal `json:"on_hand"`
	Usable     values.Decimal `json:"usable"`
	Allocated  values.Decimal `json:"allocated"`
	Quarantine values.Decimal `json:"quarantine"`
	InTransit  values.Decimal `json:"in_transit"`
	Available  values.Decimal `json:"available"`
	Watermark  string         `json:"watermark"`
}

// ItemAvailability aggregates one item across the caller's authorized
// locations. Denied sites are excluded before totals, so totals never leak
// them. No cost fields exist anywhere on this view.
type ItemAvailability struct {
	ItemID     string            `json:"item_id"`
	Total      values.Decimal    `json:"total"`
	Allocated  values.Decimal    `json:"allocated"`
	Quarantine values.Decimal    `json:"quarantine"`
	InTransit  values.Decimal    `json:"in_transit"`
	Available  values.Decimal    `json:"available"`
	Watermark  string            `json:"watermark"`
	Locations  []LocationBalance `json:"locations"`
}

// TransferReader reports open transfers blocking archival. It is the seam
// the movement owner (SMB-STOCK-001) implements; a nil reader reports none.
type TransferReader interface {
	OpenTransfers(ctx context.Context, tenantID, companyID, locationID string) (int, error)
}

type locationIdem struct {
	Key         string
	Fingerprint string
	Location    StockLocation
	Balance     *LocationBalance
}

type storedBalance struct {
	input   BalanceInput
	balance LocationBalance
	updates uint64
}

// LocationRegistry is the concurrency-safe location and balance port. All
// state lives on the value; snapshots carry it across restarts.
type LocationRegistry struct {
	mu        sync.Mutex
	locations map[string][]StockLocation
	byCode    map[string]string
	idem      map[string]locationIdem
	balances  map[string]map[string]*storedBalance
	stocked   map[string]bool
	transfers TransferReader
	seq       uint64
}

// NewLocationRegistry builds an empty registry. A nil TransferReader
// reports no open transfers.
func NewLocationRegistry(transfers TransferReader) *LocationRegistry {
	return &LocationRegistry{
		locations: make(map[string][]StockLocation),
		byCode:    make(map[string]string),
		idem:      make(map[string]locationIdem),
		balances:  make(map[string]map[string]*storedBalance),
		stocked:   make(map[string]bool),
		transfers: transfers,
	}
}

func locationCodeKey(op OperationContext, siteRef, code string) string {
	return scopeKey(op.TenantID, op.CompanyID, siteRef+"\x00"+code)
}

func (r *LocationRegistry) current(id string) (StockLocation, bool) {
	history := r.locations[id]
	if len(history) == 0 {
		return StockLocation{}, false
	}
	return history[len(history)-1], true
}

func (r *LocationRegistry) visible(loc StockLocation, op OperationContext) bool {
	if loc.TenantID != op.TenantID || loc.CompanyID != op.CompanyID {
		return false
	}
	if op.SiteID == "" || loc.SiteRef == op.SiteID {
		return true
	}
	return false
}

// chainContains walks the parent chain of start looking for want. It
// returns true on a match or on any broken/looping chain, so callers treat
// both as topology refusals.
func (r *LocationRegistry) chainContains(start, want string) bool {
	seen := map[string]bool{}
	for id := start; id != ""; {
		if id == want {
			return true
		}
		if seen[id] {
			return true
		}
		seen[id] = true
		parent, ok := r.current(id)
		if !ok {
			return true
		}
		id = parent.ParentID
	}
	return false
}

func (r *LocationRegistry) checkLocationIdem(opName string, op OperationContext, key, fp string) (StockLocation, LocationBalance, bool, error) {
	if strings.TrimSpace(key) == "" {
		return StockLocation{}, LocationBalance{}, false, nil
	}
	mapKey := scopeKey(op.TenantID, op.CompanyID, opName+"\x00"+strings.TrimSpace(key))
	prior, ok := r.idem[mapKey]
	if !ok {
		return StockLocation{}, LocationBalance{}, false, nil
	}
	if prior.Fingerprint != fp {
		return StockLocation{}, LocationBalance{}, false, fmt.Errorf("%w: key %q", ErrIdempotencyConflict, key)
	}
	if prior.Balance == nil {
		return prior.Location, LocationBalance{}, true, nil
	}
	return prior.Location, *prior.Balance, true, nil
}

func (r *LocationRegistry) storeLocationIdem(opName string, op OperationContext, key, fp string, loc StockLocation, bal *LocationBalance) {
	if strings.TrimSpace(key) == "" {
		return
	}
	mapKey := scopeKey(op.TenantID, op.CompanyID, opName+"\x00"+strings.TrimSpace(key))
	r.idem[mapKey] = locationIdem{Key: strings.TrimSpace(key), Fingerprint: fp, Location: loc, Balance: bal}
}

// CreateLocation registers the first revision. Codes are unique per site;
// parents must share the site and stay acyclic; bins are leaves.
func (r *LocationRegistry) CreateLocation(op OperationContext, key string, req LocationRequest) (StockLocation, error) {
	if err := op.validate(); err != nil {
		return StockLocation{}, err
	}
	if op.SiteID != "" && req.SiteRef != op.SiteID {
		return StockLocation{}, ErrStockScope
	}
	if strings.TrimSpace(req.SiteRef) == "" {
		return StockLocation{}, fmt.Errorf("%w: site ref is required", ErrLocationValidation)
	}
	if !req.Kind.valid() {
		return StockLocation{}, fmt.Errorf("%w: kind %q is unknown", ErrLocationValidation, req.Kind)
	}
	if strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.Label) == "" {
		return StockLocation{}, fmt.Errorf("%w: code and label are required", ErrLocationValidation)
	}
	fp := fingerprint("stock-location-create", op, req.SiteRef, req.ParentID, string(req.Kind), req.Code, req.Label, req.AssetRef)
	r.mu.Lock()
	defer r.mu.Unlock()
	if loc, _, done, err := r.checkLocationIdem("stock-location-create", op, key, fp); done || err != nil {
		return loc, err
	}
	if _, taken := r.byCode[locationCodeKey(op, req.SiteRef, req.Code)]; taken {
		return StockLocation{}, fmt.Errorf("%w: code %q is already used at site %q", ErrLocationConflict, req.Code, req.SiteRef)
	}
	if req.ParentID != "" {
		parent, ok := r.current(req.ParentID)
		if !ok || !r.visible(parent, op) {
			return StockLocation{}, fmt.Errorf("%w: parent is unknown or outside the authorized scope", ErrLocationValidation)
		}
		if parent.SiteRef != req.SiteRef {
			return StockLocation{}, fmt.Errorf("%w: parent must share the site", ErrLocationValidation)
		}
		if !parent.Active {
			return StockLocation{}, fmt.Errorf("%w: parent is archived", ErrLocationValidation)
		}
		if parent.Kind == LocationBin {
			return StockLocation{}, fmt.Errorf("%w: bins cannot parent locations", ErrLocationValidation)
		}
	}
	r.seq++
	loc := StockLocation{
		ID: fmt.Sprintf("loc-%06d", r.seq), TenantID: op.TenantID, CompanyID: op.CompanyID,
		SiteRef: req.SiteRef, ParentID: req.ParentID, Kind: req.Kind,
		Code: req.Code, Label: req.Label, AssetRef: req.AssetRef, Active: true, Revision: 1,
	}
	r.locations[loc.ID] = []StockLocation{loc}
	r.byCode[locationCodeKey(op, loc.SiteRef, loc.Code)] = loc.ID
	r.storeLocationIdem("stock-location-create", op, key, fp, loc, nil)
	return loc, nil
}

// ReviseLocation updates label, code, asset link or parent. Structural
// topology errors report before the reorganization gate so miswiring is
// never masked by paperwork. Balances stay attached to the location.
func (r *LocationRegistry) ReviseLocation(op OperationContext, key, id string, expected uint64, req ReviseLocationRequest) (StockLocation, error) {
	if err := op.validate(); err != nil {
		return StockLocation{}, err
	}
	fp := fingerprint("stock-location-revise", op, id, expected, req.Label, req.Code, req.AssetRef, req.ParentID, req.Reparent, req.ReorganizationKey)
	r.mu.Lock()
	defer r.mu.Unlock()
	if loc, _, done, err := r.checkLocationIdem("stock-location-revise", op, key, fp); done || err != nil {
		return loc, err
	}
	current, ok := r.current(id)
	if !ok || !r.visible(current, op) {
		return StockLocation{}, ErrStockScope
	}
	if expected != current.Revision {
		return StockLocation{}, fmt.Errorf("%w: expected revision %d, current is %d", ErrLocationConflict, expected, current.Revision)
	}
	if !current.Active {
		return StockLocation{}, fmt.Errorf("%w: archived locations accept no revisions", ErrLocationValidation)
	}
	next := current
	if req.Label != "" {
		next.Label = req.Label
	}
	if req.AssetRef != "" {
		next.AssetRef = req.AssetRef
	}
	if req.Code != "" && req.Code != current.Code {
		if _, taken := r.byCode[locationCodeKey(op, current.SiteRef, req.Code)]; taken {
			return StockLocation{}, fmt.Errorf("%w: code %q is already used at site %q", ErrLocationConflict, req.Code, current.SiteRef)
		}
		delete(r.byCode, locationCodeKey(op, current.SiteRef, current.Code))
		next.Code = req.Code
		r.byCode[locationCodeKey(op, current.SiteRef, next.Code)] = id
	}
	if req.Reparent || req.ParentID != "" {
		parentID := req.ParentID
		if parentID == "" && !req.Reparent {
			parentID = current.ParentID
		}
		if parentID == id {
			return StockLocation{}, fmt.Errorf("%w: a location cannot parent itself", ErrLocationCycle)
		}
		if parentID != "" {
			parent, ok := r.current(parentID)
			if !ok || !r.visible(parent, op) {
				return StockLocation{}, fmt.Errorf("%w: parent is unknown or outside the authorized scope", ErrLocationValidation)
			}
			if parent.SiteRef != current.SiteRef {
				return StockLocation{}, fmt.Errorf("%w: parent must share the site", ErrLocationValidation)
			}
			if !parent.Active {
				return StockLocation{}, fmt.Errorf("%w: parent is archived", ErrLocationValidation)
			}
			if parent.Kind == LocationBin {
				return StockLocation{}, fmt.Errorf("%w: bins cannot parent locations", ErrLocationValidation)
			}
			if r.chainContains(parentID, id) {
				return StockLocation{}, fmt.Errorf("%w: reparenting under %q closes a loop", ErrLocationCycle, parent.Code)
			}
		}
		if parentID != current.ParentID && r.locationHasStock(id) && strings.TrimSpace(req.ReorganizationKey) == "" {
			return StockLocation{}, fmt.Errorf("%w: reparenting a nonempty location requires an explicit reorganization key", ErrLocationValidation)
		}
		next.ParentID = parentID
	}
	next.Revision++
	r.locations[id] = append(r.locations[id], next)
	r.storeLocationIdem("stock-location-revise", op, key, fp, next, nil)
	return next, nil
}

// locationHasStock reports whether any item holds on-hand, allocated or
// in-transit quantities at the location. Callers hold r.mu.
func (r *LocationRegistry) locationHasStock(id string) bool {
	for _, stored := range r.balances[id] {
		if stored.input.OnHand.Sign() > 0 || stored.input.Allocated.Sign() > 0 || stored.input.InTransit.Sign() > 0 {
			return true
		}
	}
	return false
}

// ArchiveLocation retires an empty location. Any on-hand, allocated or
// in-transit quantity, any open transfer, or a missing relocation receipt
// for a location that ever held stock refuses the archival. History stays
// readable.
func (r *LocationRegistry) ArchiveLocation(op OperationContext, key, id string, expected uint64, req ArchiveLocationRequest) (StockLocation, error) {
	if err := op.validate(); err != nil {
		return StockLocation{}, err
	}
	fp := fingerprint("stock-location-archive", op, id, expected, req.RelocationRef)
	r.mu.Lock()
	defer r.mu.Unlock()
	if loc, _, done, err := r.checkLocationIdem("stock-location-archive", op, key, fp); done || err != nil {
		return loc, err
	}
	current, ok := r.current(id)
	if !ok || !r.visible(current, op) {
		return StockLocation{}, ErrStockScope
	}
	if expected != current.Revision {
		return StockLocation{}, fmt.Errorf("%w: expected revision %d, current is %d", ErrLocationConflict, expected, current.Revision)
	}
	if !current.Active {
		return StockLocation{}, fmt.Errorf("%w: location is already archived", ErrLocationValidation)
	}
	if r.locationHasStock(id) {
		return StockLocation{}, fmt.Errorf("%w: relocate on-hand, allocated and in-transit quantities first", ErrLocationNotEmpty)
	}
	if r.transfers != nil {
		open, err := r.transfers.OpenTransfers(context.Background(), op.TenantID, op.CompanyID, id)
		if err != nil {
			return StockLocation{}, fmt.Errorf("%w: transfer check failed", ErrLocationValidation)
		}
		if open > 0 {
			return StockLocation{}, fmt.Errorf("%w: %d open transfers reference this location", ErrLocationNotEmpty, open)
		}
	}
	if r.stocked[id] && strings.TrimSpace(req.RelocationRef) == "" {
		return StockLocation{}, fmt.Errorf("%w: relocation evidence is required", ErrLocationValidation)
	}
	next := current
	next.Active = false
	next.Revision++
	r.locations[id] = append(r.locations[id], next)
	r.storeLocationIdem("stock-location-archive", op, key, fp, next, nil)
	return next, nil
}

// GetLocation returns the current revision or the neutral scope refusal.
func (r *LocationRegistry) GetLocation(op OperationContext, id string) (StockLocation, error) {
	if err := op.validate(); err != nil {
		return StockLocation{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.current(id)
	if !ok || !r.visible(current, op) {
		return StockLocation{}, ErrStockScope
	}
	return current, nil
}

// History returns every retained revision, oldest first. Archived
// locations keep their full history.
func (r *LocationRegistry) History(op OperationContext, id string) []StockLocation {
	if err := op.validate(); err != nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.current(id)
	if !ok || !r.visible(current, op) {
		return nil
	}
	return append([]StockLocation(nil), r.locations[id]...)
}

// ListLocations returns current revisions visible to the caller,
// optionally filtered by site and a code/label search. Denied scopes are
// filtered before matching, so suggestions never leak them.
func (r *LocationRegistry) ListLocations(op OperationContext, siteRef, search string) []StockLocation {
	if err := op.validate(); err != nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	want := strings.ToLower(strings.TrimSpace(search))
	var out []StockLocation
	for id := range r.locations {
		current, ok := r.current(id)
		if !ok || !r.visible(current, op) {
			continue
		}
		if siteRef != "" && current.SiteRef != siteRef {
			continue
		}
		if want != "" && !strings.Contains(strings.ToLower(current.Code+" "+current.Label), want) {
			continue
		}
		out = append(out, current)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SiteRef != out[j].SiteRef {
			return out[i].SiteRef < out[j].SiteRef
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func validateBalanceInput(input BalanceInput) error {
	for _, qty := range []values.Decimal{input.OnHand, input.Allocated, input.Quarantine, input.InTransit} {
		if err := qty.Validate(); err != nil {
			return fmt.Errorf("%w: balance quantity invalid", ErrLocationValidation)
		}
		if qty.Sign() < 0 {
			return fmt.Errorf("%w: balance quantities cannot be negative", ErrLocationValidation)
		}
		if qty.Scale() != input.OnHand.Scale() {
			return fmt.Errorf("%w: balance quantities must share one scale", ErrLocationValidation)
		}
	}
	if input.Quarantine.Cmp(input.OnHand) > 0 {
		return fmt.Errorf("%w: quarantine exceeds on-hand", ErrLocationValidation)
	}
	usable, err := input.OnHand.Sub(input.Quarantine)
	if err != nil {
		return fmt.Errorf("%w: derive usable: %v", ErrLocationValidation, err)
	}
	if input.Allocated.Cmp(usable) > 0 {
		return fmt.Errorf("%w: allocated exceeds usable stock", ErrLocationValidation)
	}
	return nil
}

func deriveBalance(locationID, itemID string, input BalanceInput, watermark string) (LocationBalance, error) {
	usable, err := input.OnHand.Sub(input.Quarantine)
	if err != nil {
		return LocationBalance{}, err
	}
	available, err := usable.Sub(input.Allocated)
	if err != nil {
		return LocationBalance{}, err
	}
	return LocationBalance{
		LocationID: locationID, ItemID: itemID, OnHand: input.OnHand, Usable: usable,
		Allocated: input.Allocated, Quarantine: input.Quarantine, InTransit: input.InTransit,
		Available: available, Watermark: watermark,
	}, nil
}

// ReportBalance records the observed buckets for one location and item.
// Quarantine above on-hand or allocated above usable is refused, so
// availability can never double-subtract. Repeats with the same key and
// payload return the original view.
func (r *LocationRegistry) ReportBalance(op OperationContext, key, locationID, itemID string, input BalanceInput) error {
	if err := op.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(itemID) == "" {
		return fmt.Errorf("%w: item id is required", ErrLocationValidation)
	}
	if err := validateBalanceInput(input); err != nil {
		return err
	}
	idemFP := fingerprint("stock-balance-report", op, locationID, itemID,
		input.OnHand.String(), input.Allocated.String(), input.Quarantine.String(), input.InTransit.String())
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, _, done, err := r.checkLocationIdem("stock-balance-report", op, key, idemFP); done || err != nil {
		return err
	}
	current, ok := r.current(locationID)
	if !ok || !r.visible(current, op) {
		return ErrStockScope
	}
	if !current.Active {
		return fmt.Errorf("%w: archived locations accept no new balances", ErrLocationValidation)
	}
	stored := r.balances[locationID][itemID]
	var updates uint64
	if stored != nil {
		updates = stored.updates
	}
	updates++
	watermark := fmt.Sprintf("locrev:%d.bal:%d", current.Revision, updates)
	balance, err := deriveBalance(locationID, itemID, input, watermark)
	if err != nil {
		return fmt.Errorf("%w: derive availability: %v", ErrLocationValidation, err)
	}
	if r.balances[locationID] == nil {
		r.balances[locationID] = make(map[string]*storedBalance)
	}
	r.balances[locationID][itemID] = &storedBalance{input: input, balance: balance, updates: updates}
	if input.OnHand.Sign() > 0 {
		r.stocked[locationID] = true
	}
	r.storeLocationIdem("stock-balance-report", op, key, idemFP, current, &balance)
	return nil
}

// LocationStock returns the derived view for one location and item. Unknown
// and denied locations share the neutral scope refusal; an authorized
// location with no reported balance reads back honest zeroes.
func (r *LocationRegistry) LocationStock(op OperationContext, locationID, itemID string) (LocationBalance, error) {
	if err := op.validate(); err != nil {
		return LocationBalance{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.current(locationID)
	if !ok || !r.visible(current, op) {
		return LocationBalance{}, ErrStockScope
	}
	if stored := r.balances[locationID][itemID]; stored != nil {
		return stored.balance, nil
	}
	zero, err := values.NewDecimal("0", 2, values.RoundingExactRequired)
	if err != nil {
		return LocationBalance{}, fmt.Errorf("%w: zero balance: %v", ErrLocationValidation, err)
	}
	return LocationBalance{
		LocationID: locationID, ItemID: itemID, OnHand: zero, Usable: zero,
		Allocated: zero, Quarantine: zero, InTransit: zero, Available: zero,
		Watermark: fmt.Sprintf("locrev:%d.bal:0", current.Revision),
	}, nil
}

// ItemAvailability aggregates one item across the caller's authorized
// locations. Aggregation requires one shared scale; mixed scales refuse so
// the application layer canonicalizes through exact catalog conversions.
func (r *LocationRegistry) ItemAvailability(op OperationContext, itemID string) (ItemAvailability, error) {
	if err := op.validate(); err != nil {
		return ItemAvailability{}, err
	}
	if strings.TrimSpace(itemID) == "" {
		return ItemAvailability{}, fmt.Errorf("%w: item id is required", ErrLocationValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var rows []LocationBalance
	for id := range r.locations {
		current, ok := r.current(id)
		if !ok || !r.visible(current, op) {
			continue
		}
		stored := r.balances[id][itemID]
		if stored == nil {
			continue
		}
		rows = append(rows, stored.balance)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].LocationID < rows[j].LocationID })
	if len(rows) == 0 {
		zero, err := values.NewDecimal("0", 2, values.RoundingExactRequired)
		if err != nil {
			return ItemAvailability{}, fmt.Errorf("%w: zero aggregate: %v", ErrLocationValidation, err)
		}
		return ItemAvailability{ItemID: itemID, Total: zero, Allocated: zero, Quarantine: zero, InTransit: zero, Available: zero}, nil
	}
	scale := rows[0].OnHand.Scale()
	total, allocated, quarantine, inTransit := rows[0].OnHand, rows[0].Allocated, rows[0].Quarantine, rows[0].InTransit
	for _, row := range rows[1:] {
		if row.OnHand.Scale() != scale {
			return ItemAvailability{}, fmt.Errorf("%w: aggregated balances must share one scale", ErrLocationValidation)
		}
		var err error
		if total, err = total.Add(row.OnHand); err != nil {
			return ItemAvailability{}, fmt.Errorf("%w: sum on-hand: %v", ErrLocationValidation, err)
		}
		if allocated, err = allocated.Add(row.Allocated); err != nil {
			return ItemAvailability{}, fmt.Errorf("%w: sum allocated: %v", ErrLocationValidation, err)
		}
		if quarantine, err = quarantine.Add(row.Quarantine); err != nil {
			return ItemAvailability{}, fmt.Errorf("%w: sum quarantine: %v", ErrLocationValidation, err)
		}
		if inTransit, err = inTransit.Add(row.InTransit); err != nil {
			return ItemAvailability{}, fmt.Errorf("%w: sum in-transit: %v", ErrLocationValidation, err)
		}
	}
	usable, err := total.Sub(quarantine)
	if err != nil {
		return ItemAvailability{}, fmt.Errorf("%w: derive usable: %v", ErrLocationValidation, err)
	}
	available, err := usable.Sub(allocated)
	if err != nil {
		return ItemAvailability{}, fmt.Errorf("%w: derive available: %v", ErrLocationValidation, err)
	}
	marks := make([]string, 0, len(rows))
	for _, row := range rows {
		marks = append(marks, row.LocationID+"@"+row.Watermark)
	}
	return ItemAvailability{
		ItemID: itemID, Total: total, Allocated: allocated, Quarantine: quarantine,
		InTransit: inTransit, Available: available,
		Watermark: "aggregate:" + strings.Join(marks, ","),
		Locations: rows,
	}, nil
}

// LocationColumns carries the canonical decimal strings the stock table
// renders: on hand, reserved, available, quarantine and in-transit.
type LocationColumns struct {
	OnHand     string `json:"on_hand"`
	Reserved   string `json:"reserved"`
	Available  string `json:"available"`
	Quarantine string `json:"quarantine"`
	InTransit  string `json:"in_transit"`
}

// LocationTreeRow is one expandable location-tree row with its breadcrumb,
// quantity columns and freshness. Stale rows render Updating and disable
// capacity-dependent actions until refreshed.
type LocationTreeRow struct {
	LocationID     string          `json:"location_id"`
	Code           string          `json:"code"`
	Label          string          `json:"label"`
	Kind           LocationKind    `json:"kind"`
	Breadcrumb     string          `json:"breadcrumb"`
	Depth          int             `json:"depth"`
	Columns        LocationColumns `json:"columns"`
	Stale          bool            `json:"stale"`
	ActionsEnabled bool            `json:"actions_enabled"`
}

// ProjectTree builds the location-tree rows for one site and item. known
// carries the client's watermarks by location; rows whose watermark moved
// (or that never had one the client saw) render stale, except rows with no
// balance at all, which are trivially fresh zeroes.
func (r *LocationRegistry) ProjectTree(op OperationContext, siteRef, itemID string, known map[string]string) []LocationTreeRow {
	if err := op.validate(); err != nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var locs []StockLocation
	for id := range r.locations {
		current, ok := r.current(id)
		if !ok || !r.visible(current, op) {
			continue
		}
		if siteRef != "" && current.SiteRef != siteRef {
			continue
		}
		locs = append(locs, current)
	}
	sort.Slice(locs, func(i, j int) bool {
		if locs[i].ParentID != locs[j].ParentID {
			return locs[i].ParentID < locs[j].ParentID
		}
		return locs[i].Code < locs[j].Code
	})
	byID := make(map[string]StockLocation, len(locs))
	for _, loc := range locs {
		byID[loc.ID] = loc
	}
	var rows []LocationTreeRow
	for _, loc := range locs {
		crumb := []string{loc.SiteRef}
		chain := []string{}
		seen := map[string]bool{}
		for id := loc.ID; id != ""; {
			if seen[id] {
				chain = append([]string{"?"}, chain...)
				break
			}
			seen[id] = true
			node, ok := byID[id]
			if !ok {
				chain = append([]string{"?"}, chain...)
				break
			}
			chain = append([]string{node.Code}, chain...)
			id = node.ParentID
		}
		crumb = append(crumb, chain...)
		stored := r.balances[loc.ID][itemID]
		cols := LocationColumns{OnHand: "0.00", Reserved: "0.00", Available: "0.00", Quarantine: "0.00", InTransit: "0.00"}
		stale := false
		if stored != nil {
			cols = LocationColumns{
				OnHand: stored.balance.OnHand.String(), Reserved: stored.balance.Allocated.String(),
				Available: stored.balance.Available.String(), Quarantine: stored.balance.Quarantine.String(),
				InTransit: stored.balance.InTransit.String(),
			}
			stale = known[loc.ID] != stored.balance.Watermark
		}
		rows = append(rows, LocationTreeRow{
			LocationID: loc.ID, Code: loc.Code, Label: loc.Label, Kind: loc.Kind,
			Breadcrumb: strings.Join(crumb, " > "), Depth: len(chain) - 1,
			Columns: cols, Stale: stale, ActionsEnabled: !stale,
		})
	}
	return rows
}

type locationSnapshot struct {
	Seq       uint64                     `json:"seq"`
	Locations map[string][]StockLocation `json:"locations"`
	Idem      map[string]locationIdem    `json:"idem"`
	Balances  []balanceRecord            `json:"balances"`
	Stocked   map[string]bool            `json:"stocked"`
}

type balanceRecord struct {
	LocationID string          `json:"location_id"`
	ItemID     string          `json:"item_id"`
	Input      BalanceInput    `json:"input"`
	Balance    LocationBalance `json:"balance"`
	Updates    uint64          `json:"updates"`
}

// Export captures the registry for restart recovery with deterministic
// balance order.
func (r *LocationRegistry) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := locationSnapshot{
		Seq:       r.seq,
		Locations: make(map[string][]StockLocation, len(r.locations)),
		Idem:      make(map[string]locationIdem, len(r.idem)),
		Stocked:   make(map[string]bool, len(r.stocked)),
	}
	for id, history := range r.locations {
		snapshot.Locations[id] = append([]StockLocation(nil), history...)
	}
	for key, record := range r.idem {
		snapshot.Idem[key] = record
	}
	for locID, items := range r.balances {
		for itemID, stored := range items {
			snapshot.Balances = append(snapshot.Balances, balanceRecord{
				LocationID: locID, ItemID: itemID, Input: stored.input, Balance: stored.balance, Updates: stored.updates,
			})
		}
	}
	sort.Slice(snapshot.Balances, func(i, j int) bool {
		if snapshot.Balances[i].LocationID != snapshot.Balances[j].LocationID {
			return snapshot.Balances[i].LocationID < snapshot.Balances[j].LocationID
		}
		return snapshot.Balances[i].ItemID < snapshot.Balances[j].ItemID
	})
	for id, stocked := range r.stocked {
		snapshot.Stocked[id] = stocked
	}
	return json.Marshal(snapshot)
}

// Import restores a snapshot. Revision chains, parent refs, code uniqueness
// and every derived balance revalidate, so tampered snapshots fail closed.
func (r *LocationRegistry) Import(data []byte) error {
	var snapshot locationSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("%w: location snapshot is not valid", ErrSnapshot)
	}
	for id, history := range snapshot.Locations {
		if len(history) == 0 {
			return fmt.Errorf("%w: location has no revisions", ErrSnapshot)
		}
		for i, rev := range history {
			if rev.Revision != uint64(i+1) || rev.ID != id {
				return fmt.Errorf("%w: location revision chain is broken", ErrSnapshot)
			}
			if rev.TenantID == "" || rev.CompanyID == "" || rev.SiteRef == "" || !rev.Kind.valid() {
				return fmt.Errorf("%w: location revision is incomplete", ErrSnapshot)
			}
		}
	}
	for _, history := range snapshot.Locations {
		for _, rev := range history {
			if rev.ParentID != "" {
				if _, ok := snapshot.Locations[rev.ParentID]; !ok {
					return fmt.Errorf("%w: location parent is missing", ErrSnapshot)
				}
			}
		}
	}
	tips := make(map[string]string, len(snapshot.Locations))
	for id, history := range snapshot.Locations {
		tips[id] = history[len(history)-1].ParentID
	}
	for id := range tips {
		seen := map[string]bool{}
		for walk := id; walk != ""; {
			if seen[walk] {
				return fmt.Errorf("%w: location parent graph is cyclic", ErrSnapshot)
			}
			seen[walk] = true
			walk = tips[walk]
		}
	}
	seenCodes := map[string]string{}
	for id, history := range snapshot.Locations {
		current := history[len(history)-1]
		key := current.TenantID + "\x00" + current.CompanyID + "\x00" + current.SiteRef + "\x00" + current.Code
		if prior, dup := seenCodes[key]; dup {
			return fmt.Errorf("%w: code %q is claimed by %s and %s", ErrSnapshot, current.Code, prior, id)
		}
		seenCodes[key] = id
	}
	locationsEqual := func(a, b StockLocation) bool {
		return a.ID == b.ID && a.TenantID == b.TenantID && a.CompanyID == b.CompanyID &&
			a.SiteRef == b.SiteRef && a.ParentID == b.ParentID && a.Kind == b.Kind &&
			a.Code == b.Code && a.Label == b.Label && a.AssetRef == b.AssetRef &&
			a.Active == b.Active && a.Revision == b.Revision
	}
	for key, record := range snapshot.Idem {
		parts := strings.SplitN(key, "\x00", 4)
		if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
			return fmt.Errorf("%w: snapshot idempotency key is malformed", ErrSnapshot)
		}
		if record.Key == "" || record.Key != parts[3] {
			return fmt.Errorf("%w: snapshot idempotency record disagrees with its key", ErrSnapshot)
		}
		if record.Location.TenantID != parts[0] || record.Location.CompanyID != parts[1] {
			return fmt.Errorf("%w: snapshot receipt scope disagrees with its map key", ErrSnapshot)
		}
		chain, ok := snapshot.Locations[record.Location.ID]
		if !ok {
			return fmt.Errorf("%w: snapshot receipt references an unknown location", ErrSnapshot)
		}
		member := false
		for _, rev := range chain {
			if locationsEqual(rev, record.Location) {
				member = true
				break
			}
		}
		if !member {
			return fmt.Errorf("%w: snapshot receipt location matches no recorded revision", ErrSnapshot)
		}
		if record.Balance == nil {
			continue
		}
		bal := *record.Balance
		if bal.ItemID == "" || bal.Watermark == "" {
			return fmt.Errorf("%w: snapshot receipt balance is incomplete", ErrSnapshot)
		}
		balLoc, ok := snapshot.Locations[bal.LocationID]
		if !ok || balLoc[len(balLoc)-1].TenantID != parts[0] || balLoc[len(balLoc)-1].CompanyID != parts[1] {
			return fmt.Errorf("%w: snapshot receipt balance scope disagrees with its map key", ErrSnapshot)
		}
		usable, err := bal.OnHand.Sub(bal.Quarantine)
		if err != nil || !usable.Equal(bal.Usable) {
			return fmt.Errorf("%w: snapshot receipt balance is inconsistent", ErrSnapshot)
		}
		available, err := usable.Sub(bal.Allocated)
		if err != nil || !available.Equal(bal.Available) {
			return fmt.Errorf("%w: snapshot receipt balance is inconsistent", ErrSnapshot)
		}
	}
	var maxSeq uint64
	for id := range snapshot.Locations {
		var seq uint64
		if _, err := fmt.Sscanf(id, "loc-%06d", &seq); err != nil || seq == 0 || fmt.Sprintf("loc-%06d", seq) != id {
			return fmt.Errorf("%w: snapshot location id %q is malformed", ErrSnapshot, id)
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	if snapshot.Seq < maxSeq {
		return fmt.Errorf("%w: snapshot sequence regressed below issued location ids", ErrSnapshot)
	}
	for id := range snapshot.Stocked {
		if _, ok := snapshot.Locations[id]; !ok {
			return fmt.Errorf("%w: snapshot stocked flag references an unknown location", ErrSnapshot)
		}
	}
	for _, record := range snapshot.Balances {
		if record.Balance.LocationID != record.LocationID || record.Balance.ItemID != record.ItemID {
			return fmt.Errorf("%w: snapshot balance disagrees with its record keys", ErrSnapshot)
		}
		if err := validateBalanceInput(record.Input); err != nil {
			return err
		}
		derived, err := deriveBalance(record.LocationID, record.ItemID, record.Input, record.Balance.Watermark)
		if err != nil {
			return fmt.Errorf("%w: snapshot balance is underivable", ErrSnapshot)
		}
		if !derived.Usable.Equal(record.Balance.Usable) || !derived.Available.Equal(record.Balance.Available) ||
			!derived.OnHand.Equal(record.Balance.OnHand) || !derived.Allocated.Equal(record.Balance.Allocated) ||
			!derived.Quarantine.Equal(record.Balance.Quarantine) || !derived.InTransit.Equal(record.Balance.InTransit) {
			return fmt.Errorf("%w: snapshot balance disagrees with its inputs", ErrSnapshot)
		}
		if _, ok := snapshot.Locations[record.LocationID]; !ok {
			return fmt.Errorf("%w: snapshot balance references an unknown location", ErrSnapshot)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq = snapshot.Seq
	r.locations = make(map[string][]StockLocation, len(snapshot.Locations))
	for id, history := range snapshot.Locations {
		r.locations[id] = append([]StockLocation(nil), history...)
	}
	r.byCode = make(map[string]string, len(snapshot.Locations))
	for id, history := range r.locations {
		current := history[len(history)-1]
		fakeOp := OperationContext{TenantID: current.TenantID, CompanyID: current.CompanyID}
		r.byCode[locationCodeKey(fakeOp, current.SiteRef, current.Code)] = id
	}
	r.idem = make(map[string]locationIdem, len(snapshot.Idem))
	for key, record := range snapshot.Idem {
		r.idem[key] = record
	}
	r.balances = make(map[string]map[string]*storedBalance)
	for _, record := range snapshot.Balances {
		if r.balances[record.LocationID] == nil {
			r.balances[record.LocationID] = make(map[string]*storedBalance)
		}
		stored := &storedBalance{input: record.Input, balance: record.Balance, updates: record.Updates}
		r.balances[record.LocationID][record.ItemID] = stored
	}
	r.stocked = make(map[string]bool, len(snapshot.Stocked))
	for id, stocked := range snapshot.Stocked {
		r.stocked[id] = stocked
	}
	return nil
}
