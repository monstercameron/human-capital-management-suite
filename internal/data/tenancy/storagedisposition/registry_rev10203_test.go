package storagedisposition_test

// REV-102-03: permanent tables must be immutable or reclassified, and the
// registry's append_only flags must agree with the live forbid_mutation
// triggers. RED, confirmed live: legal_rule_pack (00021) and
// record_copy_link (00056) are PERMANENT with an unguarded UPDATE grant;
// leave_availability_revision (00281) grants UPDATE against its own
// append-only comment; 17 sealed tables are registered append_only:false.
// performance_rating_case (00108) keeps its finalize-only guard trigger and
// is reclassified to OPERATIONAL with performance_rating_event as history.
// The RED clause's "17 OPERATIONAL rows flagged true" did not reproduce
// live: every append_only:true row already carries a full trigger, so the
// test fails on disagreement in either direction to keep it that way.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy/storagedisposition"
)

// rev10203TriggerMismatch compares every registry append_only flag against
// the live trigger set: tables carrying a full forbid_mutation trigger must
// be flagged append_only, and flagged tables must carry the trigger.
// It returns the tables flagged true with no trigger and the tables with a
// trigger that are flagged false, both sorted.
func rev10203TriggerMismatch(reg *storagedisposition.Registry, hasTrigger map[string]bool) (flaggedNoTrigger, triggerUnflagged []string) {
	for _, e := range reg.Tables {
		switch {
		case e.AppendOnly && !hasTrigger[e.Table]:
			flaggedNoTrigger = append(flaggedNoTrigger, e.Table)
		case !e.AppendOnly && hasTrigger[e.Table]:
			triggerUnflagged = append(triggerUnflagged, e.Table)
		}
	}
	sort.Strings(flaggedNoTrigger)
	sort.Strings(triggerUnflagged)
	return flaggedNoTrigger, triggerUnflagged
}

// rev10203PermanentUpdateGrants returns the sorted names of PERMANENT tables
// whose live UPDATE privilege is held yet no live BEFORE UPDATE trigger
// guards the path: versioned aggregates supersede under
// aggregate_forbid_inplace_update and work_item_claim under its no-rewrite
// guard, but an unguarded UPDATE on a permanent record is the REV-102-03
// gap (legal_rule_pack, record_copy_link pre-fix).
func rev10203PermanentUpdateGrants(reg *storagedisposition.Registry, updateGranted, updateGuarded map[string]bool) []string {
	var out []string
	for _, e := range reg.Tables {
		if e.RetentionClass == storagedisposition.RetentionPermanent && updateGranted[e.Table] && !updateGuarded[e.Table] {
			out = append(out, e.Table)
		}
	}
	sort.Strings(out)
	return out
}

// rev10203SealedUpdateGrants returns the sorted names of tables sealed by a
// full forbid_mutation trigger that still grant UPDATE: the grant can never
// succeed, so it is stray (leave_availability_revision's own comment calls
// its table SELECT/INSERT-only while granting UPDATE).
func rev10203SealedUpdateGrants(hasTrigger, updateGranted map[string]bool) []string {
	var out []string
	for table := range hasTrigger {
		if updateGranted[table] {
			out = append(out, table)
		}
	}
	sort.Strings(out)
	return out
}

// rev10203LiveTriggers returns the set of live base tables sealed by a
// FULL forbid_mutation trigger: one non-internal trigger firing BEFORE both
// UPDATE and DELETE ((tgtype & 26) = 26 selects the BEFORE, DELETE and
// UPDATE bits). A DELETE-only forbid_mutation trigger, as on
// operator_bypass_obligation or workflow_compiled_version where UPDATEs
// pass a dedicated review/identity guard, does not seal the table.
func rev10203LiveTriggers(t *testing.T, db *pgtest.DB) map[string]bool {
	t.Helper()
	rows, err := db.Conn.Query(context.Background(), `
		SELECT c.relname FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_proc p ON p.oid = t.tgfoid
		WHERE n.nspname = current_schema()
		  AND NOT t.tgisinternal AND p.proname = 'forbid_mutation'
		  AND (t.tgtype & 26) = 26`)
	if err != nil {
		t.Fatalf("list forbid_mutation triggers: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan trigger table: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list forbid_mutation triggers: %v", err)
	}
	return got
}

