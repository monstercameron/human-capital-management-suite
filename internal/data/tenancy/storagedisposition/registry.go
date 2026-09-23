// Package storagedisposition loads and validates the STORE-001 physical
// storage-disposition registry: definitions/storage/storage-disposition.yaml.
//
// It lives here rather than under definitions/storage itself because
// definitions/ is declared never-executable
// (definitions/architecture/repository-layout.yaml: "Machine-readable
// architecture, planning and policy manifests. Never executable; consumed by
// tools/policy and tools/planning") -- definitions/storage holds only the
// YAML, and this package is its one Go consumer.
//
// The registry is the single, checked-in answer to "what physical table
// backs this data, who owns it, and what happens to it in a disaster" for
// every base table the data plane's migrations declare. Load parses the YAML
// into a typed Registry; Validate proves the registry is internally
// consistent (every table entry is complete and no table is registered
// twice, the RED clause's "accidental second authority"); registry_test.go
// additionally proves it is consistent with the live, fully migrated
// PostgreSQL schema in both directions (TestTodo_STORE_001).
package storagedisposition

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// RetentionClass values a table's retention_class may take.
const (
	RetentionPermanent   = "PERMANENT"
	RetentionOperational = "OPERATIONAL"
	RetentionRebuildable = "REBUILDABLE"
)

// EncryptionClass values a table's encryption_class may take.
const (
	EncryptionPlatformManaged = "PLATFORM_MANAGED"
	EncryptionFieldLevel      = "FIELD_LEVEL"
)

// DataRole values a table's data_role may take.
const (
	RoleLedger     = "LEDGER"
	RoleProjection = "PROJECTION"
	RoleOutbox     = "OUTBOX"
	RoleRegistry   = "REGISTRY"
	RoleControl    = "CONTROL"
	// RoleAggregate marks a domain-owned bitemporal aggregate table (e.g.
	// person, worker, legal_entity, compensation_package): authoritative
	// business state versioned by effective_from/effective_to and
	// recorded_at/superseded_at, distinct from a control-plane REGISTRY
	// (a versioned definition/pointer) and from the LEDGER fact stream
	// itself.
	RoleAggregate = "AGGREGATE"
)

var validRetentionClasses = map[string]bool{
	RetentionPermanent:   true,
	RetentionOperational: true,
	RetentionRebuildable: true,
}

var validEncryptionClasses = map[string]bool{
	EncryptionPlatformManaged: true,
	EncryptionFieldLevel:      true,
}

var validDataRoles = map[string]bool{
	RoleLedger:     true,
	RoleProjection: true,
	RoleOutbox:     true,
	RoleRegistry:   true,
	RoleControl:    true,
	RoleAggregate:  true,
}

// TableEntry is one table's disposition.
type TableEntry struct {
	Table               string   `yaml:"table"`
	Migration           string   `yaml:"migration"`
	OwnerPackage        string   `yaml:"owner_package"`
	Plane               string   `yaml:"plane"`
	DataRole            string   `yaml:"data_role"`
	TenantScopingColumn *string  `yaml:"tenant_scoping_column"`
	IsolationPackage    string   `yaml:"isolation_package"`
	AppendOnly          bool     `yaml:"append_only"`
	RetentionClass      string   `yaml:"retention_class"`
	EncryptionClass     string   `yaml:"encryption_class"`
	RebuildSource       *string  `yaml:"rebuild_source"`
	Partitions          []string `yaml:"partitions"`
	Notes               string   `yaml:"notes"`
}

// TenantScoped reports whether the entry declares a tenant scoping column.
func (e TableEntry) TenantScoped() bool {
	return e.TenantScopingColumn != nil && *e.TenantScopingColumn != ""
}

// Registry is the parsed form of storage-disposition.yaml.
type Registry struct {
	Version int          `yaml:"version"`
	Module  string       `yaml:"module"`
	Tables  []TableEntry `yaml:"tables"`
}

// Load reads and parses the registry at path.
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("storagedisposition: reading registry: %w", err)
	}
	var r Registry
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("storagedisposition: parsing registry: %w", err)
	}
	return &r, nil
}

// TableNames returns every registered table name, sorted.
func (r *Registry) TableNames() []string {
	names := make([]string, 0, len(r.Tables))
	for _, e := range r.Tables {
		names = append(names, e.Table)
	}
	sort.Strings(names)
	return names
}

// Lookup returns the entry for table, if registered.
func (r *Registry) Lookup(table string) (TableEntry, bool) {
	for _, e := range r.Tables {
		if e.Table == table {
			return e, true
		}
	}
	return TableEntry{}, false
}

