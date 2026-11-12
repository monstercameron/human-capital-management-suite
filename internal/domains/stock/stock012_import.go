// Package stock staged inventory import: SMB-STOCK-012 validates and commits
// catalog and opening-stock CSV batches through the shared DataOps import
// mechanics (staging, content digests, commit keys, checkpoints) with durable
// domain source-key uniqueness. It supplies inventory schemas, dry-run
// validators and commit callbacks to the existing import ownership; it is not
// a second importer.
package stock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/importdraft"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ImportSchemaDigest pins the inventory import schema revision. Checkpoints
// bind it so a schema change never resumes an old interpretation.
const ImportSchemaDigest = "smb-stock-012-import/v1"

var (
	ErrImportAck        = errors.New("stock: import needs acknowledged source authority")
	ErrImportValidation = errors.New("stock: import row is invalid")
	ErrImportConflict   = errors.New("stock: import identity conflicts")
	ErrImportLive       = errors.New("stock: location is live; reconcile instead of opening")
	ErrImportLinkage    = errors.New("stock: serial asset linkage is missing")
	ErrImportCorrection = errors.New("stock: correction is not valid")
	ErrImportState      = errors.New("stock: import state is not valid")
)

// Import record kinds and opening physical buckets.
const (
	ImportRowItem    = "ITEM"
	ImportRowOpening = "OPENING"

	BucketUsable     = "USABLE"
	BucketQuarantine = "QUARANTINE"
	BucketTransit    = "IN_TRANSIT"

	ReceiptApplied    = "APPLIED"
	ReceiptSuperseded = "SUPERSEDED"

	TransitPending  = "PENDING"
	TransitReceived = "RECEIVED"
)

// SiteAuthority answers which sites exist. A nil authority accepts any
// non-empty site; tests wire a fake.
type SiteAuthority interface {
	IsKnownSite(companyID, siteID string) bool
}

// ImportOptions carries the import-level acknowledgements. RetrievedAt is the
// explicit staging instant; GroupSize bounds new attempts per commit call
// with zero meaning all.
type ImportOptions struct {
	SourceAuthorityAck bool
	RetrievedAt        time.Time
	GroupSize          int
}

// ImportRowError identifies one rejected row by CSV line, field and stable
// code. Messages name submitted values only, never inaccessible records.
type ImportRowError struct {
	CSVLine int
	RowID   string
	Field   string
	Code    string
	Message string
}

// PlannedMovement previews one opening row: the live starting quantity and
// the resulting quantity after commit.
type PlannedMovement struct {
	CSVLine      int
	RowID        string
	ItemSKU      string
	Site         string
	DeltaQty     string
	StartingQty  string
	ResultingQty string
	Bucket       string
	Action       string
}

// ImportDryRun is the zero-effect validation of one staged batch.
type ImportDryRun struct {
	ImportID     string
	SourceURI    string
	RetrievedAt  time.Time
	BatchDigest  string
	DryRunDigest string
	TotalRows    int
	ValidRows    int
	InvalidRows  int
	Errors       []ImportRowError
	Planned      []PlannedMovement
}

// ImportPendingRow names a row deferred for serial asset linkage.
type ImportPendingRow struct {
	RowID   string
	CSVLine int
	Code    string
	Message string
}

// ImportCommitResult partitions one commit call: applied (fresh plus resumed
// receipts), failed, pending linkage and unattempted rows.
type ImportCommitResult struct {
	ImportID    string
	BatchDigest string
	Applied     int
	Failed      int
	Pending     int
	Unattempted int
	Complete    bool
	Receipts    []ImportReceipt
	Errors      []ImportRowError
	PendingRows []ImportPendingRow
}

// ImportReceipt is the immutable per-row commit outcome. Every opening
// balance carries an OPENING_RECEIPT movement reference for the STOCK-001
// movement owner; corrections link back instead of duplicating.
type ImportReceipt struct {
	ReceiptID         string
	ImportID          string
	RowID             string
	CSVLine           int
	RecordKind        string
	ExternalKey       string
	SourceSystem      string
	SourceVersion     string
	ItemID            string
	ItemSKU           string
	Site              string
	LotID             string
	SerialNS          string
	Serial            string
	Qty               string
	Unit              string
	Bucket            string
	QualityReason     string
	TransferRef       string
	MovementRef       string
	AsOf              string
	At                time.Time
	CorrectsReceiptID string
	Status            string
}

// OpeningBalances reports bucketed opening quantities. OnHandTotal covers
// usable plus quarantine; transit is never on hand.
type OpeningBalances struct {
	Usable      string
	Quarantine  string
	Transit     string
	OnHandTotal string
	Available   string
	Scale       int32
}

// TransitRecord tracks one in-transit opening from dispatch to receipt.
type TransitRecord struct {
	TransferRef string
	CompanyID   string
	ItemID      string
	ItemSKU     string
	FromSite    string
	ToSite      string
	Qty         string
	Scale       int32
	DispatchRef string
	Status      string
}

// ImportCorrection supersedes one applied opening receipt with a new source
// version and quantity. Bucket changes are reversals, not corrections.
type ImportCorrection struct {
	ReceiptID     string
	SourceVersion string
	Quantity      string
	Unit          string
}

// importRow is one parsed batch row with every inventory column bound.
type importRow struct {
	RowID           string
	CSVLine         int
	Kind            string
	ExternalKey     string
	SourceSystem    string
	SourceVersion   string
	SKU             string
	Name            string
	BaseUnit        string
	Scale           int32
	ScaleText       string
	Tracking        string
	BarcodeSym      string
	BarcodeCode     string
	PackUnit        string
	PackNum         string
	PackDen         string
	ItemKey         string
	Site            string
	Quantity        string
	Unit            string
	Bucket          string
	QualityReason   string
	PolicyVersion   string
	LotSupplier     string
	LotDate         string
	LotTZ           string
	SerialNS        string
	Serial          string
	AssetRef        string
	AsOf            string
	SourceAuthority string
	OriginReceipt   string
	DispatchRef     string
	TransferRef     string
	FromLocation    string
	ToLocation      string
	raw             []string
}

// admittedIdentity is the durable domain uniqueness record:
// (source_system, company, record_kind, external_key) -> version+digest.
type admittedIdentity struct {
	Version  string
	Digest   string
	Receipt  string
	ImportID string
}

// ledgerBucket holds exact bucket strings at a fixed scale.
type ledgerBucket struct {
	Usable     string
	Quarantine string
	Transit    string
	Scale      int32
}

// InventoryImporter stages, validates and commits inventory imports. Catalog
// writes go to the STOCK-002 catalog, lot and serial writes to the STOCK-008
// registry, progress to the shared checkpoint store.
type InventoryImporter struct {
	mu          sync.Mutex
	catalog     *Catalog
	lots        *LotRegistry
	checkpoints importdraft.CheckpointStore
	sites       SiteAuthority

	admitted    map[string]admittedIdentity
	rowReceipts map[string]string
	itemKeys    map[string]string
	itemSKUs    map[string]string
	ledger      map[string]*ledgerBucket
	transits    map[string]*TransitRecord
	attachments map[string]string
	live        map[string]bool
	receipts    map[string]*ImportReceipt
	corrections map[string]string
	transitKeys map[string]string
	releaseKeys map[string]string
}

// NewInventoryImporter builds an empty importer over the given owners.
func NewInventoryImporter(catalog *Catalog, lots *LotRegistry, checkpoints importdraft.CheckpointStore, sites SiteAuthority) *InventoryImporter {
	if checkpoints == nil {
		checkpoints = importdraft.NewMemoryCheckpoints()
	}
	return &InventoryImporter{
		catalog: catalog, lots: lots, checkpoints: checkpoints, sites: sites,
		admitted: make(map[string]admittedIdentity), rowReceipts: make(map[string]string),
		itemKeys: make(map[string]string), itemSKUs: make(map[string]string),
		ledger: make(map[string]*ledgerBucket), transits: make(map[string]*TransitRecord),
		attachments: make(map[string]string), live: make(map[string]bool),
		receipts: make(map[string]*ImportReceipt), corrections: make(map[string]string),
		transitKeys: make(map[string]string), releaseKeys: make(map[string]string),
	}
}

func importIdentityKey(sourceSystem, tenantID, companyID, kind, externalKey string) string {
	return sourceSystem + "\x00" + tenantID + "\x00" + companyID + "\x00" + kind + "\x00" + externalKey
}

func importRowIdentity(row importRow, op OperationContext) string {
	return importIdentityKey(row.SourceSystem, op.TenantID, op.CompanyID, row.Kind, row.ExternalKey)
}