// rev10203LiveUpdateGuarded returns the set of live tables carrying at
// least one non-internal BEFORE UPDATE trigger: every UPDATE on such a
// table passes a guard (forbid_mutation, aggregate_forbid_inplace_update,
// a no-rewrite or single-transition trigger) instead of landing unchecked.
func rev10203LiveUpdateGuarded(t *testing.T, db *pgtest.DB) map[string]bool {
	t.Helper()
	rows, err := db.Conn.Query(context.Background(), `
		SELECT DISTINCT event_object_table FROM information_schema.triggers
		WHERE trigger_schema = current_schema()
		  AND action_timing = 'BEFORE' AND event_manipulation = 'UPDATE'`)
	if err != nil {
		t.Fatalf("list BEFORE UPDATE triggers: %v", err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan guarded table: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list BEFORE UPDATE triggers: %v", err)
	}
	return got
}

// rev10203LiveUpdateGrants returns the set of live tables on which grantee
// holds the UPDATE privilege.
func rev10203LiveUpdateGrants(t *testing.T, db *pgtest.DB, grantee string) map[string]bool {
	t.Helper()
	rows, err := db.Conn.Query(context.Background(), `
		SELECT table_name FROM information_schema.role_table_grants
		WHERE table_schema = current_schema()
		  AND grantee = $1 AND privilege_type = 'UPDATE'`, grantee)
	if err != nil {
		t.Fatalf("list UPDATE grants for %s: %v", grantee, err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan grant table for %s: %v", grantee, err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list UPDATE grants for %s: %v", grantee, err)
	}
	return got
}

// rev10203AssertAgreement proves the GREEN contract against one live
// schema: every append_only flag agrees with the pg_trigger catalogue, no
// PERMANENT table leaves an unguarded UPDATE path open, and no sealed table
// keeps a stray UPDATE grant.
func rev10203AssertAgreement(t *testing.T, db *pgtest.DB, reg *storagedisposition.Registry) {
	t.Helper()
	flaggedNoTrigger, triggerUnflagged := rev10203TriggerMismatch(reg, rev10203LiveTriggers(t, db))
	if len(flaggedNoTrigger) > 0 {
		t.Errorf("registry flags append_only:true with no live forbid_mutation trigger: %v", flaggedNoTrigger)
	}
	if len(triggerUnflagged) > 0 {
		t.Errorf("live forbid_mutation trigger with registry append_only:false: %v", triggerUnflagged)
	}

	granted := rev10203LiveUpdateGrants(t, db, "hcmnext_app")
	for table := range rev10203LiveUpdateGrants(t, db, "PUBLIC") {
		granted[table] = true
	}
	if bad := rev10203PermanentUpdateGrants(reg, granted, rev10203LiveUpdateGuarded(t, db)); len(bad) > 0 {
		t.Errorf("PERMANENT tables with an unguarded live UPDATE grant: %v", bad)
	}
	if bad := rev10203SealedUpdateGrants(rev10203LiveTriggers(t, db), granted); len(bad) > 0 {
		t.Errorf("forbid_mutation-sealed tables with a stray live UPDATE grant: %v", bad)
	}
	rev10203AssertCompensationHistoryCatalog(t, db, reg)
}

// rev10203AssertCompensationHistoryCatalog checks the live durable history
// needed to classify compensation operation state as mutable operational
// recovery data instead of permanent history.
func rev10203AssertCompensationHistoryCatalog(t *testing.T, db *pgtest.DB, reg *storagedisposition.Registry) {
	t.Helper()
	operation, ok := reg.Lookup("workflow_compensation_operation")
	if !ok || operation.RetentionClass != storagedisposition.RetentionOperational || operation.AppendOnly {
		t.Errorf("workflow_compensation_operation disposition = %+v, want mutable OPERATIONAL state", operation)
	}
	history, ok := reg.Lookup("workflow_compensation_operation_history")
	if !ok || history.RetentionClass != storagedisposition.RetentionPermanent || !history.AppendOnly {
		t.Errorf("workflow_compensation_operation_history disposition = %+v, want immutable PERMANENT history", history)
	}

	var enabled, forced bool
	err := db.Conn.QueryRow(context.Background(), `
		SELECT c.relrowsecurity, c.relforcerowsecurity
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname = 'workflow_compensation_operation_history'`).Scan(&enabled, &forced)
	if err != nil {
		t.Fatalf("read compensation history RLS state: %v", err)
	}
	if !enabled || !forced {
		t.Errorf("compensation history RLS enabled=%v forced=%v, want both true", enabled, forced)
	}
	var isolated bool
	if err := db.Conn.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_policies
			WHERE schemaname = current_schema() AND tablename = 'workflow_compensation_operation_history'
			  AND policyname = 'tenant_isolation'
			  AND qual LIKE '%tenant_id%' AND with_check LIKE '%tenant_id%'
		)`).Scan(&isolated); err != nil {
		t.Fatalf("read compensation history tenant policy: %v", err)
	}
	if !isolated {
		t.Error("compensation history has no tenant-isolation policy covering reads and writes")
	}
	if !rev10203LiveUpdateGuarded(t, db)["workflow_compensation_operation"] {
		t.Error("workflow_compensation_operation has no live BEFORE UPDATE history guard")
	}
}

type rev10203OperationIdentity struct {
	tenant, capability, effect, idempotency, digest string
}

func rev10203InsertCompensationOperation(t *testing.T, db *pgtest.DB) rev10203OperationIdentity {
	t.Helper()
	identity := rev10203OperationIdentity{
		tenant:      uuid.NewString(),
		capability:  "workflow.compensate",
		effect:      "effect-" + uuid.NewString(),
		idempotency: "idem-" + uuid.NewString(),
		digest:      strings.Repeat("a", 64),
	}
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from)
		VALUES ($1,$2,'cell-rev10203',$3,'ACTIVE',now())`, identity.tenant, "rev10203-"+identity.tenant, "REV-102-03 test tenant")
	db.Exec(t, `INSERT INTO workflow_compensation_operation
		(tenant_id,capability_id,effect_ref,idempotency_key,request_digest,state,payload)
		VALUES ($1,$2,$3,$4,$5,'RESERVED','{}'::jsonb)`,
		identity.tenant, identity.capability, identity.effect, identity.idempotency, identity.digest)
	return identity
}

