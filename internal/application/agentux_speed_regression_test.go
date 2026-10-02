package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestTodo_AGENTUX_SPEED_R1_ModelWorkKeepsContractAndClassBindings(t *testing.T) {
	if got, ok := personaDataClassRouteClass(personaPolicySearchDataClass); !ok || got != string(trustdlp.ClassInternal) {
		t.Fatalf("POLICY_DOCUMENT route class = %q, %t", got, ok)
	}
	if got, ok := personaDataClassRouteClass(string(trustdlp.ClassConfidential)); !ok || got != string(trustdlp.ClassConfidential) {
		t.Fatalf("valid DLP route class = %q, %t", got, ok)
	}
	if _, ok := personaDataClassRouteClass("UNKNOWN_CLASS"); ok {
		t.Fatal("unknown data class was accepted")
	}
	instructions := "Use the approved policy."
	if !personaInstructionsMatchDigest(instructions, personaTextDigest(instructions)) || personaInstructionsMatchDigest(instructions+" changed", personaTextDigest(instructions)) {
		t.Fatal("instruction digest did not bind the exact instruction text")
	}
	req := executorAdapterRequest(t)
	req.Route.Task.MaxLatency = 30 * time.Second
	req.Route.BudgetRemainingMicros = req.Route.Task.MaxCostMicros
	req.Model.Limits.MaxCostMicros = req.Route.Task.MaxCostMicros / 2
	if err := validateExecutorRequest(req); err != nil {
		t.Fatalf("remaining request budget was rejected against contractual route: %v", err)
	}
	if req.Route.Task.MaxLatency != 30*time.Second {
		t.Fatal("contractual task latency was changed")
	}
}

func TestTodo_AGENTUX_SPEED_R2_ExecutorCostLimitMayNarrowOnly(t *testing.T) {
	req := executorAdapterRequest(t)
	req.Model.Limits.MaxCostMicros = req.Route.Task.MaxCostMicros - 1
	if err := validateExecutorRequest(req); err != nil {
		t.Fatalf("narrow request cost was rejected: %v", err)
	}
	req.Model.Limits.MaxCostMicros = req.Route.Task.MaxCostMicros + 1
	if !errors.Is(validateExecutorRequest(req), ErrAgentModelExecutorBinding) {
		t.Fatal("request cost above route contract was accepted")
	}
}

func TestTodo_AGENTUX_SPEED_R3_BudgetWallClockPrecision(t *testing.T) {
	base := agentbudget.Limits{Steps: 2, Tokens: 10, SpendMicros: 20, WallClock: time.Second}
	near := base
	near.WallClock += 2*time.Microsecond - time.Nanosecond
	if !personaRuntimeBudgetLimitsMatch(base, near) {
		t.Fatal("sub-two-microsecond database precision difference did not match")
	}
	far := base
	far.WallClock += 2 * time.Microsecond
	if personaRuntimeBudgetLimitsMatch(base, far) {
		t.Fatal("two-microsecond wall-clock difference matched")
	}
	far = base
	far.Tokens++
	if personaRuntimeBudgetLimitsMatch(base, far) {
		t.Fatal("non-wall-clock budget difference matched")
	}
}

func TestTodo_AGENTUX_SPEED_R4_ModelReservationUsesDeadlineRemainder(t *testing.T) {
	now := time.Unix(100, 0)
	if got, err := agentModelReservationWallClock(now.Add(3*time.Second), 10*time.Second, now); err != nil || got != 3*time.Second {
		t.Fatalf("deadline-limited reservation = %s, %v", got, err)
	}
	if got, err := agentModelReservationWallClock(now.Add(30*time.Second), 10*time.Second, now); err != nil || got != 10*time.Second {
		t.Fatalf("task-limited reservation = %s, %v", got, err)
	}
	if _, err := agentModelReservationWallClock(now, 10*time.Second, now); !errors.Is(err, agentmodel.ErrBudgetFailed) {
		t.Fatalf("passed deadline error = %v", err)
	}
}

