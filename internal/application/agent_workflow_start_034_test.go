package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	agentv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/agent/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

const agent034Purpose = "agent.workflow.start"

// agent034IntentsFake stands in for the intent owner behind
// AgentWorkflowIntentOwner. It records every call so the tests can prove a
// refusal reached zero owner effects.
type agent034IntentsFake struct {
	target                 app.AgentWorkflowTarget
	targetErr              error
	receipt                *intentsv1.ExecutionReceipt
	executeErr             error
	observeErr             error
	resolveCalls           int
	executeCalls           int
	observeCalls           int
	lastObservedIntent     string
	lastObservedCorrelator string
}

func (f *agent034IntentsFake) ResolveAgentWorkflowTarget(_ context.Context, _ *intentsv1.ExecuteIntentRequest) (app.AgentWorkflowTarget, error) {
	f.resolveCalls++
	if f.targetErr != nil {
		return app.AgentWorkflowTarget{}, f.targetErr
	}
	return f.target, nil
}

func (f *agent034IntentsFake) ExecuteIntent(_ context.Context, _ *intentsv1.ExecuteIntentRequest) (*intentsv1.ExecuteIntentResponse, error) {
	f.executeCalls++
	if f.executeErr != nil {
		return nil, f.executeErr
	}
	return &intentsv1.ExecuteIntentResponse{Execution: f.receipt}, nil
}

func (f *agent034IntentsFake) AuthorizeAgentWorkflowObservation(_ context.Context, intentID, correlationID string) error {
	f.observeCalls++
	f.lastObservedIntent, f.lastObservedCorrelator = intentID, correlationID
	return f.observeErr
}

// agent034ControlsFake stands in for the workflow owner's durable control
// projection and records which tenant each read was scoped to.
type agent034ControlsFake struct {
	records map[uuid.UUID]app.WorkflowControlRecord
	err     error
	tenants []values.TenantId
}

func (f *agent034ControlsFake) ReadWorkflowControlRecord(_ context.Context, tenant values.TenantId, id uuid.UUID) (app.WorkflowControlRecord, error) {
	f.tenants = append(f.tenants, tenant)
	if f.err != nil {
		return app.WorkflowControlRecord{}, f.err
	}
	record, ok := f.records[id]
	if !ok {
		return app.WorkflowControlRecord{}, errors.New("agent034: unknown workflow instance")
	}
	return record, nil
}

// agent034VersionsFake is a version.Store keyed by compiled-plan digest.
type agent034VersionsFake struct {
	byDigest       map[string]version.CompiledVersion
	getByDigestIDs []string
}

func (f *agent034VersionsFake) Put(v version.CompiledVersion) error {
	if f.byDigest == nil {
		f.byDigest = make(map[string]version.CompiledVersion)
	}
	f.byDigest[v.CompiledPlanDigest] = v
	return nil
}

func (f *agent034VersionsFake) GetByDigest(digest string) (version.CompiledVersion, bool, error) {
	f.getByDigestIDs = append(f.getByDigestIDs, digest)
	v, ok := f.byDigest[digest]
	return v, ok, nil
}

func (f *agent034VersionsFake) GetActiveForWorkflow(workflowID string) (version.CompiledVersion, bool, error) {
	for _, v := range f.byDigest {
		if v.WorkflowID == workflowID {
			return v, true, nil
		}
	}
	return version.CompiledVersion{}, false, nil
}

func (f *agent034VersionsFake) List(workflowID string) ([]version.CompiledVersion, error) {
	var out []version.CompiledVersion
	for _, v := range f.byDigest {
		if v.WorkflowID == workflowID {
			out = append(out, v)
		}
	}
	return out, nil
}

// agent034DiscoveryFake returns one exact user, subject and field binding.
type agent034DiscoveryFake struct {
	user     agentgate.UserContext
	subjects []agentgate.Subject
	fields   []authz.FieldID
	err      error
}

func (f *agent034DiscoveryFake) Resolve(context.Context, *trust.Principal, string) (agentgate.UserContext, []agentgate.Subject, []authz.FieldID, error) {
	if f.err != nil {
		return agentgate.UserContext{}, nil, nil, f.err
	}
	return f.user, f.subjects, f.fields, nil
}

// agent034PDFFake rules on the exact subject and field set the gate passes
// in, covering every requested binding with an allow effect.
type agent034PDFFake struct {
	allow map[string]bool
	err   error
}

func (f *agent034PDFFake) Authorize(_ context.Context, req agentgate.CapabilityRequest) (agentgate.CapabilityDecision, error) {
	if f.err != nil {
		return agentgate.CapabilityDecision{}, f.err
	}
	key := req.Capability.Definition.Key()
	if !f.allow[key.ID] || key.Version != 1 {
		return agentgate.CapabilityDecision{Capability: key}, nil
	}
	decision := agentgate.CapabilityDecision{Capability: key, Allowed: true}
	for _, subject := range req.Subjects {
		fields := make(map[authz.FieldID]authz.Effect, len(req.Fields))
		for _, field := range req.Fields {
			fields[field] = authz.EffectAllow
		}
		decision.Subjects = append(decision.Subjects, agentgate.SubjectDecision{Subject: subject.Ref, Fields: fields})
	}
	return decision, nil
}

