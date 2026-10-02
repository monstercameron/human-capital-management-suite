package application

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

type agentActionFixture struct {
	harness   *promoux015Harness
	service   *AgentActionService
	cell      *app.Cell
	db        *pgtest.DB
	ctx       context.Context
	req       AgentActionCompileRequest
	authority *agentActionTestAuthority
}

type agentActionTestAuthority struct {
	mu       sync.Mutex
	proposal AgentActionCompileRequest
	user     string
	tenant   string
	digest   string
	refused  bool
}

func (a *agentActionTestAuthority) VerifyAgentAction(_ context.Context, pin AgentActionAuthorityPin) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.refused || pin.TenantID != a.tenant || pin.ForUser != a.user || pin.RunID != a.proposal.AgentRunID || pin.AgentVersionRef != a.proposal.AgentVersionRef || pin.ModelDigest != a.proposal.ModelDigest {
		return ErrAgentActionApproval
	}
	if pin.Proposal != nil {
		want := proto.Clone(a.proposal.Request).(*intentsv1.CreateIntentRequest)
		want.Initiator = nil
		want.IdempotencyKey = "agent:validate"
		if !proto.Equal(a.proposal.Definition, pin.Proposal.Definition) || !proto.Equal(want, pin.Proposal.Request) || !reflect.DeepEqual(a.proposal.Sources, pin.Proposal.Sources) || !reflect.DeepEqual(a.proposal.Taint, pin.Proposal.Taint) || a.proposal.Uncertainty != pin.Proposal.Uncertainty {
			return ErrAgentActionApproval
		}
		if a.digest == "" {
			a.digest = pin.DraftDigest
		}
	}
	if a.digest == "" || a.digest != pin.DraftDigest {
		return ErrAgentActionApproval
	}
	return nil
}

func newAgentActionFixture(t *testing.T, unbound ...bool) *agentActionFixture {
	t.Helper()
	db := pgtest.New(t)
	pool, err := pgxadapter.NewPool(context.Background(), db.URL, map[string]string{"search_path": db.Schema})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := pgstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Bootstrap(context.Background(), string(fixtures.Tenant)); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	config := app.CellConfig{Store: store, Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return nil, nil }), Audience: DefaultAudience, RoleAccess: chat045Access{}, Now: func() time.Time { return now }}
	serve := executionServeConfig()
	serve.WorkflowPlan = WorkflowPlanExecute
	serve.ExecutionManagerApprover = "principal:agent-action-manager"
	if err := ComposeExecutionAuthority(&config, pool, app.NewMemoryEvidenceSink(), serve); err != nil {
		t.Fatal(err)
	}
	cell, err := app.NewCell(config)
	if err != nil {
		t.Fatal(err)
	}
	activateShippedWorkflowVersions(t, pool, now)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: fixtures.Tenant, Subject: "principal:agent-action-author", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org-test", Roles: []string{"comp_admin", "promotion_operator"}, Purposes: []string{"compensation_review"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session:agent-action", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential:agent-action"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), p)
	definition, err := cell.Service.GetIntentDefinition(ctx, &registryv1.GetIntentDefinitionRequest{Definition: &intentsv1.DefinitionReference{IntentTypeId: promotion.IntentType, Version: 1}})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := structpb.NewStruct(chat045PromotionPayload())
	if err != nil {
		t.Fatal(err)
	}
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := fixtures.WorkerRef("omar-reyes")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewAgentActionService(cell)
	if err != nil {
		t.Fatal(err)
	}
	req := AgentActionCompileRequest{Definition: definition.IntentDefinition, Request: &intentsv1.CreateIntentRequest{Definition: definition.IntentDefinition.Reference, Subjects: []*intentsv1.SubjectReference{{SubjectKind: "EMPLOYMENT", SubjectId: worker.Id, AuthorityDomain: "PEOPLE"}}, Request: &intentsv1.TypedPayload{Schema: definition.IntentDefinition.InputSchema, ProtobufWireBytes: wire}, ExecutionMode: intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE}, AgentRunID: "run-action", AgentVersionRef: "agent-action@1", ModelDigest: "sha256:model", Sources: []string{"worker:omar-reyes"}, Taint: []string{"AGENT_DERIVED"}, Uncertainty: "subject to current owner simulation"}
	authority := &agentActionTestAuthority{proposal: req, user: p.Subject(), tenant: p.Tenant().String()}
	if len(unbound) == 0 || !unbound[0] {
		if err := service.BindAuthority(authority); err != nil {
			t.Fatal(err)
		}
	}
	return &agentActionFixture{service: service, cell: cell, db: db, ctx: ctx, req: req, authority: authority}
}

