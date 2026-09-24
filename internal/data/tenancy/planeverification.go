package tenancy

import (
	"bytes"
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
	tenant "github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/envelope"
)

// This file composes tenant.PlaneVerifier implementations backed by what
// this repository can actually prove today for a bootstrapped tenant --
// TENANT-002's remaining GREEN clause. Each verifier below binds one
// already-delivered, independently owned contract (this package's own
// bootstrap receipt ledger and row level security, internal/trust/envelope
// tenant KEK separation over an injected internal/trust/custody.Provider,
// internal/data/configregistry's durable configuration-object registry,
// internal/data/tenancy/storagedisposition's storage registry, and a
// governed row-level-security-scoped read) into the typed
// tenant.PlaneVerifier port internal/domains/tenant declares.
//
// internal/domains/tenant.PlaneIdentity, PlaneKeys, PlanePolicies,
// PlaneSchemas and PlaneHealth are the five planes this file can verify for
// real. The remaining five (PlaneAdmin, PlanePlacement, PlaneProducts,
// PlaneRecoveryContacts, PlaneAudit) have no repository-backed proof yet --
// each depends on its own undelivered todo -- so a caller composing a full
// tenant.ProvisioningRun for those planes uses tenant.FakeVerifier,
// documented as such at the call site; TENANT-002's own GREEN clause is
// unaffected, since the closed set in tenant.AllPlanes is what gates
// ACTIVE, not which verifier happens to implement each one yet.

// IdentityVerifier proves the identity plane: the tenant's own row is
// registered and reads ACTIVE, and row level security genuinely confines
// this tenant's evidence -- an hcmnext_app-role transaction with no tenant
// scope set sees none of this tenant's bootstrap receipts, and the same
// role scoped to this tenant (via [WithTenant]) sees its own.
//
// Admin must be a connection that reads the tenant table unscoped -- the
// same elevated identity that registers a tenant row in the first place
// (see internal/intent/app/pgstore.Store.Bootstrap's own doc comment).
// AppConn must be a connection already running as [AppRole] (see
// isolation_test.go's own appRoleConn helper for how a caller reaches it).
type IdentityVerifier struct {
	TenantID uuid.UUID
	Admin    dbport.Querier
	AppConn  dbport.Beginner
}

var _ tenant.PlaneVerifier = IdentityVerifier{}

// Plane implements [tenant.PlaneVerifier].
func (v IdentityVerifier) Plane() tenant.Plane { return tenant.PlaneIdentity }

// Verify implements [tenant.PlaneVerifier].
func (v IdentityVerifier) Verify(ctx context.Context, req tenant.VerificationRequest) (tenant.Verified, error) {
	var status string
	if err := v.Admin.QueryRow(ctx, `SELECT status FROM tenant WHERE tenant_id = $1`, v.TenantID).Scan(&status); err != nil {
		return tenant.Verified{}, fmt.Errorf("tenancy: identity plane: read tenant row for %s: %w", v.TenantID, err)
	}
	if status != "ACTIVE" {
		return tenant.Verified{}, fmt.Errorf("tenancy: identity plane: tenant %s status is %q, want ACTIVE", v.TenantID, status)
	}

	unscopedCount, err := v.receiptCount(ctx, uuid.Nil)
	if err != nil {
		return tenant.Verified{}, err
	}
	if unscopedCount != 0 {
		return tenant.Verified{}, fmt.Errorf(
			"tenancy: identity plane: an unscoped app-role transaction saw %d bootstrap receipt row(s) for tenant %s; row level security is not isolating",
			unscopedCount, v.TenantID)
	}

	scopedCount, err := v.receiptCount(ctx, v.TenantID)
	if err != nil {
		return tenant.Verified{}, err
	}
	if scopedCount == 0 {
		return tenant.Verified{}, fmt.Errorf(
			"tenancy: identity plane: a transaction scoped to tenant %s saw no bootstrap receipts", v.TenantID)
	}

	return tenant.Verified{
		Plane:             tenant.PlaneIdentity,
		Tenant:            req.Tenant,
		VerifierPrincipal: "system:identity-plane-verifier",
		VerifiedAt:        req.Now,
		EvidenceRefs: []string{
			fmt.Sprintf("tenant_row:%s:ACTIVE", v.TenantID),
			fmt.Sprintf("rls_isolation:tenant_bootstrap_receipt:%d_rows_scoped:%d_rows_unscoped", scopedCount, unscopedCount),
		},
	}, nil
}

