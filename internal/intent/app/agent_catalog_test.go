package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	intentdefinitions "github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/endpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const rev019DefinitionID = "hcmnext.people.promote_worker"

type rev019Draft struct {
	Fields []string
	Refs   []string
	Claims []string
}

func (d rev019Draft) DraftFields() []string     { return d.Fields }
func (d rev019Draft) DraftReferences() []string { return d.Refs }
func (d rev019Draft) DraftClaims() []string     { return d.Claims }
func (d rev019Draft) DraftCanonicalBytes() ([]byte, error) {
	return json.Marshal(struct {
		Fields []string `json:"fields"`
		Refs   []string `json:"refs"`
		Claims []string `json:"claims"`
	}{d.Fields, d.Refs, d.Claims})
}
func (d rev019Draft) DetachDraft() (agentsecurity.DraftValue, error) { return d, nil }

type rev019Refs map[string]bool

func (r rev019Refs) Exists(_ context.Context, id string) (bool, error) { return r[id], nil }

type rev019Fields struct{ allow bool }

func (f rev019Fields) AuthorizeFields(_ context.Context, _, _ string, _ []string) error {
	if !f.allow {
		return context.Canceled
	}
	return nil
}

type rev019Claims map[string]bool

func (c rev019Claims) Supports(_ context.Context, claim string) (bool, error) { return c[claim], nil }

