// Package rebuildcheck is WF-REV-011's shared rebuild-and-compare
// harness: for each ledger-derived REBUILDABLE table, commit a fact, apply
// its compensation or correction, rebuild the projection from the ledger,
// and prove the rebuilt state equals the live one. A projection that
// ignores the reversal cannot pass: the comparison is over the exact
// derived rows, and the negative controls prove the comparison is not
// blind.
//
// The harness ties every case to the storage-disposition registry: a case
// names its table, and Check refuses a table the registry does not carry
// as REBUILDABLE from the ledger. observation_checkpoint is the one
// REBUILDABLE table with no case: its rebuild source is
// external_observation, which never carries compensation or correction
// facts, so no reversal can touch it.
package rebuildcheck

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// RegistryPath is the storage-disposition registry, relative to this
// package's directory.
const RegistryPath = "../../../definitions/storage/storage-disposition.yaml"

// LedgerRebuildSource is the rebuild source every cased table must declare.
const LedgerRebuildSource = "ledger_event"

// Conn is the database handle a Fixture works through: transactions for
// writes, direct reads for digests.
type Conn interface {
	dbport.Beginner
	dbport.Conn
}

// Fixture is one tenant's database handle for a rebuild case.
type Fixture struct {
	conn   Conn
	Tenant uuid.UUID
	Slug   values.TenantId
}

// NewFixture binds a tenant to a database handle.
func NewFixture(conn Conn, tenant uuid.UUID, slug values.TenantId) Fixture {
	return Fixture{conn: conn, Tenant: tenant, Slug: slug}
}

// Conn returns the underlying handle for direct reads.
func (f Fixture) Conn() Conn { return f.conn }

// InTx runs fn inside a tenant-scoped transaction, committing on success
// and rolling back on failure.
func (f Fixture) InTx(ctx context.Context, fn func(tx dbport.Tx) error) error {
	tx, err := f.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.Tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RegistryEntry returns the disposition entry for table, refusing tables
// the registry does not carry as ledger-rebuildable.
func RegistryEntry(table string) (storagedisposition.TableEntry, error) {
	registry, err := storagedisposition.Load(filepath.Clean(RegistryPath))
	if err != nil {
		return storagedisposition.TableEntry{}, err
	}
	entry, ok := registry.Lookup(table)
	if !ok {
		return storagedisposition.TableEntry{}, fmt.Errorf("rebuildcheck: %s is not in the storage-disposition registry", table)
	}
	if entry.RetentionClass != storagedisposition.RetentionRebuildable {
		return storagedisposition.TableEntry{}, fmt.Errorf("rebuildcheck: %s is %s, not REBUILDABLE", table, entry.RetentionClass)
	}
	if entry.RebuildSource == nil || *entry.RebuildSource != LedgerRebuildSource {
		return storagedisposition.TableEntry{}, fmt.Errorf("rebuildcheck: %s rebuilds from %v, not the ledger", table, entry.RebuildSource)
	}
	return entry, nil
}

// LedgerRebuildables returns every registry table that rebuilds from the
// ledger: the exact set CONFORMANCE requires a case for.
func LedgerRebuildables() ([]string, error) {
	registry, err := storagedisposition.Load(filepath.Clean(RegistryPath))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range registry.TableNames() {
		entry, ok := registry.Lookup(name)
		if !ok {
			continue
		}
		if entry.RetentionClass == storagedisposition.RetentionRebuildable &&
			entry.RebuildSource != nil && *entry.RebuildSource == LedgerRebuildSource {
			out = append(out, name)
		}
	}
	return out, nil
}
