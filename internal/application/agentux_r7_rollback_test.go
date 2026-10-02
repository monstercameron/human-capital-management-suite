package application

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"testing"
)

// Publication decisions are deterministic fixtures; rollout persistence and installation fences use PostgreSQL.
type agentUXR7RolloutTenant struct{ *agentpersonastore.TenantStore }

func (s agentUXR7RolloutTenant) ResolvePublicationEvidence(context.Context, string, int64) (agentpersonastore.PublicationEvidence, error) {
	return agentpersonastore.PublicationEvidence{ReviewID: "review", EvaluationRunID: "evaluation"}, nil
}

type agentUXR7RolloutStore struct {
	tenant values.TenantId
	scoped agentUXR7RolloutTenant
}

func (s agentUXR7RolloutStore) ForRolloutTenant(_ context.Context, tenant values.TenantId) (AgentVersionRolloutTenant, error) {
	if tenant != s.tenant {
		return nil, ErrAgentVersionRolloutDenied
	}
	return s.scoped, nil
}

func TestAgentUXR7_L22_RollbackIntegration(t *testing.T) {
	ctx, service, fixture, _ := versionRolloutFixture(t)
	tenant := fixture.row.TenantID
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantUUID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1)`, tenantUUID)
	mapper := func(key values.TenantId) uuid.UUID {
		if key == tenant {
			return tenantUUID
		}
		return uuid.Nil
	}
	store, err := agentpersonastore.NewWithPublicationAuthorities(db.NewConn(t), mapper, agentUXR7ReviewSource{}, agentUXR7EvaluationSource{})
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := store.ForTenant(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	target := fixture.row
	target.Version = 1
	var profile map[string]any
	if err = json.Unmarshal(target.Profile, &profile); err != nil {
		t.Fatal(err)
	}
	profile["version"] = float64(1)
	// The governed fixture rebuilds the same sealed profile for version 1.
	target.Profile, _ = json.Marshal(profile)
	var typedProfile agentpersona.PersonaProfile
	if err = json.Unmarshal(target.Profile, &typedProfile); err != nil {
		t.Fatal(err)
	}
	sealed, err := agentpersona.Seal(typedProfile)
	if err != nil {
		t.Fatal(err)
	}
	target.ContentDigest = sealed.Digest
	db.Exec(t, `INSERT INTO persona_versions (tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at) VALUES ($1,'persona',1,'agent-v1','assistant','Assistant',$2::jsonb,$3,now())`, tenantUUID, string(target.Profile), target.ContentDigest)
	db.Exec(t, `INSERT INTO persona_owners (tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES ($1,'persona','BUSINESS_OWNER','owner','admin',now())`, tenantUUID)
	db.Exec(t, `INSERT INTO persona_lifecycle_events (tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES ($1,'publish-1','persona',1,'IN_REVIEW','PUBLISHED','reviewed','reviewer',now(),$2,'review-proof','reviewer','evaluation-proof',$2,'suite-proof')`, tenantUUID, target.ContentDigest)
	policy, _ := json.Marshal(fixture.installations["install-a"].ChannelPolicy)
	for _, id := range []string{"install-a", "install-b"} {
		db.Exec(t, `INSERT INTO persona_installations (tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES ($1,$2,'persona',2,'room','PRIVATE','admin',$3::jsonb,'ACTIVE',1,1,now(),now())`, tenantUUID, id, string(policy))
	}
	service.Store = agentUXR7RolloutStore{tenant: tenant, scoped: agentUXR7RolloutTenant{scoped}}
	service.Governed.Versions = personaAdminVersionFake{target}
	command := versionRolloutPreview()
	command.TargetVersion = 1
	command.InstallationIDs = []string{"install-a"}
	command.CanaryIDs = []string{"install-a"}
	receipt, err := service.Execute(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	before, err := scoped.GetInstallation(ctx, "install-a")
	if err != nil || before.PersonaVersion != 2 {
		t.Fatalf("preview changed actual version: %+v %v", before, err)
	}
	receipt, err = service.Execute(ctx, versionRolloutAction("APPROVE", receipt))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = service.Execute(ctx, versionRolloutAction("ADVANCE", receipt))
	if err != nil || receipt.Progress.Stage != "COMPLETE" {
		t.Fatalf("rollback %+v %v", receipt.Progress, err)
	}
	fresh, err := store.ForTenant(ctx, tenant)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fresh.GetInstallation(ctx, "install-a")
	if err != nil || got.PersonaVersion != 1 || got.RevocationEpoch != 2 || got.Revision != 2 {
		t.Fatalf("persisted rollback %+v %v", got, err)
	}
	other, err := fresh.GetInstallation(ctx, "install-b")
	if err != nil || other.PersonaVersion != 2 {
		t.Fatalf("unselected conversation changed %+v %v", other, err)
	}
	if _, err = service.Store.ForRolloutTenant(ctx, "other-tenant"); err == nil {
		t.Fatal("rollback store admitted another tenant")
	}
}

type agentUXR7ReviewSource struct{}

func (agentUXR7ReviewSource) ResolvePersonaReview(_ context.Context, _ dbport.Tx, tenant values.TenantId, persona string, version int64, digest, review string) (agentpersonastore.VerifiedReview, error) {
	return agentpersonastore.VerifiedReview{ReviewID: review, TenantID: string(tenant), PersonaID: persona, PersonaVersion: version, ProfileDigest: digest, ReviewDigest: "review-proof", ReviewerID: "reviewer", Permission: "persona:review", GrantCurrent: true, Decision: "APPROVE"}, nil
}

type agentUXR7EvaluationSource struct{}

func (agentUXR7EvaluationSource) ResolvePersonaEvaluation(_ context.Context, _ dbport.Tx, tenant values.TenantId, run, persona string, version int64, digest string) (agentpersonastore.VerifiedEvaluation, error) {
	return agentpersonastore.VerifiedEvaluation{RunID: run, TenantID: string(tenant), PersonaID: persona, PersonaVersion: version, ProfileDigest: digest, RunDigest: "evaluation-proof", SuiteDigest: "suite-proof", Passed: true, Fresh: true}, nil
}
