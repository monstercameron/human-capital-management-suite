package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentcandidateevalstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_021_LiveModelProofRejectsUnpinnedAndUnsettledCalls(t *testing.T) {
	now := time.Now().UTC()
	digest := "sha256:" + strings.Repeat("a", 64)
	selection := agentmodel.ModelSelection{ProfileDigest: strings.Repeat("b", 64), Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-5-mini", Version: "gpt-5-mini-2025-08-07"}}
	target := agenteval.PersonaEvaluationTarget{ModelDigest: "sha256:" + selection.ProfileDigest}
	run := runstate.Run{ID: "task", UpdatedAt: now, Checkpoints: []runstate.Checkpoint{{Phase: runstate.PhaseModelCall, Ref: "step", Digest: digest, At: now}}}
	call := agentcandidateevalstore.ModelCall{Target: target, TaskID: run.ID, StepID: "step", RequestDigest: digest, ResultDigest: digest, LeaseID: "lease", Provider: selection.Identity, CompletedAt: now.Add(-time.Second), Usage: agentmodel.ModelUsage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14, CostMicros: 11}}
	usage := agentbudget.SettledTaskUsage{Usage: agentbudget.Usage{Steps: 1, Tokens: 14, SpendMicros: 11}}
	if err := verifyPersonaLiveModelCalls(target, selection, run, usage, []agentcandidateevalstore.ModelCall{call}, true); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*agentcandidateevalstore.ModelCall){
		func(c *agentcandidateevalstore.ModelCall) { c.Provider.Version = "unpinned" },
		func(c *agentcandidateevalstore.ModelCall) { c.Usage.CostMicros++ },
		func(c *agentcandidateevalstore.ModelCall) { c.ResultDigest = "sha256:" + strings.Repeat("c", 64) },
		func(c *agentcandidateevalstore.ModelCall) { c.CompletedAt = now.Add(time.Second) },
	} {
		changed := call
		mutate(&changed)
		if err := verifyPersonaLiveModelCalls(target, selection, run, usage, []agentcandidateevalstore.ModelCall{changed}, true); err == nil {
			t.Fatal("unbound model observation accepted")
		}
	}
	if err := verifyPersonaLiveModelCalls(target, selection, run, usage, nil, true); err == nil {
		t.Fatal("settled cost without an observed model call accepted")
	}
}

type liveEvaluationTestScope struct {
	denied bool
	checks int
}

func (s *liveEvaluationTestScope) AuthorizeSyntheticPersonaEvaluation(context.Context, agenteval.PersonaEvaluationTarget) error {
	s.checks++
	if s.denied {
		return agenteval.ErrPersonaEvaluation
	}
	return nil
}
func (*liveEvaluationTestScope) AuthorizePersonaCandidateModelRequest(context.Context, agenteval.PersonaEvaluationTarget, AgentModelExecutorRequest) error {
	return nil
}