type agent034Fixture struct {
	ctx           context.Context
	now           time.Time
	principal     *trust.Principal
	runtime       *CommonAgentRuntime
	authority     *commonAgentTestAuthority
	record        agentrun.Record
	tenant        uuid.UUID
	manifest      agentmanifest.Manifest
	plan          *workflow.CompiledWorkflow
	planJSON      []byte
	recursive     *workflow.CompiledWorkflow
	recursiveJSON []byte
	gate          *agentgate.Gate
	skill         agentskills.SkillPin
	skillRecord   agentskills.SkillRecord
	discovery     *agent034DiscoveryFake
	pdp           *agent034PDFFake
	intents       *agent034IntentsFake
	controls      *agent034ControlsFake
	versions      *agent034VersionsFake
	service       AgentGovernedWorkflowService
	instanceID    uuid.UUID
	correlation   string
	executeReq    *intentsv1.ExecuteIntentRequest
	startReq      AgentWorkflowStartRequest
}

func agent034PlanJSON(workflowID string) []byte {
	return []byte(`{"workflow_id":"` + workflowID + `","version":2}`)
}

func newAgent034Fixture(t *testing.T) *agent034Fixture {
	t.Helper()
	ctx := context.Background()
	f := &agent034Fixture{correlation: "correlation-034", instanceID: uuid.New(), tenant: uuid.New()}

	agentRuntime, _, authority, now := commonAgentTestRuntime(t)
	f.runtime, f.authority, f.now = agentRuntime, authority, *now

	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "common-tenant", Subject: "user-obo-034", SubjectKind: trust.SubjectKindHuman,
		Purposes: []string{agent034Purpose}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-034", IssuedAt: f.now.Add(-time.Minute), ExpiresAt: f.now.Add(time.Hour), CredentialDigest: "credential-034"})
	if err != nil {
		t.Fatal(err)
	}
	f.principal = principal
	f.ctx = trust.WithPrincipal(ctx, principal)

	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, f.tenant)
	agents := commonAgentOpenIntegrationStore(t, db)

	f.planJSON = agent034PlanJSON("allowed-workflow")
	if f.plan, err = workflow.DecodeCanonicalPlan(f.planJSON); err != nil {
		t.Fatal(err)
	}
	f.recursiveJSON = []byte(`{"workflow_id":"recursive-workflow","version":2,"nodes":[{"id":"call-agent","capability":{"id":"agents.invoke"}}]}`)
	if f.recursive, err = workflow.DecodeCanonicalPlan(f.recursiveJSON); err != nil {
		t.Fatal(err)
	}
	if len(f.recursive.Nodes) != 1 || f.recursive.Nodes[0].Capability == nil || f.recursive.Nodes[0].Capability.ID != "agents.invoke" {
		t.Fatal("recursive fixture plan carries no agent invocation node")
	}

	instructions, err := agents.SaveInstructionContent(ctx, f.tenant, "Start only the pinned governed workflow.")
	if err != nil {
		t.Fatal(err)
	}
	edge := func(plan *workflow.CompiledWorkflow) agentmanifest.Reference {
		return agentmanifest.Reference{ID: "workflows.request_start:" + plan.WorkflowID, Version: uint64(plan.Version), SchemaVersion: 1, Digest: "sha256:" + plan.Digest()}
	}
	ceil := agentmanifest.Reference{ID: "reference-a", Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("c", 64)}
	f.manifest = agentmanifest.Manifest{SchemaVersion: 1, ID: "agent-a", Version: 1, OwnerID: "owner-a", Purpose: agent034Purpose, InstructionsDigest: instructions,
		SourceCeiling: []agentmanifest.Reference{ceil}, ToolCeiling: []agentmanifest.Reference{edge(f.plan), edge(f.recursive)},
		ModelPolicy: ceil, AutonomyCeiling: "ASSISTED", Budget: agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 200, MaxOutputTokens: 100, MaxConcurrentRuns: 1},
		OutputSchema: ceil, EvaluationRefs: []agentmanifest.Reference{ceil}}
	manifestDigest, err := f.manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agents.SaveManifest(ctx, f.tenant, f.manifest, 0); err != nil {
		t.Fatal(err)
	}

	f.skill = agentskills.SkillPin{ID: "skill.workflow.start", Version: 1, Digest: "sha256:" + strings.Repeat("d", 64)}
	f.skillRecord = agentskills.SkillRecord{
		Definition: agentskills.SkillDefinition{ID: f.skill.ID, Version: f.skill.Version, Description: "Governed workflow start", RequiredPurposes: []string{agent034Purpose}},
		Digest:     f.skill.Digest, Status: agentskills.StatusActive,
		ResolvedOperations: []agentskills.ResolvedOperation{
			{Capability: capability.Record{Definition: capability.Definition{ID: "workflows.request_start", Version: 1}}, HasCapability: true},
			{Capability: capability.Record{Definition: capability.Definition{ID: "workflows.read_status", Version: 1}}, HasCapability: true},
		},
	}
	grant := agentgate.SkillGrant{ID: "grant-034", Tenant: "common-tenant", Skill: f.skillRecord.Definition.Key(), Roles: []string{"employee"},
		Population: "employees", OrganizationScopes: []string{"org-a"}, Purposes: []string{agent034Purpose}}
	f.pdp = &agent034PDFFake{allow: map[string]bool{"workflows.request_start": true, "workflows.read_status": true}}
	if f.gate, err = agentgate.New(agentgate.Config{Skills: sourceSkillCatalog{records: []agentskills.SkillRecord{f.skillRecord}},
		Grants: agentgate.StaticGrants{grant}, PDP: f.pdp, Now: func() time.Time { return f.now }}); err != nil {
		t.Fatal(err)
	}
	f.discovery = &agent034DiscoveryFake{
		user: agentgate.UserContext{Principal: principal, Population: "employees", Roles: []string{"employee"}, OrganizationScopes: []string{"org-a"}},
		subjects: []agentgate.Subject{{Ref: values.EntityRef{Tenant: "common-tenant", Kind: "worker", Id: uuid.NewString()},
			Organization: authz.OrgUnitRef{Tenant: "common-tenant", ID: "org-a"}}},
		fields: []authz.FieldID{authz.FieldWorkerNumber},
	}

	f.executeReq = &intentsv1.ExecuteIntentRequest{IntentId: "intent-034", ExpectedInstanceVersion: 3, IdempotencyKey: "idem-034"}
	f.intents = &agent034IntentsFake{
		target:  app.AgentWorkflowTarget{Selection: runtime.WorkflowSelection{WorkflowID: f.plan.WorkflowID, Plan: f.plan}, CorrelationID: f.correlation},
		receipt: &intentsv1.ExecutionReceipt{InstanceId: f.instanceID.String()},
	}
	f.controls = &agent034ControlsFake{records: map[uuid.UUID]app.WorkflowControlRecord{
		f.instanceID: {Instance: runtime.Instance{TenantID: f.tenant, InstanceID: f.instanceID, WorkflowID: f.plan.WorkflowID,
			WorkflowVersion: f.plan.Version, CompiledPlanHash: f.plan.Digest(), RuntimeStatus: runtime.InstanceRunning,
			CurrentNodeIDs: []string{"start"}, InstanceVersion: 1, CorrelationID: f.correlation}},
	}}
	f.versions = &agent034VersionsFake{byDigest: map[string]version.CompiledVersion{
		f.plan.Digest():      {WorkflowID: f.plan.WorkflowID, CompiledPlanDigest: f.plan.Digest(), CanonicalPlanBytes: f.planJSON},
		f.recursive.Digest(): {WorkflowID: f.recursive.WorkflowID, CompiledPlanDigest: f.recursive.Digest(), CanonicalPlanBytes: f.recursiveJSON},
	}}
	f.service = AgentGovernedWorkflowService{Evidence: f.runtime, Current: f.runtime, Agents: agents,
		ResolveTenant: func(tenant string) uuid.UUID {
			if tenant == "common-tenant" {
				return f.tenant
			}
			return uuid.Nil
		},
		Gate: f.gate, Context: f.discovery, Intents: f.intents, Controls: f.controls, Versions: f.versions,
		Now: func() time.Time { return f.now }}

	f.record = f.admit(t, agent034Admission{key: "agent034-chat-1", ref: "chat/dm/1", agent: agentrun.VersionRef{AgentID: "agent-a", Version: "1", Digest: manifestDigest},
		principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-principal-034", InvokerID: principal.Subject(), DelegatedCredentialRef: "delegated-cred-034"}})
	f.startReq = AgentWorkflowStartRequest{RunID: f.record.ID, Intent: f.executeReq, Skill: f.skill, StepID: "step-1"}
	return f
}

type agent034Admission struct {
	key       string
	ref       string
	kind      agentrun.SourceKind
	agent     agentrun.VersionRef
	principal agentrun.PrincipalChain
}

func (f *agent034Fixture) admit(t *testing.T, admission agent034Admission) agentrun.Record {
	t.Helper()
	request := commonAgentTestRequest(f.now)
	request.Purpose = agent034Purpose
	request.Source = agentrun.SourceIdentity{TenantID: "common-tenant", Kind: admission.kind, Key: admission.key, Ref: admission.ref}
	if request.Source.Kind == "" {
		request.Source.Kind = agentrun.SourceChat
	}
	request.Agent, request.Principal = admission.agent, admission.principal
	record, created, err := f.runtime.Admit(f.ctx, request)
	if err != nil || !created || record.Decision != agentrun.DecisionAccepted {
		t.Fatalf("admission %q = %+v created=%t err=%v", admission.key, record, created, err)
	}
	return record
}

func TestTodo_AGENT_034(t *testing.T) {
	f := newAgent034Fixture(t)

	receipt, err := f.service.RequestStart(f.ctx, f.startReq)
	if err != nil {
		t.Fatalf("RequestStart: %v", err)
	}
	if receipt.GetInstanceId() != f.instanceID.String() {
		t.Fatalf("receipt instance = %q, want %q", receipt.GetInstanceId(), f.instanceID)
	}
	if f.intents.resolveCalls != 1 || f.intents.executeCalls != 1 {
		t.Fatalf("owner calls = resolve %d execute %d, want 1 and 1", f.intents.resolveCalls, f.intents.executeCalls)
	}
	if len(f.controls.tenants) != 1 || f.controls.tenants[0] != "common-tenant" {
		t.Fatalf("control reads scoped to %v, want [common-tenant]", f.controls.tenants)
	}

	observed, err := f.service.Inspect(f.ctx, f.startReq, f.instanceID)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	inst := observed.Instance
	if inst.InstanceID != f.instanceID || inst.WorkflowID != f.plan.WorkflowID || inst.WorkflowVersion != f.plan.Version ||
		inst.CompiledPlanHash != f.plan.Digest() || inst.RuntimeStatus != runtime.InstanceRunning || inst.InstanceVersion != 1 ||
		len(inst.CurrentNodeIDs) != 1 || inst.CurrentNodeIDs[0] != "start" || inst.CorrelationID != f.correlation {
		t.Fatalf("observed state = %+v, want the workflow owner's record", inst)
	}
	if f.intents.observeCalls != 1 || f.intents.lastObservedIntent != "intent-034" || f.intents.lastObservedCorrelator != f.correlation {
		t.Fatalf("observation authorization calls = %d for %q/%q", f.intents.observeCalls, f.intents.lastObservedIntent, f.intents.lastObservedCorrelator)
	}
	if len(f.versions.getByDigestIDs) != 1 || f.versions.getByDigestIDs[0] != f.plan.Digest() {
		t.Fatalf("version pins resolved = %v, want [%s]", f.versions.getByDigestIDs, f.plan.Digest())
	}

	t.Run("a recursive workflow start is refused with zero execution", func(t *testing.T) {
		intents := &agent034IntentsFake{
			target:  app.AgentWorkflowTarget{Selection: runtime.WorkflowSelection{WorkflowID: f.recursive.WorkflowID, Plan: f.recursive}, CorrelationID: f.correlation},
			receipt: &intentsv1.ExecutionReceipt{InstanceId: f.instanceID.String()},
		}
		service := f.service
		service.Intents = intents
		// The manifest pins this edge, so only the cause-depth limit can refuse.
		if _, err := service.RequestStart(f.ctx, f.startReq); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("recursive start = %v, want denied", err)
		}
		if intents.resolveCalls != 1 || intents.executeCalls != 0 {
			t.Fatalf("owner calls = resolve %d execute %d, want 1 and 0", intents.resolveCalls, intents.executeCalls)
		}
	})

	t.Run("a recursive workflow cannot be inspected into an execution claim", func(t *testing.T) {
		recursiveID := uuid.New()
		controls := &agent034ControlsFake{records: map[uuid.UUID]app.WorkflowControlRecord{
			recursiveID: {Instance: runtime.Instance{TenantID: f.tenant, InstanceID: recursiveID, WorkflowID: f.recursive.WorkflowID,
				WorkflowVersion: f.recursive.Version, CompiledPlanHash: f.recursive.Digest(), RuntimeStatus: runtime.InstanceRunning,
				CurrentNodeIDs: []string{"call-agent"}, InstanceVersion: 1, CorrelationID: f.correlation}},
		}}
		service := f.service
		service.Controls = controls
		if _, err := service.Inspect(f.ctx, f.startReq, recursiveID); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("recursive inspect = %v, want denied", err)
		}
	})

	t.Run("a post is never reported as an execution", func(t *testing.T) {
		mismatch := func(mutate func(*runtime.Instance)) func(*testing.T) {
			return func(t *testing.T) {
				intents := &agent034IntentsFake{target: f.intents.target, receipt: &intentsv1.ExecutionReceipt{InstanceId: f.instanceID.String()}}
				record := f.controls.records[f.instanceID]
				mutate(&record.Instance)
				service := f.service
				service.Intents, service.Controls = intents, &agent034ControlsFake{records: map[uuid.UUID]app.WorkflowControlRecord{f.instanceID: record}}
				if _, err := service.RequestStart(f.ctx, f.startReq); !errors.Is(err, ErrAgentWorkflowStartDenied) {
					t.Fatalf("mismatched execution claim = %v, want denied", err)
				}
				if intents.executeCalls != 1 {
					t.Fatalf("execute calls = %d, want the one owner submission whose receipt was then disowned", intents.executeCalls)
				}
			}
		}
		t.Run("workflow", mismatch(func(inst *runtime.Instance) { inst.WorkflowID = "other-workflow" }))
		t.Run("digest", mismatch(func(inst *runtime.Instance) { inst.CompiledPlanHash = strings.Repeat("e", 64) }))
		t.Run("correlation", mismatch(func(inst *runtime.Instance) { inst.CorrelationID = "correlation-other" }))
	})

	t.Run("an unknown execution identity is not an execution", func(t *testing.T) {
		ghost := uuid.New()
		intents := &agent034IntentsFake{target: f.intents.target, receipt: &intentsv1.ExecutionReceipt{InstanceId: ghost.String()}}
		service := f.service
		service.Intents = intents
		if _, err := service.RequestStart(f.ctx, f.startReq); err == nil {
			t.Fatal("unknown instance accepted as an execution")
		}
	})

	t.Run("agent text has no start path", func(t *testing.T) {
		before := f.intents.executeCalls
		for name, req := range map[string]AgentWorkflowStartRequest{
			"empty":        {},
			"missing run":  {Intent: f.executeReq, Skill: f.skill, StepID: "step-1"},
			"missing text": {RunID: f.record.ID, Skill: f.skill, StepID: "step-1"},
		} {
			if _, err := f.service.RequestStart(f.ctx, req); !errors.Is(err, ErrAgentWorkflowStartDenied) {
				t.Fatalf("%s request = %v, want denied", name, err)
			}
		}
		if f.intents.executeCalls != before {
			t.Fatalf("execute calls moved to %d on untyped input", f.intents.executeCalls)
		}
	})

	t.Run("an unpinned workflow edge is refused before submission", func(t *testing.T) {
		other, err := workflow.DecodeCanonicalPlan(agent034PlanJSON("other-workflow"))
		if err != nil {
			t.Fatal(err)
		}
		intents := &agent034IntentsFake{
			target:  app.AgentWorkflowTarget{Selection: runtime.WorkflowSelection{WorkflowID: other.WorkflowID, Plan: other}, CorrelationID: f.correlation},
			receipt: &intentsv1.ExecutionReceipt{InstanceId: f.instanceID.String()},
		}
		service := f.service
		service.Intents = intents
		if _, err := service.RequestStart(f.ctx, f.startReq); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("unpinned start = %v, want denied", err)
		}
		if intents.executeCalls != 0 {
			t.Fatalf("execute calls = %d, want 0", intents.executeCalls)
		}
	})

	t.Run("an unadmitted intent starts nothing", func(t *testing.T) {
		intents := &agent034IntentsFake{targetErr: errors.New("app: unknown intent"), receipt: f.intents.receipt}
		service := f.service
		service.Intents = intents
		if _, err := service.RequestStart(f.ctx, f.startReq); err == nil {
			t.Fatal("unadmitted intent started a workflow")
		}
		if intents.executeCalls != 0 {
			t.Fatalf("execute calls = %d, want 0", intents.executeCalls)
		}
	})

	t.Run("typed capabilities are registered under the workflow owner", func(t *testing.T) {
		registry := capability.NewRegistry()
		if err := RegisterAgentGovernedWorkflowCapabilities(registry, f.service); err != nil {
			t.Fatal(err)
		}
		start, ok := registry.Lookup(capability.Key{ID: "workflows.request_start", Version: 1})
		if !ok {
			t.Fatal("workflows.request_start is not registered")
		}
		if start.Definition.EffectClass != capability.EffectInternalMutation || !start.Definition.AgentEligible ||
			start.Definition.OwnerDomain != "workflows" || start.Definition.TestRef != "TestTodo_AGENT_034" ||
			start.Definition.RequestSchema.ProtobufFullName != "hcmnext.agent.v1.AgentWorkflowStartRequest" {
			t.Fatalf("start definition = %+v", start.Definition)
		}
		status, ok := registry.Lookup(capability.Key{ID: "workflows.read_status", Version: 1})
		if !ok {
			t.Fatal("workflows.read_status is not registered")
		}
		if status.Definition.EffectClass != capability.EffectReadOnly || !status.Definition.AgentEligible ||
			status.Definition.OwnerDomain != "workflows" || status.Definition.TestRef != "TestTodo_AGENT_034" {
			t.Fatalf("status definition = %+v", status.Definition)
		}
	})
}