// receiptCount counts tenant_bootstrap_receipt rows visible to a fresh
// transaction on v.AppConn: unscoped when scopeTo is uuid.Nil, otherwise
// scoped to scopeTo via [WithTenant]. The transaction is always rolled
// back -- this is a read-only probe.
func (v IdentityVerifier) receiptCount(ctx context.Context, scopeTo uuid.UUID) (int, error) {
	tx, err := v.AppConn.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("tenancy: identity plane: begin app-role transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if scopeTo != uuid.Nil {
		if err := WithTenant(ctx, tx, scopeTo); err != nil {
			return 0, fmt.Errorf("tenancy: identity plane: scope transaction to tenant %s: %w", scopeTo, err)
		}
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tenant_bootstrap_receipt WHERE tenant_id = $1`, v.TenantID).Scan(&count); err != nil {
		return 0, fmt.Errorf("tenancy: identity plane: count bootstrap receipts for tenant %s: %w", v.TenantID, err)
	}
	return count, nil
}

// KeysVerifier proves the keys plane: this tenant's key-encryption key,
// registered on Manager (internal/trust/envelope) and ultimately backed by
// an injected internal/trust/custody.Provider, is live -- a fresh envelope
// can be sealed and opened through it right now.
//
// Manager already carries a tenant KEK registered for the tenant this
// verifier will be asked about ([envelope.Manager.RegisterTenant]);
// production composes Manager over a real custody.Provider, while
// TENANT-002's own pgtest-backed composition (this package's test suite)
// registers it over an in-memory custody.Provider fake -- "the custody
// fake" this plane's evidence rests on until a real provider adapter is
// qualified.
type KeysVerifier struct {
	Manager *envelope.Manager
	Region  string
}

var _ tenant.PlaneVerifier = KeysVerifier{}

// Plane implements [tenant.PlaneVerifier].
func (v KeysVerifier) Plane() tenant.Plane { return tenant.PlaneKeys }

// Verify implements [tenant.PlaneVerifier].
func (v KeysVerifier) Verify(_ context.Context, req tenant.VerificationRequest) (tenant.Verified, error) {
	rc := custody.Context{RequestContext: custody.RequestContext{
		Workload:    "tenant-bootstrap-plane-verifier",
		Tenant:      req.Tenant,
		Region:      v.Region,
		Purpose:     "tenant-kek-liveness-check",
		Destination: "internal",
	}}
	marker := []byte("tenant-bootstrap-plane-check:" + req.Tenant)

	env, evidence, err := v.Manager.Encrypt(rc, "tenant-kek-plane-check", marker)
	if err != nil {
		return tenant.Verified{}, fmt.Errorf("tenancy: keys plane: seal a marker through tenant %s's KEK: %w", req.Tenant, err)
	}
	plaintext, _, err := v.Manager.Decrypt(rc, env, "tenant-kek-plane-check")
	if err != nil {
		return tenant.Verified{}, fmt.Errorf("tenancy: keys plane: open the sealed marker for tenant %s: %w", req.Tenant, err)
	}
	if !bytes.Equal(plaintext, marker) {
		return tenant.Verified{}, fmt.Errorf("tenancy: keys plane: round trip for tenant %s did not return the original marker", req.Tenant)
	}

	return tenant.Verified{
		Plane:             tenant.PlaneKeys,
		Tenant:            req.Tenant,
		VerifierPrincipal: "system:keys-plane-verifier",
		VerifiedAt:        req.Now,
		// EvidenceRefs deliberately names only the stable KEK identity, not
		// evidence.ReceiptID: a fresh round trip mints a new custody
		// receipt id on every call by design (it is a distinct operation
		// each time), and tenant.ProvisioningRun.RecordVerified treats two
		// Verified records with different EvidenceRefs as materially
		// different evidence. Including the receipt id here would make
		// every re-verification of an already-VERIFIED keys plane look
		// like a conflicting change instead of an idempotent replay.
		EvidenceRefs: []string{fmt.Sprintf("kek:%s@%s", evidence.KEKID, evidence.KEKVersion)},
	}, nil
}

// PoliciesVerifier proves the policies plane: a published configuration
// object -- a policy, in production -- resolves to an active revision for
// this tenant through internal/data/configregistry's durable, RLS-scoped
// Store (migration 00027).
type PoliciesVerifier struct {
	Store platformconfig.Store
	Scope platformconfig.Scope
	Kind  platformconfig.Kind
	ID    string
}

var _ tenant.PlaneVerifier = PoliciesVerifier{}

// Plane implements [tenant.PlaneVerifier].
func (v PoliciesVerifier) Plane() tenant.Plane { return tenant.PlanePolicies }

// Verify implements [tenant.PlaneVerifier].
func (v PoliciesVerifier) Verify(_ context.Context, req tenant.VerificationRequest) (tenant.Verified, error) {
	obj, err := platformconfig.Resolve(v.Store, v.Scope, v.Kind, v.ID)
	if err != nil {
		return tenant.Verified{}, fmt.Errorf("tenancy: policies plane: resolve active %s/%s for tenant %s: %w", v.Kind, v.ID, req.Tenant, err)
	}
	if obj.Scope.TenantID != v.Scope.TenantID {
		return tenant.Verified{}, fmt.Errorf("tenancy: policies plane: resolved object scope %q does not match tenant %s", obj.Scope.TenantID, req.Tenant)
	}
	if err := obj.Verify(); err != nil {
		return tenant.Verified{}, fmt.Errorf("tenancy: policies plane: resolved object failed its own digest check: %w", err)
	}

	return tenant.Verified{
		Plane:             tenant.PlanePolicies,
		Tenant:            req.Tenant,
		VerifierPrincipal: "system:policies-plane-verifier",
		VerifiedAt:        req.Now,
		EvidenceRefs: []string{
			fmt.Sprintf("config_object:%s/%s@%d", obj.Kind, obj.ID, obj.Revision),
			"object_digest:" + obj.Digest(),
		},
	}, nil
}

// SchemaVerifier proves the schemas plane: the tenant-scoped tables this
// plane names are both registered in STORE-001's storage-disposition
// registry as tenant scoped and present in the live PostgreSQL schema --
// the same registry-driven check internal/data/schema's own
// TestTodo_DATA_001 performs, applied here to exactly the tables a tenant
// bootstrap depends on.
type SchemaVerifier struct {
	// RegistryPath is the storage-disposition registry file, e.g.
	// health.DefaultRegistryPath resolved relative to the process's
	// working directory (a test resolves it relative to its own package
	// directory instead; see this file's test for the exact path).
	RegistryPath string
	// Tables are the base table names this plane proves, e.g.
	// {"tenant", "tenant_bootstrap_receipt"}.
	Tables []string
	// Live reads the live schema this test/process is actually connected
	// to -- a plain connection is enough; no tenant scope is needed to
	// enumerate table names.
	Live dbport.Querier
}

var _ tenant.PlaneVerifier = SchemaVerifier{}

// Plane implements [tenant.PlaneVerifier].
func (v SchemaVerifier) Plane() tenant.Plane { return tenant.PlaneSchemas }

// Verify implements [tenant.PlaneVerifier].
func (v SchemaVerifier) Verify(ctx context.Context, req tenant.VerificationRequest) (tenant.Verified, error) {
	if len(v.Tables) == 0 {
		return tenant.Verified{}, fmt.Errorf("tenancy: schemas plane: no tables declared to verify")
	}
	reg, err := storagedisposition.Load(v.RegistryPath)
	if err != nil {
		return tenant.Verified{}, fmt.Errorf("tenancy: schemas plane: load storage-disposition registry %s: %w", v.RegistryPath, err)
	}

	refs := make([]string, 0, len(v.Tables))
	for _, table := range v.Tables {
		entry, ok := reg.Lookup(table)
		if !ok {
			return tenant.Verified{}, fmt.Errorf("tenancy: schemas plane: table %s is not registered in %s", table, v.RegistryPath)
		}
		if !entry.TenantScoped() {
			return tenant.Verified{}, fmt.Errorf("tenancy: schemas plane: table %s is not declared tenant scoped", table)
		}
		var exists bool
		if err := v.Live.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = current_schema() AND table_name = $1
			)`, table).Scan(&exists); err != nil {
			return tenant.Verified{}, fmt.Errorf("tenancy: schemas plane: check live schema for table %s: %w", table, err)
		}
		if !exists {
			return tenant.Verified{}, fmt.Errorf("tenancy: schemas plane: table %s is registered but absent from the live schema", table)
		}
		refs = append(refs, "storage_disposition:"+table)
	}

	return tenant.Verified{
		Plane:             tenant.PlaneSchemas,
		Tenant:            req.Tenant,
		VerifierPrincipal: "system:schemas-plane-verifier",
		VerifiedAt:        req.Now,
		EvidenceRefs:      refs,
	}, nil
}