func TestTodo_AGENTP_021_LiveCandidateScopeStopsModelEffects(t *testing.T) {
	if _, err := NewPersonaCandidateModelGateway(PersonaCandidateModelConfig{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatal("incomplete candidate gateway accepted")
	}
	scope := &liveEvaluationTestScope{denied: true}
	gate := &PersonaCandidateModelGateway{config: PersonaCandidateModelConfig{Scope: scope}}
	if _, err := gate.Execute(context.Background(), AgentModelExecutorRequest{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) || scope.checks != 1 {
		t.Fatalf("revoked scope: %v checks=%d", err, scope.checks)
	}
	scope.denied = false
	gate.config.Target = agenteval.PersonaEvaluationTarget{TenantID: "production", SyntheticTenantID: "synthetic", InvokerID: "invoker"}
	if _, err := gate.Execute(context.Background(), AgentModelExecutorRequest{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("ordinary request escaped candidate scope: %v", err)
	}
	var current *DatabasePersonaCandidateScope
	if err := current.AuthorizeSyntheticPersonaEvaluation(context.Background(), agenteval.PersonaEvaluationTarget{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("missing scope accepted: %v", err)
	}
	if err := current.AuthorizePersonaCandidateModelRequest(context.Background(), agenteval.PersonaEvaluationTarget{}, AgentModelExecutorRequest{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("missing current scope accepted: %v", err)
	}
}

func TestTodo_AGENTP_021_LiveReaderAndExecutorRequireOwnerEvidence(t *testing.T) {
	var owners *PersonaCandidateCurrentOwners
	if _, err := owners.VerifyAdmission(context.Background(), agentrun.Request{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("missing candidate owner accepted: %v", err)
	}
	if _, err := owners.CreateOnBehalfOfGrant(context.Background(), agentinvoke.GrantRequest{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("missing current grant issuer accepted: %v", err)
	}
	if _, err := NewPersonaLiveEvaluationRuntime(context.Background(), PersonaLiveEvaluationRuntimeConfig{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("unprovisioned runtime composed: %v", err)
	}
	if _, err := NewPersonaCandidateOutputAuthority(PersonaCandidateOutputAuthorityConfig{}); !errors.Is(err, ErrPersonaRunOutputValidatorUnavailable) {
		t.Fatalf("unsigned output authority accepted: %v", err)
	}
	var sources *PersonaCandidateSourceEvidence
	if err := sources.VerifySourceClassification(context.Background(), agentegress.SourceClassificationRequest{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("unverified synthetic source accepted: %v", err)
	}
	var fixtures *PersonaLiveChatFixtureSource
	if _, _, err := fixtures.CreateSyntheticPersonaCase(context.Background(), agenteval.PersonaEvaluationTarget{}, agenteval.PersonaCase{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("missing fixture owners accepted: %v", err)
	}
	if _, ok := personaEvaluationPeerPrincipal(nil, agenteval.PersonaEvaluationTarget{}); ok {
		t.Fatal("missing authenticated peer accepted")
	}
	reader := &PersonaLiveCaseEvidenceReader{}
	if _, err := reader.ReadPersonaCase(context.Background(), agenteval.PersonaEvaluationTarget{}, agenteval.PersonaCaseExecution{InvocationID: "i"}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("missing owner records: %v", err)
	}
	if _, err := NewPersonaLiveCaseExecutor(PersonaLiveCaseExecutorConfig{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("missing runtime composed: %v", err)
	}
	var executor *PersonaLiveCaseExecutor
	if _, err := executor.ExecutePersonaCase(context.Background(), agenteval.PersonaEvaluationTarget{}, agenteval.PersonaCase{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("nil executor: %v", err)
	}
	var definitions *PersonaCandidateDefinitionSource
	if _, _, _, err := definitions.Resolve(context.Background()); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("missing candidate definition: %v", err)
	}
	var source *PersonaCandidateRunRequestSource
	if _, err := source.ResolvePersonaRun(context.Background(), agentinvoke.RunRequest{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("missing owner facts: %v", err)
	}
	var work *PersonaCandidateModelWorkSource
	if _, err := work.BuildPersonaRunModelWork(context.Background(), agentrun.Record{}, runstate.Run{}); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("missing candidate model work: %v", err)
	}
}

func TestTodo_AGENTP_021_LivePrivateRecipientProofRequiresCurrentOutputAuthority(t *testing.T) {
	ctx := personaRunAudienceContext(t, "synthetic", "invoker")
	now := time.Now().UTC()
	store := chatcore.NewMemoryEphemeralStore(func() time.Time { return now })
	post := chatcore.EphemeralPost{ID: "private-1", TenantID: "synthetic", ConversationID: "room", ThreadID: "thread", RecipientHomeTenantID: "synthetic", RecipientSubjectID: "invoker", OnlyVisibleToYou: true, CreatedAt: now, ExpiresAt: now.Add(time.Hour), DurableCopyPostID: "dm-post", DurableCopyConversationID: "dm-room"}
	if _, err := store.PutEphemeral(ctx, post); err != nil {
		t.Fatal(err)
	}
	output := agentpersonastore.FinalOutputRecord{TenantID: values.TenantId("synthetic"), ConversationID: "room", ThreadID: "thread", InvokerID: "invoker"}
	digest, err := personaRunDeliveryDigest(PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: post.ID, DurableCopyPostID: post.DurableCopyPostID, DurableCopyConversationID: post.DurableCopyConversationID})
	if err != nil {
		t.Fatal(err)
	}
	reader := &PersonaLivePrivateDeliveryReader{Store: store, InvokerContext: ctx}
	actual, err := reader.ReadPersonaEvaluationDelivery(ctx, output, digest)
	if err != nil || len(actual) != 1 || actual[0] != "invoker" {
		t.Fatalf("actual=%v err=%v", actual, err)
	}
	if _, err := reader.ReadPersonaEvaluationDisclosure(ctx, output, digest); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("delivery without current output authority accepted: %v", err)
	}
	output.InvokerID = "other-user"
	if _, err := reader.ReadPersonaEvaluationDelivery(ctx, output, digest); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("recipient redirected: %v", err)
	}
	output.InvokerID = "invoker"
	if _, err := reader.ReadPersonaEvaluationDelivery(ctx, output, "sha256:"+strings.Repeat("f", 64)); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("unbound receipt accepted: %v", err)
	}
}

func TestTodo_AGENTP_021_LivePlanProjectionDetectsAuthorityAndSkillChanges(t *testing.T) {
	profile := "sha256:" + strings.Repeat("a", 64)
	scopes := agentinvoke.SkillScopes{"policy.read": {"tenant-policy"}}
	tools := []agentpersonastore.ToolResultRecord{{SkillID: "policy.read", SkillVersion: 1, SkillDigest: profile, InvocationID: "baseline", Arguments: []byte(`{"query":"leave"}`)}}
	baseline := PersonaLivePlanDigest(profile, scopes, tools)
	tools[0].InvocationID = "peer-case"
	tools[0].Arguments = []byte(`{"query":"governed leave policy"}`)
	if got := PersonaLivePlanDigest(profile, scopes, tools); got != baseline {
		t.Fatal("invocation identity changed authority/skill projection")
	}
	if got := PersonaLivePlanDigest(profile, agentinvoke.SkillScopes{"payroll.read": {"tenant-payroll"}}, tools); got == baseline {
		t.Fatal("authority widening omitted from plan projection")
	}
	tools[0].SkillID = "payroll.read"
	if got := PersonaLivePlanDigest(profile, scopes, tools); got == baseline {
		t.Fatal("executed skill change omitted from projection")
	}
}

type qualificationTestRuntime struct {
	cases  map[string]agenteval.PersonaCase
	failed bool
}

func (*qualificationTestRuntime) AuthorizeSyntheticPersonaEvaluation(context.Context, agenteval.PersonaEvaluationTarget) error {
	return nil
}
func (r *qualificationTestRuntime) ExecutePersonaCase(_ context.Context, _ agenteval.PersonaEvaluationTarget, c agenteval.PersonaCase) (agenteval.PersonaCaseExecution, error) {
	r.cases[c.ID] = c
	return agenteval.PersonaCaseExecution{TaskID: "task-" + c.ID, InvocationID: c.ID}, nil
}
func (r *qualificationTestRuntime) ReadPersonaCase(_ context.Context, target agenteval.PersonaEvaluationTarget, execution agenteval.PersonaCaseExecution) (agenteval.PersonaCaseEvidence, error) {
	c := r.cases[execution.InvocationID]
	digest := "sha256:" + strings.Repeat("c", 64)
	value := agenteval.PersonaCaseEvidence{SyntheticTenantID: target.SyntheticTenantID, PersonaID: target.PersonaID, PersonaVersion: target.PersonaVersion, ProfileDigest: target.ProfileDigest, ModelDigest: target.ModelDigest, CaseDigest: agenteval.PersonaCaseDigest(c), TaskID: execution.TaskID, InvocationID: execution.InvocationID, EvidenceDigest: digest, Outcome: "COMPLETED", Skills: c.ExpectedSkills, DeliveredTo: []string{target.InvokerID}, AuthorizedRecipients: []string{target.InvokerID}, AudienceFloorDigest: digest, PlanDigest: digest, BaselinePlanDigest: digest, SettledCostMicros: 17, CompletedAt: time.Now().UTC()}
	if c.Kind == agenteval.PersonaOutOfScope {
		value.Outcome = "REFUSED"
		value.RefusalCode = "OUT_OF_SCOPE"
		value.RefusalPointer = "/agents/policy"
		value.DeliveredTo = nil
	}
	if c.Kind == agenteval.PersonaDenied {
		value.Outcome = "REFUSED"
		value.RefusalCode = "AUTHORITY_DENIED"
		value.DeliveredTo = nil
	}
	if r.failed && c.Kind == agenteval.PersonaMixedAudience {
		value.DeliveredTo = []string{"other-user"}
	}
	return value, nil
}

func TestTodo_AGENTP_021_QualificationUsesMeasuredCandidateDigest(t *testing.T) {
	profile := agentmodel.ModelProfile{ID: "openai-candidate", Identity: agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-5-mini", Version: "2025-08-07"}, Regions: []string{"global"}, DataClasses: []string{"PUBLIC"}, TaskProfileIDs: []string{"policy.reply"}}
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	digest := "sha256:" + strings.Repeat("a", 64)
	target := agenteval.PersonaEvaluationTarget{TenantID: "production", SyntheticTenantID: "synthetic", PersonaID: "policy", PersonaVersion: 1, ProfileDigest: digest, ModelDigest: "sha256:" + profile.ProfileDigest, InvokerID: "invoker"}
	suite := agenteval.PolicyHelperSuite("policy.read")
	runtime := &qualificationTestRuntime{cases: map[string]agenteval.PersonaCase{}}
	report, err := agenteval.EvaluatePersonaSuite(context.Background(), target, suite, runtime, runtime)
	if err != nil {
		t.Fatal(err)
	}
	qualified, err := QualifyPersonaCandidateModelProfile(report, profile, digest, suite)
	if err != nil || !qualified.Evaluation.Passed || qualified.ProfileDigest == profile.ProfileDigest || qualified.Evaluation.AgentVersionDigest != digest {
		t.Fatalf("qualified=%+v err=%v", qualified, err)
	}
	qualified.Regions[0] = "changed"
	if profile.Regions[0] != "global" {
		t.Fatal("qualified profile aliases candidate slices")
	}
	changed := profile
	changed.Identity.Version = "other-version"
	changed.ProfileDigest = agentmodel.ModelProfileDigest(changed)
	if _, err := QualifyPersonaCandidateModelProfile(report, changed, digest, suite); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("changed model qualified: %v", err)
	}
	suite.MaxCostMicros++
	if _, err := QualifyPersonaCandidateModelProfile(report, profile, digest, suite); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("changed suite qualified: %v", err)
	}
	suite.MaxCostMicros--
	runtime.failed = true
	report, err = agenteval.EvaluatePersonaSuite(context.Background(), target, suite, runtime, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := QualifyPersonaCandidateModelProfile(report, profile, digest, suite); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("failed audience gate qualified: %v", err)
	}
}