func TestTodo_AGENTUX_SPEED_R5_ModelShapeCauseCarriesNoText(t *testing.T) {
	secret := "model text must not enter the cause"
	err := personaRunModelShapeCause("tool proposal", agentmodel.ModelResult{Text: secret, Finish: agentmodel.FinishToolCalls, ToolProposals: []agentmodel.ToolProposal{{ID: "call", Name: "search"}}})
	if strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "text_bytes=") {
		t.Fatalf("shape cause leaked model text or omitted safe length: %v", err)
	}
	ctx := withPersonaRunReplyDelivered(context.Background(), "admission-a")
	if !personaRunReplyDelivered(ctx, "admission-a") || personaRunReplyDelivered(ctx, "admission-b") {
		t.Fatal("delivery checkpoint marker was not exact")
	}
}

type agentUXSpeedCaptureReply struct {
	invocation agentinvoke.RunRequest
}

func (r *agentUXSpeedCaptureReply) Deliver(ctx context.Context, _ PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	r.invocation, _ = personaDMInvocationFromContext(ctx)
	return PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: "captured"}, nil
}

func TestTodo_AGENTUX_SPEED_R5_DeliveryBindsInvocationDeadline(t *testing.T) {
	validator, admission, run, _, _ := personaRunOutputFixture(t)
	now := time.Now().UTC()
	admission.Request.Deadline = now.Add(time.Minute)
	admission.Request.Principal.Mode = agentrun.ModeOnBehalfOf
	admission.Request.Principal.DelegatedCredentialRef = "grant-a"
	projection, err := validator.ValidateAndPersistPersonaOutput(context.Background(), admission, run, agentmodel.ModelResult{Text: "A grounded response.", Finish: agentmodel.FinishComplete})
	if err != nil {
		t.Fatal(err)
	}
	principal := foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), admission.Request.Purpose, admission.Request.Principal.InvokerID, admission.Request.Source.TenantID)
	capture := &agentUXSpeedCaptureReply{}
	executor := &personaAdmittedRunExecutor{reply: capture}
	if _, err := executor.deliver(trust.WithPrincipal(context.Background(), principal), admission, run, projection); err != nil {
		t.Fatal(err)
	}
	if !capture.invocation.Grant.ExpiresAt.Equal(admission.Request.Deadline) || capture.invocation.InvocationID != admission.Request.Source.Key {
		t.Fatalf("delivery invocation = %+v", capture.invocation)
	}
}

func TestTodo_AGENTUX_SPEED_R6_DocumentSearchArgumentsNormalizeModelNoise(t *testing.T) {
	args, err := decodePersonaDocumentSearchArguments([]byte(`{"query":"  leave policy  ","team_id":"team-a","channel_id":"channel-a"}`))
	if err != nil || args.Query != "leave policy" || args.TeamID != "" || args.ChannelID != "" {
		t.Fatalf("normalized arguments = %+v, %v", args, err)
	}
}

func TestTodo_AGENTUX_SPEED_R7_AgentVersionMatching(t *testing.T) {
	if !personaAgentVersionMatches("2", "agent.starter.policy_helper@2") || !personaAgentVersionMatches("agent.starter.policy_helper@2", "2") {
		t.Fatal("bare and qualified equal versions did not match")
	}
	for _, pair := range [][2]string{{"2", "agent@3"}, {"", "agent@2"}, {"agent-a@2", "agent-b@2"}} {
		if personaAgentVersionMatches(pair[0], pair[1]) {
			t.Fatalf("different or empty versions matched: %q, %q", pair[0], pair[1])
		}
	}
}

func TestTodo_AGENTUX_SPEED_AuthoritySnapshotLivesForOneBoundary(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	request := foregroundAuthorityRequest(now)
	grant := foregroundPositiveGrant(now)
	calls := 0
	policy := &foregroundPositivePolicy{}
	authority := &PersonaForegroundRunAuthority{
		builder: &PersonaRunRequestBuilder{source: foregroundRequestSourceFunc(func(context.Context, agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
			calls++
			return foregroundFactsFromRequest(request), nil
		})},
		persona: foregroundPositivePersona{admission: agentinvoke.Admission{Persona: agentinvoke.Persona{ID: "persona-a", Version: "7", InstallationID: "install-a", Current: true}, Installation: agentinvoke.Installation{ID: "install-a", Current: true}, Discoverable: map[string][]string{"skill.read": {"scope:read"}}, HumanMember: true, AudienceMember: true, PersonaInstalled: true}},
		grants:  foregroundPositiveGrantFactory{store: foregroundPositiveStore{grant: grant, epoch: 1}}, policy: policy, now: func() time.Time { return now },
	}
	base := trust.WithPrincipal(context.Background(), foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), "persona-mention", "user-a", "tenant-a"))
	ctx, _ := withAgentUXRunTiming(base)
	if _, err := authority.VerifyAdmission(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.VerifyAdmission(ctx, request); err != nil || calls != 1 {
		t.Fatalf("helper repeated authority inside one boundary: calls=%d err=%v", calls, err)
	}
	agentUXSpeedInvalidateAuthority(ctx)
	if _, err := authority.VerifyAdmission(ctx, request); err != nil || calls != 2 {
		t.Fatalf("next boundary did not re-resolve mutable authority: calls=%d err=%v", calls, err)
	}
}

