package agentpersonastore

import (
	"context"
	"errors"
	"sync"
	"testing"

	agentrollout "github.com/monstercameron/human-capital-management-suite/internal/agentsystem/rollout"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func rolloutPlan(tenant values.TenantId) agentrollout.VersionPlan {
	r, _ := agentrollout.PreviewVersion(agentrollout.VersionRequest{
		ID: "rollout-1", TenantID: string(tenant), AgentID: "persona-1", Version: 2,
		ProfileDigest: "sha256:persona-v8", EvaluationRef: "test-run", ReviewRef: "test-review",
		BatchLimit: 1, CanaryIDs: []string{"install-1"}, Candidates: []agentrollout.VersionCandidate{{
			InstallationID: "install-1", ConversationID: "channel-1", Version: 1, Revision: 1,
			RevocationEpoch: 1, AuthorityRevision: 1, PolicyDigest: policyDigest(testChannelPolicy()),
		}},
	})
	return r
}

// TestTodo_AGENT_044_Integration runs the version rollout against the real
// persona store: the previewed plan and its approval are revisioned, the
// affected placements are read from stored rows, applying a step moves one
// placement and advances its fences, and a canary must be promoted by the
// approver before the remainder.
func TestTodo_AGENT_044_Integration(t *testing.T) {
	t.Run("previewed plan and approval are revisioned", versionRolloutSnapshotAndApproval)
	t.Run("affected placements come from stored rows", listVersionRolloutInstallations)
	t.Run("applying a step advances the placement fences", applyRolloutBumpsInstallationFences)
	t.Run("a canary is promoted by its approver", canaryPromotionFence)
}

func versionRolloutSnapshotAndApproval(t *testing.T) {
	f := newFixture(t, values.TenantId("rollout-integration"))
	s := f.store(t, values.TenantId("rollout-integration"))
	ctx := context.Background()
	plan := rolloutPlan(values.TenantId("rollout-integration"))
	got, err := s.SaveVersionRollout(ctx, plan)
	if err != nil || got.Revision != 1 || got.Stage != "PREVIEWED" {
		t.Fatalf("save progress = %+v, %v", got, err)
	}
	read, progress, err := s.GetVersionRollout(ctx, plan.ID)
	if err != nil || read.Digest != plan.Digest || progress != got {
		t.Fatalf("rollout round trip = %+v, %+v, %v", read, progress, err)
	}
	approved, err := s.ApproveVersionRollout(ctx, plan.ID, plan.Digest, "user:approver", 1)
	if err != nil || approved.Revision != 2 || approved.Stage != "APPROVED" || approved.ApproverID != "user:approver" {
		t.Fatalf("approval progress = %+v, %v", approved, err)
	}
	if _, err := s.ApproveVersionRollout(ctx, plan.ID, plan.Digest, "user:other", 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale approval error = %v, want conflict", err)
	}
}

func TestTodo_AGENT_044_Security_VersionRolloutRejectsForgedPlan(t *testing.T) {
	f := newFixture(t, values.TenantId("rollout-security"))
	s := f.store(t, values.TenantId("rollout-security"))
	plan := rolloutPlan(values.TenantId("rollout-security"))
	plan.Digest = "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := s.SaveVersionRollout(context.Background(), plan); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forged plan error = %v, want invalid", err)
	}
}

func listVersionRolloutInstallations(t *testing.T) {
	f := newFixture(t, values.TenantId("rollout-catalog"))
	s := f.store(t, values.TenantId("rollout-catalog"))
	policy := marshalChannelPolicy(testChannelPolicy())
	f.db.Exec(t, `INSERT INTO persona_installations (tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES ($1,'rollout-install','persona-1',3,'room-1','PRIVATE','user:installer',$2::jsonb,'ACTIVE',4,7,now(),now())`, f.ids[values.TenantId("rollout-catalog")], policy)
	items, err := s.ListVersionRolloutInstallations(context.Background(), "persona-1")
	if err != nil || len(items) != 1 || items[0].PersonaVersion != 3 || items[0].Revision != 4 || items[0].RevocationEpoch != 7 {
		t.Fatalf("rollout installation catalog = %+v, %v", items, err)
	}
}

