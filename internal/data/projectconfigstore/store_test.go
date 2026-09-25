package projectconfigstore

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	projectdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/pressly/goose/v3"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func configFixture() projectworkflow.Config { return StarterConfig() }

func configStoreFixture(t *testing.T) (*projectstore.Store, *Store, string, string, string) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	fsys, err := fs.Sub(projectstore.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db.SQL, fsys, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	projects, err := projectstore.New(context.Background(), projectstore.Config{DSN: db.URL, CoreDSN: "postgres://other:secret@127.0.0.1:5432/postgres?sslmode=disable", Schema: db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(projects.Close)
	store := New(projects)
	tenant, project := "tenant-"+strings.ReplaceAll(uuid.NewString(), "-", ""), "project-"+strings.ReplaceAll(uuid.NewString(), "-", "")
	err = projects.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES($1,$2,'owner','Board','UTC')`, tenant, project)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return projects, store, tenant, project, db.Schema
}

func TestTodo_PM_012_Integration(t *testing.T) {
	projects, store, tenant, project, _ := configStoreFixture(t)
	ctx := context.Background()
	if err := store.InitializeProject(ctx, tenant, project, "owner"); err != nil {
		t.Fatal(err)
	}
	draft, published, err := store.Get(ctx, tenant, project)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Revision != 1 || len(draft.Validation) != 0 || published == nil || published.Version != 1 || published.Digest != draft.Digest {
		t.Fatalf("starter config not active: draft=%+v published=%+v", draft, published)
	}
	invalid := configFixture()
	invalid.Statuses = append(invalid.Statuses, projectworkflow.Status{ID: "stranded", Name: "Stranded", Category: projectworkflow.CategoryBlocked})
	saved, err := store.SaveDraft(ctx, tenant, project, "owner", 1, invalid, "draft-invalid")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 2 || len(saved.Validation) == 0 {
		t.Fatalf("invalid draft not retained with validation: %+v", saved)
	}
	replayed, err := store.SaveDraft(ctx, tenant, project, "owner", 1, invalid, "draft-invalid")
	if err != nil || replayed.Revision != saved.Revision {
		t.Fatalf("idempotent replay: %+v err=%v", replayed, err)
	}
	if _, err := store.SaveDraft(ctx, tenant, project, "owner", 1, configFixture(), "draft-stale"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision error=%v", err)
	}
	valid, err := store.SaveDraft(ctx, tenant, project, "owner", 2, configFixture(), "draft-valid")
	if err != nil || valid.Revision != 3 || len(valid.Validation) != 0 {
		t.Fatalf("valid save: %+v err=%v", valid, err)
	}
	if _, err := store.Publish(ctx, tenant, project, "owner", valid.Revision, 1, valid.Digest, "publish-missing-review", ReviewEvidence{Required: true, ReviewerID: "owner", DecisionID: "same-actor", ReviewedDigest: valid.Digest}); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("self review accepted: %v", err)
	}
	if _, err := store.Publish(ctx, tenant, project, "owner", valid.Revision, 1, strings.Repeat("0", 64), "publish-wrong-digest", ReviewEvidence{}); !errors.Is(err, ErrDigestConflict) {
		t.Fatalf("wrong digest accepted: %v", err)
	}
	evidence := ReviewEvidence{Required: true, ReviewerID: "reviewer", DecisionID: "decision-1", ReviewedDigest: valid.Digest, PolicyReference: "tenant-policy"}
	active, err := store.Publish(ctx, tenant, project, "owner", valid.Revision, 1, valid.Digest, "publish-valid", evidence)
	if err != nil {
		t.Fatal(err)
	}
	if active.Version != 2 || active.SourceRevision != valid.Revision || active.Digest != valid.Digest || active.ReviewerID != "reviewer" {
		t.Fatalf("published snapshot mismatch: %+v", active)
	}
	replayPublish, err := store.Publish(ctx, tenant, project, "owner", valid.Revision, 1, valid.Digest, "publish-valid", evidence)
	if err != nil || replayPublish.Version != active.Version {
		t.Fatalf("publish replay: %+v err=%v", replayPublish, err)
	}
	if _, err := store.Publish(ctx, tenant, project, "owner", valid.Revision, 1, valid.Digest, "publish-stale-active", evidence); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale active configuration version accepted: %v", err)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_workflow_version WHERE tenant_id=$1 AND project_id=$2`, tenant, project).Scan(&count); err != nil {
			return err
		}
		if count != 2 {
			t.Fatalf("immutable publication history count=%d", count)
		}
		var reviewer, digest string
		if err := tx.QueryRow(ctx, `SELECT reviewer_id,digest FROM project_workflow_version WHERE tenant_id=$1 AND project_id=$2 AND version=2`, tenant, project).Scan(&reviewer, &digest); err != nil {
			return err
		}
		if reviewer != "reviewer" || digest != valid.Digest {
			t.Fatalf("review evidence lost: reviewer=%q digest=%q", reviewer, digest)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveDraft(ctx, tenant, project, "owner", 99, configFixture(), "draft-tenant-bait"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("unexpected revision behavior: %v", err)
	}
}

func TestTodo_PM_012_WorkflowFieldsRoundTrip(t *testing.T) {
	_, store, tenant, project, _ := configStoreFixture(t)
	ctx := context.Background()
	if err := store.InitializeProject(ctx, tenant, project, "owner"); err != nil {
		t.Fatal(err)
	}
	config := StarterConfig()
	config.Fields = []projectworkflow.Field{{ID: "owner", Name: "Owner", Type: projectworkflow.FieldPerson, Required: true, Classification: "INTERNAL"}}
	config.TaskTypes[0].FieldIDs = []string{"owner"}
	config.TaskTypes[0].RequiredFields = []string{"owner"}
	config.Statuses[0].AllowedNextStatusIDs = []string{"doing"}
	config.Statuses[1].AllowedNextStatusIDs = []string{"done"}
	config.Statuses[1].RequiredFieldIDs = []string{"owner"}
	config.Transitions[0].RequiredFields = []string{"owner"}
	draft, err := store.SaveDraft(ctx, tenant, project, "owner", 1, config, "round-trip-save")
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Validation) != 0 {
		t.Fatalf("expanded valid configuration rejected: %#v", draft.Validation)
	}
	got, current, err := store.Get(ctx, tenant, project)
	if err != nil {
		t.Fatal(err)
	}
	assertWorkflowFields(t, got.Config)
	if current == nil {
		t.Fatal("starter published version missing")
	}
	if _, err := store.Publish(ctx, tenant, project, "owner", draft.Revision, current.Version, draft.Digest, "round-trip-publish", ReviewEvidence{}); err != nil {
		t.Fatal(err)
	}
	published, err := store.GetPublished(ctx, tenant, project)
	if err != nil {
		t.Fatal(err)
	}
	assertWorkflowFields(t, published.Config)
}

func assertWorkflowFields(t *testing.T, config projectworkflow.Config) {
	t.Helper()
	if len(config.TaskTypes) != 1 || len(config.TaskTypes[0].FieldIDs) != 1 || config.TaskTypes[0].FieldIDs[0] != "owner" {
		t.Fatalf("task type field IDs lost: %#v", config.TaskTypes)
	}
	if len(config.Fields) != 1 || !config.Fields[0].Required {
		t.Fatalf("required field metadata lost: %#v", config.Fields)
	}
	if len(config.Statuses[0].AllowedNextStatusIDs) != 1 || config.Statuses[0].AllowedNextStatusIDs[0] != "doing" || len(config.Statuses[1].AllowedNextStatusIDs) != 1 || config.Statuses[1].AllowedNextStatusIDs[0] != "done" {
		t.Fatalf("status transition graph lost: %#v", config.Statuses)
	}
	if len(config.Statuses[1].RequiredFieldIDs) != 1 || config.Statuses[1].RequiredFieldIDs[0] != "owner" || len(config.Transitions[0].RequiredFields) != 1 || config.Transitions[0].RequiredFields[0] != "owner" {
		t.Fatalf("transition requirements lost: statuses=%#v transitions=%#v", config.Statuses, config.Transitions)
	}
}

func TestTodo_PM_013_PublishMigrationRecheck(t *testing.T) {
	projects, store, tenant, project, _ := configStoreFixture(t)
	ctx := context.Background()
	if err := store.InitializeProject(ctx, tenant, project, "owner"); err != nil {
		t.Fatal(err)
	}
	active, err := store.GetPublished(ctx, tenant, project)
	if err != nil {
		t.Fatal(err)
	}
	labelOnly := active.Config
	labelOnly.Statuses = append([]projectworkflow.Status(nil), active.Config.Statuses...)
	labelOnly.Statuses[0].Name = "Queue"
	draft, err := store.SaveDraft(ctx, tenant, project, "owner", 1, labelOnly, "label-only-draft")
	if err != nil {
		t.Fatal(err)
	}
	published, err := store.Publish(ctx, tenant, project, "owner", draft.Revision, active.Version, draft.Digest, "label-only-publish", ReviewEvidence{})
	if err != nil || published.Version != active.Version+1 {
		t.Fatalf("non-impacting publish version=%d err=%v", published.Version, err)
	}

	affectedConfig := published.Config
	affectedConfig.Statuses = append([]projectworkflow.Status(nil), published.Config.Statuses...)
	affectedConfig.Statuses[0].Category = projectworkflow.CategoryActive
	draft, err = store.SaveDraft(ctx, tenant, project, "owner", draft.Revision, affectedConfig, "affected-draft")
	if err != nil {
		t.Fatal(err)
	}
	precheck, err := projectworkflow.PreviewMigration(projectworkflow.MigrationRequest{Current: published.Config, Pending: affectedConfig, Tasks: nil})
	if err != nil || !precheck.Safe || precheck.AffectedTaskCount != 0 {
		t.Fatalf("empty-task application preview was not non-impacting: %+v err=%v", precheck, err)
	}
	if err := projects.CreateTaskWithConfig(ctx, projectstore.TaskRecord{ID: "arrived-after-preview", TenantID: tenant, ProjectID: project, Title: "New work", StatusID: "todo", TypeID: "task_default", Fields: json.RawMessage(`{}`)}, published.Version, "owner", "HUMAN", "create-after-preview"); err != nil {
		t.Fatal(err)
	}
	migrated, err := store.Publish(ctx, tenant, project, "owner", draft.Revision, published.Version, draft.Digest, "migrate-raced-impact", ReviewEvidence{})
	if err != nil || migrated.Version != published.Version+1 {
		t.Fatalf("transaction-time affected task was not migrated with publication: version=%d err=%v", migrated.Version, err)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var status string
		var revision int64
		if err := tx.QueryRow(ctx, `SELECT status_id,revision FROM project_task WHERE tenant_id=$1 AND project_id=$2 AND id='arrived-after-preview'`, tenant, project).Scan(&status, &revision); err != nil {
			return err
		}
		if status != "todo" || revision != 2 {
			t.Fatalf("transaction-time task migration status=%q revision=%d", status, revision)
		}
		var eventCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id='arrived-after-preview' AND event_type='task.workflow_migrated' AND config_version=$3`, tenant, project, migrated.Version).Scan(&eventCount); err != nil {
			return err
		}
		if eventCount != 1 {
			t.Fatalf("task migration activity count=%d", eventCount)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,type_id,priority,archived)
			SELECT $1,'extra-'||g::text,$2,'Overflow','todo','task_default','NORMAL',false FROM generate_series(1,$3) g`, tenant, project, projectworkflow.MaxAffectedTasksPerPublish-1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetPublished(ctx, tenant, project)
	if err != nil {
		t.Fatal(err)
	}
	secondConfig := current.Config
	secondConfig.Statuses = append([]projectworkflow.Status(nil), current.Config.Statuses...)
	secondConfig.Statuses[0].Category = projectworkflow.CategoryDone
	secondDraft, err := store.SaveDraft(ctx, tenant, project, "owner", draft.Revision, secondConfig, "second-1000-draft")
	if err != nil {
		t.Fatal(err)
	}
	secondPublish, err := store.Publish(ctx, tenant, project, "owner", secondDraft.Revision, current.Version, secondDraft.Digest, "migrate-exact-limit", ReviewEvidence{})
	if err != nil || secondPublish.Version != current.Version+1 {
		t.Fatalf("exactly 1000 affected tasks should migrate atomically: version=%d err=%v", secondPublish.Version, err)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND event_type='task.workflow_migrated' AND config_version=$3`, tenant, project, secondPublish.Version).Scan(&count); err != nil {
			return err
		}
		if count != projectworkflow.MaxAffectedTasksPerPublish {
			t.Fatalf("exact-limit migration activity count=%d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,type_id,priority,archived) VALUES($1,'extra-last',$2,'Overflow','todo','task_default','NORMAL',false)`, tenant, project)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	thirdConfig := secondPublish.Config
	thirdConfig.Statuses = append([]projectworkflow.Status(nil), secondPublish.Config.Statuses...)
	thirdConfig.Statuses[0].Category = projectworkflow.CategoryBlocked
	thirdDraft, err := store.SaveDraft(ctx, tenant, project, "owner", secondDraft.Revision, thirdConfig, "third-1001-draft")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Publish(ctx, tenant, project, "owner", thirdDraft.Revision, secondPublish.Version, thirdDraft.Digest, "reject-overflow", ReviewEvidence{})
	if !errors.Is(err, ErrMigrationRequired) || !errors.Is(err, projectworkflow.ErrMigrationLimitExceeded) {
		t.Fatalf("affected task cap not enforced by planner: %v", err)
	}
	stillActive, err := store.GetPublished(ctx, tenant, project)
	if err != nil || stillActive.Version != secondPublish.Version {
		t.Fatalf("affected-task rejection changed active pointer: version=%d err=%v", stillActive.Version, err)
	}

	bulkProject := "project-bulk-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES($1,$2,'owner','Bulk','UTC')`, tenant, bulkProject)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeProject(ctx, tenant, bulkProject, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,type_id,priority,archived)
			SELECT $1,'bulk-'||g::text,$2,'Bulk task','todo','task_default','NORMAL',false FROM generate_series(1,$3) g`, tenant, bulkProject, projectworkflow.MaxAffectedTasksPerPublish+1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	bulkCurrent, err := store.GetPublished(ctx, tenant, bulkProject)
	if err != nil {
		t.Fatal(err)
	}
	bulkConfig := bulkCurrent.Config
	bulkConfig.Statuses = append([]projectworkflow.Status(nil), bulkCurrent.Config.Statuses...)
	bulkConfig.Statuses[0].Name = "Queue"
	bulkDraft, err := store.SaveDraft(ctx, tenant, bulkProject, "owner", 1, bulkConfig, "bulk-small-impact-draft")
	if err != nil {
		t.Fatal(err)
	}
	bulkPublished, err := store.Publish(ctx, tenant, bulkProject, "owner", bulkDraft.Revision, bulkCurrent.Version, bulkDraft.Digest, "bulk-small-impact-publish", ReviewEvidence{})
	if err != nil || bulkPublished.Version != bulkCurrent.Version+1 {
		t.Fatalf("1001 unaffected tasks blocked publish: version=%d err=%v", bulkPublished.Version, err)
	}

	bulkConfig = bulkPublished.Config
	bulkConfig.Statuses = append([]projectworkflow.Status(nil), bulkPublished.Config.Statuses...)
	bulkConfig.Statuses[0].Name = "Queue v3"
	bulkDraft, err = store.SaveDraft(ctx, tenant, bulkProject, "owner", bulkDraft.Revision, bulkConfig, "bulk-overflow-draft")
	if err != nil {
		t.Fatal(err)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,type_id,priority,archived)
			SELECT $1,'bulk-'||g::text,$2,'Bulk task','todo','task_default','NORMAL',false FROM generate_series($3::int,$4::int) g`, tenant, bulkProject, projectworkflow.MaxAffectedTasksPerPublish+2, projectworkflow.MaxTaskSnapshotsPerPreview+1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, err = store.Publish(ctx, tenant, bulkProject, "owner", bulkDraft.Revision, bulkPublished.Version, bulkDraft.Digest, "reject-snapshot-overflow", ReviewEvidence{})
	if !errors.Is(err, ErrMigrationRequired) || !errors.Is(err, projectworkflow.ErrSnapshotLimitExceeded) {
		t.Fatalf("10,001 task snapshot overflow not rejected: %v", err)
	}
	bulkStillActive, err := store.GetPublished(ctx, tenant, bulkProject)
	if err != nil || bulkStillActive.Version != bulkPublished.Version {
		t.Fatalf("snapshot overflow changed active pointer: version=%d err=%v", bulkStillActive.Version, err)
	}
}

func TestTodo_PM_014_PublishSnapshotUsesPreviewFieldValues(t *testing.T) {
	projects, _, tenant, project, _ := configStoreFixture(t)
	ctx := context.Background()
	stored, err := json.Marshal(map[string]projectdomain.TaskFieldEdit{
		"region": {FieldID: "region", Type: "TEXT", CanonicalValue: "west"},
		"ticket": {FieldID: "ticket", Type: "TEXT", CanonicalValue: "123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project_task(tenant_id,id,project_id,title,status_id,type_id,priority,fields_json) VALUES($1,'field-task',$2,'Field task','todo','task_default','NORMAL',$3::jsonb)`, tenant, project, stored)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := projects.ActiveTaskSnapshots(ctx, tenant, project)
	if err != nil {
		t.Fatal(err)
	}
	var publish []projectworkflow.TaskSnapshot
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var err error
		publish, err = loadActiveTaskSnapshots(ctx, tx, tenant, project)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preview, publish) || len(publish) != 1 || string(publish[0].Fields["region"]) != `"west"` || string(publish[0].Fields["ticket"]) != `"123"` {
		t.Fatalf("publish and preview field snapshots differ: preview=%+v publish=%+v", preview, publish)
	}
}

func TestTodo_PM_015_PublishMigratesDefaultAndRollsBackAtomically(t *testing.T) {
	projects, store, tenant, project, _ := configStoreFixture(t)
	ctx := context.Background()
	if err := store.InitializeProject(ctx, tenant, project, "owner"); err != nil {
		t.Fatal(err)
	}
	current, err := store.GetPublished(ctx, tenant, project)
	if err != nil {
		t.Fatal(err)
	}
	if err := projects.CreateTaskWithConfig(ctx, projectstore.TaskRecord{ID: "defaulted-task", TenantID: tenant, ProjectID: project, Title: "Needs owner", StatusID: "todo", TypeID: "task_default", Fields: json.RawMessage(`{}`)}, current.Version, "owner", "HUMAN", "create-defaulted-task"); err != nil {
		t.Fatal(err)
	}
	pending := current.Config
	pending.Fields = []projectworkflow.Field{{ID: "owner", Name: "Owner", Type: projectworkflow.FieldText, Required: true, Classification: "INTERNAL", Default: json.RawMessage(`"triage"`)}}
	pending.TaskTypes = append([]projectworkflow.TaskType(nil), current.Config.TaskTypes...)
	pending.TaskTypes[0].FieldIDs = []string{"owner"}
	pending.TaskTypes[0].RequiredFields = []string{"owner"}
	draft, err := store.SaveDraft(ctx, tenant, project, "owner", 1, pending, "defaulted-task-draft")
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Validation) != 0 {
		t.Fatalf("pending config invalid: %#v", draft.Validation)
	}

	// An outbox insertion fault must roll back task mutation, activity, config
	// activation, and the idempotency claim as one project transaction.
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE FUNCTION fail_project_migration_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='task.workflow_migrated' THEN RAISE EXCEPTION 'injected migration outbox fault'; END IF; RETURN NEW; END $$`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `CREATE TRIGGER fail_project_migration_outbox BEFORE INSERT ON project_outbox FOR EACH ROW EXECUTE FUNCTION fail_project_migration_outbox()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, tenant, project, "owner", draft.Revision, current.Version, draft.Digest, "atomic-migration-publish", ReviewEvidence{}); err == nil {
		t.Fatal("injected outbox failure did not abort publication")
	}
	unchanged, err := projects.GetTask(ctx, tenant, project, "defaulted-task")
	if err != nil || unchanged.Revision != 1 || string(unchanged.Fields) != `{}` {
		t.Fatalf("failed publication partially migrated task: task=%+v err=%v", unchanged, err)
	}
	stillCurrent, err := store.GetPublished(ctx, tenant, project)
	if err != nil || stillCurrent.Version != current.Version {
		t.Fatalf("failed publication activated workflow: current=%+v err=%v", stillCurrent, err)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DROP TRIGGER fail_project_migration_outbox ON project_outbox`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DROP FUNCTION fail_project_migration_outbox()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	published, err := store.Publish(ctx, tenant, project, "owner", draft.Revision, current.Version, draft.Digest, "atomic-migration-publish", ReviewEvidence{})
	if err != nil || published.Version != current.Version+1 {
		t.Fatalf("retry after rollback did not publish: published=%+v err=%v", published, err)
	}
	task, err := projects.GetTask(ctx, tenant, project, "defaulted-task")
	if err != nil || task.Revision != 2 {
		t.Fatalf("task revision after migration=%d err=%v", task.Revision, err)
	}
	var fields map[string]projectdomain.TaskFieldEdit
	if err := json.Unmarshal(task.Fields, &fields); err != nil {
		t.Fatal(err)
	}
	if owner := fields["owner"]; owner.FieldID != "owner" || owner.Type != string(projectworkflow.FieldText) || owner.CanonicalValue != "triage" {
		t.Fatalf("planner default was not persisted: %+v", fields)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id='defaulted-task' AND event_type='task.workflow_migrated' AND config_version=$3`, tenant, project, published.Version).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("migration activity count=%d", count)
		}
		var payload string
		if err := tx.QueryRow(ctx, `SELECT payload::text FROM project_activity WHERE tenant_id=$1 AND project_id=$2 AND aggregate_id='defaulted-task' AND event_type='task.workflow_migrated'`, tenant, project).Scan(&payload); err != nil {
			return err
		}
		if strings.Contains(payload, "triage") {
			t.Fatalf("migration event leaked field value: %s", payload)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if replay, err := store.Publish(ctx, tenant, project, "owner", draft.Revision, current.Version, draft.Digest, "atomic-migration-publish", ReviewEvidence{}); err != nil || replay.Version != published.Version {
		t.Fatalf("idempotent publish replay=%+v err=%v", replay, err)
	}
	task, err = projects.GetTask(ctx, tenant, project, "defaulted-task")
	if err != nil || task.Revision != 2 {
		t.Fatalf("publish replay repeated task migration: revision=%d err=%v", task.Revision, err)
	}
}

func TestTodo_PM_014_PublishAppliesReviewedStatusAndFieldMappings(t *testing.T) {
	projects, store, tenant, project, _ := configStoreFixture(t)
	ctx := context.Background()
	if err := store.InitializeProject(ctx, tenant, project, "owner"); err != nil {
		t.Fatal(err)
	}
	base, err := store.GetPublished(ctx, tenant, project)
	if err != nil {
		t.Fatal(err)
	}
	baseConfig := base.Config
	baseConfig.Fields = []projectworkflow.Field{{ID: "owner", Name: "Owner", Type: projectworkflow.FieldText, Classification: "INTERNAL"}}
	baseConfig.TaskTypes = append([]projectworkflow.TaskType(nil), base.Config.TaskTypes...)
	baseConfig.TaskTypes[0].FieldIDs = []string{"owner"}
	baseDraft, err := store.SaveDraft(ctx, tenant, project, "owner", 1, baseConfig, "mapped-base-draft")
	if err != nil {
		t.Fatal(err)
	}
	basePublished, err := store.Publish(ctx, tenant, project, "owner", baseDraft.Revision, base.Version, baseDraft.Digest, "mapped-base-publish", ReviewEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	fieldsJSON, err := json.Marshal(map[string]projectdomain.TaskFieldEdit{"owner": {FieldID: "owner", Type: string(projectworkflow.FieldText), CanonicalValue: "alice"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := projects.CreateTaskWithConfig(ctx, projectstore.TaskRecord{ID: "mapped-task", TenantID: tenant, ProjectID: project, Title: "Migrate me", StatusID: "todo", TypeID: "task_default", Fields: fieldsJSON}, basePublished.Version, "owner", "HUMAN", "mapped-task-create"); err != nil {
		t.Fatal(err)
	}

	pending := basePublished.Config
	pending.Statuses = append([]projectworkflow.Status(nil), basePublished.Config.Statuses...)
	pending.Statuses[0].ID = "queue"
	pending.Statuses[0].AllowedNextStatusIDs = []string{"doing"}
	pending.Transitions = append([]projectworkflow.Transition(nil), basePublished.Config.Transitions...)
	pending.Transitions[0].From = "queue"
	pending.Columns = append([]projectworkflow.Column(nil), basePublished.Config.Columns...)
	pending.Columns[0].ID = "queue"
	pending.Columns[0].StatusIDs = []string{"queue"}
	pending.TaskTypes = append([]projectworkflow.TaskType(nil), basePublished.Config.TaskTypes...)
	pending.TaskTypes[0].InitialStatus = "queue"
	pending.TaskTypes[0].FieldIDs = []string{"owner_v2"}
	pending.Fields = []projectworkflow.Field{{ID: "owner_v2", Name: "Owner", Type: projectworkflow.FieldEnum, Classification: "INTERNAL", Validation: projectworkflow.FieldValidation{Options: []string{"alice", "bob"}}}}
	draft, err := store.SaveDraft(ctx, tenant, project, "owner", baseDraft.Revision, pending, "mapped-pending-draft")
	if err != nil {
		t.Fatal(err)
	}
	mappings := projectworkflow.MigrationMappings{Statuses: map[string]string{"todo": "queue"}, Fields: map[string]string{"owner": "owner_v2"}}
	previewPlan := func(m projectworkflow.MigrationMappings) (string, error) {
		current, err := store.GetPublished(ctx, tenant, project)
		if err != nil {
			return "", err
		}
		activeTasks, err := projects.ActiveTaskSnapshots(ctx, tenant, project)
		if err != nil {
			return "", err
		}
		sealedPending, err := projectworkflow.Publish(draft.Config, 1)
		if err != nil {
			return "", err
		}
		preview, err := projectworkflow.PreviewMigration(projectworkflow.MigrationRequest{Current: current.Config, Pending: draft.Config, Tasks: activeTasks, StatusMappings: m.Statuses, FieldMappings: m.Fields})
		if err != nil || !preview.Safe {
			return "", errors.Join(projectworkflow.ErrUnsafeMigration, err)
		}
		return projectworkflow.DigestMigrationPlan(current.Digest, sealedPending.Digest(), uint64(current.Version), uint64(draft.Revision), m, preview, activeTasks), nil
	}

	firstPlan, err := previewPlan(mappings)
	if err != nil {
		t.Fatal(err)
	}
	// Revise the task through the normal writer after review. The plan digest
	// includes task revision/status fences, so the stale review cannot publish.
	title := "Updated after review"
	if _, err := projects.PatchTask(ctx, tenant, project, "mapped-task", 1, basePublished.Version, projectstore.TaskPatch{Title: &title}, "owner", "HUMAN", "mapped-task-patch"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishWithMigration(ctx, tenant, project, "owner", draft.Revision, basePublished.Version, draft.Digest, firstPlan, "mapped-publish", ReviewEvidence{}, mappings); !errors.Is(err, ErrMigrationPlanDigestConflict) {
		t.Fatalf("changed task snapshot did not invalidate reviewed plan: %v", err)
	}
	current, err := store.GetPublished(ctx, tenant, project)
	if err != nil || current.Version != basePublished.Version {
		t.Fatalf("stale plan changed current workflow: current=%+v err=%v", current, err)
	}

	planDigest, err := previewPlan(mappings)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishWithMigration(ctx, tenant, project, "owner", draft.Revision, basePublished.Version, draft.Digest, planDigest, "missing-plan-review", ReviewEvidence{Required: true, ReviewerID: "reviewer", DecisionID: "decision", ReviewedDigest: draft.Digest, ReviewedPlanDigest: "stale"}, mappings); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("review evidence for another plan was accepted: %v", err)
	}
	wrongMappings := projectworkflow.MigrationMappings{Statuses: map[string]string{"todo": "doing"}, Fields: map[string]string{"owner": "owner_v2"}}
	if _, err := store.PublishWithMigration(ctx, tenant, project, "owner", draft.Revision, basePublished.Version, draft.Digest, planDigest, "mapped-publish", ReviewEvidence{}, wrongMappings); !errors.Is(err, ErrMigrationPlanDigestConflict) {
		t.Fatalf("mapping change did not invalidate reviewed plan: %v", err)
	}
	evidence := ReviewEvidence{Required: true, ReviewerID: "reviewer", DecisionID: "decision", ReviewedDigest: draft.Digest, ReviewedPlanDigest: planDigest}
	published, err := store.PublishWithMigration(ctx, tenant, project, "owner", draft.Revision, basePublished.Version, draft.Digest, planDigest, "mapped-publish", evidence, mappings)
	if err != nil || published.Version != basePublished.Version+1 {
		t.Fatalf("reviewed mapped workflow publication: published=%+v err=%v", published, err)
	}
	task, err := projects.GetTask(ctx, tenant, project, "mapped-task")
	if err != nil || task.StatusID != "queue" || task.Revision != 3 {
		t.Fatalf("status mapping migration task=%+v err=%v", task, err)
	}
	var migratedFields map[string]projectdomain.TaskFieldEdit
	if err := json.Unmarshal(task.Fields, &migratedFields); err != nil {
		t.Fatal(err)
	}
	if len(migratedFields) != 1 || migratedFields["owner_v2"].Type != string(projectworkflow.FieldEnum) || migratedFields["owner_v2"].CanonicalValue != "alice" {
		t.Fatalf("field mapping migration fields=%+v", migratedFields)
	}
	if replay, err := store.PublishWithMigration(ctx, tenant, project, "owner", draft.Revision, basePublished.Version, draft.Digest, planDigest, "mapped-publish", evidence, mappings); err != nil || replay.Version != published.Version {
		t.Fatalf("mapped publish replay=%+v err=%v", replay, err)
	}
	task, err = projects.GetTask(ctx, tenant, project, "mapped-task")
	if err != nil || task.Revision != 3 {
		t.Fatalf("mapped publish replay repeated task migration: task=%+v err=%v", task, err)
	}
}

func TestTodo_PM_013_Security(t *testing.T) {
	projects, store, tenant, project, schema := configStoreFixture(t)
	ctx := context.Background()
	if err := store.InitializeProject(ctx, tenant, project, "owner"); err != nil {
		t.Fatal(err)
	}
	otherTenant, otherProject := "tenant-"+strings.ReplaceAll(uuid.NewString(), "-", ""), "project-"+strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := projects.RunTenantTx(ctx, otherTenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,owner_id,name,project_timezone) VALUES($1,$2,'owner','Private','UTC')`, otherTenant, otherProject)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeProject(ctx, otherTenant, otherProject, "owner"); err != nil {
		t.Fatal(err)
	}
	role := "project_config_rls_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE ROLE `+`"`+role+`"`+` NOLOGIN NOSUPERUSER NOBYPASSRLS`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `GRANT USAGE ON SCHEMA `+`"`+schema+`"`+` TO `+`"`+role+`"`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA `+`"`+schema+`"`+` TO `+`"`+role+`"`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA `+`"`+schema+`"`+` TO `+`"`+role+`"`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
			if _, err := tx.Exec(ctx, `DROP OWNED BY `+`"`+role+`"`); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `DROP ROLE `+`"`+role+`"`)
			return err
		}); err != nil {
			t.Errorf("drop RLS test role: %v", err)
		}
	})
	if err := projects.RunTx(ctx, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+`"`+role+`"`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('hcmnext.tenant_id',$1,true)`, tenant); err != nil {
			return err
		}
		var visible int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_workflow_version WHERE project_id=$1`, otherProject).Scan(&visible); err != nil {
			return err
		}
		if visible != 0 {
			t.Errorf("tenant scope exposed %d foreign workflow versions", visible)
		}
		_, err := tx.Exec(ctx, `UPDATE project_workflow_version SET digest=repeat('0',64) WHERE tenant_id=$1 AND project_id=$2`, tenant, project)
		return err
	}); err == nil {
		t.Fatal("non-owner changed append-only published workflow version")
	}
	otherDraft, _, err := store.Get(ctx, "different-tenant", project)
	if !errors.Is(err, ErrNotFound) || otherDraft.ProjectID != "" {
		t.Fatalf("cross-tenant read leaked: %+v err=%v", otherDraft, err)
	}
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var enabled, forced bool
		if err := tx.QueryRow(ctx, `SELECT c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname='project_workflow_version'`).Scan(&enabled, &forced); err != nil {
			return err
		}
		if !enabled || !forced {
			t.Errorf("published config RLS enabled=%t forced=%t", enabled, forced)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := projects.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT config_json FROM project_workflow_version WHERE tenant_id=$1 AND project_id=$2`, tenant, project).Scan(&stored)
	}); err != nil {
		t.Fatal(err)
	}
	var decoded projectworkflow.Config
	if err := json.Unmarshal(stored, &decoded); err != nil || len(projectworkflow.Validate(decoded)) != 0 {
		t.Fatalf("starter persisted config invalid: %v", err)
	}
}
