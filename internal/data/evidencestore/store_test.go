package evidencestore_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/evidencestore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var at = time.Date(2026, 9, 15, 10, 30, 0, 123456789, time.UTC)

func seedTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, id, key, "evidencestore "+key)
	return id
}

func mapper(ids map[values.TenantId]uuid.UUID) evidencestore.TenantIDs {
	return func(t values.TenantId) uuid.UUID { return ids[t] }
}

func gate(tenant, intentID, decision string) capability.InvocationEvidence {
	return capability.InvocationEvidence{
		CapabilityID: "workflow.execution_authority_gate", CapabilityVersion: 1,
		SubjectRef: intentID, Tenant: tenant, Decision: decision, OccurredAt: at,
	}
}

func count(t *testing.T, db *pgtest.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM capability_invocation_evidence`).Scan(&n); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	return n
}

// TestTodo_WF_RUN_035_Integration proves the durable evidence store against
// PostgreSQL: capability decisions and execution evidence become tenant-scoped
// rows with deterministic ids; re-recording the same decision replays the
// same row (no second row); the governed envelope and instants round-trip
// exactly; a store composed over a fresh connection (a restart) reads the
// journey back in recording order; and concurrent replays still record one
// row.
func TestTodo_WF_RUN_035_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	acme := seedTenant(t, db, "evs-acme")
	ids := map[values.TenantId]uuid.UUID{"acme": acme}
	store := evidencestore.New(db.Conn, mapper(ids))

	admitted, err := store.RecordInvocation(ctx, gate("acme", "intent-1", "GATE_ADMITTED"))
	if err != nil || !strings.HasPrefix(admitted, "ev:capability:") || len(admitted) != len("ev:capability:")+24 {
		t.Fatalf("RecordInvocation = %q, %v", admitted, err)
	}
	replayed, err := store.RecordInvocation(ctx, gate("acme", "intent-1", "GATE_ADMITTED"))
	if err != nil || replayed != admitted || count(t, db) != 1 {
		t.Fatalf("replay = %q, %v with %d rows; want the same id and one row", replayed, err, count(t, db))
	}

	deadline := at.Add(30 * time.Second)
	invoked := capability.InvocationEvidence{
		CapabilityID: "hcmnext.people.explain_worker_state", CapabilityVersion: 3, SubjectRef: "principal:mgr",
		Tenant: "acme", Decision: "INVOKED", OccurredAt: at.Add(time.Second), Purpose: "compensation_review",
		IdempotencyKey: "inst-1/snapshot/1/hcmnext.people.explain_worker_state", Deadline: deadline, EffectClass: capability.EffectReadOnly,
	}
	invokedID, err := store.RecordInvocation(ctx, invoked)
	if err != nil || invokedID == admitted {
		t.Fatalf("RecordInvocation(invoked) = %q, %v", invokedID, err)
	}
	instance := uuid.NewString()
	approval, err := store.RecordExecutionEvidence(ctx, acme, "APPROVAL_COMPLETED", instance, "approve", "wi-1", "sha256:out", at.Add(2*time.Second))
	if err != nil {
		t.Fatalf("RecordExecutionEvidence: %v", err)
	}
	terminal, err := store.RecordExecutionEvidence(ctx, acme, "TERMINAL_WRITTEN", instance, "end", "event:1", "sha256:term", at.Add(3*time.Second))
	if err != nil || terminal == approval {
		t.Fatalf("RecordExecutionEvidence(terminal) = %q, %v", terminal, err)
	}
	if again, err := store.RecordExecutionEvidence(ctx, acme, "TERMINAL_WRITTEN", instance, "end", "event:1", "sha256:term", at.Add(3*time.Second)); err != nil || again != terminal {
		t.Fatalf("execution evidence replay = %q, %v; want %s", again, err, terminal)
	}
	// Different content at the same instant is a different row.
	if other, err := store.RecordInvocation(ctx, gate("acme", "intent-1", "GATE_REFUSED")); err != nil || other == admitted {
		t.Fatalf("a refusal replayed the admission: %q, %v", other, err)
	}
	if _, err := store.RecordInvocation(ctx, gate("acme", "intent-other", "GATE_ADMITTED")); err != nil {
		t.Fatal(err)
	}

	restarted := evidencestore.New(db.NewConn(t), mapper(ids))
	journey, err := restarted.JourneyEvidenceIDs(ctx, "acme", acme, "intent-1", instance)
	if err != nil {
		t.Fatalf("JourneyEvidenceIDs after restart: %v", err)
	}
	want := []string{admitted, approval, terminal}
	if len(journey) != 4 || journey[0] != want[0] || journey[1] != want[1] || journey[2] != want[2] {
		t.Fatalf("journey evidence = %v, want %v then the refusal", journey, want)
	}
	if byInstance, err := restarted.JourneyEvidenceIDs(ctx, "acme", uuid.Nil, "", instance); err != nil || len(byInstance) != 2 {
		t.Fatalf("instance-only journey = %v, %v", byInstance, err)
	}
	if none, err := restarted.JourneyEvidenceIDs(ctx, "acme", acme, "", ""); err != nil || len(none) != 0 {
		t.Fatalf("an empty journey = %v, %v", none, err)
	}

	records, err := restarted.List(ctx, "acme")
	if err != nil || len(records) != 6 {
		t.Fatalf("List = %d records, %v; want 6", len(records), err)
	}
	for i := 1; i < len(records); i++ {
		if records[i].Sequence <= records[i-1].Sequence {
			t.Fatalf("records are not in recording order: %+v", records)
		}
	}
	got := records[1]
	if got.EvidenceID != invokedID || got.TenantID != acme || got.CapabilityVersion != 3 || got.Purpose != invoked.Purpose ||
		got.IdempotencyKey != invoked.IdempotencyKey || !got.Deadline.Equal(deadline.Truncate(time.Microsecond)) ||
		got.EffectClass != string(capability.EffectReadOnly) || !got.OccurredAt.Equal(invoked.OccurredAt.Truncate(time.Microsecond)) ||
		!strings.HasPrefix(got.Digest, "sha256:") || !strings.Contains(got.EvidenceID, got.Digest[len("sha256:"):len("sha256:")+24]) {
		t.Fatalf("round-tripped invocation = %+v", got)
	}
	exec := records[2]
	if exec.CapabilityID != "workflow.execution.evidence" || exec.SubjectRef != instance+"|approve" || exec.ReasonCode != "wi-1|sha256:out" || !exec.Deadline.IsZero() {
		t.Fatalf("execution evidence packing = %+v", exec)
	}

	// Concurrent replays of one new decision still land exactly one row.
	before := count(t, db)
	var wg sync.WaitGroup
	idsSeen := make(chan string, 6)
	for range 6 {
		racer := evidencestore.New(db.NewConn(t), mapper(ids))
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := racer.RecordInvocation(ctx, gate("acme", "intent-race", "GATE_ADMITTED"))
			if err != nil {
				t.Errorf("concurrent RecordInvocation: %v", err)
			}
			idsSeen <- id
		}()
	}
	wg.Wait()
	close(idsSeen)
	first := ""
	for id := range idsSeen {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("concurrent replays minted different ids %s and %s", first, id)
		}
	}
	if after := count(t, db); after != before+1 {
		t.Fatalf("concurrent replays recorded %d rows, want 1", after-before)
	}
}

// TestTodo_WF_RUN_035_Security proves the evidence table and store fail
// closed: a record or read naming no tenant, a nil storage tenant, a tenant
// key that disagrees with its storage tenant and malformed evidence are
// refused; row-level security hides one tenant's evidence from another under
// the application role; the application role cannot UPDATE or DELETE a row
// and the forbid_mutation trigger refuses both even for the owner; and a row
// tampered with behind the trigger's back is detected on read by its digest.
func TestTodo_WF_RUN_035_Security(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	acme, globex := seedTenant(t, db, "evs-sec-acme"), seedTenant(t, db, "evs-sec-globex")
	ids := map[values.TenantId]uuid.UUID{"acme": acme, "globex": globex}
	store := evidencestore.New(db.Conn, mapper(ids))

	acmeID, err := store.RecordInvocation(ctx, gate("acme", "intent-shared", "GATE_ADMITTED"))
	if err != nil {
		t.Fatal(err)
	}
	globexID, err := store.RecordInvocation(ctx, gate("globex", "intent-shared", "GATE_ADMITTED"))
	if err != nil {
		t.Fatal(err)
	}
	if acmeID == globexID {
		t.Fatal("two tenants' identical decisions share one evidence id; the tenant is not part of the digest")
	}
	if got, err := store.JourneyEvidenceIDs(ctx, "globex", globex, "intent-shared", ""); err != nil || len(got) != 1 || got[0] != globexID {
		t.Fatalf("globex journey = %v, %v; want only its own evidence", got, err)
	}

	for name, bad := range map[string]func() error{
		"no tenant on the record": func() error { _, err := store.RecordInvocation(ctx, gate("", "i", "X")); return err },
		"nil storage tenant": func() error {
			_, err := store.RecordExecutionEvidence(ctx, uuid.Nil, "TERMINAL_WRITTEN", "i", "n", "r", "d", at)
			return err
		},
		"no tenant on the read": func() error { _, err := store.JourneyEvidenceIDs(ctx, "", acme, "i", ""); return err },
		"no tenant on the list": func() error { _, err := store.List(ctx, " "); return err },
	} {
		if err := bad(); !errors.Is(err, evidencestore.ErrTenantRequired) {
			t.Errorf("%s = %v, want ErrTenantRequired", name, err)
		}
	}
	if _, err := store.JourneyEvidenceIDs(ctx, "acme", globex, "intent-shared", ""); !errors.Is(err, evidencestore.ErrTenantMismatch) {
		t.Errorf("a read naming acme with globex's storage tenant = %v, want ErrTenantMismatch", err)
	}
	if _, err := store.RecordInvocation(ctx, capability.InvocationEvidence{Tenant: "acme", Decision: "X", OccurredAt: at}); !errors.Is(err, evidencestore.ErrInvalid) {
		t.Errorf("evidence without a capability = %v, want ErrInvalid", err)
	}
	if _, err := store.RecordInvocation(ctx, capability.InvocationEvidence{CapabilityID: "c", CapabilityVersion: 1, Tenant: "acme", Decision: "X"}); !errors.Is(err, evidencestore.ErrInvalid) {
		t.Errorf("evidence without an instant = %v, want ErrInvalid", err)
	}
	if _, err := evidencestore.New(nil, mapper(ids)).RecordInvocation(ctx, gate("acme", "i", "X")); !errors.Is(err, evidencestore.ErrInvalid) {
		t.Errorf("a store without a database = %v, want ErrInvalid", err)
	}
	if _, err := evidencestore.New(db.Conn, nil).RecordInvocation(ctx, gate("acme", "i", "X")); !errors.Is(err, evidencestore.ErrInvalid) {
		t.Errorf("a store without a tenant mapping = %v, want ErrInvalid", err)
	}

	// Row-level security under the application role.
	app := db.NewConn(t)
	if _, err := app.Exec(ctx, "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("SET ROLE hcmnext_app: %v", err)
	}
	if _, err := app.Exec(ctx, `SELECT set_config('app.tenant_id', $1, false)`, acme.String()); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := app.QueryRow(ctx, `SELECT count(*) FROM capability_invocation_evidence`).Scan(&visible); err != nil {
		t.Fatalf("count as the application role: %v", err)
	}
	if visible != 1 {
		t.Fatalf("acme sees %d evidence rows under RLS, want only its own 1", visible)
	}
	if _, err := app.Exec(ctx, `INSERT INTO capability_invocation_evidence
		(tenant_id, evidence_id, capability_id, capability_version, subject_ref, decision, reason_code, occurred_at, purpose, idempotency_key, effect_class, record_digest)
		VALUES ($1, 'ev:capability:000000000000000000000000', 'c', 1, 's', 'X', '', now(), '', '', '', 'sha256:0000000000000000000000000000000000000000000000000000000000000000')`, globex); err == nil {
		t.Fatal("acme's session inserted a row into globex's evidence")
	}
	if _, err := app.Exec(ctx, `UPDATE capability_invocation_evidence SET reason_code = 'forged'`); err == nil {
		t.Fatal("the application role updated evidence")
	}
	if _, err := app.Exec(ctx, `DELETE FROM capability_invocation_evidence`); err == nil {
		t.Fatal("the application role deleted evidence")
	}

	// forbid_mutation refuses the owner too, inside the row's own tenant.
	mutate := func(sql string) error {
		tx, err := db.Conn.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, acme.String()); err != nil {
			return err
		}
		n, err := tx.Exec(ctx, sql, acme)
		if err == nil && n == 0 {
			t.Fatalf("%q matched no row, so it proves nothing about the trigger", sql)
		}
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err := mutate(`UPDATE capability_invocation_evidence SET reason_code = 'forged' WHERE tenant_id = $1`); err == nil {
		t.Fatal("forbid_mutation allowed an UPDATE")
	}
	if err := mutate(`DELETE FROM capability_invocation_evidence WHERE tenant_id = $1`); err == nil {
		t.Fatal("forbid_mutation allowed a DELETE")
	}

	// Tamper behind the trigger: the digest re-derivation catches a rewritten
	// decision on read, and a replay onto a row whose sealed digest was
	// rewritten is refused rather than silently accepted.
	sealedID, err := store.RecordInvocation(ctx, gate("acme", "intent-sealed", "GATE_ADMITTED"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`ALTER TABLE capability_invocation_evidence DISABLE TRIGGER capability_invocation_evidence_forbid_mutation`,
		`SELECT set_config('app.tenant_id', '` + acme.String() + `', true)`,
		`UPDATE capability_invocation_evidence SET decision = 'GATE_REFUSED' WHERE evidence_id = '` + acmeID + `'`,
		`UPDATE capability_invocation_evidence
			SET record_digest = substr(record_digest, 1, 70) || CASE WHEN right(record_digest, 1) = '0' THEN '1' ELSE '0' END
			WHERE evidence_id = '` + sealedID + `'`,
		`ALTER TABLE capability_invocation_evidence ENABLE TRIGGER capability_invocation_evidence_forbid_mutation`,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("tamper step %q: %v", stmt, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ctx, "acme"); !errors.Is(err, evidencestore.ErrDigestMismatch) {
		t.Fatalf("List over a tampered row = %v, want ErrDigestMismatch", err)
	}
	if _, err := store.JourneyEvidenceIDs(ctx, "acme", acme, "intent-shared", ""); !errors.Is(err, evidencestore.ErrDigestMismatch) {
		t.Fatalf("a journey read over a tampered row = %v, want ErrDigestMismatch", err)
	}
	if _, err := store.RecordInvocation(ctx, gate("acme", "intent-sealed", "GATE_ADMITTED")); !errors.Is(err, evidencestore.ErrDigestMismatch) {
		t.Fatalf("a replay onto a row whose digest was rewritten = %v, want ErrDigestMismatch", err)
	}
	if _, err := store.List(ctx, "globex"); err != nil {
		t.Fatalf("globex's untampered evidence = %v", err)
	}
}

// TestTodo_Unit5_EvidenceJoinsCallerTransaction proves the in-advance
// recording shares the caller's transaction fate: an entry recorded on a
// transaction that rolls back leaves no orphan row, and the same entry
// recorded on a transaction that commits lands durably under the same
// deterministic id.
func TestTodo_Unit5_EvidenceJoinsCallerTransaction(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	acme := seedTenant(t, db, "evs-tx")
	store := evidencestore.New(db.Conn, mapper(map[values.TenantId]uuid.UUID{"acme": acme}))
	instance := uuid.NewString()

	aborted, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, aborted, acme); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	rolledBackID, err := store.RecordExecutionEvidenceTx(ctx, aborted, acme,
		"TERMINAL_WRITTEN", instance, "end", "event:1", "sha256:term", at)
	if err != nil {
		t.Fatalf("RecordExecutionEvidenceTx: %v", err)
	}
	if err := aborted.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if records, err := store.List(ctx, "acme"); err != nil || len(records) != 0 {
		t.Fatalf("List after rollback = %d records, %v; want no orphan evidence", len(records), err)
	}

	committed, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = committed.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, committed, acme); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	committedID, err := store.RecordExecutionEvidenceTx(ctx, committed, acme,
		"TERMINAL_WRITTEN", instance, "end", "event:1", "sha256:term", at)
	if err != nil {
		t.Fatalf("RecordExecutionEvidenceTx: %v", err)
	}
	if committedID != rolledBackID {
		t.Fatalf("committed id %q != rolled-back id %q: the entry must be content-addressed, not commit-addressed", committedID, rolledBackID)
	}
	if err := committed.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	records, err := store.List(ctx, "acme")
	if err != nil || len(records) != 1 || records[0].EvidenceID != committedID {
		t.Fatalf("List after commit = %+v, %v; want the one entry %s", records, err, committedID)
	}

	if _, err := store.RecordExecutionEvidenceTx(ctx, nil, acme,
		"TERMINAL_WRITTEN", instance, "end", "event:1", "sha256:term", at); err == nil {
		t.Fatal("a nil caller transaction was accepted: the entry would have nowhere atomic to land")
	}
}