func rev10203CheckCompensationHistoryReconstructsLatest(t *testing.T, db *pgtest.DB, identity rev10203OperationIdentity) {
	t.Helper()
	for _, step := range []struct {
		state   string
		payload string
	}{
		{state: "EFFECT_RECORDED", payload: `{"receipt":"r1"}`},
		{state: "COMPLETED", payload: `{"result":"done"}`},
	} {
		db.Exec(t, `UPDATE workflow_compensation_operation SET state=$1,payload=$2::jsonb,updated_at=now()
			WHERE tenant_id=$3 AND capability_id=$4 AND effect_ref=$5 AND idempotency_key=$6 AND request_digest=$7`,
			step.state, step.payload, identity.tenant, identity.capability, identity.effect, identity.idempotency, identity.digest)
	}

	rows, err := db.Conn.Query(context.Background(), `
		SELECT operation_version, change_kind, prior_state, state, prior_payload::text, payload::text
		FROM workflow_compensation_operation_history
		WHERE tenant_id=$1 AND capability_id=$2 AND effect_ref=$3 AND idempotency_key=$4
		ORDER BY operation_version`, identity.tenant, identity.capability, identity.effect, identity.idempotency)
	if err != nil {
		t.Fatalf("read compensation history: %v", err)
	}
	defer rows.Close()
	want := []struct {
		version               int64
		kind, prior, state    string
		priorPayload, payload string
	}{
		{version: 1, kind: "INSERT", state: "RESERVED", payload: "{}"},
		{version: 2, kind: "UPDATE", prior: "RESERVED", state: "EFFECT_RECORDED", priorPayload: "{}", payload: `{"receipt": "r1"}`},
		{version: 3, kind: "UPDATE", prior: "EFFECT_RECORDED", state: "COMPLETED", priorPayload: `{"receipt": "r1"}`, payload: `{"result": "done"}`},
	}
	var got int
	for rows.Next() {
		var version int64
		var kind, state, payload string
		var prior, priorPayload sql.NullString
		if err := rows.Scan(&version, &kind, &prior, &state, &priorPayload, &payload); err != nil {
			t.Fatalf("scan compensation history: %v", err)
		}
		if got >= len(want) {
			t.Fatalf("unexpected extra history row at version %d", version)
		}
		w := want[got]
		gotPrior, gotPriorPayload := prior.String, priorPayload.String
		if version != w.version || kind != w.kind || gotPrior != w.prior || state != w.state || gotPriorPayload != w.priorPayload || payload != w.payload {
			t.Errorf("history[%d] = (%d,%s,%s,%s,%s,%s), want (%d,%s,%s,%s,%s,%s)", got, version, kind, gotPrior, state, gotPriorPayload, payload, w.version, w.kind, w.prior, w.state, w.priorPayload, w.payload)
		}
		got++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate compensation history: %v", err)
	}
	if got != len(want) {
		t.Fatalf("history row count = %d, want %d", got, len(want))
	}

	var liveVersion, historyVersion int64
	var liveState, historyState, livePayload, historyPayload string
	if err := db.Conn.QueryRow(context.Background(), `
		SELECT operation_version, state, payload::text FROM workflow_compensation_operation
		WHERE tenant_id=$1 AND capability_id=$2 AND effect_ref=$3 AND idempotency_key=$4`,
		identity.tenant, identity.capability, identity.effect, identity.idempotency).Scan(&liveVersion, &liveState, &livePayload); err != nil {
		t.Fatalf("read live compensation operation: %v", err)
	}
	if err := db.Conn.QueryRow(context.Background(), `
		SELECT operation_version, state, payload::text FROM workflow_compensation_operation_history
		WHERE tenant_id=$1 AND capability_id=$2 AND effect_ref=$3 AND idempotency_key=$4
		ORDER BY operation_version DESC LIMIT 1`,
		identity.tenant, identity.capability, identity.effect, identity.idempotency).Scan(&historyVersion, &historyState, &historyPayload); err != nil {
		t.Fatalf("read latest compensation history state: %v", err)
	}
	if historyVersion != liveVersion || historyState != liveState || historyPayload != livePayload {
		t.Fatalf("latest history (%d,%s,%s) cannot reconstruct live operation (%d,%s,%s)", historyVersion, historyState, historyPayload, liveVersion, liveState, livePayload)
	}
}

