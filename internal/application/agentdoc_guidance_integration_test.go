package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENTDOC_008_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, tenantUUID := values.TenantId("agentdoc-guidance"), uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantUUID)
	mapper := func(value values.TenantId) uuid.UUID {
		if value == tenant {
			return tenantUUID
		}
		return uuid.Nil
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	suite := agenteval.PolicyHelperSuite(personaPolicyHelperSkillID)
	keyID, err := PersonaEvaluationVerificationKeyID(string(tenant), suite.ID, localAgentDemoEvaluationKeyID)
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := agentpersonastore.NewDurableReviewAuthority(mapper)
	if err != nil {
		t.Fatal(err)
	}
	evaluations, err := agentpersonastore.NewEvaluationSealAuthority(map[string]ed25519.PublicKey{keyID: publicKey}, mapper, func() time.Time { return now.Add(time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	root, err := agentpersonastore.NewWithPublicationAuthorities(conn, mapper, reviews, evaluations)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := root.Scoped(tenant)
	if err != nil {
		t.Fatal(err)
	}

	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	ref := agentdocref.Reference{DocumentID: "doc-travel-policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 7, Label: "Travel policy"}
	profile := agentpersona.PersonaProfile{
		Manifest: agentpersona.AgentManifestRef{ID: "agent.policy-helper", Version: 1, SchemaVersion: 1, Digest: "manifest"}, PersonaID: "policy-helper-guided", Version: 1,
		Handle: "policy-helper-guided", DisplayName: "Policy Helper", AvatarRef: "avatar", Purpose: "Answer policy questions", Owner: "admin", Steward: "steward",
		Audience: agentpersona.Audience{Roles: []string{"hcm_admin"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org:agentdoc-guidance"}}, SkillPins: []agentskills.SkillPin{starter.SkillPins[0]}, TierCeiling: agentskills.TierT0,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		Instructions: "Use approved policy sources.", Guidance: "Follow {{doc:doc-travel-policy}} before answering.", DocumentReferences: []agentdocref.Reference{ref},
		EvalSuiteRef: suite.ID, EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1_000_000, MaxSteps: 8, MaxLatencyMS: 120_000},
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(sealed.Profile)
	if err != nil {
		t.Fatal(err)
	}
	row := agentpersonastore.PersonaVersion{TenantID: tenant, PersonaID: profile.PersonaID, Version: int64(profile.Version), AgentVersion: "agent.policy-helper@1", Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: body, ContentDigest: sealed.Digest, CreatedAt: now}
	if err := scoped.CreateDraft(ctx, row, agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: row.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: "admin"}, agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: row.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: "steward"}, "admin", now); err != nil {
		t.Fatal(err)
	}
	if err := scoped.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: tenant, EventID: "request-review", PersonaID: row.PersonaID, PersonaVersion: row.Version, From: agentpersonastore.StateDraft, To: agentpersonastore.StateInReview, Reason: "independent review", ActorID: "admin", OccurredAt: now}); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO persona_review_grant(tenant_id,grant_id,principal_id,permission,granted_at,expires_at) VALUES($1,'review-grant','reviewer','persona:review',now()-interval '1 hour',now()+interval '1 day')`, tenantUUID)
	db.Exec(t, `INSERT INTO persona_review_decision(tenant_id,review_id,persona_id,persona_version,profile_digest,author_id,reviewer_id,grant_id,permission,decision,review_digest,reviewed_at,expires_at) VALUES($1,'review-guidance',$2,$3,$4,'admin','reviewer','review-grant','persona:review','APPROVE',$5,now()-interval '1 hour',now()+interval '1 day')`, tenantUUID, row.PersonaID, row.Version, row.ContentDigest, "sha256:"+strings.Repeat("c", 64))

	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: "admin", SubjectKind: trust.SubjectKindHuman, Roles: []string{"hcm_admin"}, OrganizationScopeID: "org", AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "agentdoc-guidance", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:agentdoc-guidance"})
	if err != nil {
		t.Fatal(err)
	}
	ctx = trust.WithPrincipal(ctx, principal)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: tenant, Subject: principal.Subject()}
	modelDigest := "sha256:" + strings.Repeat("d", 64)
	service := &PersonaAdminEvaluationService{Store: personaAdminLifecycleStoreAdapter{store: root}, Versions: personaAdminInstallationVersions{store: root}, Recorder: &PersonaEvaluationEvidenceService{store: root}, Executor: agentUXSetupEvaluationExecutor{now: now}, SyntheticTenant: "agentdoc-guidance-synthetic", KeyID: localAgentDemoEvaluationKeyID, PrivateKey: privateKey, ModelDigest: modelDigest, Now: func() time.Time { return now.Add(time.Second) }, FreshFor: time.Hour, NewRunID: func() string { return "guidance-evaluation" }}
	result, err := service.RunPersonaEvaluation(ctx, actor, row.PersonaID)
	if err != nil || result.Status != "PASSED" {
		t.Fatalf("evaluation result=%+v err=%v", result, err)
	}
	evidence, err := scoped.ResolvePublicationEvidence(ctx, row.PersonaID, row.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := scoped.Publish(ctx, agentpersonastore.LifecycleEvent{TenantID: tenant, EventID: "publish-guidance", PersonaID: row.PersonaID, PersonaVersion: row.Version, From: agentpersonastore.StateInReview, To: agentpersonastore.StatePublished, Reason: "reviewed guidance passed evaluation", ActorID: actor.Subject, OccurredAt: now.Add(2 * time.Second)}, evidence); err != nil {
		t.Fatal(err)
	}

	persisted, err := scoped.GetVersion(ctx, row.PersonaID, row.Version)
	if err != nil {
		t.Fatal(err)
	}
	var candidate agentpersona.PersonaProfile
	if err := json.Unmarshal(persisted.Profile, &candidate); err != nil {
		t.Fatal(err)
	}
	candidate.Guidance = renderPersonaGuidanceDocumentTokens(candidate, []agentdocref.ResolvedDocument{{Reference: ref, Version: 7, Title: "Current travel policy"}})
	deadline := now.Add(time.Minute)
	admission := agentrun.Record{Request: agentrun.Request{Persona: &agentrun.PersonaRef{Digest: persisted.ContentDigest}, Principal: agentrun.PrincipalChain{InvokerID: "reader"}, Budget: agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 200, MaxOutputTokens: 50}}}
	run := runstate.Run{ID: "run-guidance", TenantID: string(tenant), AgentDigest: "agent-digest", Deadline: deadline}
	manifest := agentmanifest.Manifest{Purpose: "Answer policy questions.", Budget: agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 200, MaxOutputTokens: 50}}
	route := PersonaRunModelRoute{Route: agentmodel.RouteRequest{Task: agentmodel.TaskProfile{ID: "persona-reply"}, Pin: agentmodel.ModelPin{Primary: agentmodel.ModelSelection{ProfileID: "model"}}}}
	request := buildPersonaRunModelRequest(admission, run, candidate, manifest, route, PersonaRunEffectivePolicy{Budget: admission.Request.Budget, Deadline: deadline}, "What is the travel policy?", nil, nil)
	if len(request.Messages) != 3 || !strings.Contains(request.Messages[1].Content, `Follow "Current travel policy" before answering.`) || !strings.HasSuffix(request.Messages[1].Content, personaUntrustedThreadInstruction) {
		t.Fatalf("published guidance model request = %+v", request.Messages)
	}
	if state, err := scoped.Lifecycle(ctx, row.PersonaID, row.Version); err != nil || state != agentpersonastore.StatePublished {
		t.Fatalf("published state=%s err=%v", state, err)
	}
}