func applyRolloutBumpsInstallationFences(t *testing.T) {
	tenant := values.TenantId("rollout-apply")
	f := newFixture(t, tenant)
	s := f.store(t, tenant)
	ctx := context.Background()
	policy := marshalChannelPolicy(testChannelPolicy())
	profile := `{"owner":"user:business-owner"}`
	f.db.Exec(t, `INSERT INTO persona_versions (tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at) VALUES ($1,'persona-1',2,'agent-v2','persona','Persona',$2::jsonb,'sha256:persona-v8',now())`, f.ids[tenant], profile)
	f.db.Exec(t, `INSERT INTO persona_owners (tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES ($1,'persona-1','BUSINESS_OWNER','user:business-owner','user:admin',now())`, f.ids[tenant])
	f.db.Exec(t, `INSERT INTO persona_lifecycle_events (tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES ($1,'publish-v2','persona-1',2,'IN_REVIEW','PUBLISHED','reviewed','user:independent-reviewer',now(),'sha256:persona-v8','sha256:review-proof','user:independent-reviewer','sha256:run','sha256:persona-v8','sha256:suite')`, f.ids[tenant])
	f.db.Exec(t, `INSERT INTO persona_installations (tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES ($1,'install-1','persona-1',1,'channel-1','PRIVATE','user:installer',$2::jsonb,'ACTIVE',1,1,now(),now())`, f.ids[tenant], policy)
	plan := rolloutPlan(tenant)
	if _, err := s.SaveVersionRollout(ctx, plan); err != nil {
		t.Fatal(err)
	}
	p, err := s.ApproveVersionRollout(ctx, plan.ID, plan.Digest, "user:approver", 1)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.GetInstallation(ctx, "install-1")
	if err != nil {
		t.Fatal(err)
	}
	target := before
	target.PersonaVersion = plan.Version
	got, progress, err := s.ApplyVersionRollout(ctx, plan.ID, plan.Digest, "user:approver", p.Revision, plan.Candidates[0], target)
	if err != nil {
		t.Fatalf("apply rollout: %v", err)
	}
	if progress.Stage != "COMPLETE" || got.PersonaVersion != 2 || got.Revision != 2 || got.RevocationEpoch != 2 || got.ChannelPolicy.MaxTier != before.ChannelPolicy.MaxTier {
		t.Fatalf("apply result = %+v progress=%+v", got, progress)
	}
}

