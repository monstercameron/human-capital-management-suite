package application

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentUXSetupResultExecutor struct {
	result productui.PersonaAdminEvaluationResult
	calls  int
}

func (e *agentUXSetupResultExecutor) ExecutePersonaAdminCommand(context.Context, PersonaAdminCommandActor, PersonaAdminCommand) error {
	return errors.New("unexpected non-result execution")
}

func (e *agentUXSetupResultExecutor) ExecutePersonaAdminCommandWithResult(context.Context, PersonaAdminCommandActor, PersonaAdminCommand) (productui.PersonaAdminEvaluationResult, error) {
	e.calls++
	return e.result, nil
}

func TestTodo_AGENTUX_014_Security(t *testing.T) {
	ctx, _ := personaAdminCommandContext(t)
	authorizer := &personaAdminCommandAuthFake{err: errors.New("evaluation permission denied")}
	executor := &agentUXSetupResultExecutor{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = factory.ExecutePersonaAdminCommandWithResult(ctx, productui.PersonaAdminCommandRequest{Action: "RUN_EVALUATION", PersonaID: "policy-helper"})
	var typed interface{ PersonaAdminCommandCode() string }
	if !errors.As(err, &typed) || typed.PersonaAdminCommandCode() != "forbidden" || executor.calls != 0 {
		t.Fatalf("evaluation denial err=%v calls=%d", err, executor.calls)
	}
}

func TestTodo_AGENTUX_014_EvaluationUnavailable(t *testing.T) {
	ctx, _ := personaAdminCommandContext(t)
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = factory.ExecutePersonaAdminCommandWithResult(ctx, productui.PersonaAdminCommandRequest{Action: "RUN_EVALUATION", PersonaID: "policy-helper"})
	var typed interface{ PersonaAdminCommandCode() string }
	if !errors.As(err, &typed) || typed.PersonaAdminCommandCode() != "evaluation_unavailable" || executor.calls != 0 {
		t.Fatalf("unconfigured evaluation err=%v calls=%d", err, executor.calls)
	}
}

func TestTodo_AGENTUX_014_LocalRuntimeBinding(t *testing.T) {
	runner := &agentUXSetupEvaluationRunner{}
	executor := &PersonaAdminLifecycleExecutor{}
	factory := &PersonaAdminCommandFactory{executor: executor}
	wiring := &personaServeWiring{adminFactory: factory}
	if err := wiring.bindAdminEvaluation(runner); err != nil || executor.Evaluations != runner {
		t.Fatalf("bind local evaluator: evaluator=%T err=%v", executor.Evaluations, err)
	}
	if _, err := wiring.prepareLocalAdminEvaluation(nil, ServeProfileStandard, localAgentDemoTenant, time.Now); !errors.Is(err, ErrPersonaAdminEvaluationUnavailable) {
		t.Fatalf("standard profile admitted local evaluation authority: %v", err)
	}
}

type agentUXSetupEvaluationRunner struct{}

func (*agentUXSetupEvaluationRunner) RunPersonaEvaluation(context.Context, PersonaAdminCommandActor, string) (productui.PersonaAdminEvaluationResult, error) {
	return productui.PersonaAdminEvaluationResult{Status: "PASSED"}, nil
}

type agentUXSetupEvaluationExecutor struct {
	now  time.Time
	fail bool
}

func (e agentUXSetupEvaluationExecutor) EvaluatePersonaVersion(ctx context.Context, target agenteval.PersonaEvaluationTarget) (agenteval.PersonaEvaluationReport, agenteval.PersonaSuite, error) {
	suite := agenteval.PolicyHelperSuite(personaPolicyHelperSkillID)
	fixture := &agentUXSetupEvaluationFixture{localAgentDemoModelFixture: localAgentDemoModelFixture{cases: make(map[string]agenteval.PersonaCase), now: e.now}, fail: e.fail}
	report, err := agenteval.EvaluatePersonaSuite(ctx, target, suite, fixture, fixture)
	return report, suite, err
}

type agentUXSetupEvaluationFixture struct {
	localAgentDemoModelFixture
	fail bool
}

func (f *agentUXSetupEvaluationFixture) ReadPersonaCase(ctx context.Context, target agenteval.PersonaEvaluationTarget, execution agenteval.PersonaCaseExecution) (agenteval.PersonaCaseEvidence, error) {
	evidence, err := f.localAgentDemoModelFixture.ReadPersonaCase(ctx, target, execution)
	if err == nil && f.fail && strings.HasSuffix(execution.InvocationID, "in-scope") {
		evidence.Outcome = "REFUSED"
		evidence.RefusalCode = "UNEXPECTED_REFUSAL"
	}
	return evidence, err
}

func TestTodo_AGENTUX_014_Integration(t *testing.T) {
	for _, test := range []struct {
		name string
		fail bool
	}{
		{name: "reviewed version evaluates and becomes publishable"},
		{name: "failed evaluation cannot authorize publication", fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, scoped, ctx, actor, row := agentUXSetupEvaluationStore(t, test.fail)
			result, err := service.RunPersonaEvaluation(ctx, actor, row.PersonaID)
			if err != nil {
				t.Fatal(err)
			}
			if test.fail {
				if result.Status != "FAILED" || result.Failed == 0 || len(result.FailingCases) == 0 {
					t.Fatalf("failed evaluation receipt=%+v", result)
				}
				if _, err := scoped.ResolvePublicationEvidence(ctx, row.PersonaID, row.Version); !errors.Is(err, agentpersonastore.ErrPublicationEvidenceRequired) {
					t.Fatalf("failed evaluation became publish evidence: %v", err)
				}
				publishErr := scoped.Publish(ctx, agentpersonastore.LifecycleEvent{TenantID: actor.Tenant, EventID: "publish-after-failed-evaluation", PersonaID: row.PersonaID, PersonaVersion: row.Version, From: agentpersonastore.StateInReview, To: agentpersonastore.StatePublished, Reason: "must remain blocked", ActorID: actor.Subject, OccurredAt: time.Now().UTC()}, agentpersonastore.PublicationEvidence{ReviewID: "review-1", EvaluationRunID: "failed-evaluation"})
				if publishErr == nil {
					t.Fatal("failed evaluation was accepted by the publication transaction")
				}
				if state, err := scoped.Lifecycle(ctx, row.PersonaID, row.Version); err != nil || state != agentpersonastore.StateInReview {
					t.Fatalf("failed evaluation changed lifecycle state=%s err=%v", state, err)
				}
				return
			}
			if result.Status != "PASSED" || result.Passed < 8 || result.Failed != 0 {
				t.Fatalf("passing evaluation receipt=%+v", result)
			}
			evidence, err := scoped.ResolvePublicationEvidence(ctx, row.PersonaID, row.Version)
			if err != nil || evidence.ReviewID == "" || evidence.EvaluationRunID == "" {
				t.Fatalf("publish evidence=%+v err=%v", evidence, err)
			}
			if err := scoped.Publish(ctx, agentpersonastore.LifecycleEvent{TenantID: actor.Tenant, EventID: "publish-after-evaluation", PersonaID: row.PersonaID, PersonaVersion: row.Version, From: agentpersonastore.StateInReview, To: agentpersonastore.StatePublished, Reason: "reviewed and evaluated", ActorID: actor.Subject, OccurredAt: time.Now().UTC()}, evidence); err != nil {
				t.Fatalf("publish after passing evaluation: %v", err)
			}
			if state, err := scoped.Lifecycle(ctx, row.PersonaID, row.Version); err != nil || state != agentpersonastore.StatePublished {
				t.Fatalf("published state=%s err=%v", state, err)
			}
		})
	}
}