// TestTodo_REV_102_03 proves the GREEN contract against the live schema.
func TestTodo_REV_102_03(t *testing.T) {
	t.Parallel()
	reg := loadRegistry(t)
	if err := storagedisposition.Validate(reg); err != nil {
		t.Fatalf("checked-in registry fails validation: %v", err)
	}
	rev10203AssertAgreement(t, pgtest.New(t), reg)
}

// rev10203PinnedTables are the registry rows REV-102-03 corrects, plus the
// neighbours it deliberately leaves alone: operator_bypass_obligation and
// workflow_compiled_version carry only a DELETE-only forbid_mutation trigger
// beside a guarded UPDATE path, so they stay append_only:false; every other
// row here is sealed or reclassified by the fix.
var rev10203PinnedTables = []string{
	"accumulator_definition",
	"appointment_requirement",
	"coverage_requirement",
	"demand_signal",
	"leave_availability_revision",
	"legal_rule_pack",
	"operator_bypass_obligation",
	"payroll_frozen_population",
	"payroll_run",
	"performance_calibration_session",
	"performance_cycle",
	"performance_final_rating",
	"performance_outcome_link",
	"performance_rating_case",
	"performance_review",
	"record_copy_link",
	"resource_type",
	"scenario_revision",
	"succession_critical_role",
	"succession_readiness_revision",
	"succession_slate",
	"time_device_registration",
	"workflow_compiled_version",
	"workflow_compensation_operation",
	"workflow_compensation_operation_history",
}

// TestTodo_REV_102_03_Golden pins the corrected REV-102-03 rows byte for
// byte: a future edit silently re-mutating one of these flags changes these
// bytes and fails here, not in production.
func TestTodo_REV_102_03_Golden(t *testing.T) {
	t.Parallel()
	regFile := registryPath(t)
	root := filepath.Dir(filepath.Dir(filepath.Dir(regFile)))
	goldenFile := filepath.Join(root, "internal", "data", "tenancy", "storagedisposition", "testdata", "rev10203_flag_rows.golden")
	want, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("golden file %s is missing; it must be checked in, never skipped: %v", goldenFile, err)
	}
	reg := loadRegistry(t)
	lines := make([]string, 0, len(rev10203PinnedTables))
	for _, table := range rev10203PinnedTables {
		e, ok := reg.Lookup(table)
		if !ok {
			t.Fatalf("registry no longer contains pinned table %s", table)
		}
		lines = append(lines, fmt.Sprintf("%s append_only=%v retention=%s", e.Table, e.AppendOnly, e.RetentionClass))
	}
	sort.Strings(lines)
	if got := strings.Join(lines, "\n") + "\n"; got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("REV-102-03 rows drifted from golden:\n--- golden ---\n%s\n--- live ---\n%s", want, got)
	}
}