func agent034WireRequest(f *agent034Fixture) *agentv1.AgentWorkflowStartRequest {
	return &agentv1.AgentWorkflowStartRequest{RunId: f.record.ID, Intent: f.executeReq,
		Skill: &agentv1.WorkflowStartSkillPin{Id: f.skill.ID, Version: uint64(f.skill.Version), Digest: f.skill.Digest}, StepId: "step-1"}
}

func TestTodo_AGENT_034_Integration(t *testing.T) {
	f := newAgent034Fixture(t)

	registry := capability.NewRegistry()
	if err := RegisterAgentGovernedWorkflowCapabilities(registry, f.service); err != nil {
		t.Fatal(err)
	}
	if err := RegisterAgentGovernedWorkflowCapabilities(registry, f.service); err == nil {
		t.Fatal("registering the governed workflow capabilities twice succeeded")
	}

	wire := agent034WireRequest(f)
	converted, err := agentWorkflowStartFromProto(wire)
	if err != nil {
		t.Fatalf("wire conversion: %v", err)
	}
	if converted.RunID != f.record.ID || converted.Intent != f.executeReq || converted.Skill != f.skill || converted.StepID != "step-1" {
		t.Fatalf("converted request = %+v, want the exact wire bindings", converted)
	}

	receipt, err := f.service.RequestStart(f.ctx, converted)
	if err != nil {
		t.Fatalf("RequestStart: %v", err)
	}
	result := &agentv1.AgentWorkflowStartResult{Execution: receipt}
	if result.GetExecution().GetInstanceId() != f.instanceID.String() {
		t.Fatalf("wire result instance = %q, want %q", result.GetExecution().GetInstanceId(), f.instanceID)
	}

	statusWire := &agentv1.AgentWorkflowStatusRequest{Authority: wire, InstanceId: f.instanceID.String()}
	authority, err := agentWorkflowStartFromProto(statusWire.GetAuthority())
	if err != nil {
		t.Fatalf("status authority conversion: %v", err)
	}
	instanceID, err := uuid.Parse(statusWire.GetInstanceId())
	if err != nil {
		t.Fatalf("status instance identity: %v", err)
	}
	observed, err := f.service.Inspect(f.ctx, authority, instanceID)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	state := &agentv1.AgentWorkflowObservedState{InstanceId: observed.Instance.InstanceID.String(), WorkflowId: observed.Instance.WorkflowID,
		WorkflowVersion: observed.Instance.WorkflowVersion, CompiledPlanDigest: observed.Instance.CompiledPlanHash,
		RuntimeStatus: string(observed.Instance.RuntimeStatus), InstanceVersion: observed.Instance.InstanceVersion,
		CurrentNodeIds: append([]string(nil), observed.Instance.CurrentNodeIDs...)}
	if state.GetInstanceId() != f.instanceID.String() || state.GetWorkflowId() != f.plan.WorkflowID ||
		state.GetWorkflowVersion() != f.plan.Version || state.GetCompiledPlanDigest() != f.plan.Digest() ||
		state.GetRuntimeStatus() != string(runtime.InstanceRunning) || state.GetInstanceVersion() != 1 ||
		len(state.GetCurrentNodeIds()) != 1 || state.GetCurrentNodeIds()[0] != "start" {
		t.Fatalf("observed wire state = %+v, want the workflow owner's record", state)
	}

	t.Run("the wire boundary admits only typed authority", func(t *testing.T) {
		before := f.intents.executeCalls
		broken := func(mutate func(*agentv1.AgentWorkflowStartRequest)) *agentv1.AgentWorkflowStartRequest {
			req := agent034WireRequest(f)
			mutate(req)
			return req
		}
		for name, req := range map[string]*agentv1.AgentWorkflowStartRequest{
			"missing intent": broken(func(r *agentv1.AgentWorkflowStartRequest) { r.Intent = nil }),
			"missing skill":  broken(func(r *agentv1.AgentWorkflowStartRequest) { r.Skill = nil }),
			"unpinned skill": broken(func(r *agentv1.AgentWorkflowStartRequest) { r.Skill.Version = 0 }),
			"overflowed pin": broken(func(r *agentv1.AgentWorkflowStartRequest) { r.Skill.Version = uint64(1) << 32 }),
			"stale pin":      broken(func(r *agentv1.AgentWorkflowStartRequest) { r.Skill.Digest = "sha256:" + strings.Repeat("0", 64) }),
			"missing step":   broken(func(r *agentv1.AgentWorkflowStartRequest) { r.StepId = "  " }),
			"missing run":    broken(func(r *agentv1.AgentWorkflowStartRequest) { r.RunId = "" }),
			"missing idem": broken(func(r *agentv1.AgentWorkflowStartRequest) {
				r.Intent = &intentsv1.ExecuteIntentRequest{IntentId: "intent-034"}
			}),
			"unknown intent": broken(func(r *agentv1.AgentWorkflowStartRequest) {
				r.Intent = &intentsv1.ExecuteIntentRequest{IdempotencyKey: "idem-034"}
			}),
		} {
			converted, err := agentWorkflowStartFromProto(req)
			if err != nil {
				if !errors.Is(err, ErrAgentWorkflowStartDenied) {
					t.Fatalf("%s wire conversion = %v, want denied", name, err)
				}
				continue
			}
			if _, err := f.service.RequestStart(f.ctx, converted); err == nil {
				t.Fatalf("%s wire request started a workflow", name)
			}
		}
		if _, err := agentWorkflowStartFromProto(nil); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("nil wire request = %v, want denied", err)
		}
		if f.intents.executeCalls != before {
			t.Fatalf("execute calls moved to %d on broken wire input", f.intents.executeCalls)
		}
	})

	t.Run("an unparseable observed identity never reaches the owner", func(t *testing.T) {
		if _, err := uuid.Parse("not-a-workflow-instance"); err == nil {
			t.Fatal("malformed instance identity parsed")
		}
		if _, err := f.service.Inspect(f.ctx, f.startReq, uuid.Nil); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("nil inspect identity = %v, want denied", err)
		}
	})
}

