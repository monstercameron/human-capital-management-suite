// Package storeprivacy implements the WF-REV-014 governance check: every
// field written to a PERMANENT table is classified, and the check fails when
// a personal field is stored inline rather than as a payload-vault
// reference.
//
// Append-only tables block deletion and no table uses field-level
// encryption, so personal data inside an immutable payload or column would
// survive any erasure request, including for reversed records. The secure
// deletion contract (planning/specs/platform-foundation-gap-closure.md
// section 8) requires the ledger to retain only a minimally necessary
// tombstone without the deleted payload; this checker produces the audited
// inventory that WF-REV-015 consumes when it moves flagged fields behind
// per-subject keys.
//
// The checker is static and kernel-pure: it reads the storage-disposition
// registry (definitions/storage/storage-disposition.yaml) and the migration
// DDL, never a database. It writes nothing to definitions/ or planning/;
// findings are returned to the caller and recorded with the table owner.
package storeprivacy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

const (
	// SchemaVersion changes only when the inventory shape changes.
	SchemaVersion = 1
	// DefaultRegistryPath is the source storage registry. It is read, never
	// written, by this package.
	DefaultRegistryPath = "definitions/storage/storage-disposition.yaml"
	// DefaultMigrationsDir holds the DDL this checker classifies.
	DefaultMigrationsDir = "migrations"
)

// Version identifies this policy contract.
func Version() int { return SchemaVersion }

// Field location vocabulary: where the value physically sits.
const (
	// LocationColumn is a scalar DDL column in the table row.
	LocationColumn = "column"
	// LocationPayload is an opaque envelope (bytea/json) carried in the row.
	LocationPayload = "payload"
)

// Field class vocabulary: what kind of data the field holds.
const (
	// ClassPersonal marks data relating to an identifiable subject that an
	// erasure request must reach.
	ClassPersonal = "personal"
	// ClassNonPersonal marks operational facts (identifiers of config,
	// digests, timestamps, policy) with no subject content.
	ClassNonPersonal = "non_personal"
	// ClassUnreviewed marks a payload-capable column no reviewed rule
	// covers. It fails closed: the table needs an explicit declaration.
	ClassUnreviewed = "unreviewed"
)

// Storage vocabulary: how the field value is held.
const (
	// StorageInline is a value stored directly in the PERMANENT row. For a
	// personal field this is the WF-REV-014 violation: erasure cannot reach
	// it without rewriting history.
	StorageInline = "inline"
	// StorageVaultRef is a reference to a payload vault entry held under a
	// per-subject key that destruction can crypto-shred (WF-REV-015). A
	// vault claim is honored only when the registry records FIELD_LEVEL
	// encryption for the table; otherwise it is reported as unproven.
	StorageVaultRef = "vault_ref"
)

// Finding codes. Each code maps to a sentinel via CheckError.Unwrap.
const (
	// CodePersonalInline is a personal field stored inline in a PERMANENT
	// table instead of as a payload-vault reference.
	CodePersonalInline = "PERSONAL_INLINE"
	// CodePayloadUndeclared is a payload-capable column in a PERMANENT
	// table with no reviewed classification. Fail closed, not silent.
	CodePayloadUndeclared = "PAYLOAD_UNDECLARED"
	// CodeVaultUnproven is a payload-vault claim for a table whose
	// registry row still says PLATFORM_MANAGED. A reference that no
	// per-subject key protects is inline storage with an extra hop.
	CodeVaultUnproven = "VAULT_UNPROVEN"
)

var (
	// ErrPersonalInline is matched (via errors.Is) when any personal field
	// is stored inline in a PERMANENT table.
	ErrPersonalInline = errors.New("storeprivacy: personal field stored inline in a PERMANENT table")
	// ErrPayloadUndeclared is matched when a PERMANENT payload column has
	// no reviewed classification.
	ErrPayloadUndeclared = errors.New("storeprivacy: PERMANENT payload column without reviewed classification")
	// ErrVaultUnproven is matched when a vault reference is claimed without
	// FIELD_LEVEL encryption in the registry.
	ErrVaultUnproven = errors.New("storeprivacy: payload-vault claim without FIELD_LEVEL encryption")
)

