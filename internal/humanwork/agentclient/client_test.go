package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/outbound"
)

var fixedNow = time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

const (
	tenantKey = "harborcare-demo"
	readCap   = "hcmnext.people.read_worker"
	purpose   = "agent.lookup"
)

// memoryScopers hands each tenant its own in-memory stores, as the durable
// stores' ForTenant does.
type memoryScopers struct {
	mu     sync.Mutex
	grants map[values.TenantId]*agentdelegation.MemoryGrantStore
	tasks  map[values.TenantId]*agentrun.MemoryStore
}

type grantScoper struct{ *memoryScopers }
type taskScoper struct{ *memoryScopers }

func (s grantScoper) ForTenant(_ context.Context, tenant values.TenantId) (agentdelegation.GrantStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.grants[tenant] == nil {
		s.grants[tenant] = agentdelegation.NewMemoryGrantStore()
	}
	return s.grants[tenant], nil
}

func (s taskScoper) ForTenant(_ context.Context, tenant values.TenantId) (agentrun.TaskStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tasks[tenant] == nil {
		s.tasks[tenant] = agentrun.NewMemoryStore()
	}
	return s.tasks[tenant], nil
}

// idleOwner is never reached: these tests only start and read tasks.
type idleOwner struct{}

func (idleOwner) Prepare(context.Context, agentsystem.PrepareRequest) (agentsystem.Prepared, error) {
	return agentsystem.Prepared{}, errors.New("not used")
}
func (idleOwner) Invoke(context.Context, agentsystem.Invocation) (agentsystem.Result, error) {
	return agentsystem.Result{}, errors.New("not used")
}
func (idleOwner) Retain(context.Context, agentrun.AgentTask, agentrun.PlanStep, string, agentsecurity.QuarantineExtraction) error {
	return errors.New("not used")
}
func (idleOwner) Verify(context.Context, agentrun.AgentTask, agentrun.PlanStep, agentrun.StepResult) error {
	return errors.New("not used")
}

type passRedactor struct{}

func (passRedactor) Redact(_ context.Context, text string, _ []string) (agentmodel.Redaction, error) {
	return agentmodel.Redaction{Text: text}, nil
}

func authority(userID string, tenant values.TenantId) agentdelegation.UserAuthority {
	return agentdelegation.UserAuthority{UserID: userID, Active: true, Authority: trust.AuthorityScope{
		Tenant: tenant, OrganizationScopeID: "org:harborcare-demo:people-ops",
		Capabilities: []string{readCap}, Resources: []string{"worker:42"}, Fields: []string{"display_name"},
		Purposes: []string{purpose}, Assurance: trust.AssuranceSubstantial,
		NotBefore: fixedNow.Add(-time.Hour), ExpiresAt: fixedNow.Add(30 * 24 * time.Hour),
	}}
}

func controlAuthority(userID string, tenant values.TenantId) agentdelegation.UserAuthority {
	return agentdelegation.UserAuthority{UserID: userID, Active: true, Authority: trust.AuthorityScope{
		Tenant: tenant, OrganizationScopeID: "org:harborcare-demo:people-ops",
		Capabilities: []string{"hcmnext.agent.read_own_worker_state"},
		Purposes:     []string{"agent.self_service"}, Assurance: trust.AssuranceSubstantial,
		NotBefore: fixedNow.Add(-time.Hour), ExpiresAt: fixedNow.Add(30 * 24 * time.Hour),
	}}
}

