package idempotency_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
)

func TestTodo_REV_102_06(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "rev10206")
	conn := appConn(t, db)
	store := idempotency.PostgresStore{}
	now := fixedInstant
	policy := idempotency.RetentionPolicy{
		Retention: 24 * time.Hour, RetryWindow: 12 * time.Hour,
	}
	scope := defaultScope(tenant, "financial-close")
	scope.Capability = "intent:hcmnext.payroll.payment_settlement"
	digest := digestOf("financial-close:period-2026-09")
	var calls int

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := idempotency.Guard(ctx, tx, store, scope, digest, policy, now,
			func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
				calls++
				return idempotency.ResultIdentity{ResultRef: "payroll-close-17", EvidenceID: "evidence-17"}, nil
			})
		return err
	})

	var compacted int64
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		compacted, err = store.Expire(ctx, tx, tenant, now.Add(25*time.Hour))
		return err
	})
	if compacted != 1 {
		t.Fatalf("Expire changed %d rows, want one compacted tombstone", compacted)
	}

	var rec idempotency.Record
	var found bool
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		rec, found, err = store.Lookup(ctx, tx, scope)
		return err
	})
	if !found || rec.Status != idempotency.StatusTombstone {
		t.Fatalf("record after expiry = found %v, status %q; want retained TOMBSTONE", found, rec.Status)
	}
	if rec.RequestDigest != digest || rec.RetentionClass != idempotency.RetentionPermanentTombstone {
		t.Fatalf("tombstone lost key binding: digest=%q class=%q", rec.RequestDigest, rec.RetentionClass)
	}
	if !rec.Identity.Empty() || !rec.ExpiresAt.IsZero() {
		t.Fatalf("tombstone retained result detail or expiry: identity=%+v expiry=%s", rec.Identity, rec.ExpiresAt)
	}

	// Exercise Guard's real replay path after compaction. A new store value
	// models a process restart while the durable tombstone remains authoritative.
	restartedStore := idempotency.PostgresStore{}
	for _, attempt := range []struct {
		name, hash string
		want       string
	}{
		{name: "same request", hash: digest, want: idempotency.CodeTombstoned},
		{name: "different request", hash: digestOf("different close"), want: idempotency.CodeConflict},
	} {
		t.Run(attempt.name, func(t *testing.T) {
			err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				_, guardErr := idempotency.Guard(ctx, tx, restartedStore, scope, attempt.hash, policy, now.Add(48*time.Hour),
					func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
						calls++
						t.Fatal("effect ran after tombstone expiry")
						return idempotency.ResultIdentity{}, nil
					})
				return guardErr
			})
			if got := idempotency.CodeOf(err); got != attempt.want {
				t.Fatalf("Guard code = %q, want %q (%v)", got, attempt.want, err)
			}
		})
	}
	if calls != 1 {
		t.Fatalf("effect ran %d times; want exactly once", calls)
	}

	// Ordinary capabilities retain the existing bounded expiry behavior.
	expiringScope := defaultScope(tenant, "ordinary-expiry")
	expiringScope.Capability = "ordinary.operations.write"
	expiringPolicy := idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: 12 * time.Hour}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, _, err := store.Reserve(ctx, tx, expiringScope, digestOf("ordinary"), expiringPolicy, now)
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.Expire(ctx, tx, tenant, now.Add(25*time.Hour))
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, found, err := store.Lookup(ctx, tx, expiringScope)
		if found {
			t.Fatal("ordinary expired key was retained")
		}
		return err
	})

	// Re-run the migration's one-time backfill against a legacy row staged as
	// EXPIRING. This exercises the exact migration SQL and proves a later sweep
	// compacts the pre-existing generic transaction.commit key.
	legacyDigest := digestOf("legacy-transaction-commit")
	legacyScope := idempotency.Scope{
		Tenant: tenant, Capability: "transaction.commit",
		EffectScope: "legacy-plan", Key: "legacy-commit-key",
	}
	migration, err := os.ReadFile("../../../migrations/00331_idempotency_policy_backfill.sql")
	if err != nil {
		t.Fatalf("read backfill migration: %v", err)
	}
	migrationParts := strings.SplitN(string(migration), "-- +goose Up", 2)
	if len(migrationParts) != 2 {
		t.Fatal("backfill migration has no Goose Up section")
	}
	backfill := strings.SplitN(migrationParts[1], "-- +goose Down", 2)[0]
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO idempotency_record (
			tenant_id, capability_id, effect_scope, idempotency_key, request_digest,
			status, retention_class, result_ref, created_at, expires_at
		) VALUES ($1,$2,$3,$4,$5,'COMPLETED','EXPIRING','legacy-result',$6,$7)`,
			legacyScope.Tenant, legacyScope.Capability, legacyScope.EffectScope, legacyScope.Key,
			legacyDigest, now, now.Add(time.Hour)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, backfill); err != nil {
			return err
		}
		return nil
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.Expire(ctx, tx, tenant, now.Add(2*time.Hour)); err != nil {
			return err
		}
		legacy, found, err := store.Lookup(ctx, tx, legacyScope)
		if err == nil && (!found || legacy.Status != idempotency.StatusTombstone || legacy.RequestDigest != legacyDigest || !legacy.Identity.Empty()) {
			t.Fatalf("backfilled legacy commit = %+v, found %v", legacy, found)
		}
		return err
	})
}

func TestTodo_REV_102_06_Property(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "rev10206-property")
	conn := appConn(t, db)
	store := idempotency.PostgresStore{}
	policy := idempotency.RetentionPolicy{
		Retention: time.Hour,
	}

	want := make(map[string]string, 12)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		for i := 0; i < 12; i++ {
			key := "irreversible-" + time.Duration(i+1).String()
			digest := digestOf("request:" + key)
			scope := defaultScope(tenant, key)
			scope.Capability = "intent:hcmnext.irreversible.operation"
			if _, _, err := store.Reserve(ctx, tx, scope, digest, policy, fixedInstant); err != nil {
				return err
			}
			if _, err := store.Complete(ctx, tx, scope, idempotency.ResultIdentity{EffectIdentity: "effect:" + key}, fixedInstant); err != nil {
				return err
			}
			want[key] = digest
		}
		return nil
	})

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		n, err := store.Expire(ctx, tx, tenant, fixedInstant.Add(2*time.Hour))
		if err == nil && n != int64(len(want)) {
			t.Fatalf("Expire changed %d rows, want %d", n, len(want))
		}
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		for key, digest := range want {
			scope := defaultScope(tenant, key)
			scope.Capability = "intent:hcmnext.irreversible.operation"
			rec, found, err := store.Lookup(ctx, tx, scope)
			if err != nil {
				return err
			}
			if !found || rec.Status != idempotency.StatusTombstone || rec.RequestDigest != digest || !rec.Identity.Empty() {
				t.Fatalf("key %q compacted record = %+v, found %v", key, rec, found)
			}
		}
		return nil
	})

	// A record keeps the policy class chosen at reservation time. A later
	// registry version that classifies the capability as expiring cannot
	// downgrade an already committed permanent tombstone.
	versionedScope := defaultScope(tenant, "policy-version")
	versionedScope.Capability = "risk.versioned"
	permanentRegistry, err := idempotency.NewCapabilityRetentionRegistry(
		map[string]idempotency.RetentionClass{"risk.versioned": idempotency.RetentionPermanentTombstone}, nil)
	if err != nil {
		t.Fatal(err)
	}
	permanentStore := idempotency.PostgresStore{RetentionRegistry: permanentRegistry}
	versionedDigest := digestOf("versioned-policy-request")
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if _, _, err := permanentStore.Reserve(ctx, tx, versionedScope, versionedDigest,
			idempotency.RetentionPolicy{Retention: time.Hour}, fixedInstant); err != nil {
			return err
		}
		_, err := permanentStore.Complete(ctx, tx, versionedScope,
			idempotency.ResultIdentity{ResultRef: "versioned-result"}, fixedInstant)
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := permanentStore.Expire(ctx, tx, tenant, fixedInstant.Add(2*time.Hour))
		return err
	})
	expiringRegistry, err := idempotency.NewCapabilityRetentionRegistry(
		map[string]idempotency.RetentionClass{"risk.versioned": idempotency.RetentionExpiring}, nil)
	if err != nil {
		t.Fatal(err)
	}
	newVersionStore := idempotency.PostgresStore{RetentionRegistry: expiringRegistry}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := newVersionStore.Expire(ctx, tx, tenant, fixedInstant.Add(48*time.Hour)); err != nil {
			return err
		}
		rec, found, err := newVersionStore.Lookup(ctx, tx, versionedScope)
		if err == nil && (!found || rec.Status != idempotency.StatusTombstone || rec.RequestDigest != versionedDigest) {
			t.Fatalf("record after registry version change = %+v, found %v", rec, found)
		}
		return err
	})
}

func TestTodo_REV_102_06_Policy(t *testing.T) {
	ctx := context.Background()
	protected := idempotency.Scope{
		Tenant: uuid.New(), Capability: "intent:hcmnext.payroll.unlisted_action",
		EffectScope: "payroll:run", Key: "policy-key",
	}
	digest := digestOf("authoritative-capability-retention")

	t.Run("protected capability rejects caller downgrade", func(t *testing.T) {
		store := idempotency.PostgresStore{}
		_, _, err := store.Reserve(ctx, nil, protected, digest, idempotency.RetentionPolicy{
			Retention: time.Hour, Class: idempotency.RetentionExpiring,
		}, fixedInstant)
		if got := idempotency.CodeOf(err); got != idempotency.CodeRetentionPolicyConflict {
			t.Fatalf("code = %q, want %q (%v)", got, idempotency.CodeRetentionPolicyConflict, err)
		}
	})

	t.Run("canonical financial and government namespaces require tombstones", func(t *testing.T) {
		capabilities := []string{
			"intent:hcmnext.payroll.payment_settlement",
			"intent:hcmnext.rewards.change_base_pay",
			"intent:hcmnext.benefits.benefit_enrollment_process",
			"intent:hcmnext.regulatory.government_filing_submit",
			"intent:hcmnext.mobility.mobility_tax_assess",
		}
		store := idempotency.PostgresStore{}
		for _, capability := range capabilities {
			scope := protected
			scope.Capability = capability
			_, _, err := store.Reserve(ctx, nil, scope, digest, idempotency.RetentionPolicy{
				Retention: time.Hour, Class: idempotency.RetentionExpiring,
			}, fixedInstant)
			if got := idempotency.CodeOf(err); got != idempotency.CodeRetentionPolicyConflict {
				t.Errorf("%s: code = %q, want %q (%v)", capability, got, idempotency.CodeRetentionPolicyConflict, err)
			}
		}
	})

	t.Run("protected capability without a registry entry fails closed", func(t *testing.T) {
		registry, err := idempotency.NewCapabilityRetentionRegistry(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		store := idempotency.PostgresStore{RetentionRegistry: registry}
		missing := protected
		missing.Capability = "intent:hcmnext.irreversible.unlisted_action"
		_, _, err = store.Reserve(ctx, nil, missing, digest, idempotency.RetentionPolicy{Retention: time.Hour}, fixedInstant)
		if got := idempotency.CodeOf(err); got != idempotency.CodeRetentionPolicyMissing {
			t.Fatalf("code = %q, want %q (%v)", got, idempotency.CodeRetentionPolicyMissing, err)
		}
	})

	t.Run("registry refuses expiring policy for a protected capability", func(t *testing.T) {
		_, err := idempotency.NewCapabilityRetentionRegistry(
			map[string]idempotency.RetentionClass{"intent:hcmnext.payroll.payment_settlement": idempotency.RetentionExpiring}, nil)
		if got := idempotency.CodeOf(err); got != idempotency.CodeRetentionPolicyConflict {
			t.Fatalf("code = %q, want %q (%v)", got, idempotency.CodeRetentionPolicyConflict, err)
		}
	})
}