func TestTodo_AGENTUX_SPEED_OutputAuthorityReusesImmutableRunSnapshot(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	request := foregroundAuthorityRequest(now)
	id, err := agentrun.AdmissionRequestID(request.Source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := agentrun.AdmissionRequestDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := agentrun.AuthoritySnapshot{
		Agent: request.Agent, InstallationID: request.InstallationID, Principal: request.Principal,
		Audience: request.Audience, Context: request.Context, BudgetCeiling: request.Budget,
		GrantRef: request.Principal.DelegatedCredentialRef, PolicyDigest: foregroundDigest("policy"),
	}
	record := agentrun.Record{ID: id, Request: request, RequestDigest: digest, Decision: agentrun.DecisionAccepted, Authority: snapshot, AdmittedAt: now}
	if err := agentrun.ValidateAdmissionRecord(record); err != nil {
		t.Fatal(err)
	}
	run := runstate.Run{
		ID: id, TenantID: request.Source.TenantID, AdmissionID: id, PrincipalMode: request.Principal.Mode,
		ActorID: request.Principal.InvokerID, RequestDigest: digest, AgentID: request.Agent.AgentID,
		AgentVersion: request.Agent.Version, AgentDigest: request.Agent.Digest, ContextDigest: request.Context.Digest,
		Deadline: request.Deadline,
	}
	ctx, _ := withAgentUXRunTiming(context.Background())
	agentUXSpeedCacheImmutableModelWork(ctx, run.ID, agentUXImmutableModelWork{
		manifest: agentmanifest.Manifest{OutputSchema: agentmanifest.Reference{ID: PersonaChatReplySchema, Version: 1, Digest: PersonaChatReplySchemaDigest}},
		route: PersonaRunModelRoute{Route: agentmodel.RouteRequest{Pin: agentmodel.ModelPin{Primary: agentmodel.ModelSelection{
			ProfileDigest: "sha256:" + strings.Repeat("f", 64),
		}}}},
	})
	grant := privateChatGatewayGrant(record, run, now)
	authority := &personaRuntimeOutputAuthority{
		authority: &runtimeCurrentOwnerFake{snapshot: snapshot},
		work: &DatabasePersonaRunModelWorkSource{threads: personaRunModelThreadFake{posts: []agentinvoke.ThreadPost{{
			TenantID: request.Source.TenantID, ConversationID: request.Audience.ID, ThreadID: request.Context.ID,
			ID: request.Source.Ref, AuthorID: request.Principal.InvokerID, Body: "Explain the leave policy.",
		}}}, now: func() time.Time { return now }},
		grants: privateChatGatewayGrantStoreFactoryFake{grant: grant, epoch: grant.RevocationEpoch},
		worker: privateChatGatewayVerifiedWorker(t, now),
	}
	got, err := authority.ResolvePersonaRunChatReplyAuthority(ctx, record, run)
	if err != nil {
		t.Fatalf("cached output authority: %v", err)
	}
	if got.Schema.ID != PersonaChatReplySchema || got.ModelDigest != "sha256:"+strings.Repeat("f", 64) || len(got.Grounding) != 1 {
		t.Fatalf("cached output authority = %+v", got)
	}
}

type agentUXSpeedBlockingRun struct {
	started chan context.Context
	release chan struct{}
}

func (r *agentUXSpeedBlockingRun) Start(ctx context.Context, _ agentinvoke.RunRequest) error {
	r.started <- ctx
	<-r.release
	return nil
}

func TestTodo_AGENTUX_SPEED_R10_PostCommitIsDetachedAndBounded(t *testing.T) {
	now := time.Now().UTC()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "alice", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceLow, SessionRef: "speed-test", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "speed-test"})
	if err != nil {
		t.Fatal(err)
	}
	run := &agentUXSpeedBlockingRun{started: make(chan context.Context, 1), release: make(chan struct{})}
	service, err := newPersonaChatInvocation(personaChatInvocationConfig{
		Chat: &personaChatWriterFake{}, References: &personaReferenceResolverFake{mentions: []agentinvoke.Mention{{Kind: agentinvoke.PersonaMention, PersonaID: "persona-comp", Canonical: true}}},
		Authority: personaAuthorityFake{admission: personaAdmission()}, Grants: &personaGrantFake{}, Runs: run,
		T0Skills: personaT0PolicyFake{allowed: true}, Repository: agentinvoke.NewMemoryRepository(), DetachedAfterCommit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requestCtx, cancel := context.WithCancel(trust.WithPrincipal(context.Background(), principal))
	cancel()
	started := time.Now()
	post, err := service.SendPost(requestCtx, personaSendRequest())
	if err != nil || post.ID == "" || time.Since(started) > 250*time.Millisecond {
		t.Fatalf("committed post waited for run: post=%+v err=%v elapsed=%s", post, err, time.Since(started))
	}
	select {
	case runCtx := <-run.started:
		if runCtx.Err() != nil {
			t.Fatalf("detached run inherited request cancellation: %v", runCtx.Err())
		}
		deadline, ok := runCtx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > personaMentionAdmissionTimeout {
			t.Fatalf("detached run deadline = %v, %t", deadline, ok)
		}
		close(run.release)
	case <-time.After(time.Second):
		t.Fatal("detached run did not start")
	}
}

type agentUXSpeedInstantModel struct{ calls int }

func (m *agentUXSpeedInstantModel) Execute(context.Context, AgentModelExecutorRequest) (AgentModelExecutorResult, error) {
	m.calls++
	if m.calls == 1 {
		return AgentModelExecutorResult{Result: agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Text: "I will search.", Finish: agentmodel.FinishToolCalls, ToolProposals: []agentmodel.ToolProposal{{ID: "search-1", Name: personaDocumentSearchTool, Arguments: []byte(`{"query":"leave"}`)}}}}, nil
	}
	return AgentModelExecutorResult{Result: agentmodel.ModelResult{ContractVersion: agentmodel.ContractVersion, Text: "A grounded response.", Finish: agentmodel.FinishComplete}}, nil
}