func skills(t *testing.T) *agentskills.Registry {
	t.Helper()
	caps := capability.NewRegistry()
	def := capability.Definition{
		ID: readCap, Version: 1, OwnerDomain: "people",
		RequestSchema:  capability.SchemaRef{SchemaID: readCap + ".request", Version: 1, ProtobufFullName: "test.Request"},
		ResponseSchema: capability.SchemaRef{SchemaID: readCap + ".response", Version: 1, ProtobufFullName: "test.Response"},
		ErrorSchema:    capability.SchemaRef{SchemaID: readCap + ".error", Version: 1, ProtobufFullName: "test.Error"},
		EffectClass:    capability.EffectReadOnly, RiskClass: "LOW", IdempotencyPolicyRef: "idem:test", AgentEligible: true,
		AuthZScopeRef: "scope:test", LegalBasisRef: "legal:test", EntitlementRef: "entitlement:test",
		SLOClassRef: "slo:test", TestRef: "test:capability",
	}
	if err := caps.Register(def, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	registry := agentskills.NewRegistry(caps)
	if err := registry.Publish(agentskills.SkillDefinition{
		ID: "skill.lookup", Version: 1, Owner: "people", Description: "Look up a worker",
		InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`),
		Operations:     []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: readCap, Version: 1}}},
		SideEffectTier: agentskills.TierRead, RequiredPurposes: []string{purpose},
		DataClassesRead: []string{"PUBLIC"}, IdempotencyRule: "read-only", CostClass: "LOW", EvalRefs: []string{"eval:x"},
	}); err != nil {
		t.Fatal(err)
	}
	return registry
}

func egress(t *testing.T) *agentegress.Evaluator {
	t.Helper()
	trustPolicy, err := outbound.NewPolicy(outbound.Destination{Name: "model.eu", TrustBundleRef: "bundle:model:v1", Purposes: []string{purpose}, DataClasses: []string{string(trustdlp.ClassPublic)}})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := trustdlp.NewPolicy(trustPolicy, trustdlp.Clearance{Destination: "model.eu", Classes: []trustdlp.DataClass{trustdlp.ClassPublic}, Decision: trustdlp.Allow})
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := trustdlp.NewInspector()
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := agentegress.NewEvaluator(policy, inspector, trustdlp.NewReceiptLog())
	if err != nil {
		t.Fatal(err)
	}
	return evaluator
}

// newPlatform composes the real agent runtime over in-memory stores.
func newPlatform(t *testing.T) *agentsystem.Platform {
	t.Helper()
	ledger, err := agentbudget.NewWithClock(agentbudget.Policy{
		TaskDefault:   agentbudget.Limits{Steps: 50, Tokens: 100000, WallClock: time.Hour, SpendMicros: 1_000_000},
		UserDaily:     agentbudget.Limits{Steps: 500, Tokens: 1_000_000, WallClock: 10 * time.Hour, SpendMicros: 10_000_000},
		TenantMonthly: agentbudget.Limits{Steps: 5000, Tokens: 10_000_000, WallClock: 100 * time.Hour, SpendMicros: 100_000_000},
	}, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	scopers := &memoryScopers{grants: map[values.TenantId]*agentdelegation.MemoryGrantStore{}, tasks: map[values.TenantId]*agentrun.MemoryStore{}}
	platform, err := agentsystem.NewPlatform(agentsystem.Config{
		Skills: skills(t), Budget: ledger, Audit: agentaudit.NewMemoryStore(), Redactor: passRedactor{}, Egress: egress(t),
		Grants: grantScoper{scopers}, Tasks: taskScoper{scopers},
		Authority: agentdelegation.ResolverFunc(func(userID string, tenant values.TenantId, purpose string, _ time.Time) (agentdelegation.UserAuthority, error) {
			if purpose == "agent.self_service" {
				return controlAuthority(userID, tenant), nil
			}
			return authority(userID, tenant), nil
		}),
		Owner:       idleOwner{},
		TokenSecret: []byte("0123456789abcdef0123456789abcdef"), Audience: "tool-gateway", Workload: "workload/agent-worker",
		ModelEstimate: agentbudget.Usage{Steps: 1, Tokens: 1000, WallClock: time.Minute, SpendMicros: 100_000},
		Clock:         func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return platform
}

func startTask(t *testing.T, platform *agentsystem.Platform, tenant values.TenantId, taskID, userID, goal string, confirm bool) {
	t.Helper()
	runner, err := platform.ForTenant(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	task, err := runner.StartTask(context.Background(), agentsystem.StartRequest{
		TaskID: taskID, UserID: userID, AgentVersion: "agent-v1", InstallationID: "install-1", Purpose: purpose,
		OrganizationScopeID: "org:harborcare-demo:people-ops", Goal: goal,
		Steps:         []agentrun.PlanStep{{ID: "read", Type: agentrun.StepRead, SkillID: "skill.lookup", SkillVersion: 1, ExpectedOutput: "worker", Tier: agentrun.TierRead}},
		UserAuthority: authority(userID, tenant).Authority, Lifetime: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if confirm {
		if _, err := runner.Runtime.ConfirmPlan(context.Background(), task.ID, userID, task.Version, fixedNow); err != nil {
			t.Fatalf("ConfirmPlan: %v", err)
		}
	}
}

func agentsPageHTML(t *testing.T, snapshot productui.AgentSnapshot) string {
	t.Helper()
	view := productui.ApplyLocale(productui.NewView(productui.PageAgents, tenantKey, "principal", ""), productui.ResolveProductLocale("en-US"))
	view = productui.ApplyAgentsAvailability(view, productui.AgentsAvailabilityProjection{Enabled: true, Snapshot: snapshot})
	markup, err := ui.RenderToString(productui.BuildAgentsSurface(view))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_UXBLIND_122_Integration(t *testing.T) {
	platform := newPlatform(t)
	startTask(t, platform, tenantKey, "task-linh", "hc-051-linh-tran", "Summarise my open onboarding requests", true)
	startTask(t, platform, tenantKey, "task-rafael", "hc-050-rafael-torres", "Draft the payroll variance memo", false)
	client := FromPlatformWithControls(platform, fixedAgentSetting{enabled: true}, inertController{})

	linh, err := client.Snapshot(context.Background(), productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "hc-051-linh-tran"})
	if err != nil {
		t.Fatal(err)
	}
	if linh.Availability != productui.AgentsAvailable || len(linh.Tasks) != 1 || linh.Threads == nil || len(linh.Threads) != 0 {
		t.Fatalf("snapshot = %+v", linh)
	}
	task := linh.Tasks[0]
	if task.ID != "task-linh" || task.State != productui.AgentTaskRunning || task.LiveStep != "skill.lookup" || task.BudgetLimit != "50" || task.BudgetUsed != "0" ||
		len(task.Steps) != 1 || task.Steps[0].Tier != "T0" || task.Steps[0].Name != "skill.lookup" {
		t.Fatalf("task projection = %+v", task)
	}
	page := agentsPageHTML(t, linh)
	if !strings.Contains(page, "Summarise my open onboarding requests") || strings.Contains(page, "payroll variance") {
		t.Fatalf("Linh's page does not list exactly her task: %s", page)
	}
	rafael, err := client.Snapshot(context.Background(), productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "hc-050-rafael-torres"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rafael.Tasks) != 1 || rafael.Tasks[0].State != productui.AgentTaskAwaitingPlanConfirmation || !rafael.Tasks[0].Actions.ConfirmPlan || !rafael.Tasks[0].Actions.Cancel {
		t.Fatalf("unconfirmed plan should expose its exact confirmation state and actions: %+v", rafael.Tasks)
	}
	if page := agentsPageHTML(t, rafael); strings.Contains(page, "onboarding requests") {
		t.Fatalf("a second user saw another user's task: %s", page)
	}
	// Another tenant's runtime holds nothing for the same subject.
	other, err := client.Snapshot(context.Background(), productui.AgentSnapshotRequest{TenantID: "ironridge-demo", Principal: "hc-051-linh-tran"})
	if err != nil || len(other.Tasks) != 0 {
		t.Fatalf("other tenant snapshot = %+v, %v", other, err)
	}
}

type leakyReader struct {
	tasks []agentrun.AgentTask
	err   error
}

func (r leakyReader) UserTasks(context.Context, string) ([]agentrun.AgentTask, error) {
	return r.tasks, r.err
}
func (leakyReader) BudgetUsage(string) (agentbudget.Limits, agentbudget.Limits, bool) {
	return agentbudget.Limits{}, agentbudget.Limits{}, false
}

type fixedRunners struct {
	reader TaskReader
	err    error
}

func (r fixedRunners) Runner(context.Context, values.TenantId) (TaskReader, error) {
	return r.reader, r.err
}

type fixedAgentSetting struct {
	enabled bool
	err     error
}

func (s fixedAgentSetting) AgentsEnabled(context.Context, values.TenantId) (bool, error) {
	return s.enabled, s.err
}

type inertController struct{}

func (inertController) ControlTask(context.Context, *trust.Principal, TaskControl) (ControlledTask, error) {
	return ControlledTask{}, nil
}

func TestTodo_UXBLIND_122_Security(t *testing.T) {
	ctx := context.Background()
	// A store that returns other users' and other tenants' rows discloses none.
	leaky := New(fixedRunners{reader: leakyReader{tasks: []agentrun.AgentTask{
		{ID: "own", TenantID: tenantKey, UserID: "me", Goal: "mine", State: agentrun.StateCompleted,
			Ledger: agentrun.TaskLedger{}, FailureDetail: "secret failure detail"},
		{ID: "theirs", TenantID: tenantKey, UserID: "someone-else", Goal: "their secret", State: agentrun.StateRunning},
		{ID: "foreign", TenantID: "other-tenant", UserID: "me", Goal: "foreign secret", State: agentrun.StateRunning},
	}}})
	snapshot, err := leaky.Snapshot(ctx, productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "me"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].ID != "own" {
		t.Fatalf("snapshot disclosed rows it does not own: %+v", snapshot.Tasks)
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("snapshot carries failure detail or foreign goals: %s", encoded)
	}
	// Nil, unbound, erroring and unauthenticated requests fail closed.
	for name, tc := range map[string]struct {
		client *Client
		req    productui.AgentSnapshotRequest
		want   error
	}{
		"nil client":     {client: nil, req: productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "me"}, want: ErrUnavailable},
		"no platform":    {client: FromPlatform(nil), req: productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "me"}, want: ErrUnavailable},
		"no principal":   {client: leaky, req: productui.AgentSnapshotRequest{TenantID: tenantKey}, want: ErrInvalid},
		"no tenant":      {client: leaky, req: productui.AgentSnapshotRequest{Principal: "me"}, want: ErrInvalid},
		"no runner":      {client: New(fixedRunners{}), req: productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "me"}, want: ErrUnavailable},
		"runner error":   {client: New(fixedRunners{err: agentsystem.ErrNotConfigured}), req: productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "me"}, want: agentsystem.ErrNotConfigured},
		"list error":     {client: New(fixedRunners{reader: leakyReader{err: agentsystem.ErrInvalid}}), req: productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "me"}, want: agentsystem.ErrInvalid},
		"invalid tenant": {client: FromPlatform(newPlatform(t)), req: productui.AgentSnapshotRequest{TenantID: "bad tenant!", Principal: "me"}, want: agentsystem.ErrInvalid},
	} {
		got, err := tc.client.Snapshot(ctx, tc.req)
		if !errors.Is(err, tc.want) || got.Availability == productui.AgentsAvailable || len(got.Tasks) != 0 {
			t.Fatalf("%s: snapshot=%+v err=%v, want %v", name, got, err, tc.want)
		}
	}
}

func TestTodo_UXBLIND_122_StateMapping(t *testing.T) {
	for state, want := range map[agentrun.TaskState]productui.AgentTaskState{
		agentrun.StateRunning: productui.AgentTaskRunning, agentrun.StateAwaitingApproval: productui.AgentTaskAwaitingApproval,
		agentrun.StateWaiting: productui.AgentTaskWaiting, agentrun.StateAwaitingPlanConfirmation: productui.AgentTaskAwaitingPlanConfirmation,
		agentrun.StateDrafting: productui.AgentTaskDrafting, agentrun.StatePaused: productui.AgentTaskPaused,
		agentrun.StateCompleted: productui.AgentTaskCompleted, agentrun.StateFailed: productui.AgentTaskFailed,
		agentrun.StateCancelled: productui.AgentTaskCancelled, agentrun.StateExpired: productui.AgentTaskExpired, "UNKNOWN": productui.AgentTaskUnknown,
	} {
		if got := TaskState(state); got != want {
			t.Fatalf("TaskState(%s) = %s, want %s", state, got, want)
		}
	}
	task := projectTask(context.Background(), agentrun.AgentTask{
		ID: "t", Goal: strings.Repeat("long goal ", 20) + "\nsecond line", State: agentrun.StateAwaitingApproval, CurrentStep: 1,
		Plan: agentrun.AgentPlan{Revision: 3, Steps: []agentrun.PlanStep{
			{ID: "a", SkillID: "skill.lookup", Tier: agentrun.TierRead, State: agentrun.StepCompleted},
			{ID: "b", Tier: agentrun.TierSubmitGoverned, State: agentrun.StepAwaitingApproval, ApprovalDigest: "sha256:abc"},
		}},
	}, leakyReader{})
	if task.PlanRevision != "3" || task.LiveStep != "b" || len(task.Approvals) != 1 || task.Approvals[0].Digest != "sha256:abc" || task.Approvals[0].ID != "t/b" ||
		task.Steps[1].Tier != "T3" || task.Steps[1].State != "awaiting_approval" || strings.Contains(task.Title, "second line") || len([]rune(task.Title)) != maxTitleRunes {
		t.Fatalf("projected task = %+v", task)
	}
	if done := projectTask(context.Background(), agentrun.AgentTask{ID: "d", State: agentrun.StateCompleted, Plan: agentrun.AgentPlan{Steps: []agentrun.PlanStep{{ID: "a"}}}}, leakyReader{}); done.LiveStep != "" || done.BudgetUsed != "" {
		t.Fatalf("a finished task shows a live step or budget: %+v", done)
	}
}

type recordingPersonaCatalog struct {
	calls     int
	principal *trust.Principal
	agents    []productui.AgentSummary
	err       error
}

func (c *recordingPersonaCatalog) List(_ context.Context, principal *trust.Principal) ([]productui.AgentSummary, error) {
	c.calls++
	c.principal = principal
	return c.agents, c.err
}

func catalogPrincipal(t *testing.T, subjectKind trust.SubjectKind) *trust.Principal {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               values.TenantId(tenantKey),
		Subject:              "me",
		SubjectKind:          subjectKind,
		OrganizationScopeID:  "org:harborcare-demo:people-ops",
		Roles:                []string{"worker_self"},
		Purposes:             []string{"agent.self_service"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceSubstantial,
		SessionRef:           "session-catalog",
		IssuedAt:             fixedNow.Add(-time.Minute),
		ExpiresAt:            fixedNow.Add(time.Hour),
		CredentialDigest:     "sha256:catalog-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func catalogClient(catalog PersonaCatalog) *Client {
	return NewWithControlsAndCatalog(
		fixedRunners{reader: leakyReader{}}, fixedAgentSetting{enabled: true}, inertController{}, catalog,
	)
}

func TestSnapshot_CatalogRequiresVerifiedMatchingHuman(t *testing.T) {
	catalog := &recordingPersonaCatalog{}
	client := catalogClient(catalog)
	tests := []struct {
		name string
		ctx  context.Context
		req  productui.AgentSnapshotRequest
	}{
		{name: "missing principal", ctx: context.Background(), req: productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "me"}},
		{name: "nil context", ctx: nil, req: productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "me"}},
		{name: "subject mismatch", ctx: trust.WithPrincipal(context.Background(), catalogPrincipal(t, trust.SubjectKindHuman)), req: productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "other"}},
		{name: "tenant mismatch", ctx: trust.WithPrincipal(context.Background(), catalogPrincipal(t, trust.SubjectKindHuman)), req: productui.AgentSnapshotRequest{TenantID: "ironridge-demo", Principal: "me"}},
		{name: "agent principal", ctx: trust.WithPrincipal(context.Background(), catalogPrincipal(t, trust.SubjectKindAgent)), req: productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "me"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := client.Snapshot(tc.ctx, tc.req)
			if !errors.Is(err, ErrNoPrincipal) {
				t.Fatalf("Snapshot error = %v, want ErrNoPrincipal", err)
			}
			if got.Availability != productui.AgentsAvailability("") || len(got.Agents) != 0 || len(got.Tasks) != 0 {
				t.Fatalf("failed catalog request returned data: %+v", got)
			}
		})
	}
	if catalog.calls != 0 {
		t.Fatalf("catalog calls = %d, want no calls for unverified viewers", catalog.calls)
	}
}

func TestSnapshot_CatalogReceivesVerifiedPrincipalAndCopiesSummaries(t *testing.T) {
	catalog := &recordingPersonaCatalog{agents: []productui.AgentSummary{{ID: "coach", Name: "People Coach", Skills: []string{"lookup"}}}}
	principal := catalogPrincipal(t, trust.SubjectKindHuman)
	got, err := catalogClient(catalog).Snapshot(
		trust.WithPrincipal(context.Background(), principal),
		productui.AgentSnapshotRequest{TenantID: tenantKey, Principal: "me"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.calls != 1 || catalog.principal != principal {
		t.Fatalf("catalog received principal=%p calls=%d, want %p and one call", catalog.principal, catalog.calls, principal)
	}
	if len(got.Agents) != 1 || got.Agents[0].ID != "coach" || len(got.Agents[0].Skills) != 1 {
		t.Fatalf("agents = %+v", got.Agents)
	}
	catalog.agents[0].Skills[0] = "mutated"
	if got.Agents[0].Skills[0] != "lookup" {
		t.Fatalf("snapshot aliases catalog skills: %+v", got.Agents)
	}
}