// Column is one DDL column declared for a table.
type Column struct {
	Table string `json:"table"`
	Name  string `json:"name"`
	Type  string `json:"type"`
}

// FieldRecord is the classification of one field written to a PERMANENT
// table. Owner is the registry owner_package; remediation is tracked by the
// owning team and consumed by WF-REV-015.
type FieldRecord struct {
	Table    string `json:"table"`
	Field    string `json:"field"`
	Location string `json:"location"`
	Class    string `json:"class"`
	Storage  string `json:"storage"`
	Owner    string `json:"owner"`
	Reason   string `json:"reason"`
}

// Finding is one machine-readable privacy violation. Table and Field locate
// the value, Code is stable for consumers, and Owner names the accountable
// package from the storage-disposition registry.
type Finding struct {
	Table    string `json:"table"`
	Field    string `json:"field"`
	Location string `json:"location"`
	Code     string `json:"code"`
	Owner    string `json:"owner"`
	Detail   string `json:"detail"`
}

func (f Finding) Error() string {
	return fmt.Sprintf("storeprivacy: %s.%s: %s: %s", f.Table, f.Field, f.Code, f.Detail)
}

// TableSummary is the per-table rollup pinned by the golden inventory.
type TableSummary struct {
	Table          string `json:"table"`
	Owner          string `json:"owner"`
	Fields         int    `json:"fields"`
	PersonalInline int    `json:"personal_inline"`
}

// Inventory is the deterministic WF-REV-014 output: every classified field
// written to a PERMANENT table plus the findings that fail the check.
type Inventory struct {
	Version         int            `json:"version"`
	PermanentTables int            `json:"permanent_tables"`
	Tables          []TableSummary `json:"tables"`
	Fields          []FieldRecord  `json:"fields"`
	Findings        []Finding      `json:"findings"`
}