// Validate proves the registry is internally consistent: every entry
// declares every required dimension (RED: "has no truth class, physical
// system, consistency, retention ... decision"), and no table name is
// registered twice (RED: "accidental second authority").
func Validate(r *Registry) error {
	seen := make(map[string]bool, len(r.Tables))
	for _, e := range r.Tables {
		if e.Table == "" {
			return fmt.Errorf("storagedisposition: an entry has no table name")
		}
		if seen[e.Table] {
			return fmt.Errorf("storagedisposition: table %q is registered more than once (accidental second authority)", e.Table)
		}
		seen[e.Table] = true

		if e.Migration == "" {
			return fmt.Errorf("storagedisposition: %s: migration is required", e.Table)
		}
		if e.OwnerPackage == "" {
			return fmt.Errorf("storagedisposition: %s: owner_package is required", e.Table)
		}
		if e.Plane == "" {
			return fmt.Errorf("storagedisposition: %s: plane is required", e.Table)
		}
		if !validDataRoles[e.DataRole] {
			return fmt.Errorf("storagedisposition: %s: data_role %q is not one of the declared roles", e.Table, e.DataRole)
		}
		if !validRetentionClasses[e.RetentionClass] {
			return fmt.Errorf("storagedisposition: %s: retention_class %q is not one of the declared classes", e.Table, e.RetentionClass)
		}
		if !validEncryptionClasses[e.EncryptionClass] {
			return fmt.Errorf("storagedisposition: %s: encryption_class %q is not one of the declared classes", e.Table, e.EncryptionClass)
		}
		if e.TenantScoped() && e.IsolationPackage == "" {
			return fmt.Errorf("storagedisposition: %s: tenant scoped but declares no isolation_package", e.Table)
		}
		if e.RetentionClass == RetentionRebuildable {
			if e.RebuildSource == nil || *e.RebuildSource == "" {
				return fmt.Errorf("storagedisposition: %s: retention_class REBUILDABLE but declares no rebuild_source", e.Table)
			}
			if *e.RebuildSource == e.Table {
				return fmt.Errorf("storagedisposition: %s: rebuild_source names itself", e.Table)
			}
			source, ok := r.Lookup(*e.RebuildSource)
			if !ok {
				return fmt.Errorf("storagedisposition: %s: rebuild_source %q is not a registered table", e.Table, *e.RebuildSource)
			}
			if source.DataRole != RoleLedger {
				return fmt.Errorf("storagedisposition: %s: rebuild_source %q is not LEDGER-role (got %s); a rebuild must trace to the fact stream",
					e.Table, *e.RebuildSource, source.DataRole)
			}
		}
	}
	return nil
}

// ValidateOwnerPackages proves every entry's owner_package names a
// directory that exists under moduleRoot (the repository root holding
// go.mod). An owner that names nothing on disk is documentation, not
// ownership: REV-102-07 found worker_id_reservation pointing at
// internal/data/workerstore, which never existed.
func ValidateOwnerPackages(r *Registry, moduleRoot string) error {
	for _, e := range r.Tables {
		dir := filepath.Join(moduleRoot, filepath.FromSlash(e.OwnerPackage))
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("storagedisposition: %s: owner_package %q is not a directory under %q", e.Table, e.OwnerPackage, moduleRoot)
		}
	}
	return nil
}

// Diff compares the registry's table names against a live schema's table
// names and reports what is out of step in either direction: tables the live
// schema holds that the registry never named (RED: "unregistered storage"),
// and tables the registry names that the live schema does not hold (a stale
// or aspirational entry, which is exactly as much a second-authority risk as
// an unregistered table is).
type Diff struct {
	UnregisteredInLive []string
	StaleInRegistry    []string
}

// Empty reports whether the diff found nothing out of step.
func (d Diff) Empty() bool {
	return len(d.UnregisteredInLive) == 0 && len(d.StaleInRegistry) == 0
}

// CompareToLiveSchema computes the Diff between the registry and liveTables
// (every base table name a live-schema walk returned).
func (r *Registry) CompareToLiveSchema(liveTables []string) Diff {
	registered := make(map[string]bool, len(r.Tables))
	for _, e := range r.Tables {
		registered[e.Table] = true
	}
	live := make(map[string]bool, len(liveTables))
	for _, t := range liveTables {
		live[t] = true
	}

	var d Diff
	for _, t := range liveTables {
		if !registered[t] {
			d.UnregisteredInLive = append(d.UnregisteredInLive, t)
		}
	}
	for name := range registered {
		if !live[name] {
			d.StaleInRegistry = append(d.StaleInRegistry, name)
		}
	}
	sort.Strings(d.UnregisteredInLive)
	sort.Strings(d.StaleInRegistry)
	return d
}
