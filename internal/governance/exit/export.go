package exit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const ExportSchemaVersion = "tenant-export/v1"

var ErrExportPackage = errors.New("exit: invalid tenant export package")

// ExportCategory contains a category's schema and the records read from its
// tenant-scoped source. Record values are copied into the package; source
// adapters are responsible for reading only the named tenant and copy.
type ExportCategory struct {
	Category Category          `json:"category"`
	CopyID   string            `json:"copy_id"`
	Fields   []string          `json:"fields"`
	Records  []json.RawMessage `json:"records"`
}

// ExportManifest describes the exact inventory and data included in a package.
type ExportManifest struct {
	SchemaVersion   string           `json:"schema_version"`
	Tenant          string           `json:"tenant"`
	Recipient       string           `json:"recipient"`
	InventoryDigest string           `json:"inventory_digest"`
	Categories      []ExportCategory `json:"categories"`
}

// ExportPackage carries the manifest, bytes, and checksum produced by
// BuildExportPackage. The output is immutable by convention and independently
// verifiable with VerifyExportPackage.
type ExportPackage struct {
	Manifest ExportManifest `json:"manifest"`
	Bytes    []byte         `json:"bytes"`
	Checksum string         `json:"checksum"`
	Receipt  ExportReceipt  `json:"receipt"`
}

// ExportBuildRequest contains data already read from tenant-scoped stores.
// This pure package validates and serializes those values; it does not read or
// delete source data.
type ExportBuildRequest struct {
	ID         string           `json:"id"`
	Tenant     string           `json:"tenant"`
	Recipient  string           `json:"recipient"`
	Copies     []Copy           `json:"copies"`
	Categories []ExportCategory `json:"categories"`
}

// BuildExportPackage builds a deterministic, schema-described package bound
// to the provided tenant copy inventory.
func BuildExportPackage(req ExportBuildRequest) (ExportPackage, error) {
	if strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.Tenant) == "" || strings.TrimSpace(req.Recipient) == "" || len(req.Copies) == 0 {
		return ExportPackage{}, fmt.Errorf("%w: id, tenant, recipient and copy inventory are required", ErrExportPackage)
	}
	copies := cloneCopies(req.Copies)
	sort.Slice(copies, func(i, j int) bool { return copies[i].ID < copies[j].ID })
	copyByCategory := make(map[Category]string, len(copies))
	seenCopies := make(map[string]bool, len(copies))
	for _, copy := range copies {
		if copy.ID == "" || seenCopies[copy.ID] || copy.Tenant != req.Tenant || copy.Category == "" {
			return ExportPackage{}, fmt.Errorf("%w: copy inventory has missing, duplicate or cross-tenant entries", ErrExportPackage)
		}
		seenCopies[copy.ID] = true
		if _, exists := copyByCategory[copy.Category]; !exists {
			copyByCategory[copy.Category] = copy.ID
		}
	}
	byCategory := make(map[Category]ExportCategory, len(req.Categories))
	for _, category := range req.Categories {
		if _, ok := copyByCategory[category.Category]; !ok || category.CopyID == "" || !seenCopies[category.CopyID] {
			return ExportPackage{}, fmt.Errorf("%w: category source is outside the copy inventory", ErrExportPackage)
		}
		var sourceCopy *Copy
		for i := range copies {
			if copies[i].ID == category.CopyID {
				sourceCopy = &copies[i]
				break
			}
		}
		if sourceCopy == nil || sourceCopy.Category != category.Category {
			return ExportPackage{}, fmt.Errorf("%w: category source does not match inventory", ErrExportPackage)
		}
		if _, duplicate := byCategory[category.Category]; duplicate {
			return ExportPackage{}, fmt.Errorf("%w: duplicate category %s", ErrExportPackage, category.Category)
		}
		fields := append([]string(nil), category.Fields...)
		sort.Strings(fields)
		if len(fields) == 0 {
			return ExportPackage{}, fmt.Errorf("%w: category %s has no schema fields", ErrExportPackage, category.Category)
		}
		for i, field := range fields {
			if strings.TrimSpace(field) == "" || (i > 0 && fields[i-1] == field) {
				return ExportPackage{}, fmt.Errorf("%w: category %s schema has empty or duplicate fields", ErrExportPackage, category.Category)
			}
		}
		records := make([]json.RawMessage, len(category.Records))
		for i, record := range category.Records {
			var values map[string]json.RawMessage
			if len(record) == 0 || json.Unmarshal(record, &values) != nil || values == nil {
				return ExportPackage{}, fmt.Errorf("%w: category %s record %d is not a JSON object", ErrExportPackage, category.Category, i)
			}
			if len(values) != len(fields) {
				return ExportPackage{}, fmt.Errorf("%w: category %s record %d does not match its schema", ErrExportPackage, category.Category, i)
			}
			for _, field := range fields {
				if _, ok := values[field]; !ok {
					return ExportPackage{}, fmt.Errorf("%w: category %s record %d lacks field %q", ErrExportPackage, category.Category, i, field)
				}
			}
			canonical, err := json.Marshal(values)
			if err != nil {
				return ExportPackage{}, fmt.Errorf("%w: encode category %s record: %v", ErrExportPackage, category.Category, err)
			}
			records[i] = canonical
		}
		byCategory[category.Category] = ExportCategory{Category: category.Category, CopyID: category.CopyID, Fields: fields, Records: records}
	}
	categories := make([]ExportCategory, 0, len(RequiredCategories))
	for _, required := range RequiredCategories {
		category, ok := byCategory[required]
		if !ok {
			return ExportPackage{}, fmt.Errorf("%w: required category %s has no exported data", ErrExportPackage, required)
		}
		categories = append(categories, category)
	}
	manifest := ExportManifest{SchemaVersion: ExportSchemaVersion, Tenant: req.Tenant, Recipient: req.Recipient, InventoryDigest: digestValue(copies), Categories: categories}
	data, err := json.Marshal(manifest)
	if err != nil {
		return ExportPackage{}, fmt.Errorf("%w: encode manifest: %v", ErrExportPackage, err)
	}
	sum := sha256.Sum256(data)
	checksum := "sha256:" + hex.EncodeToString(sum[:])
	receipt := ExportReceipt{ID: req.ID, SchemaVersion: ExportSchemaVersion, Checksum: checksum, ExpectedDigest: checksum, Recipient: req.Recipient, Verified: true}
	return ExportPackage{Manifest: manifest, Bytes: data, Checksum: checksum, Receipt: receipt}, nil
}

