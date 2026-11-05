// SMB-ASSET-002: the equipment register binds each physical unit to its
// purchase receipt, site and custody history without double-counting
// stock. Receipts create exactly one asset per serial; a serial repeats
// only across companies, never within one. Stock linkage stays optional
// and pending until reconciled, and it never gates lawful asset
// operations: pending links block only inventory-derived availability.
// Staged imports enforce domain source-key uniqueness with replay,
// conflict and explicit-correction semantics, and the whole register
// snapshots for restart recovery with the custody stream replayed
// exactly. Shared custody facts live in contracts.go; this file owns
// registration, linkage, import and recovery around that store.
package asset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrEquipmentValidation refuses malformed registration, link, import
	// or correction input before any fact is written.
	ErrEquipmentValidation = errors.New("asset: equipment input is invalid")
	// ErrEquipmentConflict refuses a replay whose payload disagrees with
	// the admitted fact, a duplicate serial, or a stale import version.
	ErrEquipmentConflict = errors.New("asset: equipment fact conflicts")
	// ErrSnapshot refuses a restart snapshot that fails revalidation.
	ErrSnapshot = errors.New("asset: equipment snapshot is invalid")
)

// SerialScheme names one accepted serial-number plate family. Serials are
// canonicalized per scheme (trimmed, upper-cased) before any uniqueness
// check, so one plate never registers twice under different spellings.
type SerialScheme struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

// WorkerValidator answers whether a worker reference is a real,
// assignable worker in the tenant. The register never invents custody.
type WorkerValidator func(tenant values.TenantId, worker values.EntityRef) bool

// EquipmentRegister is the SMB-ASSET-002 register: serial-unique asset
// facts in one tenant/company scope, optional stock linkage, staged
// imports with domain source keys, and restart recovery. The zero value
// is not usable; construct with NewEquipmentRegister.
type EquipmentRegister struct {
	mu       sync.Mutex
	schemes  map[string]SerialScheme
	records  map[string]*EquipmentRecord
	bySerial map[string]string
	bySource map[string]*sourceEntry
	idem     map[string]equipmentIdem
	seq      uint64
	store    *CustodyStore
	workers  WorkerValidator
}

// NewEquipmentRegister returns an empty register fronting a fresh
// custody store. workers may be nil only when no assignment will ever
// be attempted; worker rows then refuse as unverifiable.
func NewEquipmentRegister(workers WorkerValidator) *EquipmentRegister {
	return &EquipmentRegister{
		schemes:  make(map[string]SerialScheme),
		records:  make(map[string]*EquipmentRecord),
		bySerial: make(map[string]string),
		bySource: make(map[string]*sourceEntry),
		idem:     make(map[string]equipmentIdem),
		store:    NewCustodyStore(),
		workers:  workers,
	}
}

// RegisterSerialScheme admits one serial plate family. IDs are exact and
// unique; re-registering an ID refuses rather than redefining it.
func (r *EquipmentRegister) RegisterSerialScheme(scheme SerialScheme) error {
	if strings.TrimSpace(scheme.ID) == "" {
		return fmt.Errorf("%w: serial scheme id is required", ErrEquipmentValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.schemes[scheme.ID]; dup {
		return fmt.Errorf("%w: serial scheme %q is already registered", ErrEquipmentConflict, scheme.ID)
	}
	r.schemes[scheme.ID] = scheme
	return nil
}

// EquipmentVerification tracks whether an imported assignment still
// needs human verification. Receipt and manual registrations are born
// verified; imports with a worker but no custody evidence wait.
type EquipmentVerification string

const (
	// VerificationVerified marks a record whose custody facts are final.
	VerificationVerified EquipmentVerification = "VERIFIED"
	// VerificationPending marks an imported assignment waiting for
	// verified receipts. Pending assignments assert no custody.
	VerificationPending EquipmentVerification = "PENDING"
)

// EquipmentRecord is one registered physical unit: the immutable
// inventory fact, its site and condition, optional stock linkage,
// verification state, import lineage, and the custody journal that
// replays into the store after a restart.
type EquipmentRecord struct {
	Inventory          InventoryRevision     `json:"inventory"`
	Company            string                `json:"company"`
	SiteRef            string                `json:"site_ref"`
	Condition          string                `json:"condition"`
	AcquiredSource     string                `json:"acquired_source"`
	AssetSeq           uint64                `json:"asset_seq"`
	InventorySeq       uint64                `json:"inventory_seq"`
	TrackInStock       bool                  `json:"track_in_stock"`
	StockItemRef       string                `json:"stock_item_ref,omitempty"`
	StockLocationRef   string                `json:"stock_location_ref,omitempty"`
	LinkState          StockLinkState        `json:"link_state"`
	LinkEvidence       string                `json:"link_evidence,omitempty"`
	LinkRequestedBy    string                `json:"link_requested_by,omitempty"`
	LinkSteps          int                   `json:"link_steps"`
	Verification       EquipmentVerification `json:"verification"`
	VerificationNote   string                `json:"verification_note,omitempty"`
	PendingWorker      *values.EntityRef     `json:"pending_worker,omitempty"`
	Source             ImportSourceKey       `json:"source"`
	SourceVersion      string                `json:"source_version,omitempty"`
	Revision           uint64                `json:"revision"`
	CorrectionCount    int                   `json:"correction_count"`
	CorrectionEvidence string                `json:"correction_evidence,omitempty"`
	CorrectionActor    string                `json:"correction_actor,omitempty"`
	CorrectionAt       time.Time             `json:"correction_at,omitempty"`
	CustodyLog         []CustodyEvent        `json:"custody_log,omitempty"`
}

// CustodyEvent journals one assignment for restart replay: the worker,
// the verified receipts, the expected and resulting custody tokens as
// stream/sequence descriptors, and the effective time.
type CustodyEvent struct {
	Worker         values.EntityRef `json:"worker"`
	Location       string           `json:"location"`
	Condition      string           `json:"condition"`
	Assignee       Receipt          `json:"assignee"`
	Issuer         Receipt          `json:"issuer"`
	ExpectedStream string           `json:"expected_stream,omitempty"`
	ExpectedSeq    uint64           `json:"expected_seq,omitempty"`
	RevisionStream string           `json:"revision_stream"`
	RevisionSeq    uint64           `json:"revision_seq"`
	At             time.Time        `json:"at"`
}

type sourceEntry struct {
	RecordKey       string             `json:"record_key"`
	AdmittedVersion uint64             `json:"admitted_version"`
	PayloadHash     string             `json:"payload_hash"`
	OriginalReceipt string             `json:"original_receipt"`
	AdmittedRow     EquipmentImportRow `json:"admitted_row"`
}

type equipmentIdem struct {
	Key         string   `json:"key"`
	Kind        string   `json:"kind"`
	Fingerprint string   `json:"fingerprint"`
	RecordIDs   []string `json:"record_ids"`
}

func equipmentRecordKey(tenant values.TenantId, company, assetID string) string {
	return string(tenant) + "\x00" + company + "\x00" + assetID
}

func equipmentSerialKey(tenant values.TenantId, company, serial string) string {
	return string(tenant) + "\x00" + company + "\x00" + serial
}

func equipmentSourceKey(tenant values.TenantId, key ImportSourceKey) string {
	return string(tenant) + "\x00" + key.SourceSystem + "\x00" + key.Company + "\x00" + key.RecordKind + "\x00" + key.ExternalKey
}

func equipmentIdemKey(tenant values.TenantId, company, kind, userKey string) string {
	return string(tenant) + "\x00" + company + "\x00" + kind + "\x00" + userKey
}

func normalizeEquipmentSerial(serial string) string {
	return strings.ToUpper(strings.TrimSpace(serial))
}

func equipmentUUID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	raw := hex.EncodeToString(sum[:16])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
}

// equipmentOrgRef synthesizes the deterministic owning-organization
// reference for a tenant/company scope. Imports name no owner; the
// company owns its equipment, and the reference must be stable across
// restarts so replays never mint a second owner.
func equipmentOrgRef(tenant values.TenantId, company string) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: "organization", Id: equipmentUUID("equipment-org\x00" + string(tenant) + "\x00" + company)}
}