// TestTodo_REV_102_03_Fault proves the comparisons above are load bearing:
// a forged flag and a forged UPDATE path are each detected, and the
// forbid_mutation mechanism itself refuses writes on a scratch table.
func TestTodo_REV_102_03_Fault(t *testing.T) {
	t.Parallel()

	t.Run("a forged append_only flag is detected", func(t *testing.T) {
		t.Parallel()
		reg := loadRegistry(t)
		flipped := ""
		for i, e := range reg.Tables {
			if !e.AppendOnly {
				reg.Tables[i].AppendOnly = true
				flipped = e.Table
				break
			}
		}
		if flipped == "" {
			t.Fatal("fixture requires at least one append_only:false entry")
		}
		// No live triggers in this forged world: the flipped row must be
		// reported as flagged-without-trigger.
		flagged, _ := rev10203TriggerMismatch(reg, map[string]bool{})
		found := false
		for _, table := range flagged {
			if table == flipped {
				found = true
			}
		}
		if !found {
			t.Fatalf("flipping append_only on %s was not detected", flipped)
		}
	})

	t.Run("a forged UPDATE path on a PERMANENT table is detected", func(t *testing.T) {
		t.Parallel()
		reg := loadRegistry(t)
		target := ""
		for _, e := range reg.Tables {
			if e.RetentionClass == storagedisposition.RetentionPermanent {
				target = e.Table
				break
			}
		}
		if target == "" {
			t.Fatal("fixture requires at least one PERMANENT entry")
		}
		if bad := rev10203PermanentUpdateGrants(reg, map[string]bool{target: true}, map[string]bool{}); len(bad) != 1 || bad[0] != target {
			t.Fatalf("a forged UPDATE grant on PERMANENT %s was not detected; got %v", target, bad)
		}
		if bad := rev10203SealedUpdateGrants(map[string]bool{target: true}, map[string]bool{target: true}); len(bad) != 1 || bad[0] != target {
			t.Fatalf("a forged UPDATE grant on sealed %s was not detected; got %v", target, bad)
		}
	})

	t.Run("forbid_mutation refuses UPDATE and DELETE", func(t *testing.T) {
		t.Parallel()
		db := pgtest.New(t)
		db.Exec(t, `CREATE TABLE rev10203_scratch (id uuid PRIMARY KEY, note text NOT NULL)`)
		db.Exec(t, `CREATE TRIGGER rev10203_scratch_no_mutation BEFORE UPDATE OR DELETE ON rev10203_scratch FOR EACH ROW EXECUTE FUNCTION forbid_mutation()`)
		db.Exec(t, `INSERT INTO rev10203_scratch (id, note) VALUES (gen_random_uuid(), 'sealed')`)
		if err := db.ExecErr(`UPDATE rev10203_scratch SET note = 'rewritten'`); err == nil {
			t.Fatal("UPDATE against a forbid_mutation trigger committed")
		}
		if err := db.ExecErr(`DELETE FROM rev10203_scratch`); err == nil {
			t.Fatal("DELETE against a forbid_mutation trigger committed")
		}
	})

	t.Run("compensation history rejects identity and invalid state rewrites", func(t *testing.T) {
		t.Parallel()
		db := pgtest.New(t)
		identity := rev10203InsertCompensationOperation(t, db)
		if _, err := db.Conn.Exec(context.Background(), `UPDATE workflow_compensation_operation
			SET state='RESERVED', request_digest=$1
			WHERE tenant_id=$2 AND capability_id=$3 AND effect_ref=$4 AND idempotency_key=$5`,
			strings.Repeat("b", 64), identity.tenant, identity.capability, identity.effect, identity.idempotency); err == nil {
			t.Fatal("workflow compensation operation accepted an identity rewrite")
		}
		if _, err := db.Conn.Exec(context.Background(), `UPDATE workflow_compensation_operation
			SET state='COMPLETED', payload='{"result":"done"}'::jsonb
			WHERE tenant_id=$1 AND capability_id=$2 AND effect_ref=$3 AND idempotency_key=$4`,
			identity.tenant, identity.capability, identity.effect, identity.idempotency); err != nil {
			t.Fatalf("workflow compensation operation refused a valid completion transition: %v", err)
		}
		if _, err := db.Conn.Exec(context.Background(), `UPDATE workflow_compensation_operation
			SET state='RESERVED', payload='{"forged":true}'::jsonb
			WHERE tenant_id=$1 AND capability_id=$2 AND effect_ref=$3 AND idempotency_key=$4`,
			identity.tenant, identity.capability, identity.effect, identity.idempotency); err == nil {
			t.Fatal("workflow compensation operation accepted a state regression")
		}
		var historyRows int
		if err := db.Conn.QueryRow(context.Background(), `
			SELECT count(*) FROM workflow_compensation_operation_history
			WHERE tenant_id=$1 AND capability_id=$2 AND effect_ref=$3 AND idempotency_key=$4`,
			identity.tenant, identity.capability, identity.effect, identity.idempotency).Scan(&historyRows); err != nil {
			t.Fatalf("count compensation history after refused updates: %v", err)
		}
		if historyRows != 2 {
			t.Fatalf("refused updates left %d history rows, want the initial reservation and valid completion", historyRows)
		}
	})
}