func (f *agentActionFixture) count(t *testing.T, table string) int {
	t.Helper()
	var count int
	// Table names are test-owned literals; callers cannot supply identifiers.
	if table != "workflow_instance" && table != "intent_instance" && table != "work_item" {
		t.Fatal("unsupported test table")
	}
	if f.harness != nil {
		if err := f.harness.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if err := f.db.Conn.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func agentActionSubmission(state AgentActionState) AgentActionSubmitRequest {
	return AgentActionSubmitRequest{IntentID: state.IntentID, ProposalRevisionID: state.ProposalRevisionID, ProposalDigest: state.ProposalDigest, ExpectedInstanceVersion: state.InstanceVersion}
}

func TestTodo_AGENT_036(t *testing.T) {
	if _, err := NewAgentActionService(nil); !errors.Is(err, ErrAgentActionInput) {
		t.Fatalf("nil cell: %v", err)
	}
	var service *AgentActionService
	if _, err := service.Compile(context.Background(), AgentActionCompileRequest{}); !errors.Is(err, ErrAgentActionInput) {
		t.Fatalf("uncomposed compile: %v", err)
	}
	instance := &intentsv1.IntentInstance{IntentId: "intent", InstanceVersion: 2, Lifecycle: &intentsv1.LifecycleDimensions{Execution: intentsv1.ExecutionState_EXECUTION_STATE_COMMITTED, Business: intentsv1.BusinessState_BUSINESS_STATE_IN_PROGRESS, Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_PENDING_OBSERVATION}}
	if state := agentActionState(instance); state.State != "executing" {
		t.Fatalf("unobserved commit reported success: %+v", state)
	}
	instance.Lifecycle.Execution = intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED
	if state := agentActionState(instance); state.State != "needs repair" {
		t.Fatalf("repair state: %+v", state)
	}
	instance.Lifecycle.Execution = intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED
	instance.Lifecycle.Request = intentsv1.RequestState_REQUEST_STATE_REJECTED
	if state := agentActionState(instance); state.State != "failed" {
		t.Fatalf("rejection state: %+v", state)
	}
}

func TestTodo_AGENT_035_Integration(t *testing.T) {
	f := newAgentActionFixture(t)
	draft, err := f.service.Compile(f.ctx, f.req)
	if err != nil {
		t.Fatalf("compile: %v %s", err, promoux013Diagnostic(err))
	}
	if draft.IntentID == "" || draft.ProposalDigest == "" || draft.ProposalRevisionID == "" || draft.State != "draft" || draft.OwnerURL != "/workspace/app/journeys?journey="+draft.IntentID || len(draft.Subjects) != 1 || len(draft.PlannedWrites) == 0 || len(draft.Sources) != 1 || len(draft.Taint) != 1 || draft.Uncertainty != f.req.Uncertainty {
		t.Fatalf("draft: %+v", draft)
	}
	replay, err := f.service.Compile(f.ctx, f.req)
	if err != nil || replay.IntentID != draft.IntentID || replay.ProposalDigest != draft.ProposalDigest {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	p, _ := trust.FromContext(f.ctx)
	now := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	renewed, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: p.Tenant(), Subject: p.Subject(), SubjectKind: p.SubjectKind(), OrganizationScopeID: p.OrganizationScopeID(), Roles: p.Roles(), Purposes: p.Purposes(), AuthenticationMethod: p.AuthenticationMethod(), Assurance: p.Assurance(), SessionRef: "session:agent-action-renewed", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential:agent-action-renewed"})
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := f.service.Compile(trust.WithPrincipal(context.Background(), renewed), f.req)
	if err != nil || refreshed.IntentID != draft.IntentID || refreshed.ProposalDigest != draft.ProposalDigest {
		t.Fatalf("renewed authenticated session duplicated or lost draft: %+v %v", refreshed, err)
	}
	if f.count(t, "intent_instance") != 1 || f.count(t, "workflow_instance") != 0 {
		t.Fatal("compile did not persist exactly one draft without execution")
	}
	got, err := f.cell.Service.GetIntent(f.ctx, &intentsv1.GetIntentRequest{IntentId: draft.IntentID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Intent.GetInitiator().GetPrincipalId() != "principal:agent-action-author" || got.Intent.GetOriginEventRef() == "" {
		t.Fatalf("actor attribution: %v", got.Intent)
	}
	fresh, err := NewAgentActionService(f.cell)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := fresh.Observe(f.ctx, draft.IntentID)
	if err != nil || observed.State != "draft" || len(observed.Sources) != 1 || observed.Uncertainty != f.req.Uncertainty {
		t.Fatalf("recomposed durable observation: %+v %v", observed, err)
	}
}

func TestTodo_AGENT_036_Integration(t *testing.T) {
	f, h, _ := newAgentActionPublishedFixture(t)
	draft, err := f.service.Compile(f.ctx, f.req)
	if err != nil {
		t.Fatal(err)
	}
	// The same person signed in without the cell's execution role cannot start
	// execution through the agent: the agent adds no authority to its user.
	_, proposerCtx := h.engine("hiring-manager")
	if _, err := f.service.Submit(proposerCtx, agentActionSubmission(draft)); !errors.Is(err, workspace.ErrDenied) || f.count(t, "workflow_instance") != 0 {
		t.Fatalf("submission without the execution role: %v, workflows=%d", err, f.count(t, "workflow_instance"))
	}
	submitted, err := f.service.Submit(f.ctx, agentActionSubmission(draft))
	if err != nil {
		t.Fatalf("submit: %v %s", err, promoux013Diagnostic(err))
	}
	if submitted.State != "awaiting approval" || f.count(t, "workflow_instance") != 1 {
		t.Fatalf("normal business approval was not reached: %+v", submitted)
	}
	detail, err := f.cell.Journey.Inspect(f.ctx, draft.IntentID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.Stage != workspace.JourneyStageFinanceApproval || len(detail.WorkItems) == 0 || detail.Ledger != nil {
		t.Fatalf("draft bypassed business approval: %+v", detail.Summary)
	}
	replay, err := f.service.Submit(f.ctx, agentActionSubmission(draft))
	if err != nil || replay.State != "awaiting approval" || f.count(t, "workflow_instance") != 1 {
		t.Fatalf("submit replay duplicated workflow: %+v %v", replay, err)
	}
}

func TestTodo_AGENT_036_Security(t *testing.T) {
	f := newAgentActionFixture(t)
	if _, err := f.service.Compile(context.Background(), f.req); !errors.Is(err, ErrAgentActionApproval) {
		t.Fatalf("unauthenticated draft: %v", err)
	}
	draft, err := f.service.Compile(f.ctx, f.req)
	if err != nil {
		t.Fatal(err)
	}
	req := agentActionSubmission(draft)
	req.ProposalDigest = "sha256:forged-chat-reaction"
	if _, err := f.service.Submit(f.ctx, req); !errors.Is(err, ErrAgentActionApproval) {
		t.Fatalf("forged approval: %v", err)
	}
	req = agentActionSubmission(draft)
	req.ExpectedInstanceVersion++
	if _, err := f.service.Submit(f.ctx, req); !errors.Is(err, ErrAgentActionApproval) {
		t.Fatalf("stale proposal: %v", err)
	}
	if f.count(t, "workflow_instance") != 0 || f.count(t, "work_item") != 0 {
		t.Fatal("refused action caused workflow effects")
	}
}

func TestAgentActionRefusesUnverifiedOrRevokedRun(t *testing.T) {
	t.Run("missing authority", func(t *testing.T) {
		f := newAgentActionFixture(t, true)
		if _, err := f.service.Compile(f.ctx, f.req); !errors.Is(err, ErrAgentActionApproval) {
			t.Fatalf("unverified output accepted: %v", err)
		}
		if f.count(t, "intent_instance") != 0 {
			t.Fatal("missing authority created draft")
		}
	})
	t.Run("changed validated output", func(t *testing.T) {
		f := newAgentActionFixture(t)
		changed := f.req
		changed.Sources = []string{"invented:source"}
		if _, err := f.service.Compile(f.ctx, changed); !errors.Is(err, ErrAgentActionApproval) {
			t.Fatalf("changed model evidence accepted: %v", err)
		}
		if f.count(t, "intent_instance") != 0 {
			t.Fatal("changed output created draft")
		}
	})
	t.Run("revoked after compile", func(t *testing.T) {
		f := newAgentActionFixture(t)
		draft, err := f.service.Compile(f.ctx, f.req)
		if err != nil {
			t.Fatal(err)
		}
		f.authority.refused = true
		if _, err := f.service.Submit(f.ctx, agentActionSubmission(draft)); !errors.Is(err, ErrAgentActionApproval) {
			t.Fatalf("revoked run started workflow: %v", err)
		}
		if f.count(t, "workflow_instance") != 0 || f.count(t, "work_item") != 0 {
			t.Fatal("revocation caused workflow effects")
		}
	})
}

func TestTodo_AGENT_036_Race(t *testing.T) {
	f, _, _ := newAgentActionPublishedFixture(t)
	draft, err := f.service.Compile(f.ctx, f.req)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make([]error, 4)
	for i := range results {
		group.Add(1)
		go func(i int) { defer group.Done(); _, results[i] = f.service.Submit(f.ctx, agentActionSubmission(draft)) }(i)
	}
	group.Wait()
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes == 0 || f.count(t, "workflow_instance") != 1 {
		t.Fatalf("concurrent exact action created wrong effects: successes=%d errors=%v", successes, results)
	}
	observed, err := f.service.Observe(f.ctx, draft.IntentID)
	if err != nil || observed.State != "awaiting approval" {
		t.Fatalf("race durable state: %+v %v", observed, err)
	}
}

func TestTodo_AGENT_036_Mutation(t *testing.T) {
	f := newAgentActionFixture(t)
	f.req.Definition = proto.Clone(f.req.Definition).(*intentsv1.IntentDefinition)
	f.req.Definition.RequiredCapabilityRefs = nil
	if _, err := f.service.Compile(f.ctx, f.req); !errors.Is(err, app.ErrAgentActionDefinition) {
		t.Fatalf("changed served contract: %v", err)
	}
	if f.count(t, "intent_instance") != 0 || f.count(t, "workflow_instance") != 0 {
		t.Fatal("changed contract caused durable effect")
	}
}