// Roster returns the pinned subset: every finding in stable order. The
// golden file pins exactly this plus the table/field counts, so a new
// personal field or a new PERMANENT table breaks the golden until reviewed.
func (inv Inventory) Roster() []Finding {
	out := append([]Finding(nil), inv.Findings...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Table != out[j].Table {
			return out[i].Table < out[j].Table
		}
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// Digest is a stable identity for the pinned roster and counts.
func (inv Inventory) Digest() string {
	roster := inv.Roster()
	b, _ := json.Marshal(struct {
		Version         int       `json:"version"`
		PermanentTables int       `json:"permanent_tables"`
		Fields          int       `json:"fields"`
		Roster          []Finding `json:"roster"`
	}{inv.Version, inv.PermanentTables, len(inv.Fields), roster})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns bounded structural facts suitable for logs and evidence.
func (inv Inventory) Explain() string {
	return fmt.Sprintf("store privacy inventory v%d: %d PERMANENT tables, %d fields, %d findings (%s)",
		inv.Version, inv.PermanentTables, len(inv.Fields), len(inv.Findings), inv.Digest())
}

// CheckError is the failing result. It supports errors.Is against
// ErrPersonalInline, ErrPayloadUndeclared and ErrVaultUnproven according to
// which finding codes are present.
type CheckError struct {
	Findings []Finding
}

func (e *CheckError) Error() string {
	parts := make([]string, len(e.Findings))
	for i, f := range e.Findings {
		parts[i] = f.Error()
	}
	return strings.Join(parts, "; ")
}

// Unwrap maps present finding codes to their sentinels.
func (e *CheckError) Unwrap() []error {
	seen := map[string]bool{}
	var out []error
	for _, f := range e.Findings {
		if seen[f.Code] {
			continue
		}
		seen[f.Code] = true
		switch f.Code {
		case CodePersonalInline:
			out = append(out, ErrPersonalInline)
		case CodePayloadUndeclared:
			out = append(out, ErrPayloadUndeclared)
		case CodeVaultUnproven:
			out = append(out, ErrVaultUnproven)
		}
	}
	return out
}

// Scan loads the storage registry and classifies every DDL column written
// to a PERMANENT table. root is the repository root. It never connects to a
// database and never writes files.
func Scan(root string) (Inventory, error) {
	registryPath := root + "/" + DefaultRegistryPath
	registry, err := storagedisposition.Load(registryPath)
	if err != nil {
		return Inventory{}, fmt.Errorf("storeprivacy: load registry: %w", err)
	}
	if err := storagedisposition.Validate(registry); err != nil {
		return Inventory{}, fmt.Errorf("storeprivacy: validate registry: %w", err)
	}
	return Assemble(registry, root+"/"+DefaultMigrationsDir)
}

// Assemble classifies columns from a migrations directory against an
// already-loaded registry. It is the seam the security tests drive with
// synthetic registries.
func Assemble(registry *storagedisposition.Registry, migrationsDir string) (Inventory, error) {
	permanent := map[string]storagedisposition.TableEntry{}
	for _, entry := range registry.Tables {
		if entry.RetentionClass == storagedisposition.RetentionPermanent {
			permanent[strings.ToLower(entry.Table)] = entry
		}
	}
	inScope := make(map[string]bool, len(permanent))
	for name := range permanent {
		inScope[name] = true
	}
	columns, err := scanColumns(migrationsDir, inScope)
	if err != nil {
		return Inventory{}, err
	}
	if err := checkDeclarations(permanent, columns); err != nil {
		return Inventory{}, err
	}
	inv := Inventory{Version: SchemaVersion, PermanentTables: len(permanent)}
	summaries := map[string]TableSummary{}
	for _, col := range columns {
		entry := permanent[strings.ToLower(col.Table)]
		record := Classify(col, entry)
		inv.Fields = append(inv.Fields, record)
		s, ok := summaries[col.Table]
		if !ok {
			s = TableSummary{Table: col.Table, Owner: entry.OwnerPackage}
		}
		s.Fields++
		if record.Class == ClassPersonal && record.Storage == StorageInline {
			s.PersonalInline++
			inv.Findings = append(inv.Findings, Finding{
				Table:    col.Table,
				Field:    col.Name,
				Location: record.Location,
				Code:     CodePersonalInline,
				Owner:    entry.OwnerPackage,
				Detail:   record.Reason + "; stored inline, no payload-vault reference",
			})
		}
		if record.Class == ClassUnreviewed {
			inv.Findings = append(inv.Findings, Finding{
				Table:    col.Table,
				Field:    col.Name,
				Location: record.Location,
				Code:     CodePayloadUndeclared,
				Owner:    entry.OwnerPackage,
				Detail:   record.Reason,
			})
		}
		if record.Class == ClassPersonal && record.Storage == StorageVaultRef &&
			entry.EncryptionClass != storagedisposition.EncryptionFieldLevel {
			inv.Findings = append(inv.Findings, Finding{
				Table:    col.Table,
				Field:    col.Name,
				Location: record.Location,
				Code:     CodeVaultUnproven,
				Owner:    entry.OwnerPackage,
				Detail:   "vault reference claimed while registry encryption_class is " + entry.EncryptionClass,
			})
		}
		summaries[col.Table] = s
	}
	for _, s := range summaries {
		inv.Tables = append(inv.Tables, s)
	}
	sort.Slice(inv.Tables, func(i, j int) bool { return inv.Tables[i].Table < inv.Tables[j].Table })
	sort.Slice(inv.Fields, func(i, j int) bool {
		if inv.Fields[i].Table != inv.Fields[j].Table {
			return inv.Fields[i].Table < inv.Fields[j].Table
		}
		return inv.Fields[i].Field < inv.Fields[j].Field
	})
	sort.Slice(inv.Findings, func(i, j int) bool {
		if inv.Findings[i].Table != inv.Findings[j].Table {
			return inv.Findings[i].Table < inv.Findings[j].Table
		}
		if inv.Findings[i].Field != inv.Findings[j].Field {
			return inv.Findings[i].Field < inv.Findings[j].Field
		}
		return inv.Findings[i].Code < inv.Findings[j].Code
	})
	return inv, nil
}

// Check scans and returns a *CheckError when any finding fails the policy.
// A nil error means no personal field is stored inline without a proven
// vault reference and every payload column is reviewed.
func Check(root string) error {
	inv, err := Scan(root)
	if err != nil {
		return err
	}
	if len(inv.Findings) != 0 {
		return &CheckError{Findings: inv.Roster()}
	}
	return nil
}