// rev019CompilerFixture binds an ActionCompiler to the given definition
// catalog with a working draft-ingestion owner set, following the same
// exported-only recipe as the workflow triage conformance test.
func rev019CompilerFixture(t *testing.T, catalog agentsecurity.DefinitionCatalog) (*agentsecurity.ActionCompiler, agentsecurity.Admission) {
	t.Helper()
	gateway, err := agentsecurity.NewToolGateway([]agentsecurity.ToolDescriptor{{
		Name: "agent.probe", Capability: "agent.probe", Version: 1, Class: agentsecurity.ToolDraft,
		DataScope: []string{"agent.basic"}, Cost: 1, Schema: "probe.v1",
		Validate: func(v any) (agentsecurity.TypedResult, error) {
			return agentsecurity.TypedResult{Schema: "probe.v1", Value: v, Validated: true, Taint: []string{"DERIVED"}, Provenance: []string{"rev019-test"}}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	owners, err := agentsecurity.NewOwnerRegistry(gateway)
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Register("agent.probe", agentsecurity.DraftOwners{
		References: rev019Refs{"worker:1": true},
		Fields:     rev019Fields{allow: true},
		Claims:     rev019Claims{"cited": true},
	}); err != nil {
		t.Fatal(err)
	}
	call := agentsecurity.ToolCall{
		Agent:      agentsecurity.AgentIdentity{Identity: "probe-agent", AgentID: "probe", Tenant: "acme", Purpose: "probe", ToolSet: []string{"agent.probe"}, DataScope: []string{"agent.basic"}, Budget: 2},
		Delegation: []agentsecurity.DelegationLink{{GrantID: "grant", Delegator: "root", Delegate: "probe", Tenant: "acme", Purpose: "probe", ToolSet: []string{"agent.probe"}, DataScope: []string{"agent.basic"}, Budget: 2}},
		Tenant:     "acme", Purpose: "probe", Tool: "agent.probe", Capability: "agent.probe", Version: 1,
		Nonce: "nonce", Args: map[string]any{"worker": "worker-1"},
		InputTaint: []string{"DERIVED"}, Provenance: []string{"rev019-test"}, CostBudget: 1, DataScope: []string{"agent.basic"},
	}
	call.ArgsDigest, _ = agentsecurity.DigestArguments(call.Args)
	admission, err := gateway.Admit(call)
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := agentsecurity.NewActionCompiler(catalog, owners)
	if err != nil {
		t.Fatal(err)
	}
	return compiler, admission
}

func rev019Output() agentsecurity.AgentOutput {
	return agentsecurity.AgentOutput{
		Schema: "probe.v1",
		Value: rev019Draft{
			Fields: []string{"definition", "arguments"},
			Refs:   []string{"worker:1"},
			Claims: []string{"cited"},
		},
		References: []string{"worker:1"},
		Fields:     []string{"definition", "arguments"},
		Claims:     []string{"cited"},
		Narrative:  "Draft promotion for review.",
	}
}

func rev019Action(id string) agentsecurity.ProposedAction {
	return agentsecurity.ProposedAction{
		DefinitionID: id,
		Arguments:    map[string]string{"subject": "worker:1", "action": "promote"},
		Sources:      []string{"worker:1"},
		Taint:        []string{"DERIVED"},
		Uncertainty:  "eligibility read is point-in-time",
		Bulk:         1,
	}
}

func rev019Attribution() agentsecurity.Attribution {
	return agentsecurity.Attribution{Agent: "concierge", Model: "m1", ModelDigest: "sha256:m", Delegation: "grant", Purpose: "probe", Cost: 1}
}

func rev019LiveCatalog(t *testing.T) *AgentCatalog {
	t.Helper()
	reg, err := intentdefinitions.NewRegistry()
	if err != nil {
		t.Fatalf("definitions.NewRegistry: %v", err)
	}
	catalog, err := NewAgentCatalog(reg)
	if err != nil {
		t.Fatalf("NewAgentCatalog: %v", err)
	}
	return catalog
}

// TestTodo_REV_019_01 is the PRIMARY test for REV-019-01: the compiler binds
// to the live catalog through the adapter and agrees with the served
// GetIntentDefinition contract on review, simulation, bulk, schema and
// capability constraints for one real registered definition.
func TestTodo_REV_019_01(t *testing.T) {
	catalog := rev019LiveCatalog(t)
	ctx := context.Background()

	view, err := catalog.LookupDefinition(ctx, rev019DefinitionID)
	if err != nil {
		t.Fatalf("LookupDefinition(%s): %v", rev019DefinitionID, err)
	}
	if view.ID != rev019DefinitionID || view.Version != "v1" {
		t.Fatalf("adapter resolved %s@%s, want %s@v1", view.ID, view.Version, rev019DefinitionID)
	}
	// promote_worker requires approval and cannot execute (P1A simulates it),
	// so review and simulation gates must both hold; the catalog declares no
	// bulk ceiling so fan-out stays refused.
	if !view.RequiresReview {
		t.Error("adapter lost RequiresReview for an approval-gated definition")
	}
	if !view.RequiresSimulation {
		t.Error("adapter lost RequiresSimulation for a simulate-only definition")
	}
	if view.MaxBulk != 1 {
		t.Errorf("adapter MaxBulk = %d, want fail-closed 1", view.MaxBulk)
	}
	if view.InputSchemaRef != "hcmnext.people.v1.PromoteWorkerRequest/v1" {
		t.Errorf("adapter input schema = %q", view.InputSchemaRef)
	}
	if !reflect.DeepEqual(view.CapabilityRefs, []string{"people.promote.plan/v1", "people.promote.execute/v1"}) {
		t.Errorf("adapter capabilities = %q", view.CapabilityRefs)
	}
	if !reflect.DeepEqual(view.GovernanceRefs, []string{"governance.promotion/v1", "approval.promotion/v1"}) {
		t.Errorf("adapter governance refs = %q", view.GovernanceRefs)
	}
	if view.SideEffectProfile != intent.SideEffectInternalMutation.String() || view.RiskClass != "R3" {
		t.Errorf("adapter side-effect/risk = %q/%q", view.SideEffectProfile, view.RiskClass)
	}

	// The served contract must carry the same schema and capability constraints.
	reg, err := intentdefinitions.NewRegistry()
	if err != nil {
		t.Fatalf("definitions.NewRegistry: %v", err)
	}
	served, err := (&IntentService{defs: reg}).GetIntentDefinition(
		trust.WithPrincipal(ctx, lifecyclePrincipal(t)),
		&registryv1.GetIntentDefinitionRequest{Definition: &intentsv1.DefinitionReference{IntentTypeId: rev019DefinitionID, Version: 1}},
	)
	if err != nil {
		t.Fatalf("GetIntentDefinition: %v", err)
	}
	got := served.GetIntentDefinition()
	if got.GetReference().GetIntentTypeId() != rev019DefinitionID || got.GetReference().GetVersion() != 1 {
		t.Fatalf("served reference = %v", got.GetReference())
	}
	if !reflect.DeepEqual(got.GetRequiredCapabilityRefs(), view.CapabilityRefs) {
		t.Errorf("served capabilities = %q, adapter = %q", got.GetRequiredCapabilityRefs(), view.CapabilityRefs)
	}
	if !reflect.DeepEqual(got.GetGovernanceRequirementRefs(), view.GovernanceRefs) {
		t.Errorf("served governance refs = %q, adapter = %q", got.GetGovernanceRequirementRefs(), view.GovernanceRefs)
	}
	servedSchema := fmt.Sprintf("%s/v%d", got.GetInputSchema().GetSchemaId(), got.GetInputSchema().GetVersion())
	if servedSchema != view.InputSchemaRef {
		t.Errorf("served input schema = %q, adapter = %q", servedSchema, view.InputSchemaRef)
	}
	if got.GetRiskClass() != view.RiskClass {
		t.Errorf("served risk = %q, adapter = %q", got.GetRiskClass(), view.RiskClass)
	}
	executeMode, err := protomap.ModeToProto(intent.ModeExecute)
	if err != nil {
		t.Fatalf("ModeToProto(execute): %v", err)
	}
	for _, m := range got.GetAllowedExecutionModes() {
		if m == executeMode {
			t.Fatal("served promote_worker allows EXECUTE, contradicting the adapter's RequiresSimulation")
		}
	}

	// The compiler over the live adapter compiles the real definition and
	// refuses an unpublished one.
	compiler, admission := rev019CompilerFixture(t, catalog)
	draft, err := compiler.CompileAction(ctx, admission, "agent.probe", rev019Output(), rev019Action(rev019DefinitionID), rev019Attribution())
	if err != nil {
		t.Fatalf("CompileAction(live definition): %v", err)
	}
	if draft.DefinitionID != rev019DefinitionID || draft.DefinitionVersion != "v1" {
		t.Fatalf("draft targets %s@%s", draft.DefinitionID, draft.DefinitionVersion)
	}
	if !draft.RequiresReview || !draft.RequiresSimulation {
		t.Fatal("draft compiled from the live catalog bypassed review/simulation")
	}
	if _, err := compiler.CompileAction(ctx, admission, "agent.probe", rev019Output(), rev019Action("hcmnext.people.nope"), rev019Attribution()); err == nil {
		t.Fatal("compiler accepted a definition id the live catalog never published")
	}
}

// TestTodo_REV_019_01_Integration drives the fully composed IntentService and
// proves its served output agrees with the adapter the compiler resolves.
func TestTodo_REV_019_01_Integration(t *testing.T) {
	reg, err := intentdefinitions.NewRegistry()
	if err != nil {
		t.Fatalf("definitions.NewRegistry: %v", err)
	}
	caps := capability.NewRegistry()
	sink := NewMemoryEvidenceSink()
	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("NewDefaultDigester: %v", err)
	}
	store := &promotionScanStore{memLifecycleStore: newMemLifecycleStore()}
	svc, err := NewIntentService(Options{
		Definitions: reg, Capabilities: caps, Gateway: capability.NewGateway(caps, sink), Store: store,
		Inputs: noLifecycleInputs{}, Digester: digester, Controls: NewControls(reg, caps),
		Clock: func() values.Instant { return values.NewInstant(lifecycleFixedNow) }, Evidence: sink,
		Idempotency: endpoint.NewCoordinator(),
	})
	if err != nil {
		t.Fatalf("NewIntentService: %v", err)
	}
	ctx := trust.WithPrincipal(context.Background(), lifecyclePrincipal(t))

	listed, err := svc.ListIntentDefinitions(ctx, &registryv1.ListIntentDefinitionsRequest{})
	if err != nil {
		t.Fatalf("ListIntentDefinitions: %v", err)
	}
	found := false
	for _, d := range listed.GetIntentDefinitions() {
		if d.GetReference().GetIntentTypeId() == rev019DefinitionID {
			found = true
		}
	}
	if !found {
		t.Fatalf("composed service does not serve %s", rev019DefinitionID)
	}
	served, err := svc.GetIntentDefinition(ctx, &registryv1.GetIntentDefinitionRequest{
		Definition: &intentsv1.DefinitionReference{IntentTypeId: rev019DefinitionID, Version: 1},
	})
	if err != nil {
		t.Fatalf("GetIntentDefinition: %v", err)
	}
	catalog, err := NewAgentCatalog(reg)
	if err != nil {
		t.Fatalf("NewAgentCatalog: %v", err)
	}
	view, err := catalog.LookupDefinition(ctx, rev019DefinitionID)
	if err != nil {
		t.Fatalf("LookupDefinition: %v", err)
	}
	if !reflect.DeepEqual(served.GetIntentDefinition().GetRequiredCapabilityRefs(), view.CapabilityRefs) ||
		!reflect.DeepEqual(served.GetIntentDefinition().GetGovernanceRequirementRefs(), view.GovernanceRefs) ||
		served.GetIntentDefinition().GetRiskClass() != view.RiskClass {
		t.Fatalf("composed service and adapter disagree: served=%v adapter=%+v", served.GetIntentDefinition(), view)
	}

	compiler, admission := rev019CompilerFixture(t, catalog)
	draft, err := compiler.CompileAction(ctx, admission, "agent.probe", rev019Output(), rev019Action(rev019DefinitionID), rev019Attribution())
	if err != nil {
		t.Fatalf("CompileAction through composed-service catalog: %v", err)
	}
	if draft.DefinitionVersion != "v1" || !draft.RequiresReview || !draft.RequiresSimulation {
		t.Fatalf("draft disagrees with served definition: %+v", draft)
	}
}

// TestTodo_REV_019_01_Mutation proves the adapter refuses retired,
// unknown, hollow and cancelled lookups instead of resolving them.
func TestTodo_REV_019_01_Mutation(t *testing.T) {
	if _, err := NewAgentCatalog(nil); err == nil {
		t.Fatal("NewAgentCatalog accepted a nil registry")
	}
	var base intent.Definition
	for _, d := range intentdefinitions.All() {
		if d.Ref.TypeID == rev019DefinitionID {
			base = d
		}
	}
	if base.Ref.TypeID == "" {
		t.Fatalf("live catalog has no %s", rev019DefinitionID)
	}
	base.Maturity = intent.MaturityRetired
	synthetic, err := intent.NewRegistry(intent.ProfileBootstrap, []intent.Definition{base}, intentdefinitions.Policies(), intentdefinitions.Catalog())
	if err != nil {
		t.Fatalf("synthetic registry: %v", err)
	}
	retired, err := NewAgentCatalog(synthetic)
	if err != nil {
		t.Fatalf("NewAgentCatalog: %v", err)
	}
	if _, err := retired.LookupDefinition(context.Background(), rev019DefinitionID); !errors.Is(err, agentsecurity.ErrUnknownDefinition) {
		t.Fatalf("retired definition lookup error = %v, want ErrUnknownDefinition", err)
	}

	live := rev019LiveCatalog(t)
	for _, id := range []string{"hcmnext.people.nope", "", "   "} {
		if _, err := live.LookupDefinition(context.Background(), id); !errors.Is(err, agentsecurity.ErrUnknownDefinition) {
			t.Fatalf("lookup(%q) error = %v, want ErrUnknownDefinition", id, err)
		}
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := live.LookupDefinition(cancelled, rev019DefinitionID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup error = %v, want context.Canceled", err)
	}
}