func fingerprintParts(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (r *EquipmentRegister) getRecordLocked(tenant values.TenantId, asset values.EntityRef) (*EquipmentRecord, error) {
	if err := asset.Validate(); err != nil || asset.Tenant != tenant {
		return nil, ErrTenantBoundary
	}
	for key, record := range r.records {
		if !strings.HasPrefix(key, string(tenant)+"\x00") {
			continue
		}
		if record.Inventory.InventoryID == asset {
			return record, nil
		}
	}
	return nil, ErrUnknownInventory
}

func tokenSeq(token values.RevisionToken) uint64 {
	seq, _ := token.Sequence()
	return seq
}

// sequenceToken rejects opaque and unspecified tokens where the custody
// journal needs a stream/sequence descriptor to replay after a restart.
func sequenceToken(token values.RevisionToken, label string) (string, uint64, error) {
	if !token.IsSpecified() {
		return "", 0, fmt.Errorf("%w: %s revision is required", ErrEquipmentValidation, label)
	}
	seq, ok := token.Sequence()
	if !ok {
		return "", 0, fmt.Errorf("%w: %s revision must be a sequence token", ErrEquipmentValidation, label)
	}
	return token.Stream(), seq, nil
}

// ReceiptInput registers the assets on one purchase receipt: exactly one
// asset per serial, all born verified and unlinked.
type ReceiptInput struct {
	Key            string
	ReceiptID      string
	Tenant         values.TenantId
	Company        string
	Owner          values.EntityRef
	Classification string
	SchemeID       string
	Serials        []string
	SiteRef        string
	Condition      string
	AcquiredAt     time.Time
	Evidence       string
}

// RegisterEquipmentFromReceipt creates one asset per serial on the
// receipt. The batch prevalidates fully before anything is written, so
// a refused receipt leaves zero records. Retrying the same key with the
// same payload replays the original records; the same key with a
// changed payload is a conflict. The idempotency check runs before the
// taken check, so retries replay instead of conflicting with themselves.
func (r *EquipmentRegister) RegisterEquipmentFromReceipt(input ReceiptInput) ([]EquipmentRecord, error) {
	if input.Key == "" || input.ReceiptID == "" || input.Company == "" {
		return nil, fmt.Errorf("%w: receipt key, receipt id and company are required", ErrEquipmentValidation)
	}
	if err := input.Tenant.Validate(); err != nil {
		return nil, fmt.Errorf("%w: receipt tenant: %v", ErrEquipmentValidation, err)
	}
	if err := input.Owner.Validate(); err != nil || input.Owner.Tenant != input.Tenant {
		return nil, fmt.Errorf("%w: receipt owner is not in the tenant", ErrEquipmentValidation)
	}
	if input.Classification == "" || input.SiteRef == "" || input.Condition == "" || input.Evidence == "" {
		return nil, fmt.Errorf("%w: receipt classification, site, condition and evidence are required", ErrEquipmentValidation)
	}
	if input.AcquiredAt.IsZero() {
		return nil, fmt.Errorf("%w: receipt acquisition time is required", ErrEquipmentValidation)
	}
	if len(input.Serials) == 0 {
		return nil, fmt.Errorf("%w: receipt names no serials", ErrEquipmentValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.schemes[input.SchemeID]; !ok || input.SchemeID == "" {
		return nil, fmt.Errorf("%w: serial scheme %q is not registered", ErrEquipmentValidation, input.SchemeID)
	}
	canonical := make([]string, 0, len(input.Serials))
	seen := make(map[string]bool, len(input.Serials))
	for _, serial := range input.Serials {
		normalized := normalizeEquipmentSerial(serial)
		if normalized == "" {
			return nil, fmt.Errorf("%w: receipt serial is blank", ErrEquipmentValidation)
		}
		full := input.SchemeID + ":" + normalized
		if seen[full] {
			return nil, fmt.Errorf("%w: receipt repeats serial %q", ErrEquipmentValidation, full)
		}
		seen[full] = true
		canonical = append(canonical, full)
	}
	fingerprint := fingerprintParts(append([]string{
		input.ReceiptID, string(input.Tenant), input.Company, input.Owner.String(),
		input.Classification, input.SchemeID, input.SiteRef, input.Condition,
		input.AcquiredAt.UTC().Format(time.RFC3339Nano), input.Evidence,
	}, canonical...)...)
	key := equipmentIdemKey(input.Tenant, input.Company, "receipt", input.Key)
	if prior, ok := r.idem[key]; ok {
		if prior.Fingerprint != fingerprint {
			return nil, fmt.Errorf("%w: receipt key %q was used with a different payload", ErrEquipmentConflict, input.Key)
		}
		return r.replayIdemLocked(prior)
	}
	for _, serial := range canonical {
		if _, taken := r.bySerial[equipmentSerialKey(input.Tenant, input.Company, serial)]; taken {
			return nil, fmt.Errorf("%w: serial %q is already registered", ErrEquipmentConflict, serial)
		}
	}
	records := make([]EquipmentRecord, 0, len(canonical))
	ids := make([]string, 0, len(canonical))
	for _, serial := range canonical {
		record, err := r.createRecordLocked(creationInput{
			Tenant: input.Tenant, Company: input.Company, Owner: input.Owner,
			Classification: input.Classification, Serial: serial,
			Site: input.SiteRef, Condition: input.Condition,
			AcquiredSource: input.Evidence, EffectiveAt: input.AcquiredAt,
		})
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
		ids = append(ids, record.Inventory.InventoryID.Id)
	}
	r.idem[key] = equipmentIdem{Key: input.Key, Kind: "receipt", Fingerprint: fingerprint, RecordIDs: ids}
	return records, nil
}

// ManualInput registers one asset by hand. Tracking fields are optional
// at creation: an asset-only record stays untracked until a link
// request adopts stock references.
type ManualInput struct {
	Key              string
	Tenant           values.TenantId
	Company          string
	Owner            values.EntityRef
	Classification   string
	SchemeID         string
	Serial           string
	SiteRef          string
	Condition        string
	AcquiredSource   string
	AcquiredAt       time.Time
	Evidence         string
	TrackInStock     bool
	StockItemRef     string
	StockLocationRef string
}

// RegisterEquipmentManual creates one hand-registered asset. Tracking
// without both stock references refuses; otherwise the record is born
// verified and unlinked, exactly like a receipt asset. The idempotency
// check runs before the taken check, so retries replay.
func (r *EquipmentRegister) RegisterEquipmentManual(input ManualInput) (EquipmentRecord, error) {
	if input.Key == "" || input.Company == "" {
		return EquipmentRecord{}, fmt.Errorf("%w: manual key and company are required", ErrEquipmentValidation)
	}
	if err := input.Tenant.Validate(); err != nil {
		return EquipmentRecord{}, fmt.Errorf("%w: manual tenant: %v", ErrEquipmentValidation, err)
	}
	if err := input.Owner.Validate(); err != nil || input.Owner.Tenant != input.Tenant {
		return EquipmentRecord{}, fmt.Errorf("%w: manual owner is not in the tenant", ErrEquipmentValidation)
	}
	if input.Classification == "" || input.SiteRef == "" || input.Condition == "" || input.AcquiredSource == "" || input.Evidence == "" {
		return EquipmentRecord{}, fmt.Errorf("%w: manual classification, site, condition, source and evidence are required", ErrEquipmentValidation)
	}
	if input.AcquiredAt.IsZero() {
		return EquipmentRecord{}, fmt.Errorf("%w: manual acquisition time is required", ErrEquipmentValidation)
	}
	if input.TrackInStock && (input.StockItemRef == "" || input.StockLocationRef == "") {
		return EquipmentRecord{}, fmt.Errorf("%w: tracked manual needs a stock item and location", ErrEquipmentValidation)
	}
	if !input.TrackInStock && (input.StockItemRef != "" || input.StockLocationRef != "") {
		return EquipmentRecord{}, fmt.Errorf("%w: untracked manual must not name stock references", ErrEquipmentValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.schemes[input.SchemeID]; !ok || input.SchemeID == "" {
		return EquipmentRecord{}, fmt.Errorf("%w: serial scheme %q is not registered", ErrEquipmentValidation, input.SchemeID)
	}
	normalized := normalizeEquipmentSerial(input.Serial)
	if normalized == "" {
		return EquipmentRecord{}, fmt.Errorf("%w: manual serial is blank", ErrEquipmentValidation)
	}
	full := input.SchemeID + ":" + normalized
	fingerprint := fingerprintParts(input.Key, string(input.Tenant), input.Company, input.Owner.String(),
		input.Classification, input.SchemeID, full, input.SiteRef, input.Condition,
		input.AcquiredSource, input.AcquiredAt.UTC().Format(time.RFC3339Nano), input.Evidence,
		fmt.Sprint(input.TrackInStock), input.StockItemRef, input.StockLocationRef)
	key := equipmentIdemKey(input.Tenant, input.Company, "manual", input.Key)
	if prior, ok := r.idem[key]; ok {
		if prior.Fingerprint != fingerprint {
			return EquipmentRecord{}, fmt.Errorf("%w: manual key %q was used with a different payload", ErrEquipmentConflict, input.Key)
		}
		replayed, err := r.replayIdemLocked(prior)
		if err != nil {
			return EquipmentRecord{}, err
		}
		return replayed[0], nil
	}
	if _, taken := r.bySerial[equipmentSerialKey(input.Tenant, input.Company, full)]; taken {
		return EquipmentRecord{}, fmt.Errorf("%w: serial %q is already registered", ErrEquipmentConflict, full)
	}
	record, err := r.createRecordLocked(creationInput{
		Tenant: input.Tenant, Company: input.Company, Owner: input.Owner,
		Classification: input.Classification, Serial: full,
		Site: input.SiteRef, Condition: input.Condition,
		AcquiredSource: input.AcquiredSource, EffectiveAt: input.AcquiredAt,
		TrackInStock: input.TrackInStock, StockItemRef: input.StockItemRef, StockLocationRef: input.StockLocationRef,
	})
	if err != nil {
		return EquipmentRecord{}, err
	}
	r.idem[key] = equipmentIdem{Key: input.Key, Kind: "manual", Fingerprint: fingerprint, RecordIDs: []string{record.Inventory.InventoryID.Id}}
	return *record, nil
}

type creationInput struct {
	Tenant           values.TenantId
	Company          string
	Owner            values.EntityRef
	Classification   string
	Serial           string
	Site             string
	Condition        string
	AcquiredSource   string
	EffectiveAt      time.Time
	TrackInStock     bool
	StockItemRef     string
	StockLocationRef string
}

func (r *EquipmentRegister) createRecordLocked(input creationInput) (*EquipmentRecord, error) {
	tenant, company := input.Tenant, input.Company
	r.seq++
	assetSeq := r.seq
	asset := values.EntityRef{Tenant: tenant, Kind: "asset", Id: equipmentUUID(fmt.Sprintf("equipment-asset\x00%s\x00%s\x00%d", tenant, company, assetSeq))}
	r.seq++
	invSeq := r.seq
	invRev, err := values.NewSequenceRevision("asset-inventory", invSeq)
	if err != nil {
		return nil, fmt.Errorf("%w: inventory token: %v", ErrEquipmentValidation, err)
	}
	inventory := InventoryRevision{
		InventoryID: asset, Owner: input.Owner, Classification: input.Classification,
		SerialNumber: input.Serial, Revision: invRev, EffectiveAt: input.EffectiveAt, Status: Available,
	}
	if err := inventory.Validate(); err != nil {
		return nil, fmt.Errorf("%w: inventory: %v", ErrEquipmentValidation, err)
	}
	if err := r.store.Register(inventory); err != nil {
		return nil, err
	}
	record := &EquipmentRecord{
		Inventory: inventory, Company: company, SiteRef: input.Site, Condition: input.Condition,
		AcquiredSource: input.AcquiredSource, AssetSeq: assetSeq, InventorySeq: invSeq,
		TrackInStock: input.TrackInStock, StockItemRef: input.StockItemRef, StockLocationRef: input.StockLocationRef,
		LinkState: StockLinkNone, Verification: VerificationVerified, Revision: 1,
	}
	recordKey := equipmentRecordKey(tenant, company, asset.Id)
	r.records[recordKey] = record
	r.bySerial[equipmentSerialKey(tenant, company, input.Serial)] = recordKey
	return record, nil
}

func (r *EquipmentRegister) replayIdemLocked(prior equipmentIdem) ([]EquipmentRecord, error) {
	records := make([]EquipmentRecord, 0, len(prior.RecordIDs))
	for _, id := range prior.RecordIDs {
		found := false
		for _, record := range r.records {
			if record.Inventory.InventoryID.Id == id {
				records = append(records, *record)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: idempotency record references an unknown asset", ErrEquipmentConflict)
		}
	}
	return records, nil
}

// LinkRequestInput asks to link one asset to its stock item and
// location. The request may carry the references for an asset-only
// record; a tracked record reuses its own. Linking is never terminal:
// the record waits PENDING until reconciled.
type LinkRequestInput struct {
	Key              string
	Asset            values.EntityRef
	ExpectedRevision uint64
	StockItemRef     string
	StockLocationRef string
	Evidence         string
	Actor            string
	At               time.Time
}

// RequestStockLink moves one asset to PENDING linkage. The expected
// revision is a compare-and-set boundary: a stale caller retries
// against the current record instead of silently winning. The
// idempotency check runs before the revision and state checks, so a
// retried request replays instead of conflicting with its own effect.
func (r *EquipmentRegister) RequestStockLink(input LinkRequestInput) (EquipmentRecord, error) {
	if input.Key == "" || input.Evidence == "" || input.Actor == "" {
		return EquipmentRecord{}, fmt.Errorf("%w: link key, evidence and actor are required", ErrEquipmentValidation)
	}
	if input.At.IsZero() {
		return EquipmentRecord{}, fmt.Errorf("%w: link request time is required", ErrEquipmentValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.getRecordLocked(input.Asset.Tenant, input.Asset)
	if err != nil {
		return EquipmentRecord{}, err
	}
	itemRef, locationRef := record.StockItemRef, record.StockLocationRef
	if !record.TrackInStock {
		if input.StockItemRef == "" || input.StockLocationRef == "" {
			return EquipmentRecord{}, fmt.Errorf("%w: untracked asset needs stock references on the link request", ErrEquipmentValidation)
		}
		itemRef, locationRef = input.StockItemRef, input.StockLocationRef
	} else if (input.StockItemRef != "" && input.StockItemRef != record.StockItemRef) ||
		(input.StockLocationRef != "" && input.StockLocationRef != record.StockLocationRef) {
		return EquipmentRecord{}, fmt.Errorf("%w: link request disagrees with the tracked stock references", ErrEquipmentValidation)
	}
	fingerprint := fingerprintParts(input.Asset.String(), fmt.Sprint(input.ExpectedRevision), itemRef, locationRef, input.Evidence, input.Actor, input.At.UTC().Format(time.RFC3339Nano))
	key := equipmentIdemKey(input.Asset.Tenant, record.Company, "link-request", input.Key)
	if prior, ok := r.idem[key]; ok {
		if prior.Fingerprint != fingerprint {
			return EquipmentRecord{}, fmt.Errorf("%w: link key %q was used with a different payload", ErrEquipmentConflict, input.Key)
		}
		replayed, err := r.replayIdemLocked(prior)
		if err != nil {
			return EquipmentRecord{}, err
		}
		return replayed[0], nil
	}
	if input.ExpectedRevision != record.Revision {
		return EquipmentRecord{}, ErrRevisionConflict
	}
	if record.LinkState != StockLinkNone {
		return EquipmentRecord{}, fmt.Errorf("%w: asset is already %s-linked", ErrEquipmentConflict, record.LinkState)
	}
	record.TrackInStock = true
	record.StockItemRef = itemRef
	record.StockLocationRef = locationRef
	record.LinkState = StockLinkPending
	record.LinkEvidence = input.Evidence
	record.LinkRequestedBy = input.Actor
	record.LinkSteps = 1
	record.Revision++
	r.idem[key] = equipmentIdem{Key: input.Key, Kind: "link-request", Fingerprint: fingerprint, RecordIDs: []string{record.Inventory.InventoryID.Id}}
	return *record, nil
}

// LinkReconcileInput completes a PENDING linkage with the authoritative
// source receipt and custody evidence.
type LinkReconcileInput struct {
	Key              string
	Asset            values.EntityRef
	ExpectedRevision uint64
	SourceReceipt    string
	CustodyEvidence  string
	Actor            string
	At               time.Time
}

// ReconcileStockLink moves one PENDING asset to LINKED. Only a pending
// record at the expected revision reconciles; anything else refuses,
// and the repair path after a crash completes the original IDs. The
// idempotency check runs first, so a retried reconcile replays.
func (r *EquipmentRegister) ReconcileStockLink(input LinkReconcileInput) (EquipmentRecord, error) {
	if input.Key == "" || input.SourceReceipt == "" || input.CustodyEvidence == "" || input.Actor == "" {
		return EquipmentRecord{}, fmt.Errorf("%w: reconcile key, source receipt, custody evidence and actor are required", ErrEquipmentValidation)
	}
	if input.At.IsZero() {
		return EquipmentRecord{}, fmt.Errorf("%w: reconcile time is required", ErrEquipmentValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.getRecordLocked(input.Asset.Tenant, input.Asset)
	if err != nil {
		return EquipmentRecord{}, err
	}
	fingerprint := fingerprintParts(input.Asset.String(), fmt.Sprint(input.ExpectedRevision), input.SourceReceipt, input.CustodyEvidence, input.Actor, input.At.UTC().Format(time.RFC3339Nano))
	key := equipmentIdemKey(input.Asset.Tenant, record.Company, "link-reconcile", input.Key)
	if prior, ok := r.idem[key]; ok {
		if prior.Fingerprint != fingerprint {
			return EquipmentRecord{}, fmt.Errorf("%w: reconcile key %q was used with a different payload", ErrEquipmentConflict, input.Key)
		}
		replayed, err := r.replayIdemLocked(prior)
		if err != nil {
			return EquipmentRecord{}, err
		}
		return replayed[0], nil
	}
	if input.ExpectedRevision != record.Revision {
		return EquipmentRecord{}, ErrRevisionConflict
	}
	if record.LinkState != StockLinkPending {
		return EquipmentRecord{}, fmt.Errorf("%w: asset link is %s, want PENDING", ErrEquipmentConflict, record.LinkState)
	}
	record.LinkState = StockLinkLinked
	record.LinkEvidence = input.SourceReceipt
	record.LinkSteps = 2
	record.Revision++
	r.idem[key] = equipmentIdem{Key: input.Key, Kind: "link-reconcile", Fingerprint: fingerprint, RecordIDs: []string{record.Inventory.InventoryID.Id}}
	return *record, nil
}

// ImportSourceKey is the domain identity of one staged import row: the
// source system, company, record kind and external key. Uniqueness is
// enforced on this key, so one legacy row never becomes two assets.
type ImportSourceKey struct {
	SourceSystem string `json:"source_system"`
	Company      string `json:"company"`
	RecordKind   string `json:"record_kind"`
	ExternalKey  string `json:"external_key"`
}

func (k ImportSourceKey) valid() bool {
	return k.SourceSystem != "" && k.Company != "" && k.RecordKind != "" && k.ExternalKey != ""
}

// receipt is the original-receipt token an explicit correction must
// present: the source system and external key joined by a colon.
func (k ImportSourceKey) receipt() string {
	return k.SourceSystem + ":" + k.ExternalKey
}

// EquipmentImportRow is one staged import row: identity, version, the
// equipment facts, and the optional worker assignment with its custody
// evidence. A set worker without custody evidence imports as a pending
// assignment that asserts no custody.
type EquipmentImportRow struct {
	RowID           string          `json:"row_id"`
	Source          ImportSourceKey `json:"source"`
	SourceVersion   string          `json:"source_version"`
	Serial          string          `json:"serial"`
	Scheme          string          `json:"scheme"`
	Classification  string          `json:"classification"`
	Site            string          `json:"site"`
	Condition       string          `json:"condition"`
	Worker          string          `json:"worker,omitempty"`
	CustodyEvidence string          `json:"custody_evidence,omitempty"`
}

// Draft write properties for the equipment.register intent. The
// committer refuses unknown properties fail-closed.
const (
	equipmentPropRowID           = "row_id"
	equipmentPropSourceSystem    = "source_system"
	equipmentPropCompany         = "company"
	equipmentPropRecordKind      = "record_kind"
	equipmentPropExternalKey     = "external_key"
	equipmentPropSourceVersion   = "source_version"
	equipmentPropSerial          = "serial"
	equipmentPropScheme          = "scheme"
	equipmentPropClassification  = "classification"
	equipmentPropSite            = "site"
	equipmentPropCondition       = "condition"
	equipmentPropWorker          = "worker"
	equipmentPropCustodyEvidence = "custody_evidence"
)

// EquipmentIntentType is the business-intent type the committer accepts.
const EquipmentIntentType = "equipment.register"

// DraftWrites renders the row as the equipment.register intent writes.
func (row EquipmentImportRow) DraftWrites() []importing.DraftWrite {
	write := func(property, proposed string) importing.DraftWrite {
		return importing.DraftWrite{Property: property, Proposed: proposed}
	}
	return []importing.DraftWrite{
		write(equipmentPropRowID, row.RowID),
		write(equipmentPropSourceSystem, row.Source.SourceSystem),
		write(equipmentPropCompany, row.Source.Company),
		write(equipmentPropRecordKind, row.Source.RecordKind),
		write(equipmentPropExternalKey, row.Source.ExternalKey),
		write(equipmentPropSourceVersion, row.SourceVersion),
		write(equipmentPropSerial, row.Serial),
		write(equipmentPropScheme, row.Scheme),
		write(equipmentPropClassification, row.Classification),
		write(equipmentPropSite, row.Site),
		write(equipmentPropCondition, row.Condition),
		write(equipmentPropWorker, row.Worker),
		write(equipmentPropCustodyEvidence, row.CustodyEvidence),
	}
}

func decodeDraftRow(draft importing.BusinessIntentDraft) (EquipmentImportRow, error) {
	var row EquipmentImportRow
	seen := make(map[string]bool, len(draft.Writes))
	for _, write := range draft.Writes {
		if seen[write.Property] {
			return EquipmentImportRow{}, fmt.Errorf("%w: intent repeats property %q", ErrEquipmentValidation, write.Property)
		}
		seen[write.Property] = true
		switch write.Property {
		case equipmentPropRowID:
			row.RowID = write.Proposed
		case equipmentPropSourceSystem:
			row.Source.SourceSystem = write.Proposed
		case equipmentPropCompany:
			row.Source.Company = write.Proposed
		case equipmentPropRecordKind:
			row.Source.RecordKind = write.Proposed
		case equipmentPropExternalKey:
			row.Source.ExternalKey = write.Proposed
		case equipmentPropSourceVersion:
			row.SourceVersion = write.Proposed
		case equipmentPropSerial:
			row.Serial = write.Proposed
		case equipmentPropScheme:
			row.Scheme = write.Proposed
		case equipmentPropClassification:
			row.Classification = write.Proposed
		case equipmentPropSite:
			row.Site = write.Proposed
		case equipmentPropCondition:
			row.Condition = write.Proposed
		case equipmentPropWorker:
			row.Worker = write.Proposed
		case equipmentPropCustodyEvidence:
			row.CustodyEvidence = write.Proposed
		default:
			return EquipmentImportRow{}, fmt.Errorf("%w: intent property %q is unknown", ErrEquipmentValidation, write.Property)
		}
	}
	if row.RowID == "" || row.RowID != draft.RowID {
		return EquipmentImportRow{}, fmt.Errorf("%w: intent row binding is malformed", ErrEquipmentValidation)
	}
	return row, nil
}

// ImportVerdictStatus is the per-row staging answer.
type ImportVerdictStatus string

const (
	// VerdictAccept admits the row for commit.
	VerdictAccept ImportVerdictStatus = "ACCEPT"
	// VerdictReject refuses the row with reasons.
	VerdictReject ImportVerdictStatus = "REJECT"
)

// EquipmentVerdict is the staging verdict for one row.
type EquipmentVerdict struct {
	RowID  string
	Status ImportVerdictStatus
	Errors []string
}

func parseSourceVersion(raw string) (uint64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, fmt.Errorf("%w: source version is blank", ErrEquipmentValidation)
	}
	var parsed uint64
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] < '0' || trimmed[i] > '9' {
			return 0, fmt.Errorf("%w: source version %q is not a positive integer", ErrEquipmentValidation, raw)
		}
		parsed = parsed*10 + uint64(trimmed[i]-'0')
	}
	if parsed == 0 {
		return 0, fmt.Errorf("%w: source version %q is not a positive integer", ErrEquipmentValidation, raw)
	}
	return parsed, nil
}

func hashImportPayload(row EquipmentImportRow) string {
	return fingerprintParts(row.RowID, row.Source.SourceSystem, row.Source.Company, row.Source.RecordKind,
		row.Source.ExternalKey, row.SourceVersion, row.Serial, row.Scheme, row.Classification,
		row.Site, row.Condition, row.Worker, row.CustodyEvidence)
}

// ValidateEquipmentImport stages every row without writing anything:
// per-row accept or reject with reasons. An invalid tenant rejects the
// whole batch; nothing half-stages.
func (r *EquipmentRegister) ValidateEquipmentImport(tenant values.TenantId, rows []EquipmentImportRow) []EquipmentVerdict {
	verdicts := make([]EquipmentVerdict, 0, len(rows))
	if err := tenant.Validate(); err != nil {
		for _, row := range rows {
			verdicts = append(verdicts, EquipmentVerdict{RowID: row.RowID, Status: VerdictReject, Errors: []string{fmt.Sprintf("tenant: %v", err)}})
		}
		return verdicts
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range rows {
		verdicts = append(verdicts, r.verdictForRowLocked(tenant, row))
	}
	return verdicts
}

func (r *EquipmentRegister) verdictForRowLocked(tenant values.TenantId, row EquipmentImportRow) EquipmentVerdict {
	verdict := EquipmentVerdict{RowID: row.RowID, Status: VerdictAccept}
	reject := func(reason string) EquipmentVerdict {
		verdict.Status = VerdictReject
		verdict.Errors = append(verdict.Errors, reason)
		return verdict
	}
	if row.RowID == "" {
		return reject("row id is required")
	}
	if !row.Source.valid() {
		return reject("source system, company, record kind and external key are required")
	}
	if _, err := parseSourceVersion(row.SourceVersion); err != nil {
		return reject(err.Error())
	}
	if _, ok := r.schemes[row.Scheme]; !ok || row.Scheme == "" {
		return reject(fmt.Sprintf("serial scheme %q is not registered", row.Scheme))
	}
	if normalizeEquipmentSerial(row.Serial) == "" {
		return reject("serial is blank")
	}
	if row.Classification == "" || row.Site == "" || row.Condition == "" {
		return reject("classification, site and condition are required")
	}
	worker := strings.TrimSpace(row.Worker)
	if worker == "" && row.CustodyEvidence != "" {
		return reject("custody evidence without a worker is meaningless")
	}
	if worker != "" {
		workerRef := values.EntityRef{Tenant: tenant, Kind: "worker", Id: worker}
		if err := requireKind(workerRef, "worker", "worker"); err != nil {
			return reject(fmt.Sprintf("worker: %v", err))
		}
		if r.workers == nil || !r.workers(tenant, workerRef) {
			return reject(fmt.Sprintf("worker %q is not a valid worker", worker))
		}
	}
	return verdict
}

// EquipmentCommitter commits approved equipment.register intents into
// the register with source-key uniqueness and idempotent replay.
type EquipmentCommitter struct {
	register *EquipmentRegister
}

// NewEquipmentCommitter binds a committer to its register.
func NewEquipmentCommitter(register *EquipmentRegister) *EquipmentCommitter {
	return &EquipmentCommitter{register: register}
}

// CommitItem commits one approved intent row. The same idempotency key
// with the same payload replays the original lineage; a changed
// payload, a duplicate serial, or a stale version is a conflict, and a
// newer version must arrive through CorrectEquipmentImport, never here.
func (c *EquipmentCommitter) CommitItem(ctx context.Context, req importing.ItemRequest) (importing.ItemLineage, error) {
	if err := ctx.Err(); err != nil {
		return importing.ItemLineage{}, err
	}
	tenant := values.TenantId(req.Tenant)
	if err := tenant.Validate(); err != nil {
		return importing.ItemLineage{}, fmt.Errorf("%w: commit tenant: %v", ErrEquipmentValidation, err)
	}
	if req.ImportID == "" || req.SimulationDigest == "" || req.IdempotencyKey == "" {
		return importing.ItemLineage{}, fmt.Errorf("%w: import id, simulation digest and idempotency key are required", ErrEquipmentValidation)
	}
	if req.Draft.RowID == "" || req.Draft.IntentType == "" || req.Draft.Status == "" {
		return importing.ItemLineage{}, fmt.Errorf("%w: intent row, type and status are required", ErrEquipmentValidation)
	}
	if req.Draft.IntentType != EquipmentIntentType {
		return importing.ItemLineage{}, fmt.Errorf("%w: intent type %q is not %q", ErrEquipmentValidation, req.Draft.IntentType, EquipmentIntentType)
	}
	if req.Draft.Status != "CREATE" {
		return importing.ItemLineage{}, fmt.Errorf("%w: intent status %q is not CREATE", ErrEquipmentValidation, req.Draft.Status)
	}
	row, err := decodeDraftRow(req.Draft)
	if err != nil {
		return importing.ItemLineage{}, err
	}
	register := c.register
	register.mu.Lock()
	defer register.mu.Unlock()
	verdict := register.verdictForRowLocked(tenant, row)
	if verdict.Status != VerdictAccept {
		return importing.ItemLineage{}, fmt.Errorf("%w: row %q: %s", ErrEquipmentValidation, row.RowID, strings.Join(verdict.Errors, "; "))
	}
	version, _ := parseSourceVersion(row.SourceVersion)
	company := row.Source.Company
	fingerprint := hashImportPayload(row)
	key := equipmentIdemKey(tenant, company, "commit", req.IdempotencyKey)
	if prior, ok := register.idem[key]; ok {
		if prior.Fingerprint != fingerprint {
			return importing.ItemLineage{}, fmt.Errorf("%w: commit key was used with a different payload", ErrEquipmentConflict)
		}
		return register.commitLineageLocked(prior, req.IdempotencyKey)
	}
	sourceKey := equipmentSourceKey(tenant, row.Source)
	if entry, seen := register.bySource[sourceKey]; seen {
		record, ok := register.records[entry.RecordKey]
		if !ok {
			return importing.ItemLineage{}, fmt.Errorf("%w: source entry references an unknown record", ErrEquipmentConflict)
		}
		switch {
		case version == entry.AdmittedVersion && entry.PayloadHash == fingerprint:
			register.idem[key] = equipmentIdem{Key: req.IdempotencyKey, Kind: "commit", Fingerprint: fingerprint, RecordIDs: []string{record.Inventory.InventoryID.Id}}
			return commitLineageFor(record.Inventory.InventoryID, req.IdempotencyKey), nil
		case version == entry.AdmittedVersion:
			return importing.ItemLineage{}, fmt.Errorf("%w: source %q version %d payload changed; correct it explicitly", ErrEquipmentConflict, row.Source.ExternalKey, version)
		case version < entry.AdmittedVersion:
			return importing.ItemLineage{}, fmt.Errorf("%w: source %q version %d is superseded by version %d", ErrEquipmentConflict, row.Source.ExternalKey, version, entry.AdmittedVersion)
		default:
			return importing.ItemLineage{}, fmt.Errorf("%w: source %q version %d is newer than admitted %d; use CorrectEquipmentImport", ErrEquipmentValidation, row.Source.ExternalKey, version, entry.AdmittedVersion)
		}
	}
	normalized := normalizeEquipmentSerial(row.Serial)
	full := row.Scheme + ":" + normalized
	if _, taken := register.bySerial[equipmentSerialKey(tenant, company, full)]; taken {
		return importing.ItemLineage{}, fmt.Errorf("%w: serial %q is already registered", ErrEquipmentConflict, full)
	}
	committedAt := time.Now().UTC()
	record, err := register.createRecordLocked(creationInput{
		Tenant: tenant, Company: company, Owner: equipmentOrgRef(tenant, company),
		Classification: row.Classification, Serial: full, Site: row.Site, Condition: row.Condition,
		AcquiredSource: row.Source.SourceSystem, EffectiveAt: committedAt,
	})
	if err != nil {
		return importing.ItemLineage{}, err
	}
	worker := strings.TrimSpace(row.Worker)
	switch {
	case worker == "":
		record.Verification = VerificationVerified
	case row.CustodyEvidence == "":
		workerRef := values.EntityRef{Tenant: tenant, Kind: "worker", Id: worker}
		record.PendingWorker = &workerRef
		record.Verification = VerificationPending
		record.VerificationNote = fmt.Sprintf("assignment to %s awaits verified receipts", worker)
	default:
		workerRef := values.EntityRef{Tenant: tenant, Kind: "worker", Id: worker}
		recordKey := equipmentRecordKey(tenant, company, record.Inventory.InventoryID.Id)
		revision, err := values.NewSequenceRevision("asset-custody", 1)
		if err != nil {
			return importing.ItemLineage{}, fmt.Errorf("%w: custody token: %v", ErrEquipmentValidation, err)
		}
		assignee := Receipt{
			ID:     values.EntityRef{Tenant: tenant, Kind: "receipt", Id: equipmentUUID("equipment-receipt\x00" + recordKey + "\x00assign")},
			Issuer: record.Inventory.Owner, IssuedAt: committedAt, Verified: true, Evidence: row.CustodyEvidence,
		}
		issuer := Receipt{
			ID:     values.EntityRef{Tenant: tenant, Kind: "receipt", Id: equipmentUUID("equipment-receipt\x00" + recordKey + "\x00issue")},
			Issuer: record.Inventory.Owner, IssuedAt: committedAt, Verified: true, Evidence: row.CustodyEvidence,
		}
		next := CustodyRevision{
			Asset: record.Inventory.InventoryID, Worker: workerRef, Location: row.Site, Condition: row.Condition,
			AssigneeReceipt: assignee, IssuerReceipt: issuer, Revision: revision, EffectiveAt: committedAt, Status: Assigned,
		}
		if err := register.store.Apply(record.Inventory.InventoryID, record.Inventory.Revision, next); err != nil {
			return importing.ItemLineage{}, err
		}
		record.CustodyLog = append(record.CustodyLog, CustodyEvent{
			Worker: workerRef, Location: row.Site, Condition: row.Condition, Assignee: assignee, Issuer: issuer,
			ExpectedStream: record.Inventory.Revision.Stream(), ExpectedSeq: tokenSeq(record.Inventory.Revision),
			RevisionStream: revision.Stream(), RevisionSeq: tokenSeq(revision), At: committedAt,
		})
		record.Verification = VerificationVerified
	}
	record.Source = row.Source
	record.SourceVersion = row.SourceVersion
	register.bySource[sourceKey] = &sourceEntry{
		RecordKey:       equipmentRecordKey(tenant, company, record.Inventory.InventoryID.Id),
		AdmittedVersion: version, PayloadHash: fingerprint,
		OriginalReceipt: row.Source.receipt(), AdmittedRow: row,
	}
	register.idem[key] = equipmentIdem{Key: req.IdempotencyKey, Kind: "commit", Fingerprint: fingerprint, RecordIDs: []string{record.Inventory.InventoryID.Id}}
	return commitLineageFor(record.Inventory.InventoryID, req.IdempotencyKey), nil
}

func commitLineageFor(asset values.EntityRef, idempotencyKey string) importing.ItemLineage {
	return importing.ItemLineage{
		TransactionRef: asset.String(), ObservationRef: idempotencyKey,
		ReconciliationStatus: importing.ReconciliationMatched,
	}
}

func (r *EquipmentRegister) commitLineageLocked(prior equipmentIdem, idempotencyKey string) (importing.ItemLineage, error) {
	replayed, err := r.replayIdemLocked(prior)
	if err != nil {
		return importing.ItemLineage{}, err
	}
	return commitLineageFor(replayed[0].Inventory.InventoryID, idempotencyKey), nil
}

// CorrectionInput explicitly corrects one admitted import row to a newer
// version. The correction presents the original receipt token and the
// full corrected row; the source identity never changes, only its facts.
type CorrectionInput struct {
	Source          ImportSourceKey
	NewVersion      string
	OriginalReceipt string
	Evidence        string
	Actor           string
	Row             EquipmentImportRow
	At              time.Time
}

// CorrectEquipmentImport admits a newer version of one imported row.
// The row must still stage clean, the new version must be newer than
// admitted, and the original receipt must match. Corrections update the
// original asset in place; they never mint a duplicate.
func (r *EquipmentRegister) CorrectEquipmentImport(input CorrectionInput) (EquipmentRecord, error) {
	if !input.Source.valid() {
		return EquipmentRecord{}, fmt.Errorf("%w: correction source is incomplete", ErrEquipmentValidation)
	}
	newVersion, err := parseSourceVersion(input.NewVersion)
	if err != nil {
		return EquipmentRecord{}, err
	}
	if input.OriginalReceipt == "" || input.Evidence == "" || input.Actor == "" {
		return EquipmentRecord{}, fmt.Errorf("%w: correction receipt, evidence and actor are required", ErrEquipmentValidation)
	}
	if input.At.IsZero() {
		return EquipmentRecord{}, fmt.Errorf("%w: correction time is required", ErrEquipmentValidation)
	}
	if input.Row.Source != input.Source {
		return EquipmentRecord{}, fmt.Errorf("%w: corrected row changes the source identity", ErrEquipmentValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var entry *sourceEntry
	var matches int
	for _, candidate := range r.bySource {
		record, ok := r.records[candidate.RecordKey]
		if !ok || record.Source != input.Source {
			continue
		}
		matches++
		entry = candidate
	}
	if matches == 0 {
		return EquipmentRecord{}, fmt.Errorf("%w: source %q was never admitted", ErrUnknownInventory, input.Source.ExternalKey)
	}
	if matches > 1 {
		return EquipmentRecord{}, fmt.Errorf("%w: source %q is ambiguous across tenants", ErrEquipmentConflict, input.Source.ExternalKey)
	}
	record, ok := r.records[entry.RecordKey]
	if !ok {
		return EquipmentRecord{}, fmt.Errorf("%w: source entry references an unknown record", ErrEquipmentConflict)
	}
	tenant := record.Inventory.InventoryID.Tenant
	if input.OriginalReceipt != input.Source.receipt() || entry.OriginalReceipt != input.Source.receipt() {
		return EquipmentRecord{}, fmt.Errorf("%w: original receipt does not match", ErrEquipmentValidation)
	}
	if newVersion <= entry.AdmittedVersion {
		return EquipmentRecord{}, fmt.Errorf("%w: version %d is not newer than admitted %d", ErrEquipmentConflict, newVersion, entry.AdmittedVersion)
	}
	rowVersion, err := parseSourceVersion(input.Row.SourceVersion)
	if err != nil || rowVersion != entry.AdmittedVersion {
		return EquipmentRecord{}, fmt.Errorf("%w: corrected row must carry admitted version %d", ErrEquipmentConflict, entry.AdmittedVersion)
	}
	verdict := r.verdictForRowLocked(tenant, input.Row)
	if verdict.Status != VerdictAccept {
		return EquipmentRecord{}, fmt.Errorf("%w: corrected row: %s", ErrEquipmentValidation, strings.Join(verdict.Errors, "; "))
	}
	normalized := normalizeEquipmentSerial(input.Row.Serial)
	full := input.Row.Scheme + ":" + normalized
	if full != record.Inventory.SerialNumber {
		if _, taken := r.bySerial[equipmentSerialKey(tenant, record.Company, full)]; taken {
			return EquipmentRecord{}, fmt.Errorf("%w: serial %q is already registered", ErrEquipmentConflict, full)
		}
		delete(r.bySerial, equipmentSerialKey(tenant, record.Company, record.Inventory.SerialNumber))
		r.bySerial[equipmentSerialKey(tenant, record.Company, full)] = equipmentRecordKey(tenant, record.Company, record.Inventory.InventoryID.Id)
		record.Inventory.SerialNumber = full
	}
	worker := strings.TrimSpace(input.Row.Worker)
	if current, assigned := r.store.Current(record.Inventory.InventoryID); assigned && current.Status == Assigned {
		if worker == "" || worker != current.Worker.Id {
			return EquipmentRecord{}, fmt.Errorf("%w: correction must not move an assigned worker", ErrEquipmentValidation)
		}
	} else if worker == "" {
		record.PendingWorker = nil
		record.Verification = VerificationVerified
		record.VerificationNote = ""
	} else {
		workerRef := values.EntityRef{Tenant: tenant, Kind: "worker", Id: worker}
		record.PendingWorker = &workerRef
		record.Verification = VerificationPending
		record.VerificationNote = fmt.Sprintf("assignment to %s awaits verified receipts", worker)
	}
	record.Inventory.Classification = input.Row.Classification
	record.SiteRef = input.Row.Site
	record.Condition = input.Row.Condition
	record.SourceVersion = input.NewVersion
	record.CorrectionCount++
	record.Revision++
	record.CorrectionEvidence = input.Evidence
	record.CorrectionActor = input.Actor
	record.CorrectionAt = input.At
	admitted := input.Row
	admitted.SourceVersion = input.NewVersion
	entry.AdmittedVersion = newVersion
	entry.PayloadHash = hashImportPayload(admitted)
	entry.AdmittedRow = admitted
	return *record, nil
}

// AssignEquipment assigns one asset through the canonical custody
// boundary and journals the assignment for restart replay. Linking
// never gates it: pending links block only inventory-derived
// availability, never lawful asset operations.
func (r *EquipmentRegister) AssignEquipment(asset, worker values.EntityRef, location, condition string, assignee, issuer Receipt, expected, revision values.RevisionToken, at time.Time) error {
	if err := asset.Validate(); err != nil {
		return ErrTenantBoundary
	}
	if err := requireKind(worker, "worker", "worker"); err != nil {
		return fmt.Errorf("%w: %v", ErrEquipmentValidation, err)
	}
	if location == "" || condition == "" {
		return fmt.Errorf("%w: assignment location and condition are required", ErrEquipmentValidation)
	}
	if err := assignee.Validate(); err != nil {
		return fmt.Errorf("%w: assignee receipt: %v", ErrEquipmentValidation, err)
	}
	if err := issuer.Validate(); err != nil {
		return fmt.Errorf("%w: issuer receipt: %v", ErrEquipmentValidation, err)
	}
	if at.IsZero() {
		return fmt.Errorf("%w: assignment time is required", ErrEquipmentValidation)
	}
	if expected.IsSpecified() {
		if _, _, err := sequenceToken(expected, "expected"); err != nil {
			return err
		}
	}
	if _, _, err := sequenceToken(revision, "custody"); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.getRecordLocked(asset.Tenant, asset)
	if err != nil {
		return err
	}
	if r.workers == nil || !r.workers(asset.Tenant, worker) {
		return fmt.Errorf("%w: worker is not valid", ErrEquipmentValidation)
	}
	next := CustodyRevision{
		Asset: asset, Worker: worker, Location: location, Condition: condition,
		AssigneeReceipt: assignee, IssuerReceipt: issuer, Revision: revision, EffectiveAt: at, Status: Assigned,
	}
	if err := r.store.Apply(asset, expected, next); err != nil {
		return err
	}
	event := CustodyEvent{
		Worker: worker, Location: location, Condition: condition, Assignee: assignee, Issuer: issuer,
		RevisionStream: revision.Stream(), RevisionSeq: tokenSeq(revision), At: at,
	}
	if expected.IsSpecified() {
		event.ExpectedStream = expected.Stream()
		event.ExpectedSeq = tokenSeq(expected)
	}
	record.CustodyLog = append(record.CustodyLog, event)
	return nil
}

// VerifyImportedAssignment completes a pending imported assignment with
// real verified receipts. Unverified receipts refuse: an unverified
// import never asserts custody.
func (r *EquipmentRegister) VerifyImportedAssignment(asset values.EntityRef, expectedRevision uint64, worker values.EntityRef, assignee, issuer Receipt) error {
	if err := asset.Validate(); err != nil {
		return ErrTenantBoundary
	}
	if err := requireKind(worker, "worker", "worker"); err != nil {
		return fmt.Errorf("%w: %v", ErrEquipmentValidation, err)
	}
	if err := assignee.Validate(); err != nil {
		return fmt.Errorf("%w: assignee receipt: %v", ErrEquipmentValidation, err)
	}
	if err := issuer.Validate(); err != nil {
		return fmt.Errorf("%w: issuer receipt: %v", ErrEquipmentValidation, err)
	}
	if !assignee.Verified || !issuer.Verified {
		return fmt.Errorf("%w: assignment receipts are not verified", ErrEquipmentValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.getRecordLocked(asset.Tenant, asset)
	if err != nil {
		return err
	}
	if expectedRevision != record.Revision {
		return ErrRevisionConflict
	}
	if record.Verification != VerificationPending || record.PendingWorker == nil || *record.PendingWorker != worker {
		return fmt.Errorf("%w: no matching pending assignment", ErrEquipmentValidation)
	}
	if r.workers == nil || !r.workers(asset.Tenant, worker) {
		return fmt.Errorf("%w: worker is not valid", ErrEquipmentValidation)
	}
	revision, err := values.NewSequenceRevision("asset-custody", uint64(len(record.CustodyLog)+1))
	if err != nil {
		return fmt.Errorf("%w: custody token: %v", ErrEquipmentValidation, err)
	}
	next := CustodyRevision{
		Asset: asset, Worker: worker, Location: record.SiteRef, Condition: record.Condition,
		AssigneeReceipt: assignee, IssuerReceipt: issuer, Revision: revision, EffectiveAt: assignee.IssuedAt, Status: Assigned,
	}
	if err := r.store.Apply(asset, record.Inventory.Revision, next); err != nil {
		return err
	}
	record.CustodyLog = append(record.CustodyLog, CustodyEvent{
		Worker: worker, Location: record.SiteRef, Condition: record.Condition, Assignee: assignee, Issuer: issuer,
		ExpectedStream: record.Inventory.Revision.Stream(), ExpectedSeq: tokenSeq(record.Inventory.Revision),
		RevisionStream: revision.Stream(), RevisionSeq: tokenSeq(revision), At: assignee.IssuedAt,
	})
	record.PendingWorker = nil
	record.Verification = VerificationVerified
	record.VerificationNote = ""
	record.Revision++
	return nil
}

// CustodyOf returns the authoritative custody revision for an asset.
func (r *EquipmentRegister) CustodyOf(asset values.EntityRef) (CustodyRevision, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.Current(asset)
}

// GetEquipment returns one record or the package boundary error.
func (r *EquipmentRegister) GetEquipment(tenant values.TenantId, company string, asset values.EntityRef) (EquipmentRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := r.getRecordLocked(tenant, asset)
	if err != nil {
		return EquipmentRecord{}, err
	}
	if record.Company != company {
		return EquipmentRecord{}, ErrTenantBoundary
	}
	return *record, nil
}

// GetEquipmentBySource resolves one record by its import identity.
func (r *EquipmentRegister) GetEquipmentBySource(tenant values.TenantId, key ImportSourceKey) (EquipmentRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.bySource[equipmentSourceKey(tenant, key)]
	if !ok {
		return EquipmentRecord{}, ErrUnknownInventory
	}
	record, ok := r.records[entry.RecordKey]
	if !ok {
		return EquipmentRecord{}, ErrUnknownInventory
	}
	return *record, nil
}

// EquipmentFilter scopes and narrows equipment reads.
type EquipmentFilter struct {
	Site                  string
	Search                string
	NeedsVerificationOnly bool
	AssignedOnly          bool
}

// ListEquipment lists records in one tenant/company scope with filters.
func (r *EquipmentRegister) ListEquipment(tenant values.TenantId, company string, filter EquipmentFilter) []EquipmentRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []EquipmentRecord
	for key, record := range r.records {
		if !strings.HasPrefix(key, string(tenant)+"\x00"+company+"\x00") {
			continue
		}
		if filter.Site != "" && record.SiteRef != filter.Site {
			continue
		}
		if filter.NeedsVerificationOnly && record.Verification != VerificationPending {
			continue
		}
		if filter.AssignedOnly {
			current, ok := r.store.Current(record.Inventory.InventoryID)
			if !ok || current.Status != Assigned {
				continue
			}
		}
		if want := strings.ToLower(strings.TrimSpace(filter.Search)); want != "" {
			haystack := strings.ToLower(record.Inventory.SerialNumber + " " + record.Inventory.InventoryID.Id + " " + record.Inventory.Classification)
			if !strings.Contains(haystack, want) {
				continue
			}
		}
		out = append(out, *record)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Inventory.InventoryID.Id < out[j].Inventory.InventoryID.Id
	})
	return out
}

// EquipmentAvailability is the assignment answer for one record:
// assignability follows inventory status only, so pending links never
// block lawful asset operations.
type EquipmentAvailability struct {
	Asset       values.EntityRef
	Assignable  bool
	Custody     string
	StockState  StockLinkState
	NeedsVerify bool
}

// EquipmentAvailability derives the availability view for one record.
func (r *EquipmentRegister) EquipmentAvailability(record EquipmentRecord) EquipmentAvailability {
	r.mu.Lock()
	defer r.mu.Unlock()
	view := EquipmentAvailability{
		Asset: record.Inventory.InventoryID, Assignable: record.Inventory.Status == Available,
		StockState: record.LinkState, NeedsVerify: record.Verification == VerificationPending,
	}
	if current, ok := r.store.Current(record.Inventory.InventoryID); ok {
		view.Custody = string(current.Status)
	} else {
		view.Custody = "UNASSIGNED"
	}
	return view
}

// EquipmentListRow is one searchable equipment row.
type EquipmentListRow struct {
	AssetID     string `json:"asset_id"`
	Name        string `json:"name"`
	AssetTag    string `json:"asset_tag"`
	Serial      string `json:"serial"`
	Location    string `json:"location"`
	Custodian   string `json:"custodian"`
	Condition   string `json:"condition"`
	StockState  string `json:"stock_state"`
	StatusText  string `json:"status_text"`
	StatusOwner string `json:"status_owner"`
}

// ProjectEquipmentList builds the scoped, filtered equipment rows.
// Foreign scopes yield no rows.
func (r *EquipmentRegister) ProjectEquipmentList(tenant values.TenantId, company string, filter EquipmentFilter) []EquipmentListRow {
	records := r.ListEquipment(tenant, company, filter)
	rows := make([]EquipmentListRow, 0, len(records))
	for _, record := range records {
		row := EquipmentListRow{
			AssetID: record.Inventory.InventoryID.Id, Name: record.Inventory.Classification,
			Serial: record.Inventory.SerialNumber, Location: record.SiteRef,
			Condition: record.Condition, StockState: string(record.LinkState),
		}
		if current, ok := r.store.Current(record.Inventory.InventoryID); ok && current.Status == Assigned {
			row.Custodian = current.Worker.Id
		}
		switch record.LinkState {
		case StockLinkPending:
			row.StatusText = "Awaiting reconciliation"
			row.StatusOwner = record.LinkRequestedBy
		case StockLinkLinked:
			row.StatusText = "Linked"
		default:
			row.StatusText = string(record.Inventory.Status)
		}
		rows = append(rows, row)
	}
	return rows
}

// SerialCapture is the serial-capture review: expected versus entered
// with duplicate row highlights.
type SerialCapture struct {
	Expected   int   `json:"expected"`
	Entered    int   `json:"entered"`
	Duplicates []int `json:"duplicates"`
	Missing    int   `json:"missing"`
}

// ProjectSerialCapture compares expected serials against entered ones.
func (r *EquipmentRegister) ProjectSerialCapture(expected, entered []string) SerialCapture {
	capture := SerialCapture{Expected: len(expected), Entered: len(entered)}
	seen := make(map[string]bool, len(entered))
	unique := 0
	for i, serial := range entered {
		normalized := normalizeEquipmentSerial(serial)
		if seen[normalized] {
			capture.Duplicates = append(capture.Duplicates, i)
			continue
		}
		seen[normalized] = true
		unique++
	}
	capture.Missing = len(expected) - unique
	if capture.Missing < 0 {
		capture.Missing = 0
	}
	return capture
}

type equipmentSnapshot struct {
	Digest  string                   `json:"digest"`
	Seq     uint64                   `json:"seq"`
	Schemes []SerialScheme           `json:"schemes"`
	Records []EquipmentRecord        `json:"records"`
	Serials map[string]string        `json:"serials"`
	Sources []snapshotSource         `json:"sources"`
	Idem    map[string]equipmentIdem `json:"idem"`
}

type snapshotSource struct {
	Key   string      `json:"key"`
	Entry sourceEntry `json:"entry"`
}

// Export captures the register for restart recovery with deterministic
// order and a digest over the whole snapshot. Custody tokens travel as
// stream/sequence descriptors; Import reconstructs and replays them.
func (r *EquipmentRegister) Export() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := equipmentSnapshot{
		Seq:     r.seq,
		Schemes: make([]SerialScheme, 0, len(r.schemes)),
		Serials: make(map[string]string, len(r.bySerial)),
		Idem:    make(map[string]equipmentIdem, len(r.idem)),
	}
	for _, scheme := range r.schemes {
		snapshot.Schemes = append(snapshot.Schemes, scheme)
	}
	sort.Slice(snapshot.Schemes, func(i, j int) bool { return snapshot.Schemes[i].ID < snapshot.Schemes[j].ID })
	for _, record := range r.records {
		snapshot.Records = append(snapshot.Records, copyRecordForSnapshot(record))
	}
	sort.Slice(snapshot.Records, func(i, j int) bool {
		return snapshot.Records[i].Inventory.InventoryID.Id < snapshot.Records[j].Inventory.InventoryID.Id
	})
	for key, recordKey := range r.bySerial {
		snapshot.Serials[key] = recordKey
	}
	for key, entry := range r.bySource {
		stored := *entry
		snapshot.Sources = append(snapshot.Sources, snapshotSource{Key: key, Entry: stored})
	}
	sort.Slice(snapshot.Sources, func(i, j int) bool { return snapshot.Sources[i].Key < snapshot.Sources[j].Key })
	for key, record := range r.idem {
		stored := record
		stored.RecordIDs = append([]string(nil), record.RecordIDs...)
		snapshot.Idem[key] = stored
	}
	unsigned, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(unsigned)
	snapshot.Digest = hex.EncodeToString(sum[:])
	return json.Marshal(snapshot)
}

func copyRecordForSnapshot(record *EquipmentRecord) EquipmentRecord {
	stored := *record
	stored.CustodyLog = append([]CustodyEvent(nil), record.CustodyLog...)
	if record.PendingWorker != nil {
		worker := *record.PendingWorker
		stored.PendingWorker = &worker
	}
	return stored
}

// Import restores a snapshot, revalidating every field fail-closed: the
// digest, schemes, serial canonicity against the exported serial index,
// stock-link and verification rules, revision arithmetic, source
// bindings with recomputed payload hashes, idempotency key bindings,
// the ID sequence floor, and the custody stream, which replays into a
// fresh store exactly.
func (r *EquipmentRegister) Import(data []byte) error {
	var snapshot equipmentSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("%w: equipment snapshot is not valid", ErrSnapshot)
	}
	digest := snapshot.Digest
	snapshot.Digest = ""
	unsigned, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("%w: equipment snapshot is not valid", ErrSnapshot)
	}
	sum := sha256.Sum256(unsigned)
	if digest == "" || digest != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("%w: snapshot digest disagrees", ErrSnapshot)
	}
	schemes := make(map[string]bool, len(snapshot.Schemes))
	for _, scheme := range snapshot.Schemes {
		if strings.TrimSpace(scheme.ID) == "" || schemes[scheme.ID] {
			return fmt.Errorf("%w: snapshot schemes are malformed", ErrSnapshot)
		}
		schemes[scheme.ID] = true
	}
	records := make(map[string]*EquipmentRecord, len(snapshot.Records))
	bySerial := make(map[string]string, len(snapshot.Records))
	assetIDs := make(map[string]bool, len(snapshot.Records))
	var maxSeq uint64
	for i := range snapshot.Records {
		record := &snapshot.Records[i]
		tenant := record.Inventory.InventoryID.Tenant
		if err := record.Inventory.InventoryID.Validate(); err != nil || record.Inventory.InventoryID.Kind != "asset" {
			return fmt.Errorf("%w: snapshot asset ref is malformed", ErrSnapshot)
		}
		if record.Company == "" || record.SiteRef == "" || record.Condition == "" || record.AcquiredSource == "" {
			return fmt.Errorf("%w: snapshot record is incomplete", ErrSnapshot)
		}
		if record.AssetSeq == 0 || assetIDs[record.Inventory.InventoryID.Id] {
			return fmt.Errorf("%w: snapshot asset sequence is malformed", ErrSnapshot)
		}
		assetIDs[record.Inventory.InventoryID.Id] = true
		if record.AssetSeq > maxSeq {
			maxSeq = record.AssetSeq
		}
		if record.InventorySeq == 0 {
			return fmt.Errorf("%w: snapshot inventory sequence is malformed", ErrSnapshot)
		}
		if record.InventorySeq > maxSeq {
			maxSeq = record.InventorySeq
		}
		invRev, err := values.NewSequenceRevision("asset-inventory", record.InventorySeq)
		if err != nil {
			return fmt.Errorf("%w: snapshot inventory token: %v", ErrSnapshot, err)
		}
		record.Inventory.Revision = invRev
		if err := record.Inventory.Validate(); err != nil {
			return fmt.Errorf("%w: snapshot inventory invalid: %v", ErrSnapshot, err)
		}
		schemeID, normalized, ok := strings.Cut(record.Inventory.SerialNumber, ":")
		if !ok || !schemes[schemeID] || normalized == "" || normalizeEquipmentSerial(normalized) != normalized {
			return fmt.Errorf("%w: snapshot serial is not canonical", ErrSnapshot)
		}
		serialKey := equipmentSerialKey(tenant, record.Company, record.Inventory.SerialNumber)
		if _, dup := bySerial[serialKey]; dup {
			return fmt.Errorf("%w: snapshot serial repeats", ErrSnapshot)
		}
		recordKey := equipmentRecordKey(tenant, record.Company, record.Inventory.InventoryID.Id)
		bySerial[serialKey] = recordKey
		if _, dup := records[recordKey]; dup {
			return fmt.Errorf("%w: snapshot asset repeats", ErrSnapshot)
		}
		if record.TrackInStock != (record.StockItemRef != "" && record.StockLocationRef != "") {
			return fmt.Errorf("%w: snapshot tracking disagrees with stock refs", ErrSnapshot)
		}
		switch record.LinkState {
		case StockLinkNone:
			if record.LinkSteps != 0 || record.LinkEvidence != "" || record.LinkRequestedBy != "" {
				return fmt.Errorf("%w: snapshot unlinked record carries link state", ErrSnapshot)
			}
		case StockLinkPending, StockLinkLinked:
			if record.LinkEvidence == "" || record.LinkRequestedBy == "" {
				return fmt.Errorf("%w: snapshot linked record lacks evidence", ErrSnapshot)
			}
			wantSteps := 1
			if record.LinkState == StockLinkLinked {
				wantSteps = 2
			}
			if record.LinkSteps != wantSteps {
				return fmt.Errorf("%w: snapshot link steps disagree", ErrSnapshot)
			}
		default:
			return fmt.Errorf("%w: snapshot link state is unknown", ErrSnapshot)
		}
		if record.Verification != VerificationVerified && record.Verification != VerificationPending {
			return fmt.Errorf("%w: snapshot verification is unknown", ErrSnapshot)
		}
		if record.Verification == VerificationPending && (record.VerificationNote == "" || record.PendingWorker == nil) {
			return fmt.Errorf("%w: snapshot pending record lacks its worker and note", ErrSnapshot)
		}
		if record.Verification == VerificationVerified && record.PendingWorker != nil {
			return fmt.Errorf("%w: snapshot verified record carries a pending worker", ErrSnapshot)
		}
		if record.PendingWorker != nil {
			worker := *record.PendingWorker
			if err := requireKind(worker, "worker", "worker"); err != nil || worker.Tenant != tenant {
				return fmt.Errorf("%w: snapshot pending worker is malformed", ErrSnapshot)
			}
		}
		if (record.SourceVersion == "") != (!record.Source.valid()) {
			return fmt.Errorf("%w: snapshot source binding is malformed", ErrSnapshot)
		}
		if record.Revision != uint64(1+record.LinkSteps+record.CorrectionCount) || record.CorrectionCount < 0 || record.LinkSteps < 0 {
			return fmt.Errorf("%w: snapshot revision disagrees with its history", ErrSnapshot)
		}
		for _, event := range record.CustodyLog {
			if err := requireKind(event.Worker, "worker", "worker"); err != nil || event.Worker.Tenant != tenant {
				return fmt.Errorf("%w: snapshot custody worker is malformed", ErrSnapshot)
			}
			if aerr := event.Assignee.Validate(); aerr != nil {
				return fmt.Errorf("%w: snapshot custody receipts are malformed", ErrSnapshot)
			}
			if ierr := event.Issuer.Validate(); ierr != nil {
				return fmt.Errorf("%w: snapshot custody receipts are malformed", ErrSnapshot)
			}
			if event.At.IsZero() {
				return fmt.Errorf("%w: snapshot custody event lacks its time", ErrSnapshot)
			}
			if event.RevisionStream == "" || event.RevisionSeq == 0 {
				return fmt.Errorf("%w: snapshot custody token is malformed", ErrSnapshot)
			}
			if event.ExpectedStream == "" && event.ExpectedSeq != 0 {
				return fmt.Errorf("%w: snapshot custody expectation is malformed", ErrSnapshot)
			}
		}
		records[recordKey] = record
	}
	if snapshot.Seq < maxSeq {
		return fmt.Errorf("%w: snapshot sequence regressed below issued ids", ErrSnapshot)
	}
	if len(snapshot.Serials) != len(bySerial) {
		return fmt.Errorf("%w: snapshot serial index disagrees", ErrSnapshot)
	}
	for key, recordKey := range bySerial {
		if snapshot.Serials[key] != recordKey {
			return fmt.Errorf("%w: snapshot serial index disagrees", ErrSnapshot)
		}
	}
	bySource := make(map[string]*sourceEntry, len(snapshot.Sources))
	for i := range snapshot.Sources {
		entry := &snapshot.Sources[i]
		parts := strings.Split(entry.Key, "\x00")
		if len(parts) != 5 || parts[0] == "" || parts[1] == "" || parts[2] == "" || parts[3] == "" || parts[4] == "" {
			return fmt.Errorf("%w: snapshot source key is malformed", ErrSnapshot)
		}
		if _, dup := bySource[entry.Key]; dup {
			return fmt.Errorf("%w: snapshot source repeats", ErrSnapshot)
		}
		record, ok := records[entry.Entry.RecordKey]
		if !ok {
			return fmt.Errorf("%w: snapshot source references an unknown record", ErrSnapshot)
		}
		wantKey := equipmentSourceKey(record.Inventory.InventoryID.Tenant, record.Source)
		if entry.Key != wantKey {
			return fmt.Errorf("%w: snapshot source key disagrees with its record", ErrSnapshot)
		}
		if entry.Entry.AdmittedVersion == 0 {
			return fmt.Errorf("%w: snapshot admitted version is malformed", ErrSnapshot)
		}
		parsed, err := parseSourceVersion(record.SourceVersion)
		if err != nil || parsed != entry.Entry.AdmittedVersion {
			return fmt.Errorf("%w: snapshot source version disagrees", ErrSnapshot)
		}
		if entry.Entry.PayloadHash != hashImportPayload(entry.Entry.AdmittedRow) {
			return fmt.Errorf("%w: snapshot source payload disagrees", ErrSnapshot)
		}
		if entry.Entry.AdmittedRow.Source != record.Source || entry.Entry.AdmittedRow.SourceVersion != record.SourceVersion {
			return fmt.Errorf("%w: snapshot admitted row disagrees with its record", ErrSnapshot)
		}
		if entry.Entry.OriginalReceipt != record.Source.receipt() {
			return fmt.Errorf("%w: snapshot original receipt disagrees", ErrSnapshot)
		}
		bySource[entry.Key] = &entry.Entry
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
		case "receipt", "manual", "link-request", "link-reconcile", "commit":
		default:
			return fmt.Errorf("%w: snapshot idempotency kind %q is unknown", ErrSnapshot, record.Kind)
		}
		if record.Kind != parts[2] {
			return fmt.Errorf("%w: snapshot idempotency kind disagrees with its key", ErrSnapshot)
		}
		if len(record.RecordIDs) == 0 {
			return fmt.Errorf("%w: snapshot idempotency record names no asset", ErrSnapshot)
		}
		for _, id := range record.RecordIDs {
			if _, ok := records[equipmentRecordKey(values.TenantId(parts[0]), parts[1], id)]; !ok {
				return fmt.Errorf("%w: snapshot idempotency record references an unknown asset", ErrSnapshot)
			}
		}
	}
	rebuilt := NewCustodyStore()
	ordered := make([]*EquipmentRecord, 0, len(records))
	for _, record := range records {
		ordered = append(ordered, record)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].AssetSeq < ordered[j].AssetSeq })
	for _, record := range ordered {
		if err := rebuilt.Register(record.Inventory); err != nil {
			return fmt.Errorf("%w: snapshot custody replay: %v", ErrSnapshot, err)
		}
		for _, event := range record.CustodyLog {
			expected := values.UnspecifiedRevision()
			if event.ExpectedStream != "" {
				expected, err = values.NewSequenceRevision(event.ExpectedStream, event.ExpectedSeq)
				if err != nil {
					return fmt.Errorf("%w: snapshot custody token: %v", ErrSnapshot, err)
				}
			}
			revision, err := values.NewSequenceRevision(event.RevisionStream, event.RevisionSeq)
			if err != nil {
				return fmt.Errorf("%w: snapshot custody token: %v", ErrSnapshot, err)
			}
			next := CustodyRevision{
				Asset: record.Inventory.InventoryID, Worker: event.Worker, Location: event.Location,
				Condition: event.Condition, AssigneeReceipt: event.Assignee, IssuerReceipt: event.Issuer,
				Revision: revision, EffectiveAt: event.At, Status: Assigned,
			}
			if err := rebuilt.Apply(record.Inventory.InventoryID, expected, next); err != nil {
				return fmt.Errorf("%w: snapshot custody replay: %v", ErrSnapshot, err)
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq = snapshot.Seq
	r.schemes = make(map[string]SerialScheme, len(schemes))
	for _, scheme := range snapshot.Schemes {
		r.schemes[scheme.ID] = scheme
	}
	r.records = make(map[string]*EquipmentRecord, len(records))
	for key, record := range records {
		stored := copyRecordForSnapshot(record)
		r.records[key] = &stored
	}
	r.bySerial = make(map[string]string, len(bySerial))
	for key, recordKey := range bySerial {
		r.bySerial[key] = recordKey
	}
	r.bySource = make(map[string]*sourceEntry, len(bySource))
	for key, entry := range bySource {
		stored := *entry
		r.bySource[key] = &stored
	}
	r.idem = make(map[string]equipmentIdem, len(snapshot.Idem))
	for key, record := range snapshot.Idem {
		stored := record
		stored.RecordIDs = append([]string(nil), record.RecordIDs...)
		r.idem[key] = stored
	}
	r.store = rebuilt
	return nil
}