type agentUXSpeedTools struct{}

func (agentUXSpeedTools) ToolSchemas(context.Context, agentrun.Record, runstate.Run) ([]agentmodel.ToolSchema, error) {
	return []agentmodel.ToolSchema{{Name: personaDocumentSearchTool, Description: "Search policy documents.", InputSchema: []byte(`{"type":"object"}`)}}, nil
}

func (agentUXSpeedTools) Execute(context.Context, agentrun.Record, runstate.Run, agentmodel.ToolProposal) ([]byte, string, string, error) {
	result := []byte(`{"hits":[{"document_id":"doc-1","title":"Leave policy"}]}`)
	return result, "speed-tool-result", personaRunBytesDigest(result), nil
}

type agentUXSpeedWork struct{ t *testing.T }

func (w agentUXSpeedWork) BuildPersonaRunModelWork(_ context.Context, admission agentrun.Record, run runstate.Run) (PersonaRunModelWork, error) {
	req := executorAdapterRequest(w.t)
	task, err := NewTrustedModelTask(run.TenantID, run.ID, run.AgentDigest, "speed-worker")
	if err != nil {
		return PersonaRunModelWork{}, err
	}
	req.Task, req.StepID = task, run.ID
	req.Route.TraceID, req.Route.Pin.AgentVersionDigest, req.Route.Task.AgentVersionDigest = run.ID, run.AgentDigest, run.AgentDigest
	req.Outbound.TaskID, req.Outbound.Tenant = run.ID, run.TenantID
	req.Lease.Tenant = run.TenantID
	req.ToolResultClass = trustdlp.ClassInternal
	req.Model = agentmodel.ModelRequest{ContractVersion: agentmodel.ContractVersion, TraceID: run.ID, TaskProfile: req.Route.Task.ID, ModelProfile: req.Route.Pin.Primary.ProfileID, Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "Explain the policy."}}, Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: admission.Request.Deadline, Limits: agentmodel.ModelLimits{MaxInputTokens: 1, MaxOutputTokens: 1, MaxCostMicros: 1}, Processing: agentmodel.ProcessingPolicy{Residency: "us-east", Retention: "NONE:0", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}}
	req.Outbound.Fields = []agentegress.Field{{Name: "model.message.0", Value: "Explain the policy.", Class: trustdlp.ClassInternal, Taint: []string{"PERSONA_INVOKING_POST"}, Provenance: []string{"persona-run:" + run.ID}}}
	req.Outbound.DeclaredFields = []string{"model.message.0"}
	req.FieldSources = map[string]string{"model.message.0": "persona-invoking-post"}
	return PersonaRunModelWork{Request: req}, nil
}