func agent034Principal(t *testing.T, now time.Time, tenant, subject string, kind trust.SubjectKind) *trust.Principal {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: kind,
		Purposes: []string{agent034Purpose}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-034", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "credential-034"})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func TestTodo_AGENT_034_Security(t *testing.T) {
	newDeniedFixture := func(t *testing.T) *agent034Fixture {
		t.Helper()
		f := newAgent034Fixture(t)
		t.Cleanup(func() {
			if f.intents.executeCalls != 0 {
				t.Fatalf("execute calls = %d, want 0 after a denied start", f.intents.executeCalls)
			}
		})
		return f
	}

	t.Run("a foreign tenant cannot use another tenant's admission", func(t *testing.T) {
		f := newDeniedFixture(t)
		foreign := trust.WithPrincipal(context.Background(), agent034Principal(t, f.now, "other-tenant", f.principal.Subject(), trust.SubjectKindHuman))
		if _, err := f.service.RequestStart(foreign, f.startReq); err == nil {
			t.Fatal("foreign tenant started a workflow")
		}
		if _, err := f.service.Inspect(foreign, f.startReq, f.instanceID); err == nil {
			t.Fatal("foreign tenant inspected a workflow")
		}
	})

	t.Run("a non-human caller cannot start or inspect", func(t *testing.T) {
		f := newDeniedFixture(t)
		service := trust.WithPrincipal(context.Background(), agent034Principal(t, f.now, "common-tenant", "service-034", trust.SubjectKindService))
		if _, err := f.service.RequestStart(service, f.startReq); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("service start = %v, want denied", err)
		}
		if _, err := f.service.Inspect(service, f.startReq, f.instanceID); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("service inspect = %v, want denied", err)
		}
	})

	t.Run("a caller who is not the invoker cannot start", func(t *testing.T) {
		f := newDeniedFixture(t)
		other := trust.WithPrincipal(context.Background(), agent034Principal(t, f.now, "common-tenant", "user-other", trust.SubjectKindHuman))
		if _, err := f.service.RequestStart(other, f.startReq); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("borrowed admission start = %v, want denied", err)
		}
	})

	t.Run("a revoked admission cannot start", func(t *testing.T) {
		f := newDeniedFixture(t)
		f.authority.revoked = true
		t.Cleanup(func() { f.authority.revoked = false })
		if _, err := f.service.RequestStart(f.ctx, f.startReq); err == nil {
			t.Fatal("revoked admission started a workflow")
		}
		if _, err := f.service.Inspect(f.ctx, f.startReq, f.instanceID); err == nil {
			t.Fatal("revoked admission inspected a workflow")
		}
	})

	t.Run("a revoked grant cannot start", func(t *testing.T) {
		f := newDeniedFixture(t)
		gate, err := agentgate.New(agentgate.Config{Skills: sourceSkillCatalog{records: []agentskills.SkillRecord{f.skillRecord}},
			Grants: agentgate.StaticGrants{}, PDP: f.pdp, Now: func() time.Time { return f.now }})
		if err != nil {
			t.Fatal(err)
		}
		service := f.service
		service.Gate = gate
		if _, err := service.RequestStart(f.ctx, f.startReq); err == nil {
			t.Fatal("ungranted skill started a workflow")
		}
	})

	t.Run("a revoked capability cannot start or inspect", func(t *testing.T) {
		f := newDeniedFixture(t)
		gate, err := agentgate.New(agentgate.Config{Skills: sourceSkillCatalog{records: []agentskills.SkillRecord{f.skillRecord}},
			Grants: agentgate.StaticGrants{{ID: "grant-034", Tenant: "common-tenant", Skill: f.skillRecord.Definition.Key(), Roles: []string{"employee"},
				Population: "employees", OrganizationScopes: []string{"org-a"}, Purposes: []string{agent034Purpose}}},
			PDP: &agent034PDFFake{allow: map[string]bool{}}, Now: func() time.Time { return f.now }})
		if err != nil {
			t.Fatal(err)
		}
		service := f.service
		service.Gate = gate
		if _, err := service.RequestStart(f.ctx, f.startReq); err == nil {
			t.Fatal("denied capability started a workflow")
		}
		if _, err := service.Inspect(f.ctx, f.startReq, f.instanceID); err == nil {
			t.Fatal("denied capability inspected a workflow")
		}
	})

	t.Run("a forged agent digest cannot start", func(t *testing.T) {
		f := newDeniedFixture(t)
		forged := f.admit(t, agent034Admission{key: "agent034-chat-forged", ref: "chat/dm/2",
			agent:     agentrun.VersionRef{AgentID: "agent-a", Version: "1", Digest: "sha256:" + strings.Repeat("f", 64)},
			principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-principal-034", InvokerID: f.principal.Subject(), DelegatedCredentialRef: "delegated-cred-034"}})
		req := f.startReq
		req.RunID = forged.ID
		if _, err := f.service.RequestStart(f.ctx, req); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("forged digest start = %v, want denied", err)
		}
	})

	t.Run("an unknown agent version cannot start", func(t *testing.T) {
		f := newDeniedFixture(t)
		unknown := f.admit(t, agent034Admission{key: "agent034-chat-unknown", ref: "chat/dm/3",
			agent:     agentrun.VersionRef{AgentID: "unknown-agent", Version: "1", Digest: "sha256:" + strings.Repeat("1", 64)},
			principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-principal-034", InvokerID: f.principal.Subject(), DelegatedCredentialRef: "delegated-cred-034"}})
		req := f.startReq
		req.RunID = unknown.ID
		if _, err := f.service.RequestStart(f.ctx, req); err == nil {
			t.Fatal("unknown agent started a workflow")
		}
	})

	t.Run("a sponsored run cannot start", func(t *testing.T) {
		f := newDeniedFixture(t)
		digest, err := f.manifest.Digest()
		if err != nil {
			t.Fatal(err)
		}
		sponsored := f.admit(t, agent034Admission{key: "agent034-schedule-1", ref: "schedule/7/firing/3", kind: agentrun.SourceSchedule,
			agent:     agentrun.VersionRef{AgentID: "agent-a", Version: "1", Digest: digest},
			principal: agentrun.PrincipalChain{Mode: agentrun.ModeSponsored, AgentPrincipalID: "agent-principal-034", SponsorID: "sponsor-034"}})
		req := f.startReq
		req.RunID = sponsored.ID
		if _, err := f.service.RequestStart(f.ctx, req); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("sponsored start = %v, want denied", err)
		}
	})

	t.Run("a workflow-sourced run cannot start another workflow", func(t *testing.T) {
		f := newDeniedFixture(t)
		digest, err := f.manifest.Digest()
		if err != nil {
			t.Fatal(err)
		}
		nested := f.admit(t, agent034Admission{key: "agent034-workflow-1", ref: "workflow:instance:node", kind: agentrun.SourceWorkflow,
			agent:     agentrun.VersionRef{AgentID: "agent-a", Version: "1", Digest: digest},
			principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-principal-034", InvokerID: f.principal.Subject(), DelegatedCredentialRef: "delegated-cred-034"}})
		req := f.startReq
		req.RunID = nested.ID
		if _, err := f.service.RequestStart(f.ctx, req); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("workflow-sourced start = %v, want denied", err)
		}
	})

	t.Run("observing another intent's execution is refused", func(t *testing.T) {
		f := newDeniedFixture(t)
		f.intents.observeErr = errors.New("app: workflow does not belong to the admitted intent")
		if _, err := f.service.Inspect(f.ctx, f.startReq, f.instanceID); err == nil {
			t.Fatal("foreign execution observed")
		}
		if f.intents.observeCalls != 1 || f.intents.lastObservedIntent != "intent-034" || f.intents.lastObservedCorrelator != f.correlation {
			t.Fatalf("observation authorization calls = %d for %q/%q", f.intents.observeCalls, f.intents.lastObservedIntent, f.intents.lastObservedCorrelator)
		}
	})

	t.Run("an unconfigured observation store cannot inspect", func(t *testing.T) {
		f := newDeniedFixture(t)
		service := f.service
		service.Versions = nil
		if _, err := service.Inspect(f.ctx, f.startReq, f.instanceID); !errors.Is(err, ErrAgentWorkflowStartDenied) {
			t.Fatalf("unconfigured inspect = %v, want denied", err)
		}
	})
}