// HealthVerifier proves the health plane as a governed read succeeding: a
// transaction scoped to this tenant through row level security
// ([WithTenant]) can execute and read back a trivial statement. AppConn
// must already be running as [AppRole] -- an ungoverned (unscoped, or
// superuser) read would prove nothing about whether this tenant's own
// governed access path is healthy.
type HealthVerifier struct {
	TenantID uuid.UUID
	AppConn  dbport.Beginner
}

var _ tenant.PlaneVerifier = HealthVerifier{}

// Plane implements [tenant.PlaneVerifier].
func (v HealthVerifier) Plane() tenant.Plane { return tenant.PlaneHealth }

// Verify implements [tenant.PlaneVerifier].
func (v HealthVerifier) Verify(ctx context.Context, req tenant.VerificationRequest) (tenant.Verified, error) {
	tx, err := v.AppConn.Begin(ctx)
	if err != nil {
		return tenant.Verified{}, fmt.Errorf("tenancy: health plane: begin governed read for tenant %s: %w", v.TenantID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := WithTenant(ctx, tx, v.TenantID); err != nil {
		return tenant.Verified{}, fmt.Errorf("tenancy: health plane: scope governed read to tenant %s: %w", v.TenantID, err)
	}
	var probe int
	if err := tx.QueryRow(ctx, `SELECT 1`).Scan(&probe); err != nil {
		return tenant.Verified{}, fmt.Errorf("tenancy: health plane: governed read failed for tenant %s: %w", v.TenantID, err)
	}
	if probe != 1 {
		return tenant.Verified{}, fmt.Errorf("tenancy: health plane: governed read for tenant %s returned %d, want 1", v.TenantID, probe)
	}

	return tenant.Verified{
		Plane:             tenant.PlaneHealth,
		Tenant:            req.Tenant,
		VerifierPrincipal: "system:health-plane-verifier",
		VerifiedAt:        req.Now,
		EvidenceRefs:      []string{fmt.Sprintf("governed_read:tenant=%s:ok", v.TenantID)},
	}, nil
}
