package agentstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_AGENT_046(t *testing.T) {
	ctx := context.Background()
	source := pgtest.NewEmpty(t)
	target := pgtest.NewEmpty(t)
	if err := Migrate(ctx, source.SQL); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, target.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	source.Exec(t, "INSERT INTO tenant(tenant_id) VALUES($1),($2)", tenant, other)
	source.Exec(t, `INSERT INTO agent_definition_version(tenant_id,definition_id,version,schema_version,digest,manifest) VALUES($1,'definition-a',1,1,$2,'{"restored":true}')`, tenant, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	source.Exec(t, `INSERT INTO agent_definition_head(tenant_id,definition_id,current_version,revision) VALUES($1,'definition-a',1,1)`, tenant)
	source.Exec(t, `INSERT INTO agent_schedule_state(tenant_id,schedule_id,revision,payload) VALUES($1,'schedule-a',1,'{"paused":false}'),($2,'schedule-other',1,'{"paused":true}')`, tenant, other)
	source.Exec(t, `INSERT INTO agent_schedule_outbox(tenant_id,source_key,schedule_id,request_digest,enqueued_at,payload) VALUES($1,'firing-a','schedule-a','exact-request',now(),'{"occurrence":"one"}')`, tenant)
	source.Exec(t, `INSERT INTO agent_schedule_receipt(tenant_id,source_key,payload) VALUES($1,'firing-a','{"run_request_id":"acknowledged-run"}')`, tenant)
	now := time.Now().UTC()
	image, err := CaptureBackup(ctx, source.SQL, tenant, now)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := RestoreBackup(ctx, target.SQL, tenant, image, now.Add(time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Rows != 6 || evidence.RPO != time.Second || evidence.RTO <= 0 || evidence.RTO > time.Minute || evidence.SnapshotDigest != image.Digest {
		t.Fatalf("measured restore = %+v", evidence)
	}
	var receipt string
	if err := target.SQL.QueryRow(`SELECT payload->>'run_request_id' FROM agent_schedule_receipt WHERE tenant_id=$1 AND source_key='firing-a'`, tenant).Scan(&receipt); err != nil || receipt != "acknowledged-run" {
		t.Fatalf("lost acknowledged firing: %s %v", receipt, err)
	}
	recovered, err := CaptureBackup(ctx, target.SQL, tenant, now)
	if err != nil || recovered.Digest != image.Digest {
		t.Fatalf("restore image differs: %s %s %v", recovered.Digest, image.Digest, err)
	}
	var count int
	if err := target.SQL.QueryRow("SELECT count(*) FROM tenant WHERE tenant_id=$1", other).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign tenant restored: %d %v", count, err)
	}
	if _, err := RestoreBackup(ctx, target.SQL, tenant, image, now, time.Minute); !errors.Is(err, ErrRestoreOccupied) {
		t.Fatalf("occupied destination overwritten: %v", err)
	}
}

func TestTodo_AGENT_046_Security(t *testing.T) {
	ctx := context.Background()
	source := pgtest.NewEmpty(t)
	target := pgtest.NewEmpty(t)
	if err := Migrate(ctx, source.SQL); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, target.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	source.Exec(t, "INSERT INTO tenant(tenant_id) VALUES($1)", tenant)
	now := time.Now().UTC()
	image, err := CaptureBackup(ctx, source.SQL, tenant, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreBackup(ctx, target.SQL, uuid.New(), image, now, time.Minute); !errors.Is(err, ErrBackupInvalid) {
		t.Fatalf("foreign restore accepted: %v", err)
	}
	altered := image
	altered.Digest = "corrupt"
	if _, err := RestoreBackup(ctx, target.SQL, tenant, altered, now, time.Minute); !errors.Is(err, ErrBackupInvalid) {
		t.Fatalf("corrupt restore accepted: %v", err)
	}
	if _, err := RestoreBackup(ctx, target.SQL, tenant, image, now.Add(time.Hour), time.Minute); !errors.Is(err, ErrBackupInvalid) {
		t.Fatalf("RPO violation accepted: %v", err)
	}
	for i := range image.Tables {
		if image.Tables[i].Name == "tenant" {
			image.Tables[i].Rows[0] = []byte(`{"tenant_id":"` + uuid.NewString() + `"}`)
		}
	}
	image.Digest, _ = backupDigest(image)
	if _, err := RestoreBackup(ctx, target.SQL, tenant, image, now, time.Minute); !errors.Is(err, ErrBackupInvalid) {
		t.Fatalf("mixed image accepted: %v", err)
	}
	var count int
	if err := target.SQL.QueryRow("SELECT count(*) FROM tenant").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed restore left rows: %d %v", count, err)
	}
}

func TestTodo_AGENT_046_Fault(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := CaptureBackup(ctx, nil, uuid.New(), now); !errors.Is(err, ErrBackupInvalid) {
		t.Fatal(err)
	}
	db := pgtest.NewEmpty(t)
	if _, err := CaptureBackup(ctx, db.SQL, uuid.New(), now); !errors.Is(err, ErrSharedDatabase) {
		t.Fatalf("non-agent store accepted: %v", err)
	}
	if err := Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureBackup(ctx, db.SQL, uuid.New(), now); !errors.Is(err, ErrBackupInvalid) {
		t.Fatalf("nonexistent tenant backed up: %v", err)
	}
	if _, err := RestoreBackup(ctx, nil, uuid.New(), BackupImage{}, now, time.Minute); !errors.Is(err, ErrBackupInvalid) {
		t.Fatal(err)
	}
}

func TestTodo_AGENT_046_RestoreKnownEmptyLegacyPolicy(t *testing.T) {
	ctx := context.Background()
	source, target := pgtest.NewEmpty(t), pgtest.NewEmpty(t)
	if err := Migrate(ctx, source.SQL); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, target.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	source.Exec(t, "INSERT INTO tenant(tenant_id) VALUES($1)", tenant)
	// Reproduce the retained user database shape, solely in the test schema.
	source.Exec(t, "ALTER TABLE persona_model_route_policy DROP COLUMN policy_schema_version CASCADE")
	now := time.Now().UTC()
	image, err := CaptureBackup(ctx, source.SQL, tenant, now)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*BackupTable){
		"nonempty": func(table *BackupTable) {
			table.Rows = []json.RawMessage{json.RawMessage(`{"tenant_id":"` + tenant.String() + `"}`)}
		},
		"unknown type": func(table *BackupTable) {
			var columns map[string]string
			if err := json.Unmarshal([]byte(table.Columns), &columns); err != nil {
				t.Fatal(err)
			}
			columns["policy_version"] = "text"
			raw, _ := json.Marshal(columns)
			table.Columns = string(raw)
		},
	} {
		altered := image
		altered.Tables = append([]BackupTable(nil), image.Tables...)
		for i := range altered.Tables {
			if altered.Tables[i].Name == "persona_model_route_policy" {
				mutate(&altered.Tables[i])
			}
		}
		altered.Digest, _ = backupDigest(altered)
		if _, err := RestoreBackup(ctx, target.SQL, tenant, altered, now, time.Minute); !errors.Is(err, ErrBackupInvalid) {
			t.Fatalf("unproven legacy %s restored: %v", name, err)
		}
	}
	if evidence, err := RestoreBackup(ctx, target.SQL, tenant, image, now, time.Minute); err != nil || evidence.Rows != 1 {
		t.Fatalf("known empty legacy policy restore: %+v %v", evidence, err)
	}
	var count int
	if err := target.SQL.QueryRow("SELECT count(*) FROM persona_model_route_policy").Scan(&count); err != nil || count != 0 {
		t.Fatalf("restore synthesized policy facts: %d %v", count, err)
	}
}