type agentUXSpeedReply struct{}

func (agentUXSpeedReply) Deliver(context.Context, PersonaReplyDeliveryRequest) (PersonaReplyDeliveryReceipt, error) {
	return PersonaReplyDeliveryReceipt{Private: true, EphemeralPostID: "speed-reply"}, nil
}

func TestTodo_AGENTUX_SPEED_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenantID)
	store, err := agentstore.New(ctx, agentstore.Config{DSN: personaChatSchemaDSN(t, db.URL, db.Schema), CoreDSN: "postgres://unused@127.0.0.1:1/unused", MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	mapper := func(tenant string) uuid.UUID {
		if tenant == "tenant-a" {
			return tenantID
		}
		return uuid.Nil
	}
	admissions, err := agentrunstore.NewAdmissionRepository(store, tenantID, values.TenantId("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	request := foregroundAuthorityRequest(now)
	request.Source.Key, request.Source.Ref, request.CauseID = "invocation-a", "post-a", "invocation-a"
	request.Persona = &agentrun.PersonaRef{ID: "persona-a", Version: "7", Digest: "sha256:" + strings.Repeat("a", 64)}
	request.Agent = agentrun.VersionRef{AgentID: "agent-a", Version: "4", Digest: "sha256:" + strings.Repeat("d", 64)}
	request.Principal.InvokerID, request.Purpose = "alice", "persona-chat"
	request.Audience.ID, request.Context.ID = "room-a", "thread-a"
	admissionService, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: personaChatAdmissionAuthorityFake{}, Store: admissions, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	admission, _, err := admissionService.Admit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	runStores, err := agentrunstate.New(store, mapper)
	if err != nil {
		t.Fatal(err)
	}
	runStore, err := runStores.ForTenant("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	state, err := runstate.New(runStore, personaChatAdmissionRecheckerFake{})
	if err != nil {
		t.Fatal(err)
	}
	validator, _, _, persister, _ := personaRunOutputFixture(t)
	model := &agentUXSpeedInstantModel{}
	executor := &personaAdmittedRunExecutor{state: state, store: runStore, model: model, work: agentUXSpeedWork{t: t}, tools: agentUXSpeedTools{}, output: validator, reply: agentUXSpeedReply{}, workerID: "speed-worker", leaseTTL: time.Minute, now: func() time.Time { return now }}
	principal := foregroundPrincipal(t, now.Add(-time.Minute), now.Add(time.Hour), request.Purpose, "alice", "tenant-a")
	timedCtx, timing := withAgentUXRunTiming(trust.WithPrincipal(ctx, principal))
	started := time.Now()
	run, err := executor.Start(timedCtx, admission)
	elapsed := time.Since(started)
	agentUXSpeedEmit(timedCtx, err != nil)
	if err != nil || run.State != runstate.StateCompleted || persister.calls != 1 || model.calls != 2 {
		t.Fatalf("instant served-shape run = %s model=%d output=%d err=%v", run.State, model.calls, persister.calls, err)
	}
	if elapsed >= 10*time.Second {
		t.Fatalf("instant model admission-to-delivery = %s, want <10s", elapsed)
	}
	timing.mu.Lock()
	authorityCount := timing.events["authority.boundary_verify"].Count
	stageCount := len(timing.stages)
	timing.mu.Unlock()
	t.Logf("hcmnext.persona_run_timing run_id=%s total_ms=%d authority_boundary_count=%d stages=%d", run.ID, elapsed.Milliseconds(), authorityCount, stageCount)
}

var _ agentinvoke.RunStarter = (*agentUXSpeedBlockingRun)(nil)
var _ PersonaRunOutputValidator = (*SealedPersonaRunOutputValidator)(nil)