// ExecuteExit produces the tenant export and immediately evaluates the same
// package in the exit certification flow. The supplied export inventory must
// be the exact inventory the exit request evaluates.
func ExecuteExit(req Request, export ExportBuildRequest) (ExitPlan, ExportPackage, error) {
	if export.Tenant != req.Tenant || digestValue(sortedCopies(export.Copies)) != digestValue(sortedCopies(req.Copies)) {
		return ExitPlan{}, ExportPackage{}, fmt.Errorf("%w: export scope does not match exit request", ErrExportPackage)
	}
	pkg, err := BuildExportPackage(export)
	if err != nil {
		return ExitPlan{}, ExportPackage{}, err
	}
	req.Export, req.ExportPackage = pkg.Receipt, &pkg
	plan, err := Build(req)
	if err != nil {
		return ExitPlan{}, ExportPackage{}, err
	}
	return plan, pkg, nil
}

func sortedCopies(copies []Copy) []Copy {
	sorted := cloneCopies(copies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	return sorted
}

// VerifyExportPackage recomputes the exact manifest bytes and digest.
func VerifyExportPackage(pkg ExportPackage) error {
	if pkg.Manifest.SchemaVersion != ExportSchemaVersion || pkg.Manifest.Tenant == "" || pkg.Manifest.Recipient == "" || pkg.Manifest.InventoryDigest == "" || len(pkg.Manifest.Categories) != len(RequiredCategories) {
		return fmt.Errorf("%w: incomplete manifest", ErrExportPackage)
	}
	data, err := json.Marshal(pkg.Manifest)
	if err != nil || !equalBytes(data, pkg.Bytes) {
		return fmt.Errorf("%w: package bytes do not match manifest", ErrExportPackage)
	}
	sum := sha256.Sum256(data)
	actual := "sha256:" + hex.EncodeToString(sum[:])
	if pkg.Checksum != actual || !receiptMatchesPackage(pkg.Receipt, pkg) {
		return fmt.Errorf("%w: checksum or receipt mismatch", ErrExportPackage)
	}
	seen := make(map[Category]bool, len(pkg.Manifest.Categories))
	for _, category := range pkg.Manifest.Categories {
		if category.Category == "" || category.CopyID == "" || len(category.Fields) == 0 || seen[category.Category] {
			return fmt.Errorf("%w: invalid or duplicate category schema", ErrExportPackage)
		}
		seen[category.Category] = true
		for _, record := range category.Records {
			var values map[string]json.RawMessage
			if json.Unmarshal(record, &values) != nil || values == nil || len(values) != len(category.Fields) {
				return fmt.Errorf("%w: record does not match category schema", ErrExportPackage)
			}
			for _, field := range category.Fields {
				if _, ok := values[field]; !ok {
					return fmt.Errorf("%w: record lacks schema field", ErrExportPackage)
				}
			}
		}
	}
	for _, required := range RequiredCategories {
		if !seen[required] {
			return fmt.Errorf("%w: required category %s missing", ErrExportPackage, required)
		}
	}
	return nil
}

func receiptMatchesPackage(receipt ExportReceipt, pkg ExportPackage) bool {
	return receipt.ID != "" && receipt.SchemaVersion == ExportSchemaVersion && receipt.SchemaVersion == pkg.Manifest.SchemaVersion && receipt.Checksum == pkg.Checksum && receipt.ExpectedDigest == pkg.Checksum && receipt.Recipient == pkg.Manifest.Recipient && receipt.Verified
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