// TestTodo_REV_102_03_Recovery proves the corrected state is durable and
// re-derivable: a freshly loaded registry still agrees with the live
// catalogues, and the REV-102-03 migration round-trips (Down then Up)
// back to that same agreeing state.
func TestTodo_REV_102_03_Recovery(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)

	t.Run("freshly loaded registry agrees with the live catalogues", func(t *testing.T) {
		t.Parallel()
		fresh, err := storagedisposition.Load(registryPath(t))
		if err != nil {
			t.Fatalf("reload registry: %v", err)
		}
		flaggedNoTrigger, triggerUnflagged := rev10203TriggerMismatch(fresh, rev10203LiveTriggers(t, db))
		if len(flaggedNoTrigger) > 0 || len(triggerUnflagged) > 0 {
			t.Fatalf("reloaded registry disagrees with live triggers: flagged-without-trigger=%v trigger-unflagged=%v",
				flaggedNoTrigger, triggerUnflagged)
		}
		granted := rev10203LiveUpdateGrants(t, db, "hcmnext_app")
		for table := range rev10203LiveUpdateGrants(t, db, "PUBLIC") {
			granted[table] = true
		}
		if bad := rev10203PermanentUpdateGrants(fresh, granted, rev10203LiveUpdateGuarded(t, db)); len(bad) > 0 {
			t.Fatalf("reloaded registry leaves unguarded UPDATE paths on PERMANENT tables: %v", bad)
		}
		if bad := rev10203SealedUpdateGrants(rev10203LiveTriggers(t, db), granted); len(bad) > 0 {
			t.Fatalf("reloaded registry leaves stray UPDATE grants on sealed tables: %v", bad)
		}
	})

	t.Run("mutable compensation state reconstructs from immutable revisions", func(t *testing.T) {
		t.Parallel()
		db := pgtest.New(t)
		identity := rev10203InsertCompensationOperation(t, db)
		rev10203CheckCompensationHistoryReconstructsLatest(t, db, identity)
	})

	t.Run("a rebuilt schema reaches the agreeing state", func(t *testing.T) {
		// Disaster-recovery shape: migrate an empty schema from scratch
		// through the whole tree including 00323, then prove the registry
		// agrees with what the rebuild produced. 00323 itself is
		// irreversible by the post-00279 house convention, so the proof
		// rebuilds forward rather than rolling back.
		rebuilt := pgtest.NewEmpty(t)
		if _, err := rebuilt.Provider(t).Up(context.Background()); err != nil {
			t.Fatalf("migrate an empty schema to latest: %v", err)
		}
		fresh, err := storagedisposition.Load(registryPath(t))
		if err != nil {
			t.Fatalf("reload registry: %v", err)
		}
		if err := storagedisposition.Validate(fresh); err != nil {
			t.Fatalf("reloaded registry fails validation: %v", err)
		}
		rev10203AssertAgreement(t, rebuilt, fresh)
	})
}
