package application

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENT_046_Recovery(t *testing.T) {
	ctx := context.Background()
	source, target, core := pgtest.NewEmpty(t), pgtest.NewEmpty(t), pgtest.New(t)
	if err := agentstore.Migrate(ctx, source.SQL); err != nil {
		t.Fatal(err)
	}
	if err := agentstore.Migrate(ctx, target.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, principal := uuid.New(), uuid.New()
	key := values.TenantId("restore-company")
	core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'restore-company','cell-local','Restore','ACTIVE',now())`, tenant)
	core.Exec(t, `INSERT INTO principal(tenant_id,principal_id,kind,subject,assurance,authn_method) VALUES($1,$2,'SERVICE','restore-agent','AAL1','workload')`, tenant, principal)
	source.Exec(t, "INSERT INTO tenant(tenant_id) VALUES($1)", tenant)
	source.Exec(t, `INSERT INTO persona_versions(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest) VALUES($1,'persona-a',1,'agent-a:1','restore','Restore','{"owner":"owner-a"}','profile-a')`, tenant)
	source.Exec(t, `INSERT INTO persona_lifecycle_events(tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES($1,'publication-a','persona-a',1,'IN_REVIEW','PUBLISHED','restore fixture','reviewer-a',now(),'profile-a','review-a','reviewer-a','eval-a','profile-a','suite-a')`, tenant)
	source.Exec(t, `INSERT INTO persona_agent_principal_binding(tenant_id,persona_id,persona_version,principal_id,provisioned_at) VALUES($1,'persona-a',1,$2,now())`, tenant, principal)
	for _, item := range []struct {
		id      string
		version int
	}{{"retired-install", 1}, {"orphan-install", 99}} {
		source.Exec(t, `INSERT INTO persona_installations(tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,created_at,updated_at) VALUES($1,$2,'persona-a',$3,$2,'ONE_TO_ONE','installer','{"max_tier":"T1","allowed_data_classes":[],"always_private":true,"conversation_search_allowed":false,"allowed_channel_classes":["ONE_TO_ONE"],"allow_external_members":false,"allow_cross_company_members":false}','ACTIVE',now(),now())`, tenant, item.id, item.version)
	}
	now := time.Now().UTC()
	image, err := agentstore.CaptureBackup(ctx, source.SQL, tenant, now)
	if err != nil {
		t.Fatal(err)
	}
	core.Exec(t, `UPDATE principal SET lifecycle='REVOKED',revocation_epoch=revocation_epoch+1 WHERE tenant_id=$1 AND principal_id=$2`, tenant, principal)
	evidence, err := agentstore.RestoreBackup(ctx, target.SQL, tenant, image, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.RTO > time.Minute {
		t.Fatal("RTO exceeded")
	}
	agents := commonAgentOpenIntegrationStore(t, target)
	personas, err := agentpersonastore.New(agents, func(values.TenantId) uuid.UUID { return tenant })
	if err != nil {
		t.Fatal(err)
	}
	currentCore := appRoleConnForPopulation(t, core)
	changed, err := ReconcileRestoredAgentInstallations(ctx, agents, personas, currentCore, key, tenant, now)
	if err != nil || changed != 2 {
		t.Fatalf("reconciled = %d %v", changed, err)
	}
	scoped, err := personas.Scoped(key)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"retired-install": "AGENT_PRINCIPAL_RETIRED_AFTER_RESTORE", "orphan-install": agentpersonastore.MissingVersionAfterRestore} {
		item, err := scoped.GetInstallation(ctx, id)
		if err != nil || item.State != agentpersonastore.InstallationSuspended || item.SuspensionReason != want || item.Revision != 2 || item.RevocationEpoch != 2 {
			t.Fatalf("%s = %+v %v", id, item, err)
		}
	}
	if changed, err := ReconcileRestoredAgentInstallations(ctx, agents, personas, currentCore, key, tenant, now); err != nil || changed != 0 {
		t.Fatalf("replay reconciliation = %d %v", changed, err)
	}
	var count int
	if err := target.SQL.QueryRow("SELECT count(*) FROM persona_security_scope WHERE tenant_id=$1 AND state='REVOKED'", tenant).Scan(&count); err != nil || count != 2 {
		t.Fatalf("stale restored lease not fenced: %d %v", count, err)
	}
	var lifecycle string
	if err := core.SQL.QueryRow("SELECT lifecycle FROM principal WHERE tenant_id=$1 AND principal_id=$2", tenant, principal).Scan(&lifecycle); err != nil || lifecycle != "REVOKED" {
		t.Fatalf("core altered by restore: %s %v", lifecycle, err)
	}
}