func agentUXSetupEvaluationStore(t *testing.T, fail bool) (*PersonaAdminEvaluationService, *agentpersonastore.TenantStore, context.Context, PersonaAdminCommandActor, agentpersonastore.PersonaVersion) {
	t.Helper()
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenant, tenantUUID := values.TenantId("agentux-evaluation"), uuid.New()
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
	profile := agentpersona.PersonaProfile{
		Manifest: agentpersona.AgentManifestRef{ID: "agent.policy-helper", Version: 1, SchemaVersion: 1, Digest: "manifest"}, PersonaID: "policy-helper", Version: 4,
		Handle: "policy-helper", DisplayName: "Policy Helper", AvatarRef: "avatar", Purpose: "Answer policy questions", Owner: "admin", Steward: "steward",
		Audience: agentpersona.Audience{Roles: []string{"hcm_admin"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org:agentux-evaluation"}}, SkillPins: []agentskills.SkillPin{starter.SkillPins[0]}, TierCeiling: agentskills.TierT0,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		Instructions: "Use approved policy sources.", EvalSuiteRef: suite.ID, EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1_000_000, MaxSteps: 8, MaxLatencyMS: 120_000},
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(sealed.Profile)
	row := agentpersonastore.PersonaVersion{TenantID: tenant, PersonaID: profile.PersonaID, Version: int64(profile.Version), AgentVersion: "agent.policy-helper@1", Handle: profile.Handle, DisplayName: profile.DisplayName, Profile: body, ContentDigest: sealed.Digest, CreatedAt: now}
	if err := scoped.CreateDraft(ctx, row, agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: row.PersonaID, Role: agentpersonastore.BusinessOwner, PrincipalID: "admin"}, agentpersonastore.PersonaOwner{TenantID: tenant, PersonaID: row.PersonaID, Role: agentpersonastore.TechnicalSteward, PrincipalID: "steward"}, "admin", now); err != nil {
		t.Fatal(err)
	}
	if err := scoped.AppendLifecycle(ctx, agentpersonastore.LifecycleEvent{TenantID: tenant, EventID: "request-review", PersonaID: row.PersonaID, PersonaVersion: row.Version, From: agentpersonastore.StateDraft, To: agentpersonastore.StateInReview, Reason: "independent review", ActorID: "admin", OccurredAt: now}); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO persona_review_grant(tenant_id,grant_id,principal_id,permission,granted_at,expires_at) VALUES($1,'review-grant','reviewer','persona:review',now()-interval '1 hour',now()+interval '1 day')`, tenantUUID)
	db.Exec(t, `INSERT INTO persona_review_decision(tenant_id,review_id,persona_id,persona_version,profile_digest,author_id,reviewer_id,grant_id,permission,decision,review_digest,reviewed_at,expires_at) VALUES($1,'review-1',$2,$3,$4,'admin','reviewer','review-grant','persona:review','APPROVE',$5,now()-interval '1 hour',now()+interval '1 day')`, tenantUUID, row.PersonaID, row.Version, row.ContentDigest, "sha256:"+strings.Repeat("c", 64))
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: tenant, Subject: "admin", SubjectKind: trust.SubjectKindHuman, Roles: []string{"hcm_admin"}, OrganizationScopeID: "org", AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "agentux-evaluation", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:agentux-evaluation"})
	if err != nil {
		t.Fatal(err)
	}
	ctx = trust.WithPrincipal(ctx, principal)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: tenant, Subject: principal.Subject()}
	modelDigest := "sha256:" + strings.Repeat("d", 64)
	service := &PersonaAdminEvaluationService{Store: personaAdminLifecycleStoreAdapter{store: root}, Versions: personaAdminInstallationVersions{store: root}, Recorder: &PersonaEvaluationEvidenceService{store: root}, Executor: agentUXSetupEvaluationExecutor{now: now, fail: fail}, SyntheticTenant: "agentux-evaluation-synthetic", KeyID: localAgentDemoEvaluationKeyID, PrivateKey: privateKey, ModelDigest: modelDigest, Now: func() time.Time { return now.Add(time.Second) }, FreshFor: time.Hour, NewRunID: func() string { return map[bool]string{true: "failed-evaluation", false: "passing-evaluation"}[fail] }}
	return service, scoped, ctx, actor, row
}