func importPayloadDigest(cells []string) string {
	sum := sha256.Sum256([]byte("smb-stock-012-row/v1\x00" + strings.Join(cells, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (r *InventoryImporter) knownSite(companyID, siteID string) bool {
	if strings.TrimSpace(siteID) == "" {
		return false
	}
	if r.sites == nil {
		return true
	}
	return r.sites.IsKnownSite(companyID, siteID)
}

// stage parses CSV text through the shared staging path. Digest, row IDs and
// limits all come from importing; this package adds no staging of its own.
func (r *InventoryImporter) stage(op OperationContext, sourceURI, csvText string, retrievedAt time.Time) (importdraft.Batch, error) {
	if strings.TrimSpace(sourceURI) == "" {
		return importdraft.Batch{}, fmt.Errorf("%w: source uri is required", ErrImportValidation)
	}
	if retrievedAt.IsZero() {
		retrievedAt = time.Now().UTC()
	}
	batch, err := importdraft.StageCSV(
		importdraft.SourceDescriptor{Kind: importdraft.SourceKindCSV, URI: strings.TrimSpace(sourceURI), Tenant: values.TenantId(op.TenantID)},
		values.NewInstant(retrievedAt),
		strings.NewReader(csvText),
	)
	if err != nil {
		return importdraft.Batch{}, fmt.Errorf("%w: staging: %v", ErrImportValidation, err)
	}
	return batch, nil
}

func batchCell(batch importdraft.Batch, row importdraft.Row, column string) string {
	index, ok := batch.ColumnIndex(column)
	if !ok {
		return ""
	}
	cell, _ := row.Cell(index)
	return strings.TrimSpace(cell)
}

func parseImportRow(batch importdraft.Batch, row importdraft.Row, index int) importRow {
	cells := row.Cells()
	get := func(column string) string { return batchCell(batch, row, column) }
	scale, _ := strconv.Atoi(get("scale"))
	return importRow{
		RowID: row.ID(), CSVLine: index + 2, Kind: get("record_kind"),
		ExternalKey: get("external_key"), SourceSystem: get("source_system"), SourceVersion: get("source_version"),
		SKU: get("sku"), Name: get("name"), BaseUnit: get("base_unit"),
		Scale: int32(scale), ScaleText: get("scale"), Tracking: get("tracking"),
		BarcodeSym: get("barcode_symbology"), BarcodeCode: get("barcode_code"),
		PackUnit: get("pack_unit"), PackNum: get("pack_num"), PackDen: get("pack_den"),
		ItemKey: get("item_key"), Site: get("site"), Quantity: get("quantity"), Unit: get("unit"),
		Bucket: get("physical_bucket"), QualityReason: get("quality_reason"), PolicyVersion: get("policy_version"),
		LotSupplier: get("lot_supplier"), LotDate: get("lot_expiry_date"), LotTZ: get("lot_expiry_tz"),
		SerialNS: get("serial_namespace"), Serial: get("serial"), AssetRef: get("asset_ref"),
		AsOf: get("as_of"), SourceAuthority: get("source_authority"), OriginReceipt: get("origin_receipt_ref"),
		DispatchRef: get("source_dispatch_ref"), TransferRef: get("transfer_ref"),
		FromLocation: get("from_location"), ToLocation: get("to_location"), raw: cells,
	}
}

// itemShape is what opening validation needs: scale, units and tracking.
type itemShape struct {
	SKU       string
	ItemID    string
	Scale     int32
	BaseUnit  string
	Tracking  ItemTracking
	PackUnits map[string]PackConversion
	FromBatch bool
}

// ValidateInventoryImport stages CSV text and dry-runs every row without
// writing to any domain. Errors identify CSV line and field; the dry-run
// digest binds the exact proposal a later commit must reproduce.
func (r *InventoryImporter) ValidateInventoryImport(_ context.Context, op OperationContext, sourceURI, csvText string, opts ImportOptions) (ImportDryRun, error) {
	if err := op.validate(); err != nil {
		return ImportDryRun{}, err
	}
	if !opts.SourceAuthorityAck {
		return ImportDryRun{}, ErrImportAck
	}
	batch, err := r.stage(op, sourceURI, csvText, opts.RetrievedAt)
	if err != nil {
		return ImportDryRun{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]importRow, 0, batch.RowCount())
	for i, staged := range batch.Rows() {
		rows = append(rows, parseImportRow(batch, staged, i))
	}
	shapes := r.batchShapes(rows)
	aliasHolders := map[string][]int{}
	serialHolders := map[string][]int{}
	keyHolders := map[string][]int{}
	for i, row := range rows {
		if row.Kind == ImportRowItem && row.BarcodeSym != "" && row.BarcodeCode != "" {
			aliasHolders[row.BarcodeSym+"\x00"+row.BarcodeCode] = append(aliasHolders[row.BarcodeSym+"\x00"+row.BarcodeCode], i)
		}
		if row.Kind == ImportRowOpening && row.Serial != "" {
			serialHolders[row.SerialNS+"\x00"+row.Serial] = append(serialHolders[row.SerialNS+"\x00"+row.Serial], i)
		}
		if row.ExternalKey != "" {
			identity := importRowIdentity(row, op)
			keyHolders[identity] = append(keyHolders[identity], i)
		}
	}
	dryRun := ImportDryRun{
		ImportID: uuid.NewString(), SourceURI: strings.TrimSpace(sourceURI),
		RetrievedAt: opts.RetrievedAt, BatchDigest: batch.Digest(), TotalRows: len(rows),
	}
	for i, row := range rows {
		errs := r.validateRow(op, row, shapes, aliasHolders, serialHolders, keyHolders, i)
		if len(errs) == 0 {
			dryRun.ValidRows++
			if row.Kind == ImportRowOpening {
				dryRun.Planned = append(dryRun.Planned, r.planMovement(op, row, shapes))
			}
		} else {
			dryRun.InvalidRows++
			dryRun.Errors = append(dryRun.Errors, errs...)
		}
	}
	dryRun.DryRunDigest = dryRunDigest(batch.Digest(), rows, dryRun.Errors)
	return dryRun, nil
}

func dryRunDigest(batchDigest string, rows []importRow, errs []ImportRowError) string {
	parts := make([]string, 0, len(rows)+len(errs))
	for _, row := range rows {
		parts = append(parts, row.RowID)
	}
	for _, rowErr := range errs {
		parts = append(parts, rowErr.RowID+"\x00"+rowErr.Code+"\x00"+rowErr.Field)
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte("smb-stock-012-dryrun/v1\x00" + batchDigest + "\x00" + strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// batchShapes indexes same-batch ITEM rows by external key for opening
// resolution. Shapes validate quantities even when the ITEM row itself is
// invalid, so one bad item row does not mask every opening error.
func (r *InventoryImporter) batchShapes(rows []importRow) map[string]itemShape {
	shapes := make(map[string]itemShape)
	for _, row := range rows {
		if row.Kind != ImportRowItem || row.ExternalKey == "" {
			continue
		}
		shape := itemShape{SKU: row.SKU, Scale: row.Scale, BaseUnit: row.BaseUnit, Tracking: ItemTracking(row.Tracking), PackUnits: map[string]PackConversion{}, FromBatch: true}
		if row.PackUnit != "" {
			num, _ := strconv.ParseInt(row.PackNum, 10, 64)
			den, _ := strconv.ParseInt(row.PackDen, 10, 64)
			shape.PackUnits[row.PackUnit] = PackConversion{PackUnit: row.PackUnit, Numerator: num, Denominator: den}
		}
		shapes[row.SourceSystem+"\x00"+row.ExternalKey] = shape
	}
	return shapes
}

func (r *InventoryImporter) resolveShape(op OperationContext, row importRow, shapes map[string]itemShape) (itemShape, bool) {
	if shape, ok := shapes[row.SourceSystem+"\x00"+row.ItemKey]; ok && row.ItemKey != "" {
		if id, found := r.itemKeys[op.TenantID+"\x00"+op.CompanyID+"\x00"+row.SourceSystem+"\x00"+row.ItemKey]; found {
			shape.ItemID = id
		}
		return shape, true
	}
	if row.ItemKey == "" {
		return itemShape{}, false
	}
	if id, found := r.itemKeys[op.TenantID+"\x00"+op.CompanyID+"\x00"+row.SourceSystem+"\x00"+row.ItemKey]; found && r.catalog != nil {
		if history, err := r.catalog.History(op, id); err == nil && len(history) > 0 {
			return catalogShape(history[len(history)-1]), true
		}
		return itemShape{}, false
	}
	if r.catalog == nil {
		return itemShape{}, false
	}
	for _, item := range r.catalog.ListItems(op, ItemFilter{}) {
		if item.SKU == row.ItemKey {
			return catalogShape(item), true
		}
	}
	return itemShape{}, false
}

func catalogShape(item CatalogItem) itemShape {
	shape := itemShape{SKU: item.SKU, ItemID: item.ItemID, Scale: item.Scale, BaseUnit: item.BaseUnit, Tracking: item.Tracking, PackUnits: map[string]PackConversion{}}
	for _, conv := range item.Conversions {
		shape.PackUnits[conv.PackUnit] = conv
	}
	return shape
}

func rowError(row importRow, field, code, message string) ImportRowError {
	return ImportRowError{CSVLine: row.CSVLine, RowID: row.RowID, Field: field, Code: code, Message: message}
}

// validateRow checks one row against schema, batch and committed state. It
// never writes; commit revalidates through the same function.
func (r *InventoryImporter) validateRow(op OperationContext, row importRow, shapes map[string]itemShape, aliasHolders, serialHolders map[string][]int, keyHolders map[string][]int, index int) []ImportRowError {
	var errs []ImportRowError
	fail := func(field, code, message string) {
		errs = append(errs, rowError(row, field, code, message))
	}
	if row.Kind != ImportRowItem && row.Kind != ImportRowOpening {
		fail("record_kind", "IMPORT_KIND", "record_kind must be ITEM or OPENING")
		return errs
	}
	if row.ExternalKey == "" || row.SourceSystem == "" || row.SourceVersion == "" {
		fail("external_key", "IMPORT_IDENTITY", "external_key, source_system and source_version are required")
	}
	if holders := keyHolders[importRowIdentity(row, op)]; len(holders) > 1 && row.ExternalKey != "" {
		fail("external_key", "IMPORT_DUPLICATE_KEY", "external_key "+strconv.Quote(row.ExternalKey)+" appears more than once in this file")
	}
	if row.ExternalKey != "" {
		if admitted, ok := r.admitted[importRowIdentity(row, op)]; ok {
			digest := importPayloadDigest(row.raw)
			if admitted.Digest == digest {
				return errs
			}
			switch {
			case admitted.Version == row.SourceVersion:
				fail("external_key", "IMPORT_IDENTITY_CONFLICT", "same identity with a changed payload at version "+strconv.Quote(row.SourceVersion)+" conflicts with receipt "+admitted.Receipt)
			default:
				fail("source_version", "IMPORT_REQUIRES_CORRECTION", "version "+strconv.Quote(row.SourceVersion)+" of an admitted record needs explicit correction against receipt "+admitted.Receipt)
			}
		}
	}
	if row.Kind == ImportRowItem {
		return append(errs, r.validateItemRow(op, row, aliasHolders, index)...)
	}
	return append(errs, r.validateOpeningRow(op, row, shapes, serialHolders, index)...)
}

func (r *InventoryImporter) validateItemRow(op OperationContext, row importRow, aliasHolders map[string][]int, index int) []ImportRowError {
	var errs []ImportRowError
	fail := func(field, code, message string) {
		errs = append(errs, rowError(row, field, code, message))
	}
	if row.SKU == "" || row.Name == "" || row.BaseUnit == "" {
		fail("sku", "IMPORT_ITEM_FIELDS", "sku, name and base_unit are required")
	}
	if _, err := strconv.Atoi(row.ScaleText); err != nil || row.Scale < 0 || row.Scale > values.MaxScale {
		fail("scale", "IMPORT_SCALE", "scale must be an integer in [0,18]")
	}
	switch ItemTracking(row.Tracking) {
	case TrackingEach, TrackingSerialized, TrackingMass:
	default:
		fail("tracking", "IMPORT_TRACKING", "tracking must be EACH, SERIALIZED or MASS")
	}
	if row.PackUnit != "" || row.PackNum != "" || row.PackDen != "" {
		num, numErr := strconv.ParseInt(row.PackNum, 10, 64)
		den, denErr := strconv.ParseInt(row.PackDen, 10, 64)
		if row.PackUnit == "" || numErr != nil || denErr != nil || num <= 0 || den <= 0 {
			fail("pack_den", "IMPORT_PACK", "pack needs a unit and positive integer numerator/denominator")
		}
	}
	if (row.BarcodeSym == "") != (row.BarcodeCode == "") {
		fail("barcode_code", "IMPORT_ALIAS_FIELDS", "barcode needs both symbology and code")
	}
	if row.BarcodeSym != "" && row.BarcodeCode != "" {
		if holders := aliasHolders[row.BarcodeSym+"\x00"+row.BarcodeCode]; len(holders) > 1 {
			fail("barcode_code", "IMPORT_DUPLICATE_ALIAS", "barcode "+row.BarcodeSym+" "+strconv.Quote(row.BarcodeCode)+" appears more than once in this file")
		} else if r.catalog != nil {
			if _, err := r.catalog.ResolveBarcode(op, row.BarcodeSym, row.BarcodeCode); err == nil {
				fail("barcode_code", "IMPORT_ALIAS_REGISTERED", "barcode is already registered")
			}
		}
	}
	if row.SKU != "" && r.catalog != nil {
		for _, item := range r.catalog.ListItems(op, ItemFilter{}) {
			if item.SKU == row.SKU && item.Status != ItemStatusRetired {
				fail("sku", "IMPORT_SKU_REGISTERED", "sku is already active; reconcile instead of importing")
				break
			}
		}
	}
	_ = index
	return errs
}

func (r *InventoryImporter) validateOpeningRow(op OperationContext, row importRow, shapes map[string]itemShape, serialHolders map[string][]int, index int) []ImportRowError {
	var errs []ImportRowError
	fail := func(field, code, message string) {
		errs = append(errs, rowError(row, field, code, message))
	}
	if row.ItemKey == "" {
		fail("item_key", "IMPORT_ITEM_KEY", "item_key is required")
	}
	shape, shapeOK := r.resolveShape(op, row, shapes)
	if row.ItemKey != "" && !shapeOK {
		fail("item_key", "IMPORT_UNKNOWN_ITEM", "item_key is unknown in scope")
	}
	if !r.knownSite(op.CompanyID, row.Site) {
		fail("site", "IMPORT_UNKNOWN_SITE", "site "+strconv.Quote(row.Site)+" is unknown")
	}
	scale := int32(0)
	if shapeOK {
		scale = shape.Scale
	}
	qty, err := values.NewDecimal(strings.TrimSpace(row.Quantity), scale, values.RoundingExactRequired)
	if err != nil {
		if errors.Is(err, values.ErrExcessScale) {
			fail("quantity", "IMPORT_PRECISION", "quantity "+strconv.Quote(row.Quantity)+" exceeds scale")
		} else {
			fail("quantity", "IMPORT_QUANTITY", "quantity "+strconv.Quote(row.Quantity)+" is not an exact decimal")
		}
	} else if qty.Sign() <= 0 {
		fail("quantity", "IMPORT_QUANTITY", "quantity must be positive")
	}
	if row.Unit == "" {
		fail("unit", "IMPORT_UNIT", "unit is required")
	} else if shapeOK && row.Unit != shape.BaseUnit {
		if _, ok := shape.PackUnits[row.Unit]; !ok {
			fail("unit", "IMPORT_UNKNOWN_UNIT", "unit "+strconv.Quote(row.Unit)+" is not defined for this item")
		}
	}
	switch row.Bucket {
	case BucketUsable, BucketQuarantine, BucketTransit:
	default:
		fail("physical_bucket", "IMPORT_BUCKET", "physical_bucket must be USABLE, QUARANTINE or IN_TRANSIT")
	}
	if row.Bucket == BucketQuarantine && row.QualityReason == "" {
		fail("quality_reason", "IMPORT_QUALITY", "quarantine needs a reason and preserves inspection")
	}
	if row.Bucket == BucketTransit {
		if row.FromLocation == "" || row.ToLocation == "" || row.DispatchRef == "" || row.TransferRef == "" {
			fail("transfer_ref", "IMPORT_TRANSIT", "transit needs from/to locations, dispatch and transfer references")
		} else {
			if !r.knownSite(op.CompanyID, row.FromLocation) {
				fail("from_location", "IMPORT_UNKNOWN_SITE", "from_location "+strconv.Quote(row.FromLocation)+" is unknown")
			}
			if !r.knownSite(op.CompanyID, row.ToLocation) {
				fail("to_location", "IMPORT_UNKNOWN_SITE", "to_location "+strconv.Quote(row.ToLocation)+" is unknown")
			}
			if record, exists := r.transits[op.TenantID+"\x00"+op.CompanyID+"\x00"+row.TransferRef]; exists && record.Status == TransitPending {
				fail("transfer_ref", "IMPORT_DUPLICATE_TRANSIT", "transfer_ref "+strconv.Quote(row.TransferRef)+" is already in transit")
			}
		}
	}
	if row.LotSupplier != "" || row.LotDate != "" || row.LotTZ != "" {
		if row.LotSupplier == "" || row.LotDate == "" || row.LotTZ == "" {
			fail("lot_expiry_tz", "IMPORT_LOT", "lot needs supplier, expiry date and expiry timezone together")
		} else if _, err := lotCutoff(ExpiryPolicy{LocalDate: row.LotDate, Timezone: row.LotTZ, Rule: ExpiryNextLocalDayStart}); err != nil {
			fail("lot_expiry_date", "IMPORT_LOT", "lot expiry is not valid: "+err.Error())
		}
	}
	if row.SerialNS != "" || row.Serial != "" || row.AssetRef != "" {
		if row.Serial == "" {
			fail("serial", "IMPORT_SERIAL", "serial rows need a serial")
		}
	}
	if row.Serial != "" {
		if holders := serialHolders[row.SerialNS+"\x00"+row.Serial]; len(holders) > 1 {
			fail("serial", "IMPORT_DUPLICATE_SERIAL", "serial appears more than once in this file")
		} else if r.lots != nil {
			namespace := row.SerialNS
			if namespace == "" {
				namespace = SerialNamespaceLegacy
			}
			if _, err := r.lots.SerialTrace(op, namespace, row.Serial); err == nil {
				fail("serial", "IMPORT_SERIAL_REGISTERED", "serial is already registered")
			}
		}
	}
	if shapeOK && shape.Tracking == TrackingSerialized && row.Serial == "" {
		fail("serial", "IMPORT_MISSING_SERIAL", "serialized items need one serial per opening row")
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(row.AsOf)); err != nil {
		fail("as_of", "IMPORT_ASOF", "as_of must be RFC3339")
	}
	if row.SourceAuthority == "" {
		fail("source_authority", "IMPORT_AUTHORITY", "source_authority is required")
	}
	if shapeOK && r.live[op.TenantID+"\x00"+op.CompanyID+"\x00"+row.Site+"\x00"+shape.SKU] {
		fail("site", "IMPORT_ALREADY_LIVE", row.Site+"/"+shape.SKU+" is live with prior balances; reconcile instead of opening")
	}
	_ = index
	return errs
}

func (r *InventoryImporter) planMovement(op OperationContext, row importRow, shapes map[string]itemShape) PlannedMovement {
	move := PlannedMovement{CSVLine: row.CSVLine, RowID: row.RowID, Site: row.Site, Bucket: row.Bucket, Action: "POST_BALANCE"}
	shape, ok := r.resolveShape(op, row, shapes)
	if !ok {
		return move
	}
	move.ItemSKU = shape.SKU
	bucket := r.bucketFor(op.CompanyID, op.TenantID, row.Site, shape.SKU, shape.Scale)
	start := bucket.Usable
	switch row.Bucket {
	case BucketQuarantine:
		start = bucket.Quarantine
	case BucketTransit:
		start = bucket.Transit
	}
	move.StartingQty = start
	qty, err := parseLotQty(row.Quantity, shape.Scale)
	if err != nil {
		return move
	}
	if row.Unit != "" && row.Unit != shape.BaseUnit {
		if conv, defined := shape.PackUnits[row.Unit]; defined && conv.Denominator > 0 {
			total := new(big.Int).Mul(qty, big.NewInt(conv.Numerator))
			quotient, remainder := new(big.Int).QuoRem(total, big.NewInt(conv.Denominator), new(big.Int))
			if remainder.Sign() != 0 {
				return move
			}
			qty = quotient
		}
	}
	if deltaText, err := canonicalQuantity(qty, shape.Scale); err == nil {
		move.DeltaQty = deltaText
	}
	base, err := parseLotQty(start, shape.Scale)
	if err != nil {
		return move
	}
	resulting, err := canonicalQuantity(new(big.Int).Add(base, qty), shape.Scale)
	if err != nil {
		return move
	}
	move.ResultingQty = resulting
	switch {
	case row.Bucket == BucketTransit:
		move.Action = "POST_TRANSIT"
	case row.LotSupplier != "":
		move.Action = "CREATE_LOT"
	case row.Serial != "":
		move.Action = "REGISTER_SERIAL"
	}
	return move
}

// PreviewOpeningMovements recomputes starting quantities live so a stale
// dry-run never promises balances that moved since validation.
func (r *InventoryImporter) PreviewOpeningMovements(op OperationContext, dryRun ImportDryRun) ([]PlannedMovement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]PlannedMovement, 0, len(dryRun.Planned))
	for _, move := range dryRun.Planned {
		updated := move
		if bucket, ok := r.ledger[op.TenantID+"\x00"+op.CompanyID+"\x00"+move.Site+"\x00"+move.ItemSKU]; ok {
			start := bucket.Usable
			switch move.Bucket {
			case BucketQuarantine:
				start = bucket.Quarantine
			case BucketTransit:
				start = bucket.Transit
			}
			if base, err := parseLotQty(start, bucket.Scale); err == nil {
				if delta, err := parseLotQty(move.DeltaQty, bucket.Scale); err == nil {
					if resulting, err := canonicalQuantity(new(big.Int).Add(base, delta), bucket.Scale); err == nil {
						updated.StartingQty = start
						updated.ResultingQty = resulting
					}
				}
			}
		}
		out = append(out, updated)
	}
	return out, nil
}

// CommitInventoryImport commits (or resumes) a validated dry-run in bounded
// row groups. The file is re-staged and its digest must match the dry-run;
// progress checkpoints to the shared store after every row; committed rows
// are never reapplied. Cancel the context to stop after the current row.
func (r *InventoryImporter) CommitInventoryImport(ctx context.Context, op OperationContext, dryRun ImportDryRun, csvText string, opts ImportOptions) (ImportCommitResult, error) {
	if err := op.validate(); err != nil {
		return ImportCommitResult{}, err
	}
	if !opts.SourceAuthorityAck {
		return ImportCommitResult{}, ErrImportAck
	}
	if dryRun.ImportID == "" || dryRun.BatchDigest == "" || dryRun.DryRunDigest == "" {
		return ImportCommitResult{}, fmt.Errorf("%w: dry-run is incomplete", ErrImportValidation)
	}
	batch, err := r.stage(op, dryRun.SourceURI, csvText, dryRun.RetrievedAt)
	if err != nil {
		return ImportCommitResult{}, err
	}
	if batch.Digest() != dryRun.BatchDigest {
		return ImportCommitResult{}, fmt.Errorf("%w: batch changed since dry-run; revalidate", ErrImportConflict)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]importRow, 0, batch.RowCount())
	for i, staged := range batch.Rows() {
		rows = append(rows, parseImportRow(batch, staged, i))
	}
	cp, err := r.loadCommitCheckpoint(ctx, dryRun)
	if err != nil {
		return ImportCommitResult{}, err
	}
	items := map[string]importdraft.ItemState{}
	for id, state := range cp.Items {
		items[id] = state
	}
	shapes := r.batchShapes(rows)
	aliasHolders, serialHolders, keyHolders := batchDuplicates(op, rows)
	result := ImportCommitResult{ImportID: dryRun.ImportID, BatchDigest: dryRun.BatchDigest}
	attempted := 0
	for i, row := range rows {
		if prior, ok := items[row.RowID]; ok && prior.Status == importdraft.ItemCommitted {
			if receiptID, found := r.rowReceipts[dryRun.ImportID+"\x00"+row.RowID]; found {
				if receipt, ok := r.receipts[receiptID]; ok && receipt.Status == ReceiptApplied {
					result.Receipts = append(result.Receipts, *receipt)
					result.Applied++
					continue
				}
			}
		}
		if opts.GroupSize > 0 && attempted >= opts.GroupSize {
			result.Unattempted++
			continue
		}
		if ctx.Err() != nil {
			for range rows[i:] {
				result.Unattempted++
			}
			break
		}
		attempted++
		outcome := r.commitRow(ctx, op, dryRun, row, shapes, aliasHolders, serialHolders, keyHolders, i)
		if outcome.pending {
			result.Pending++
			result.PendingRows = append(result.PendingRows, ImportPendingRow{RowID: row.RowID, CSVLine: row.CSVLine, Code: outcome.code, Message: outcome.message})
			continue
		}
		state := importdraft.ItemState{Status: importdraft.ItemCommitted}
		if outcome.receipt == nil {
			state = importdraft.ItemState{Status: importdraft.ItemFailed, FailureCode: outcome.code}
			result.Failed++
			result.Errors = append(result.Errors, rowError(row, outcome.field, outcome.code, outcome.message))
		} else {
			state.Lineage = importdraft.ItemLineage{TransactionRef: outcome.receipt.MovementRef, ObservationRef: outcome.receipt.ReceiptID, ReconciliationStatus: importdraft.ReconciliationMatched}
			result.Receipts = append(result.Receipts, *outcome.receipt)
			result.Applied++
		}
		next := map[string]importdraft.ItemState{}
		for id, state := range items {
			next[id] = state
		}
		next[row.RowID] = state
		saved := cp
		saved.Items, saved.Version = next, cp.Version+1
		if err := r.checkpoints.Save(ctx, saved, cp.Version); err != nil {
			return ImportCommitResult{}, fmt.Errorf("stock: save import checkpoint after row %d: %w", row.CSVLine, err)
		}
		cp, items = saved, next
	}
	result.Complete = result.Unattempted == 0 && result.Pending == 0
	return result, nil
}

func batchDuplicates(op OperationContext, rows []importRow) (map[string][]int, map[string][]int, map[string][]int) {
	aliasHolders := map[string][]int{}
	serialHolders := map[string][]int{}
	keyHolders := map[string][]int{}
	for i, row := range rows {
		if row.Kind == ImportRowItem && row.BarcodeSym != "" && row.BarcodeCode != "" {
			key := row.BarcodeSym + "\x00" + row.BarcodeCode
			aliasHolders[key] = append(aliasHolders[key], i)
		}
		if row.Kind == ImportRowOpening && row.Serial != "" {
			key := row.SerialNS + "\x00" + row.Serial
			serialHolders[key] = append(serialHolders[key], i)
		}
		if row.ExternalKey != "" {
			identity := importRowIdentity(row, op)
			keyHolders[identity] = append(keyHolders[identity], i)
		}
	}
	return aliasHolders, serialHolders, keyHolders
}

func (r *InventoryImporter) loadCommitCheckpoint(ctx context.Context, dryRun ImportDryRun) (importdraft.Checkpoint, error) {
	cp, found, err := r.checkpoints.Load(ctx, dryRun.ImportID)
	if err != nil {
		return importdraft.Checkpoint{}, fmt.Errorf("stock: load import checkpoint: %w", err)
	}
	if !found {
		return importdraft.Checkpoint{ImportID: dryRun.ImportID, SimulationDigest: dryRun.DryRunDigest, BatchDigest: dryRun.BatchDigest, MappingDigest: ImportSchemaDigest}, nil
	}
	if cp.SimulationDigest != dryRun.DryRunDigest || cp.BatchDigest != dryRun.BatchDigest || cp.MappingDigest != ImportSchemaDigest {
		return importdraft.Checkpoint{}, fmt.Errorf("%w: checkpoint belongs to a different proposal", ErrImportConflict)
	}
	return cp, nil
}

type rowOutcome struct {
	receipt *ImportReceipt
	pending bool
	field   string
	code    string
	message string
}

func failedOutcome(field, code, message string) rowOutcome {
	return rowOutcome{field: field, code: code, message: message}
}

// commitRow revalidates one row and applies it. Identical resubmissions
// return the original receipt; only genuinely new rows write.
func (r *InventoryImporter) commitRow(ctx context.Context, op OperationContext, dryRun ImportDryRun, row importRow, shapes map[string]itemShape, aliasHolders, serialHolders, keyHolders map[string][]int, index int) rowOutcome {
	if errs := r.validateRow(op, row, shapes, aliasHolders, serialHolders, keyHolders, index); len(errs) > 0 {
		first := errs[0]
		return failedOutcome(first.Field, first.Code, first.Message)
	}
	if row.ExternalKey != "" {
		if admitted, ok := r.admitted[importRowIdentity(row, op)]; ok && admitted.Digest == importPayloadDigest(row.raw) {
			if receipt, ok := r.receipts[admitted.Receipt]; ok {
				r.rowReceipts[dryRun.ImportID+"\x00"+row.RowID] = receipt.ReceiptID
				dup := *receipt
				return rowOutcome{receipt: &dup}
			}
		}
	}
	if row.Kind == ImportRowItem {
		return r.applyItemRow(ctx, op, dryRun, row)
	}
	return r.applyOpeningRow(ctx, op, dryRun, row, shapes)
}

func (r *InventoryImporter) applyItemRow(_ context.Context, op OperationContext, dryRun ImportDryRun, row importRow) rowOutcome {
	if r.catalog == nil {
		return failedOutcome("sku", "IMPORT_NO_CATALOG", "no catalog is wired")
	}
	draft := ItemDraft{SKU: row.SKU, Name: row.Name, BaseUnit: row.BaseUnit, Scale: row.Scale, Tracking: ItemTracking(row.Tracking)}
	if row.PackUnit != "" {
		num, _ := strconv.ParseInt(row.PackNum, 10, 64)
		den, _ := strconv.ParseInt(row.PackDen, 10, 64)
		draft.Conversions = []PackConversion{{PackUnit: row.PackUnit, Numerator: num, Denominator: den}}
	}
	if row.BarcodeSym != "" {
		draft.Aliases = []BarcodeAlias{{Symbology: row.BarcodeSym, Code: row.BarcodeCode}}
	}
	created, err := r.catalog.CreateItem(context.Background(), op, draft, importdraft.IdempotencyKey(dryRun.DryRunDigest, row.RowID))
	if err != nil {
		if errors.Is(err, ErrDuplicateSKU) {
			return failedOutcome("sku", "IMPORT_SKU_REGISTERED", "sku is already active; reconcile instead of importing")
		}
		if errors.Is(err, ErrAliasConflict) {
			return failedOutcome("barcode_code", "IMPORT_ALIAS_REGISTERED", "barcode is already registered")
		}
		return failedOutcome("sku", "IMPORT_ITEM_FAILED", err.Error())
	}
	r.itemKeys[op.TenantID+"\x00"+op.CompanyID+"\x00"+row.SourceSystem+"\x00"+row.ExternalKey] = created.ItemID
	r.itemSKUs[op.TenantID+"\x00"+op.CompanyID+"\x00"+created.ItemID] = created.SKU
	receipt := &ImportReceipt{
		ReceiptID: uuid.NewString(), ImportID: dryRun.ImportID, RowID: row.RowID, CSVLine: row.CSVLine,
		RecordKind: ImportRowItem, ExternalKey: row.ExternalKey, SourceSystem: row.SourceSystem, SourceVersion: row.SourceVersion,
		ItemID: created.ItemID, ItemSKU: created.SKU, At: time.Now().UTC(), Status: ReceiptApplied,
	}
	r.storeReceipt(op, row, receipt)
	return rowOutcome{receipt: receipt}
}

func (r *InventoryImporter) applyOpeningRow(_ context.Context, op OperationContext, dryRun ImportDryRun, row importRow, shapes map[string]itemShape) rowOutcome {
	shape, ok := r.resolveShape(op, row, shapes)
	if !ok || shape.ItemID == "" {
		if !ok {
			return failedOutcome("item_key", "IMPORT_UNKNOWN_ITEM", "item_key is unknown in scope")
		}
		return failedOutcome("item_key", "IMPORT_ITEM_PENDING", "item is not committed yet; resume after its ITEM row commits")
	}
	qty, err := parseLotQty(row.Quantity, shape.Scale)
	if err != nil || qty.Sign() <= 0 {
		return failedOutcome("quantity", "IMPORT_QUANTITY", "quantity must be a positive exact decimal")
	}
	baseQty := new(big.Int).Set(qty)
	if row.Unit != shape.BaseUnit {
		conv := shape.PackUnits[row.Unit]
		total := new(big.Int).Mul(qty, big.NewInt(conv.Numerator))
		quotient, remainder := new(big.Int).QuoRem(total, big.NewInt(conv.Denominator), new(big.Int))
		if remainder.Sign() != 0 {
			return failedOutcome("quantity", "IMPORT_PRECISION", "pack quantity does not convert exactly")
		}
		baseQty = quotient
	}
	receipt := &ImportReceipt{
		ReceiptID: uuid.NewString(), ImportID: dryRun.ImportID, RowID: row.RowID, CSVLine: row.CSVLine,
		RecordKind: ImportRowOpening, ExternalKey: row.ExternalKey, SourceSystem: row.SourceSystem, SourceVersion: row.SourceVersion,
		ItemID: shape.ItemID, ItemSKU: shape.SKU, Site: row.Site, Bucket: row.Bucket, QualityReason: row.QualityReason,
		AsOf: row.AsOf, At: time.Now().UTC(), Status: ReceiptApplied,
	}
	qtyText, err := canonicalQuantity(baseQty, shape.Scale)
	if err != nil {
		return failedOutcome("quantity", "IMPORT_QUANTITY", err.Error())
	}
	receipt.Qty, receipt.Unit = qtyText, shape.BaseUnit
	receipt.MovementRef = "RECEIVE/OPENING_RECEIPT:" + receipt.ReceiptID
	switch row.Bucket {
	case BucketTransit:
		record := &TransitRecord{
			TransferRef: row.TransferRef, CompanyID: op.CompanyID, ItemID: shape.ItemID, ItemSKU: shape.SKU,
			FromSite: row.FromLocation, ToSite: row.ToLocation, Qty: qtyText, Scale: shape.Scale,
			DispatchRef: row.DispatchRef, Status: TransitPending,
		}
		r.transits[op.TenantID+"\x00"+op.CompanyID+"\x00"+row.TransferRef] = record
		receipt.TransferRef = row.TransferRef
		receipt.Site = row.ToLocation
		if err := r.addToBucket(op, row.ToLocation, shape, BucketTransit, baseQty); err != nil {
			return failedOutcome("quantity", "IMPORT_QUANTITY", err.Error())
		}
	case BucketQuarantine, BucketUsable:
		asset := strings.TrimSpace(row.AssetRef)
		if row.Serial != "" && asset == "" {
			asset = r.attachments[dryRun.ImportID+"\x00"+row.RowID]
		}
		if row.Serial != "" && asset == "" {
			return rowOutcome{pending: true, code: "IMPORT_ASSET_LINKAGE", message: "serial needs a verified asset reference before it can commit"}
		}
		if row.LotSupplier != "" {
			lotID, lotErr := r.commitOpeningLot(op, dryRun, row, shape, qtyText)
			if lotErr != nil {
				return *lotErr
			}
			receipt.LotID = lotID
		}
		if row.Serial != "" {
			serialErr := r.commitOpeningSerial(op, dryRun, row, shape, receipt, asset)
			if serialErr != nil {
				return *serialErr
			}
		}
		if err := r.addToBucket(op, row.Site, shape, row.Bucket, baseQty); err != nil {
			return failedOutcome("quantity", "IMPORT_QUANTITY", err.Error())
		}
	default:
		return failedOutcome("physical_bucket", "IMPORT_BUCKET", "physical_bucket must be USABLE, QUARANTINE or IN_TRANSIT")
	}
	r.storeReceipt(op, row, receipt)
	return rowOutcome{receipt: receipt}
}

func (r *InventoryImporter) storeReceipt(op OperationContext, row importRow, receipt *ImportReceipt) {
	r.receipts[receipt.ReceiptID] = receipt
	r.rowReceipts[receipt.ImportID+"\x00"+row.RowID] = receipt.ReceiptID
	r.admitted[importRowIdentity(row, op)] = admittedIdentity{Version: row.SourceVersion, Digest: importPayloadDigest(row.raw), Receipt: receipt.ReceiptID, ImportID: receipt.ImportID}
}

func (r *InventoryImporter) commitOpeningLot(op OperationContext, dryRun ImportDryRun, row importRow, shape itemShape, qtyText string) (string, *rowOutcome) {
	if r.lots == nil {
		out := failedOutcome("lot_supplier", "IMPORT_NO_LOTS", "no lot registry is wired")
		return "", &out
	}
	lot, err := r.lots.RegisterLot(context.Background(), op, RegisterLotInput{
		ItemID: shape.ItemID, SiteID: row.Site, SupplierLot: row.LotSupplier,
		ReceivedFrom: row.SourceSystem, ReceivedDoc: row.OriginReceipt,
		ReceivedAt: dryRun.RetrievedAt, Expiry: ExpiryPolicy{LocalDate: row.LotDate, Timezone: row.LotTZ, Rule: ExpiryNextLocalDayStart},
		PolicyRevision: ifEmpty(row.PolicyVersion, "import-v1"), Scale: shape.Scale, OnHand: qtyText,
	}, importdraft.IdempotencyKey(dryRun.DryRunDigest, row.RowID+":lot"))
	if err != nil {
		out := failedOutcome("lot_supplier", "IMPORT_LOT_FAILED", err.Error())
		return "", &out
	}
	if row.Bucket == BucketQuarantine {
		if _, err := r.lots.HoldLot(context.Background(), op, lot.LotID, row.QualityReason, row.PolicyVersion, importdraft.IdempotencyKey(dryRun.DryRunDigest, row.RowID+":hold")); err != nil {
			out := failedOutcome("quality_reason", "IMPORT_HOLD_FAILED", err.Error())
			return "", &out
		}
	}
	return lot.LotID, nil
}

func (r *InventoryImporter) commitOpeningSerial(op OperationContext, dryRun ImportDryRun, row importRow, shape itemShape, receipt *ImportReceipt, asset string) *rowOutcome {
	if r.lots == nil {
		out := failedOutcome("serial", "IMPORT_NO_LOTS", "no lot registry is wired")
		return &out
	}
	namespace := row.SerialNS
	if namespace == "" {
		namespace = SerialNamespaceLegacy
	}
	unit, err := r.lots.RegisterSerial(context.Background(), op, RegisterSerialInput{
		Namespace: namespace, Serial: row.Serial, AssetRef: asset,
		ItemID: shape.ItemID, LotID: receipt.LotID, SiteID: row.Site,
	}, importdraft.IdempotencyKey(dryRun.DryRunDigest, row.RowID+":serial"))
	if err != nil {
		out := failedOutcome("serial", "IMPORT_SERIAL_FAILED", err.Error())
		return &out
	}
	receipt.SerialNS, receipt.Serial = unit.Namespace, unit.Serial
	return nil
}

func ifEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (r *InventoryImporter) bucketFor(companyID, tenantID, siteID, sku string, scale int32) *ledgerBucket {
	key := tenantID + "\x00" + companyID + "\x00" + siteID + "\x00" + sku
	bucket, ok := r.ledger[key]
	if !ok {
		zero, _ := canonicalQuantity(new(big.Int), scale)
		bucket = &ledgerBucket{Usable: zero, Quarantine: zero, Transit: zero, Scale: scale}
		r.ledger[key] = bucket
	}
	return bucket
}

func (r *InventoryImporter) addToBucket(op OperationContext, siteID string, shape itemShape, bucketName string, delta *big.Int) error {
	bucket := r.bucketFor(op.CompanyID, op.TenantID, siteID, shape.SKU, shape.Scale)
	current := bucket.Usable
	switch bucketName {
	case BucketQuarantine:
		current = bucket.Quarantine
	case BucketTransit:
		current = bucket.Transit
	}
	total, err := parseLotQty(current, shape.Scale)
	if err != nil {
		return err
	}
	updated, err := canonicalQuantity(new(big.Int).Add(total, delta), shape.Scale)
	if err != nil {
		return err
	}
	switch bucketName {
	case BucketQuarantine:
		bucket.Quarantine = updated
	case BucketTransit:
		bucket.Transit = updated
	default:
		bucket.Usable = updated
	}
	return nil
}

// CorrectOpeningRow supersedes one applied opening receipt with a new source
// version and quantity. The original stays visible and linked; the delta
// posts once under the caller key.
func (r *InventoryImporter) CorrectOpeningRow(_ context.Context, op OperationContext, correction ImportCorrection, key string) (ImportReceipt, error) {
	if err := op.validate(); err != nil {
		return ImportReceipt{}, err
	}
	if strings.TrimSpace(correction.ReceiptID) == "" || strings.TrimSpace(correction.SourceVersion) == "" {
		return ImportReceipt{}, fmt.Errorf("%w: correction needs a receipt and new source version", ErrImportCorrection)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	mapKey := op.TenantID + "\x00" + op.CompanyID + "\x00" + strings.TrimSpace(key)
	if strings.TrimSpace(key) != "" {
		if prior, ok := r.corrections[mapKey]; ok {
			if receipt, found := r.receipts[prior]; found {
				return *receipt, nil
			}
		}
	}
	original, ok := r.receipts[strings.TrimSpace(correction.ReceiptID)]
	if !ok || original.Status != ReceiptApplied || original.RecordKind != ImportRowOpening {
		return ImportReceipt{}, fmt.Errorf("%w: receipt is not a correctable opening balance", ErrImportCorrection)
	}
	if original.SourceVersion == strings.TrimSpace(correction.SourceVersion) {
		return ImportReceipt{}, fmt.Errorf("%w: correction needs a new source version", ErrImportCorrection)
	}
	if original.Bucket == BucketTransit {
		return ImportReceipt{}, fmt.Errorf("%w: transit corrections reverse through movements", ErrImportCorrection)
	}
	shape, err := r.shapeForReceipt(op, original)
	if err != nil {
		return ImportReceipt{}, err
	}
	unit := strings.TrimSpace(correction.Unit)
	if unit == "" {
		unit = original.Unit
	}
	if unit != shape.BaseUnit {
		if _, defined := shape.PackUnits[unit]; !defined {
			return ImportReceipt{}, fmt.Errorf("%w: unit %q is not defined for this item", ErrImportCorrection, unit)
		}
	}
	newQty, err := parseLotQty(correction.Quantity, shape.Scale)
	if err != nil || newQty.Sign() <= 0 {
		return ImportReceipt{}, fmt.Errorf("%w: quantity must be a positive exact decimal", ErrImportCorrection)
	}
	if unit != shape.BaseUnit {
		conv := shape.PackUnits[unit]
		total := new(big.Int).Mul(newQty, big.NewInt(conv.Numerator))
		quotient, remainder := new(big.Int).QuoRem(total, big.NewInt(conv.Denominator), new(big.Int))
		if remainder.Sign() != 0 {
			return ImportReceipt{}, fmt.Errorf("%w: pack quantity does not convert exactly", ErrImportCorrection)
		}
		newQty = quotient
	}
	oldQty, err := parseLotQty(original.Qty, shape.Scale)
	if err != nil {
		return ImportReceipt{}, err
	}
	delta := new(big.Int).Sub(newQty, oldQty)
	bucket := r.bucketFor(op.CompanyID, op.TenantID, r.receiptSite(original), shape.SKU, shape.Scale)
	current := bucket.Usable
	if original.Bucket == BucketQuarantine {
		current = bucket.Quarantine
	}
	total, err := parseLotQty(current, shape.Scale)
	if err != nil {
		return ImportReceipt{}, err
	}
	updated := new(big.Int).Add(total, delta)
	if updated.Sign() < 0 {
		return ImportReceipt{}, fmt.Errorf("%w: correction would drive the bucket negative", ErrImportCorrection)
	}
	updatedText, err := canonicalQuantity(updated, shape.Scale)
	if err != nil {
		return ImportReceipt{}, err
	}
	newQtyText, err := canonicalQuantity(newQty, shape.Scale)
	if err != nil {
		return ImportReceipt{}, err
	}
	if original.LotID != "" {
		if r.lots == nil {
			return ImportReceipt{}, fmt.Errorf("%w: no lot registry is wired", ErrImportCorrection)
		}
		if err := r.lots.AmendLotOnHand(op, original.LotID, newQtyText, "opening correction "+strings.TrimSpace(correction.SourceVersion)); err != nil {
			return ImportReceipt{}, fmt.Errorf("%w: %v", ErrImportCorrection, err)
		}
	}
	if original.Bucket == BucketQuarantine {
		bucket.Quarantine = updatedText
	} else {
		bucket.Usable = updatedText
	}
	now := time.Now().UTC()
	next := &ImportReceipt{
		ReceiptID: uuid.NewString(), ImportID: original.ImportID, RowID: original.RowID, CSVLine: original.CSVLine,
		RecordKind: ImportRowOpening, ExternalKey: original.ExternalKey, SourceSystem: original.SourceSystem,
		SourceVersion: strings.TrimSpace(correction.SourceVersion),
		ItemID:        original.ItemID, ItemSKU: original.ItemSKU, LotID: original.LotID,
		SerialNS: original.SerialNS, Serial: original.Serial,
		Qty: newQtyText, Unit: shape.BaseUnit, Bucket: original.Bucket, QualityReason: original.QualityReason,
		AsOf: original.AsOf, At: now, CorrectsReceiptID: original.ReceiptID, Status: ReceiptApplied,
	}
	next.MovementRef = "RECEIVE/OPENING_RECEIPT:" + next.ReceiptID
	original.Status = ReceiptSuperseded
	r.receipts[next.ReceiptID] = next
	r.rowReceipts[next.ImportID+"\x00"+next.RowID] = next.ReceiptID
	identity := importIdentityKey(original.SourceSystem, op.TenantID, op.CompanyID, original.RecordKind, original.ExternalKey)
	r.admitted[identity] = admittedIdentity{Version: next.SourceVersion, Digest: "correction\x00" + newQtyText, Receipt: next.ReceiptID, ImportID: next.ImportID}
	if strings.TrimSpace(key) != "" {
		r.corrections[mapKey] = next.ReceiptID
	}
	return *next, nil
}

func (r *InventoryImporter) shapeForReceipt(op OperationContext, receipt *ImportReceipt) (itemShape, error) {
	if r.catalog == nil {
		return itemShape{}, fmt.Errorf("%w: no catalog is wired", ErrImportCorrection)
	}
	history, err := r.catalog.History(op, receipt.ItemID)
	if err != nil || len(history) == 0 {
		return itemShape{}, fmt.Errorf("%w: item is gone", ErrImportCorrection)
	}
	return catalogShape(history[len(history)-1]), nil
}

func (r *InventoryImporter) receiptSite(receipt *ImportReceipt) string {
	return receipt.Site
}

// AttachSerialAsset supplies the verified asset reference for a pending
// serial row; the next resume commits it.
func (r *InventoryImporter) AttachSerialAsset(_ context.Context, op OperationContext, importID, rowID, assetRef string) error {
	if err := op.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(assetRef) == "" {
		return fmt.Errorf("%w: asset reference is required", ErrImportLinkage)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attachments[strings.TrimSpace(importID)+"\x00"+strings.TrimSpace(rowID)] = strings.TrimSpace(assetRef)
	return nil
}

// CompleteTransitReceive moves one in-transit opening to its destination
// usable bucket exactly once under the caller key.
func (r *InventoryImporter) CompleteTransitReceive(_ context.Context, op OperationContext, transferRef, key string) (TransitRecord, error) {
	if err := op.validate(); err != nil {
		return TransitRecord{}, err
	}
	trimmed := strings.TrimSpace(transferRef)
	if trimmed == "" {
		return TransitRecord{}, fmt.Errorf("%w: transfer reference is required", ErrImportState)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.transits[op.TenantID+"\x00"+op.CompanyID+"\x00"+trimmed]
	if !ok {
		return TransitRecord{}, fmt.Errorf("%w: transfer %q is unknown", ErrImportState, trimmed)
	}
	mapKey := op.TenantID + "\x00" + op.CompanyID + "\x00" + strings.TrimSpace(key)
	if record.Status == TransitReceived {
		if stored, ok := r.transitKeys[mapKey]; ok && stored == record.TransferRef {
			return *record, nil
		}
		return *record, nil
	}
	qty, err := parseLotQty(record.Qty, record.Scale)
	if err != nil {
		return TransitRecord{}, err
	}
	bucket := r.bucketFor(op.CompanyID, op.TenantID, record.ToSite, record.ItemSKU, record.Scale)
	transit, err := parseLotQty(bucket.Transit, record.Scale)
	if err != nil {
		return TransitRecord{}, err
	}
	usable, err := parseLotQty(bucket.Usable, record.Scale)
	if err != nil {
		return TransitRecord{}, err
	}
	remaining := new(big.Int).Sub(transit, qty)
	if remaining.Sign() < 0 {
		return TransitRecord{}, fmt.Errorf("%w: transit balance is short", ErrImportState)
	}
	remainingText, err := canonicalQuantity(remaining, record.Scale)
	if err != nil {
		return TransitRecord{}, err
	}
	usableText, err := canonicalQuantity(new(big.Int).Add(usable, qty), record.Scale)
	if err != nil {
		return TransitRecord{}, err
	}
	bucket.Transit, bucket.Usable = remainingText, usableText
	record.Status = TransitReceived
	if strings.TrimSpace(key) != "" {
		r.transitKeys[mapKey] = record.TransferRef
	}
	return *record, nil
}

// ReleaseOpeningQuarantine releases one quarantined opening lot to usable
// after revalidating expiry, exactly once per lot.
func (r *InventoryImporter) ReleaseOpeningQuarantine(ctx context.Context, op OperationContext, lotID, reviewer, evidence string, asOf time.Time, key string) (Lot, error) {
	if err := op.validate(); err != nil {
		return Lot{}, err
	}
	if r.lots == nil {
		return Lot{}, fmt.Errorf("%w: no lot registry is wired", ErrImportState)
	}
	r.mu.Lock()
	mapKey := op.TenantID + "\x00" + op.CompanyID + "\x00" + strings.TrimSpace(key)
	if stored, ok := r.releaseKeys[mapKey]; ok && stored != "" {
		r.mu.Unlock()
		trace, err := r.lots.TraceLot(op, lotID)
		if err != nil {
			return Lot{}, err
		}
		return trace.Lot, nil
	}
	trace, err := r.lots.TraceLot(op, lotID)
	if err != nil {
		r.mu.Unlock()
		return Lot{}, err
	}
	onHand, err := parseLotQty(trace.Lot.OnHand, trace.Lot.Scale)
	if err != nil {
		r.mu.Unlock()
		return Lot{}, err
	}
	r.mu.Unlock()
	released, err := r.lots.ReleaseLot(ctx, op, lotID, reviewer, evidence, asOf, importdraft.IdempotencyKey("smb-stock-012-release/v1", lotID+strings.TrimSpace(key)))
	if err != nil {
		return Lot{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	bucket := r.bucketFor(op.CompanyID, op.TenantID, trace.Lot.SiteID, trace.Lot.ItemSKU, trace.Lot.Scale)
	quarantine, err := parseLotQty(bucket.Quarantine, trace.Lot.Scale)
	if err != nil {
		return Lot{}, err
	}
	usable, err := parseLotQty(bucket.Usable, trace.Lot.Scale)
	if err != nil {
		return Lot{}, err
	}
	remaining := new(big.Int).Sub(quarantine, onHand)
	if remaining.Sign() < 0 {
		remaining = new(big.Int)
	}
	remainingText, err := canonicalQuantity(remaining, trace.Lot.Scale)
	if err != nil {
		return Lot{}, err
	}
	usableText, err := canonicalQuantity(new(big.Int).Add(usable, onHand), trace.Lot.Scale)
	if err != nil {
		return Lot{}, err
	}
	bucket.Quarantine, bucket.Usable = remainingText, usableText
	if strings.TrimSpace(key) != "" {
		r.releaseKeys[mapKey] = lotID
	}
	return released, nil
}

// MarkLocationLive sets the explicit cutover watermark: later opening rows
// for the site and SKU refuse with a reconcile order instead of posting.
func (r *InventoryImporter) MarkLocationLive(_ context.Context, op OperationContext, siteID, sku string) error {
	if err := op.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(siteID) == "" || strings.TrimSpace(sku) == "" {
		return fmt.Errorf("%w: site and sku are required", ErrImportValidation)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.live[op.TenantID+"\x00"+op.CompanyID+"\x00"+strings.TrimSpace(siteID)+"\x00"+strings.TrimSpace(sku)] = true
	return nil
}

// OpeningBalances reads bucketed opening quantities for one site and SKU.
// Scopes with no imported data refuse instead of answering from nothing.
func (r *InventoryImporter) OpeningBalances(op OperationContext, siteID, sku string) (OpeningBalances, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	prefix := "\x00" + op.TenantID + "\x00" + op.CompanyID + "\x00"
	scopeKnown := false
	for id := range r.admitted {
		if strings.Contains(id, prefix) {
			scopeKnown = true
			break
		}
	}
	if !scopeKnown {
		return OpeningBalances{}, ErrStockScope
	}
	bucket, ok := r.ledger[op.TenantID+"\x00"+op.CompanyID+"\x00"+strings.TrimSpace(siteID)+"\x00"+strings.TrimSpace(sku)]
	if !ok {
		return OpeningBalances{Usable: "0", Quarantine: "0", Transit: "0", OnHandTotal: "0", Available: "0"}, nil
	}
	usable, err := parseLotQty(bucket.Usable, bucket.Scale)
	if err != nil {
		return OpeningBalances{}, err
	}
	quarantine, err := parseLotQty(bucket.Quarantine, bucket.Scale)
	if err != nil {
		return OpeningBalances{}, err
	}
	total, err := canonicalQuantity(new(big.Int).Add(usable, quarantine), bucket.Scale)
	if err != nil {
		return OpeningBalances{}, err
	}
	return OpeningBalances{Usable: bucket.Usable, Quarantine: bucket.Quarantine, Transit: bucket.Transit, OnHandTotal: total, Available: bucket.Usable, Scale: bucket.Scale}, nil
}

// ImportReceipt returns one receipt by ID within the caller scope.
func (r *InventoryImporter) ImportReceipt(op OperationContext, receiptID string) (ImportReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt, ok := r.receipts[strings.TrimSpace(receiptID)]
	if !ok {
		return ImportReceipt{}, fmt.Errorf("%w: %s", ErrItemNotFound, receiptID)
	}
	return *receipt, nil
}

// importSnapshot is the durable importer state: receipts, admissions,
// ledger buckets, transit records, attachments and watermarks.
type importSnapshot struct {
	Admitted    map[string]admittedIdentity
	RowReceipts map[string]string
	ItemKeys    map[string]string
	ItemSKUs    map[string]string
	Ledger      map[string]ledgerBucket
	Transits    map[string]TransitRecord
	Attachments map[string]string
	Live        map[string]bool
	Receipts    map[string]ImportReceipt
	Corrections map[string]string
	TransitKeys map[string]string
	ReleaseKeys map[string]string
}

// Snapshot exports receipts, admissions, ledger buckets, transit records and
// watermarks so a restart resumes the same obligations.
func (r *InventoryImporter) Snapshot() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	export := importSnapshot{
		Admitted: r.admitted, RowReceipts: r.rowReceipts, ItemKeys: r.itemKeys, ItemSKUs: r.itemSKUs,
		Ledger:      make(map[string]ledgerBucket, len(r.ledger)),
		Transits:    make(map[string]TransitRecord, len(r.transits)),
		Attachments: r.attachments, Live: r.live,
		Receipts:    make(map[string]ImportReceipt, len(r.receipts)),
		Corrections: r.corrections, TransitKeys: r.transitKeys, ReleaseKeys: r.releaseKeys,
	}
	for key, bucket := range r.ledger {
		export.Ledger[key] = *bucket
	}
	for key, record := range r.transits {
		export.Transits[key] = *record
	}
	for id, receipt := range r.receipts {
		export.Receipts[id] = *receipt
	}
	return json.Marshal(export)
}

// Restore loads a Snapshot into an empty importer.
func (r *InventoryImporter) Restore(data []byte) error {
	var export importSnapshot
	if err := json.Unmarshal(data, &export); err != nil {
		return fmt.Errorf("%w: %v", ErrSnapshot, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.receipts) > 0 || len(r.admitted) > 0 {
		return fmt.Errorf("%w: importer is already populated", ErrSnapshot)
	}
	for id, receipt := range export.Receipts {
		if receipt.ReceiptID != id || receipt.ImportID == "" {
			return fmt.Errorf("%w: receipt %s is corrupt", ErrSnapshot, id)
		}
	}
	r.admitted = export.Admitted
	if r.admitted == nil {
		r.admitted = make(map[string]admittedIdentity)
	}
	r.rowReceipts = export.RowReceipts
	if r.rowReceipts == nil {
		r.rowReceipts = make(map[string]string)
	}
	r.itemKeys = export.ItemKeys
	if r.itemKeys == nil {
		r.itemKeys = make(map[string]string)
	}
	r.itemSKUs = export.ItemSKUs
	if r.itemSKUs == nil {
		r.itemSKUs = make(map[string]string)
	}
	r.ledger = make(map[string]*ledgerBucket, len(export.Ledger))
	for key, bucket := range export.Ledger {
		stored := bucket
		r.ledger[key] = &stored
	}
	r.transits = make(map[string]*TransitRecord, len(export.Transits))
	for key, record := range export.Transits {
		stored := record
		r.transits[key] = &stored
	}
	r.attachments = export.Attachments
	if r.attachments == nil {
		r.attachments = make(map[string]string)
	}
	r.live = export.Live
	if r.live == nil {
		r.live = make(map[string]bool)
	}
	r.receipts = make(map[string]*ImportReceipt, len(export.Receipts))
	for id, receipt := range export.Receipts {
		stored := receipt
		r.receipts[id] = &stored
	}
	r.corrections = export.Corrections
	if r.corrections == nil {
		r.corrections = make(map[string]string)
	}
	r.transitKeys = export.TransitKeys
	if r.transitKeys == nil {
		r.transitKeys = make(map[string]string)
	}
	r.releaseKeys = export.ReleaseKeys
	if r.releaseKeys == nil {
		r.releaseKeys = make(map[string]string)
	}
	return nil
}