func TestTodo_AGENT_044_Race_VersionRolloutApprovalCAS(t *testing.T) {
	// The database progress row is protected by a revision CAS; the integration
	// test above exercises the same transaction path used by concurrent callers.
	f := newFixture(t, values.TenantId("rollout-race"))
	s := f.store(t, values.TenantId("rollout-race"))
	s1 := f.store(t, values.TenantId("rollout-race"))
	s2 := f.store(t, values.TenantId("rollout-race"))
	plan := rolloutPlan(values.TenantId("rollout-race"))
	if _, err := s.SaveVersionRollout(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		store := s1
		if i == 1 {
			store = s2
		}
		go func() {
			defer wg.Done()
			_, err := store.ApproveVersionRollout(context.Background(), plan.ID, plan.Digest, "user:approver", 1)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var success, conflict int
	var other []error
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			other = append(other, err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("CAS outcomes success=%d conflict=%d other=%v", success, conflict, other)
	}
}

func canaryPromotionFence(t *testing.T) {
	tenant := values.TenantId("rollout-promote")
	f := newFixture(t, tenant)
	s := f.store(t, tenant)
	plan := rolloutPlan(tenant)
	if _, err := s.SaveVersionRollout(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	p, err := s.ApproveVersionRollout(context.Background(), plan.ID, plan.Digest, "user:approver", 1)
	if err != nil {
		t.Fatal(err)
	}
	f.db.Exec(t, `UPDATE persona_version_rollout_progress SET stage='CANARY_COMPLETE' WHERE tenant_id=$1 AND rollout_id=$2`, f.ids[tenant], plan.ID)
	p, err = s.PromoteVersionRollout(context.Background(), plan.ID, plan.Digest, "user:approver", p.Revision)
	if err != nil || p.Stage != "APPROVED" || p.Revision != 3 {
		t.Fatalf("promotion progress=%+v err=%v", p, err)
	}
	if _, err := s.PromoteVersionRollout(context.Background(), plan.ID, plan.Digest, "user:other", p.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong actor error=%v", err)
	}
}

// TestTodo_AGENT_044_Recovery stops a two-placement rollout after its canary,
// reopens the store as a restarted process would, and resumes from the stored
// cursor to completion.
func TestTodo_AGENT_044_Recovery(t *testing.T) {
	tenant := values.TenantId("rollout-recovery")
	f := newFixture(t, tenant)
	s := f.store(t, tenant)
	ctx := context.Background()
	policy := marshalChannelPolicy(testChannelPolicy())
	f.db.Exec(t, `INSERT INTO persona_versions (tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at) VALUES ($1,'persona-1',2,'agent-v2','persona','Persona','{"owner":"user:business-owner"}'::jsonb,'sha256:persona-v8',now())`, f.ids[tenant])
	f.db.Exec(t, `INSERT INTO persona_owners (tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES ($1,'persona-1','BUSINESS_OWNER','user:business-owner','user:admin',now())`, f.ids[tenant])
	for _, id := range []string{"install-1", "install-2"} {
		f.db.Exec(t, `INSERT INTO persona_installations (tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES ($1,$2,'persona-1',1,$3,'PRIVATE','user:installer',$4::jsonb,'ACTIVE',1,1,now(),now())`, f.ids[tenant], id, id+"-room", policy)
	}
	f.db.Exec(t, `INSERT INTO persona_lifecycle_events (tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES ($1,'publish-v2','persona-1',2,'IN_REVIEW','PUBLISHED','reviewed','user:independent-reviewer',now(),'sha256:persona-v8','sha256:review-proof','user:independent-reviewer','sha256:run','sha256:persona-v8','sha256:suite')`, f.ids[tenant])
	plan, err := agentrollout.PreviewVersion(agentrollout.VersionRequest{ID: "rollout-2", TenantID: string(tenant), AgentID: "persona-1", Version: 2, ProfileDigest: "sha256:persona-v8", EvaluationRef: "test-run", ReviewRef: "test-review", BatchLimit: 1, CanaryIDs: []string{"install-1"}, Candidates: []agentrollout.VersionCandidate{{InstallationID: "install-1", ConversationID: "install-1-room", Version: 1, Revision: 1, RevocationEpoch: 1, AuthorityRevision: 1, PolicyDigest: policyDigest(testChannelPolicy())}, {InstallationID: "install-2", ConversationID: "install-2-room", Version: 1, Revision: 1, RevocationEpoch: 1, AuthorityRevision: 1, PolicyDigest: policyDigest(testChannelPolicy())}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveVersionRollout(ctx, plan); err != nil {
		t.Fatal(err)
	}
	p, err := s.ApproveVersionRollout(ctx, plan.ID, plan.Digest, "user:approver", 1)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.GetInstallation(ctx, "install-1")
	a.PersonaVersion = 2
	_, p, err = s.ApplyVersionRollout(ctx, plan.ID, plan.Digest, "user:approver", p.Revision, plan.Candidates[0], a)
	if err != nil || p.Stage != "CANARY_COMPLETE" {
		t.Fatalf("canary apply progress=%+v err=%v", p, err)
	}
	s = f.store(t, tenant)
	readPlan, recovered, err := s.GetVersionRollout(ctx, plan.ID)
	if err != nil || readPlan.Digest != plan.Digest || recovered.Stage != "CANARY_COMPLETE" || recovered.Cursor != 1 {
		t.Fatalf("recovered rollout progress=%+v plan=%+v err=%v", recovered, readPlan, err)
	}
	if p, err = s.PromoteVersionRollout(ctx, plan.ID, plan.Digest, "user:approver", recovered.Revision); err != nil || p.Stage != "APPROVED" {
		t.Fatalf("promote progress=%+v err=%v", p, err)
	}
	b, _ := s.GetInstallation(ctx, "install-2")
	b.PersonaVersion = 2
	_, p, err = s.ApplyVersionRollout(ctx, plan.ID, plan.Digest, "user:approver", p.Revision, plan.Candidates[1], b)
	if err != nil || p.Stage != "COMPLETE" || p.Cursor != 2 {
		t.Fatalf("resume progress=%+v err=%v", p, err)
	}
	if _, persisted, err := s.GetVersionRollout(ctx, plan.ID); err != nil || persisted.Stage != "COMPLETE" || persisted.Cursor != 2 {
		t.Fatalf("persisted final progress=%+v err=%v", persisted, err)
	}
}

func TestTodo_AGENT_044_Security_RevokedTargetScopeAtomic(t *testing.T) {
	tenant := values.TenantId("rollout-revoked")
	f := newFixture(t, tenant)
	s := f.store(t, tenant)
	policy := marshalChannelPolicy(testChannelPolicy())
	f.db.Exec(t, `INSERT INTO persona_versions (tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at) VALUES ($1,'persona-1',2,'agent-v2','persona','Persona','{"owner":"user:business-owner"}'::jsonb,'sha256:persona-v8',now())`, f.ids[tenant])
	f.db.Exec(t, `INSERT INTO persona_owners (tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES ($1,'persona-1','BUSINESS_OWNER','user:business-owner','user:admin',now())`, f.ids[tenant])
	f.db.Exec(t, `INSERT INTO persona_lifecycle_events (tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES ($1,'publish-v2','persona-1',2,'IN_REVIEW','PUBLISHED','reviewed','user:independent-reviewer',now(),'sha256:persona-v8','sha256:review-proof','user:independent-reviewer','sha256:run','sha256:persona-v8','sha256:suite')`, f.ids[tenant])
	f.db.Exec(t, `INSERT INTO persona_installations (tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES ($1,'install-1','persona-1',1,'channel-1','PRIVATE','user:installer',$2::jsonb,'ACTIVE',1,1,now(),now())`, f.ids[tenant], policy)
	plan := rolloutPlan(tenant)
	if _, err := s.SaveVersionRollout(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	p, err := s.ApproveVersionRollout(context.Background(), plan.ID, plan.Digest, "user:approver", 1)
	if err != nil {
		t.Fatal(err)
	}
	f.db.Exec(t, `INSERT INTO persona_security_scope (tenant_id,scope_kind,scope_key,epoch,state,reason,revoked_at) VALUES ($1,'VERSION','persona-1:2',4,'REVOKED','revoked by test',now())`, f.ids[tenant])
	target, _ := s.GetInstallation(context.Background(), "install-1")
	target.PersonaVersion = 2
	if _, _, err := s.ApplyVersionRollout(context.Background(), plan.ID, plan.Digest, "user:approver", p.Revision, plan.Candidates[0], target); !errors.Is(err, ErrConflict) {
		t.Fatalf("revoked scope error=%v", err)
	}
	got, _ := s.GetInstallation(context.Background(), "install-1")
	_, progress, _ := s.GetVersionRollout(context.Background(), plan.ID)
	if got.PersonaVersion != 1 || got.Revision != 1 || got.RevocationEpoch != 1 || progress.Cursor != 0 || progress.Stage != "APPROVED" || progress.Revision != 2 {
		t.Fatalf("revoked apply mutated state installation=%+v progress=%+v", got, progress)
	}
}
