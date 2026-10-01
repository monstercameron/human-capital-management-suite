// Package tableinventory implements the ALIGN-008/ALIGN-009 storage-side
// alignment checks. It reads the authoritative storage disposition registry
// and the versioned migrations, then exposes a deterministic semantic-role
// inventory without creating a second table catalog.
package tableinventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

const (
	// SchemaVersion changes only when the alignment registry shape changes.
	SchemaVersion = 1
	// DefaultRegistryPath is the source, not generated, storage registry.
	DefaultRegistryPath = "definitions/storage/storage-disposition.yaml"
)

// Version identifies this policy contract.
func Version() int { return SchemaVersion }

// Table is the single table metadata shape owned by the storage registry.
// Keeping this alias prevents the policy from inventing a parallel table
// definition.
type Table = storagedisposition.TableEntry

// MigrationTable records where a CREATE TABLE statement was found.
type MigrationTable struct {
	Name      string `json:"name"`
	Migration string `json:"migration"`
}

// Finding is one inventory discrepancy. Table is always populated for a
// table-specific finding; Code is stable for machine consumers.
type Finding struct {
	Table  string `json:"table"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (f Finding) Error() string {
	if f.Table == "" {
		return "tableinventory: " + f.Code + ": " + f.Detail
	}
	return fmt.Sprintf("tableinventory: %s: %s: %s", f.Table, f.Code, f.Detail)
}

// Inventory is a deterministic view of the source registry joined to the
// physical tables declared by migrations.
type Inventory struct {
	Version         int                 `json:"version"`
	Tables          []Table             `json:"tables"`
	MigrationTables []MigrationTable    `json:"migration_tables"`
	TablesByRole    map[string][]string `json:"tables_by_role"`
	SourceFiles     []string            `json:"source_files"`
}

// AlignmentRegistry is the in-memory ALIGN-008 output. It is deliberately
// returned to callers instead of written to definitions/; definitions remain
// the authority and this package is a check.
type AlignmentRegistry struct {
	SchemaVersion int              `json:"schema_version"`
	Tables        []AlignmentTable `json:"tables"`
}

// AlignmentTable joins the physical table to its semantic role and owner.
type AlignmentTable struct {
	Table        string `json:"table"`
	Role         string `json:"role"`
	Owner        string `json:"owner"`
	Migration    string `json:"migration"`
	TenantScoped bool   `json:"tenant_scoped"`
}

// Digest is a stable identity for the generated alignment result.
func (r AlignmentRegistry) Digest() string {
	b, _ := json.Marshal(struct {
		SchemaVersion int              `json:"schema_version"`
		Tables        []AlignmentTable `json:"tables"`
	}{r.SchemaVersion, r.Tables})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain returns bounded structural facts suitable for logs and evidence.
func (r AlignmentRegistry) Explain() string {
	return fmt.Sprintf("table alignment registry v%d with %d tables (%s)", r.SchemaVersion, len(r.Tables), r.Digest())
}

// Load reads the authoritative storage registry and validates its intrinsic
// fields. It does not inspect migrations; use Scan for the joined view.
func Load(path string) (*Inventory, error) {
	registry, err := storagedisposition.Load(path)
	if err != nil {
		return nil, fmt.Errorf("tableinventory: load registry: %w", err)
	}
	if err := storagedisposition.Validate(registry); err != nil {
		return nil, fmt.Errorf("tableinventory: validate registry: %w", err)
	}
	inventory := fromRegistry(registry, nil, nil)
	return &inventory, nil
}

// Scan loads the source registry and scans migrations/*.sql. It is pure and
// never connects to PostgreSQL or writes a generated file.
func Scan(root string) (Inventory, error) {
	registryPath := filepath.Join(root, filepath.FromSlash(DefaultRegistryPath))
	registry, err := storagedisposition.Load(registryPath)
	if err != nil {
		return Inventory{}, fmt.Errorf("tableinventory: load registry: %w", err)
	}
	if err := storagedisposition.Validate(registry); err != nil {
		return Inventory{}, fmt.Errorf("tableinventory: validate registry: %w", err)
	}
	allowed := make(map[string]bool)
	registered := make(map[string]bool)
	for _, table := range registry.Tables {
		allowed[table.Migration] = true
		registered[strings.ToLower(table.Table)] = true
	}
	// A registry migration may be a later extension, so locate registered
	// tables across the migration history while keeping other registries out.
	created, sourceFiles, err := scanMigrationTables(filepath.Join(root, "migrations"), allowed, registered)
	if err != nil {
		return Inventory{}, fmt.Errorf("tableinventory: scan table declarations: %w", err)
	}
	return fromRegistry(registry, created, sourceFiles), nil
}

// Generate creates the deterministic ALIGN-008 alignment projection after
// proving the registry and migrations agree.
func Generate(root string) (AlignmentRegistry, error) {
	inventory, err := Scan(root)
	if err != nil {
		return AlignmentRegistry{}, err
	}
	if findings := Validate(inventory); len(findings) != 0 {
		return AlignmentRegistry{}, findingsError(findings)
	}
	out := AlignmentRegistry{SchemaVersion: SchemaVersion, Tables: make([]AlignmentTable, 0, len(inventory.Tables))}
	for _, table := range inventory.Tables {
		out.Tables = append(out.Tables, AlignmentTable{
			Table: table.Table, Role: table.DataRole, Owner: table.OwnerPackage,
			Migration: table.Migration, TenantScoped: table.TenantScoped(),
		})
	}
	sort.Slice(out.Tables, func(i, j int) bool { return out.Tables[i].Table < out.Tables[j].Table })
	return out, nil
}

// Check verifies that every source-registry table is represented by a real
// migration table and that every migration table is registered. Partitions
// are physical children of ledger_event and are excluded from this logical
// inventory, matching STORE-001's live-schema rule.
func Check(root string) error {
	inventory, err := Scan(root)
	if err != nil {
		return err
	}
	if findings := Validate(inventory); len(findings) != 0 {
		return findingsError(findings)
	}
	return nil
}

// Validate returns all discrepancies in stable table/code order.
func Validate(inventory Inventory) []Finding {
	registered := make(map[string]Table, len(inventory.Tables))
	for _, table := range inventory.Tables {
		name := strings.ToLower(strings.TrimSpace(table.Table))
		if name == "" {
			continue
		}
		registered[name] = table
	}
	physical := make(map[string]MigrationTable, len(inventory.MigrationTables))
	for _, table := range inventory.MigrationTables {
		physical[strings.ToLower(table.Name)] = table
	}

	var findings []Finding
	for name, table := range registered {
		if _, ok := physical[name]; !ok {
			findings = append(findings, Finding{Table: table.Table, Code: "MISSING_MIGRATION_TABLE", Detail: "registry table is not declared by migrations"})
		}
		if strings.TrimSpace(table.DataRole) == "" {
			findings = append(findings, Finding{Table: table.Table, Code: "MISSING_SEMANTIC_ROLE", Detail: "table has no data_role"})
		}
	}
	for name, table := range physical {
		if _, ok := registered[name]; !ok {
			findings = append(findings, Finding{Table: table.Name, Code: "UNREGISTERED_MIGRATION_TABLE", Detail: "migration table has no storage-disposition row"})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Table != findings[j].Table {
			return findings[i].Table < findings[j].Table
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}

func (i Inventory) Explain() string {
	return fmt.Sprintf("table inventory v%d with %d tables across %d semantic roles", i.Version, len(i.Tables), len(i.TablesByRole))
}

func fromRegistry(registry *storagedisposition.Registry, created []MigrationTable, sourceFiles []string) Inventory {
	tables := append([]Table(nil), registry.Tables...)
	roles := make(map[string][]string)
	for _, table := range tables {
		roles[table.DataRole] = append(roles[table.DataRole], table.Table)
	}
	for role := range roles {
		sort.Strings(roles[role])
	}
	partitions := make(map[string]bool)
	for _, table := range tables {
		for _, partition := range table.Partitions {
			partitions[strings.ToLower(partition)] = true
		}
	}
	migrationTables := make([]MigrationTable, 0, len(created))
	for _, table := range created {
		if !partitions[strings.ToLower(table.Name)] {
			migrationTables = append(migrationTables, table)
		}
	}
	sort.Slice(migrationTables, func(i, j int) bool {
		if migrationTables[i].Name != migrationTables[j].Name {
			return migrationTables[i].Name < migrationTables[j].Name
		}
		return migrationTables[i].Migration < migrationTables[j].Migration
	})
	return Inventory{
		Version: SchemaVersion, Tables: tables, MigrationTables: migrationTables,
		TablesByRole: roles, SourceFiles: append([]string(nil), sourceFiles...),
	}
}

func findingsError(findings []Finding) error {
	parts := make([]string, len(findings))
	for i, finding := range findings {
		parts[i] = finding.Error()
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}

// Keep the source-level parser contract visible here for fixture users that
// need to identify default-schema tables without treating companion schemas
// as default PostgreSQL storage.
var createTablePattern = regexp.MustCompile(`(?im)^\s*CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:(\w+)\.)?(\w+)`)

func scanMigrationTables(migrationsDir string, allowed, registered map[string]bool) ([]MigrationTable, []string, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, nil, err
	}
	var out []MigrationTable
	var sourceFiles []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(migrationsDir, entry.Name()))
		if err != nil {
			return nil, nil, err
		}
		sourceFiles = append(sourceFiles, entry.Name())
		for _, match := range createTablePattern.FindAllStringSubmatch(string(body), -1) {
			if match[1] != "" && match[1] != "public" {
				continue
			}
			name := strings.ToLower(match[2])
			if len(allowed) != 0 && !allowed[entry.Name()] && !registered[name] {
				continue
			}
			out = append(out, MigrationTable{Name: name, Migration: entry.Name()})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Migration < out[j].Migration
	})
	sort.Strings(sourceFiles)
	return out, sourceFiles, nil
}
