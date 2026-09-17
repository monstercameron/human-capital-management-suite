package custom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func custom007V2(t *testing.T) CustomObjectDefinition {
	t.Helper()
	def := testDefinition()
	def.Version = 2
	fields := make(map[string]FieldDefinition, len(def.Fields)+1)
	for k, v := range def.Fields {
		fields[k] = v
	}
	fields["nickname"] = FieldDefinition{Type: "string", Classification: FieldClassification{AuthZDomain: "worker.core", Classification: "INTERNAL", ResidencyRef: "NO_CONSTRAINT", RetentionClass: "OPERATIONAL"}}
	def.Fields = fields
	if err := def.Validate(); err != nil {
		t.Fatal(err)
	}
	return def
}

func custom007Options() MigrationOptions {
	return MigrationOptions{
		LiveReferences: 0, HistoryRetained: true, HoldClearanceRef: "holds:clear:2026",
		RollbackRef: "migrations:vehicle:v2:rollback", EvidenceDigest: "evidence:migration:v2",
		EffectiveAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestTodo_CUSTOM_007 is the RED contract: a breaking migration that would
// orphan history or live references, or remove held data, is refused before
// any version advances.
func TestTodo_CUSTOM_007(t *testing.T) {
	from := testDefinition()
	to := custom007V2(t)
	plan, err := PlanMigration(from, to, custom007Options())
	if err != nil {
		t.Fatalf("PlanMigration: %v", err)
	}
	if plan.FromVersion != 1 || plan.ToVersion != 2 || plan.Breaking() {
		t.Fatalf("plan = %+v, want non-breaking 1->2", plan)
	}
	if plan.RollbackRef == "" || plan.EvidenceDigest == "" || plan.Impact.Added != 1 {
		t.Fatalf("plan lacks impact/rollback/adoption evidence: %+v", plan)
	}

	// Seeded defect: v2 drops the "active" field while live references exist
	// and holds are uncleared — history would be orphaned.
	drop := custom007V2(t)
	fields := make(map[string]FieldDefinition)
	for k, v := range drop.Fields {
		if k != "active" {
			fields[k] = v
		}
	}
	drop.Fields = fields
	opts := custom007Options()
	opts.LiveReferences = 3
	if _, err := PlanMigration(from, drop, opts); !errors.Is(err, ErrMigrationBlocked) {
		t.Fatalf("orphaning migration err=%v, want blocked", err)
	}
	opts.LiveReferences = 0
	opts.HistoryRetained = false
	if _, err := PlanMigration(from, drop, opts); !errors.Is(err, ErrMigrationHeldData) {
		t.Fatalf("held-data migration err=%v, want held-data refusal", err)
	}
	var bad TypeMigration
	if bad.Breaking() {
		t.Fatal("zero migration must not report breaking")
	}

	// Retire: the superseded v1 is retired explicitly once effective.
	retired, err := MarkRetired(plan, custom007Options().EffectiveAt.Add(time.Hour), "evidence:retire:v1")
	if err != nil {
		t.Fatalf("MarkRetired: %v", err)
	}
	if retired.RetiredVersion != 1 || retired.SuccessorVersion != 2 {
		t.Fatalf("retirement = %+v", retired)
	}
	if _, err := MarkRetired(plan, custom007Options().EffectiveAt.Add(-time.Hour), "evidence:retire:v1"); !errors.Is(err, ErrInvalidMigration) {
		t.Fatalf("early retirement err=%v", err)
	}

	// Rename with a retype carries values across and re-echoes the mapped
	// type; the rollback inverts both.
	renamed := custom007V2(t)
	rfields := make(map[string]FieldDefinition, len(renamed.Fields))
	for k, v := range renamed.Fields {
		if k == "registration" {
			v.Type = "integer"
			rfields["reg_id"] = v
			continue
		}
		rfields[k] = v
	}
	renamed.Fields = rfields
	ropts := custom007Options()
	ropts.Renames = map[string]string{"registration": "reg_id"}
	rplan, err := PlanMigration(testDefinition(), renamed, ropts)
	if err != nil {
		t.Fatalf("rename plan: %v", err)
	}
	if !rplan.Breaking() || rplan.Impact.Renamed != 1 || rplan.Impact.Retyped != 1 {
		t.Fatalf("rename plan = %+v", rplan)
	}
	rmoved, err := MigrateRecord(rplan, testRecord(t, "vehicle-9", "ABC-9", true))
	if err != nil {
		t.Fatalf("rename apply: %v", err)
	}
	if rmoved.FieldValues["reg_id"].Value != "ABC-9" || rmoved.FieldValues["reg_id"].Type != "integer" {
		t.Fatalf("rename lost value or type: %+v", rmoved.FieldValues["reg_id"])
	}
	rback, err := RollbackPlan(rplan)
	if err != nil {
		t.Fatalf("rename rollback: %v", err)
	}
	restored, err := MigrateRecord(rback, rmoved)
	if err != nil {
		t.Fatalf("rename rollback apply: %v", err)
	}
	if restored.FieldValues["registration"].Value != "ABC-9" || restored.FieldValues["registration"].Type != "string" {
		t.Fatalf("rename rollback lost value or type: %+v", restored.FieldValues["registration"])
	}
}

// TestTodo_CUSTOM_007_Integration proves the expand/backfill/cutover path
// through the real event store: v1 history stays replayable and the migrated
// v2 record commits exactly once.
func TestTodo_CUSTOM_007_Integration(t *testing.T) {
	ctx := context.Background()
	store := NewEventStore()
	tenant := values.TenantId("tenant-a")
	if _, err := store.Commit(ctx, testMutation(t, tenant, "vehicle-9", "ABC-9", OperationCreate, 0, 0)); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanMigration(testDefinition(), custom007V2(t), custom007Options())
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := MigrateRecord(plan, testRecord(t, "vehicle-9", "ABC-9", true))
	if err != nil {
		t.Fatal(err)
	}
	if migrated.DefinitionVersion != 2 {
		t.Fatalf("migrated version = %d, want 2", migrated.DefinitionVersion)
	}
	if migrated.FieldValues["nickname"].Value != "" {
		t.Fatalf("added field has no default: %+v", migrated.FieldValues["nickname"])
	}
	mut := testMutation(t, tenant, "vehicle-9", "ABC-9", OperationChange, 1, 1)
	mut.Definition = custom007V2(t)
	mut.Record = migrated
	receipt, err := store.Commit(ctx, mut)
	if err != nil {
		t.Fatalf("cutover commit: %v", err)
	}
	if receipt.Event.DefinitionVersion != 2 {
		t.Fatalf("cutover event = %+v", receipt.Event)
	}
	report, err := store.Rebuild(ctx, tenant, "Vehicle", "vehicle-9")
	if err != nil {
		t.Fatal(err)
	}
	if report.SourceHead != 2 || len(store.Events(tenant, "Vehicle", "vehicle-9")) != 2 {
		t.Fatalf("history orphaned: report=%+v", report)
	}
}

// TestTodo_CUSTOM_007_Fault proves unmapped retypes, version skips and
// evidence-free plans fail before any record moves.
func TestTodo_CUSTOM_007_Fault(t *testing.T) {
	from := testDefinition()
	retype := custom007V2(t)
	fields := make(map[string]FieldDefinition, len(retype.Fields))
	for k, v := range retype.Fields {
		if k == "registration" {
			v.Type = "integer"
		}
		fields[k] = v
	}
	retype.Fields = fields
	if _, err := PlanMigration(from, retype, custom007Options()); !errors.Is(err, ErrMigrationBreaking) {
		t.Fatalf("unmapped retype err=%v", err)
	}
	skip := custom007V2(t)
	skip.Version = 3
	if _, err := PlanMigration(from, skip, custom007Options()); !errors.Is(err, ErrMigrationVersion) {
		t.Fatalf("version skip err=%v", err)
	}
	opts := custom007Options()
	opts.RollbackRef = ""
	if _, err := PlanMigration(from, custom007V2(t), opts); !errors.Is(err, ErrInvalidMigration) {
		t.Fatalf("rollback-free plan err=%v", err)
	}
	plan, err := PlanMigration(from, custom007V2(t), custom007Options())
	if err != nil {
		t.Fatal(err)
	}
	rec := testRecord(t, "vehicle-9", "ABC-9", true)
	rec.DefinitionVersion = 2
	if _, err := MigrateRecord(plan, rec); !errors.Is(err, ErrInvalidMigration) {
		t.Fatalf("wrong-version record err=%v", err)
	}
}

// TestTodo_CUSTOM_007_Recovery proves every migration ships its rollback:
// the inverse plan restores the v1 shape from retained history.
func TestTodo_CUSTOM_007_Recovery(t *testing.T) {
	plan, err := PlanMigration(testDefinition(), custom007V2(t), custom007Options())
	if err != nil {
		t.Fatal(err)
	}
	back, err := RollbackPlan(plan)
	if err != nil {
		t.Fatalf("RollbackPlan: %v", err)
	}
	if back.FromVersion != 2 || back.ToVersion != 1 || len(back.Removed) != 1 || back.Removed[0] != "nickname" {
		t.Fatalf("rollback = %+v, want 2->1 dropping nickname", back)
	}
	migrated, err := MigrateRecord(plan, testRecord(t, "vehicle-9", "ABC-9", true))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := MigrateRecord(back, migrated)
	if err != nil {
		t.Fatalf("rollback apply: %v", err)
	}
	if restored.DefinitionVersion != 1 {
		t.Fatalf("restored version = %d, want 1", restored.DefinitionVersion)
	}
	if _, ok := restored.FieldValues["nickname"]; ok {
		t.Fatal("rollback left the added field behind")
	}
	if restored.FieldValues["registration"].Value != "ABC-9" {
		t.Fatalf("rollback lost history: %+v", restored.FieldValues)
	}
}

// TestTodo_CUSTOM_007_Mutation proves tampered plans and records cannot slip
// through as clean migrations, and refused commits write nothing.
func TestTodo_CUSTOM_007_Mutation(t *testing.T) {
	plan, err := PlanMigration(testDefinition(), custom007V2(t), custom007Options())
	if err != nil {
		t.Fatal(err)
	}
	tampered := plan
	tampered.ToVersion = 3
	if err := tampered.Validate(); !errors.Is(err, ErrMigrationVersion) {
		t.Fatalf("tampered plan err=%v", err)
	}
	rec := testRecord(t, "vehicle-9", "ABC-9", true)
	rec.FieldValues["bogus"] = TypedValue{FieldName: "bogus", Type: "string", Value: "x"}
	if _, err := MigrateRecord(plan, rec); !errors.Is(err, ErrInvalidMigration) {
		t.Fatalf("undeclared field err=%v", err)
	}

	ctx := context.Background()
	store := NewEventStore()
	tenant := values.TenantId("tenant-a")
	if _, err := store.Commit(ctx, testMutation(t, tenant, "vehicle-9", "ABC-9", OperationCreate, 0, 0)); err != nil {
		t.Fatal(err)
	}
	plan, err = PlanMigration(testDefinition(), custom007V2(t), custom007Options())
	if err != nil {
		t.Fatal(err)
	}
	migrated, err := MigrateRecord(plan, testRecord(t, "vehicle-9", "ABC-9", true))
	if err != nil {
		t.Fatal(err)
	}
	stale := testMutation(t, tenant, "vehicle-9", "ABC-9", OperationChange, 0, 1)
	stale.Definition = custom007V2(t)
	stale.Record = migrated
	if _, err := store.Commit(ctx, stale); !errors.Is(err, ErrStaleHead) {
		t.Fatalf("stale cutover err=%v", err)
	}
	if len(store.Events(tenant, "Vehicle", "vehicle-9")) != 1 || len(store.Outbox()) != 1 {
		t.Fatal("refused cutover partially committed")
	}
}
