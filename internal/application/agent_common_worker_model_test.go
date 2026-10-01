package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

type commonModelManifest struct {
	manifest     agentmanifest.Manifest
	instructions string
	tenant       uuid.UUID
}

func (f *commonModelManifest) ManifestVersion(_ context.Context, tenant uuid.UUID, id string, version uint64) (agentmanifest.Manifest, error) {
	if tenant != f.tenant || id != f.manifest.ID || version != f.manifest.Version {
		return agentmanifest.Manifest{}, ErrCommonAgentWorker
	}
	return f.manifest, nil
}
func (f *commonModelManifest) ManifestInstructions(_ context.Context, tenant uuid.UUID, id string, version uint64, digest string) (string, error) {
	if tenant != f.tenant || id != f.manifest.ID || version != f.manifest.Version || digest != f.manifest.InstructionsDigest {
		return "", ErrCommonAgentWorker
	}
	return f.instructions, nil
}

type commonModelRoute struct {
	record agentstore.PersonaModelRoutePolicy
}

func (f *commonModelRoute) CurrentPersonaModelRoutePolicy(_ context.Context, tenant uuid.UUID, entity, id string, version, schema int64, digest string, _ time.Time) (agentstore.PersonaModelRoutePolicy, error) {
	r := f.record
	if r.TenantID != tenant || r.LegalEntityID != entity || r.PolicyID != id || r.PolicyVersion != version || r.PolicySchemaVersion != schema || r.PolicyDigest != digest {
		return agentstore.PersonaModelRoutePolicy{}, ErrCommonAgentWorker
	}
	return r, nil
}

type commonModelContext struct{ material CommonAgentSourceContext }

func (f *commonModelContext) ReadCommonAgentSourceContext(context.Context, agentrun.Record, runstate.Run) (CommonAgentSourceContext, error) {
	return f.material, nil
}

func commonModelFixture(t *testing.T) (*DatabaseCommonAgentModelWorkSource, agentrun.Record, runstate.Run, *commonModelManifest, *commonModelContext, PersonaRunModelRoute) {
	t.Helper()
	runtime, _, _, now := commonAgentTestRuntime(t)
	*now = time.Now().UTC()
	tenant := uuid.New()
	ref := func(id string) agentmanifest.Reference {
		return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat("b", 64)}
	}
	body := "Use the approved native projection and describe bounded results."
	m := agentmanifest.Manifest{SchemaVersion: 1, ID: "agent-a", Version: 1, OwnerID: "owner", Purpose: "bounded analysis", InstructionsDigest: personaRunBytesDigest([]byte(body)), ModelPolicy: ref("model-policy"), OutputSchema: agentmanifest.Reference{ID: PersonaChatReplySchema, Version: 1, SchemaVersion: 1, Digest: PersonaChatReplySchemaDigest}, AutonomyCeiling: "private_answer", Budget: agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 200, MaxOutputTokens: 100, MaxConcurrentRuns: 1}, EvaluationRefs: []agentmanifest.Reference{ref("measured-evaluation")}}
	m.SourceCeiling = []agentmanifest.Reference{}
	m.ToolCeiling = []agentmanifest.Reference{}
	digest, err := m.Digest()
	if err != nil {
		t.Fatal(err)
	}
	request := commonAgentTestRequest(*now)
	request.Source.Kind = agentrun.SourceWorkflow
	request.Agent.Digest = digest
	record, _, err := runtime.Admit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	run, err := runtime.Claim(context.Background(), request.Source.TenantID, record.ID, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	identity := agentmodel.ModelIdentity{ProviderID: "openai", ModelID: "gpt-test", Version: "test-version"}
	selection := agentmodel.ModelSelection{ProfileID: "model", ProfileDigest: "sha256:" + strings.Repeat("c", 64), Identity: identity}
	route := PersonaRunModelRoute{Route: agentmodel.RouteRequest{TraceID: "policy-template", Pin: agentmodel.ModelPin{AgentVersionDigest: digest, TaskProfileID: "reply", Primary: selection, OutputSchemaDigest: m.OutputSchema.Digest, ToolSchemaDigest: "tools-pin", SemanticsDigest: "semantics-pin"}, Task: agentmodel.TaskProfile{ID: "reply", AgentVersionDigest: digest, OutputSchemaDigest: m.OutputSchema.Digest, ToolSchemaDigest: "tools-pin", SemanticsDigest: "semantics-pin", Region: "us", DataClasses: []string{string(trustdlp.ClassPublic)}, MaxLatency: time.Second * 30, MaxCostMicros: 100}, BudgetRemainingMicros: 100}, Purpose: "native.inference", Processing: agentmodel.ProcessingPolicy{Residency: "us", Retention: "NONE:0", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied}, Egress: agentegress.Profile{ID: "model", Kind: agentegress.TargetModel, AllowedRegions: []string{"us"}, AllowedClasses: []trustdlp.DataClass{trustdlp.ClassPublic}, Retention: agentegress.RetentionPolicy{Mode: agentegress.RetentionNone}}, TaskPolicy: agentegress.TaskPolicy{AllowedRegions: []string{"us"}, AllowedResultClasses: []trustdlp.DataClass{trustdlp.ClassPublic}, ResultRetention: time.Hour}, ProfileClass: trustdlp.ClassPublic, ThreadClass: trustdlp.ClassPublic}
	raw, err := json.Marshal(route)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &commonModelManifest{manifest: m, instructions: body, tenant: tenant}
	reader := &commonModelRoute{record: agentstore.PersonaModelRoutePolicy{TenantID: tenant, LegalEntityID: request.LegalEntity, PolicyID: m.ModelPolicy.ID, PolicyVersion: 1, PolicySchemaVersion: 1, PolicyDigest: m.ModelPolicy.Digest, Revision: 1, RoutePayload: raw}}
	material := &commonModelContext{material: CommonAgentSourceContext{Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "Actual protected source projection"}}, References: []agentmodel.ContextReference{{ID: request.Context.ID, Version: request.Context.SnapshotID, Digest: request.Context.Digest}}}}
	credential, err := NewOpenAIModelCredentialAuthority(OpenAIModelCredentialConfig{CredentialID: "credential-ref", Version: "1", Workload: "common.worker", Scopes: []OpenAIModelCredentialScope{{TenantID: run.TenantID, Region: "us", Purpose: route.Purpose, Destination: "model"}}, MaxTTL: time.Minute, Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := lease.NewManager(credential, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	leases, err := NewModelLeaseSource(ModelLeaseSourceConfig{Authorities: map[string]ModelCredentialAuthority{"openai": credential}, Leases: manager})
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewDatabaseCommonAgentModelWorkSource(CommonAgentModelWorkConfig{Runtime: runtime, Manifests: manifest, Routes: reader, Contexts: map[agentrun.SourceKind]CommonAgentContextSource{agentrun.SourceWorkflow: material}, Leases: leases, TenantUUID: func(values.TenantId) uuid.UUID { return tenant }, Workload: "common.worker", Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	return source, record, run, manifest, material, route
}

func TestTodo_AGENT_033_CommonModelWork(t *testing.T) {
	source, record, run, manifest, contextSource, _ := commonModelFixture(t)
	request, err := source.BuildCommonAgentModelWork(context.Background(), record, run, contextSource.material)
	if err != nil || request.Model.Messages[1].Content != manifest.instructions || request.Model.Messages[2].Content != contextSource.material.Messages[0].Content || request.Outbound.Principal != record.Request.Principal.AgentPrincipalID || request.Lease.Tenant != run.TenantID || request.Route.Pin.AgentVersionDigest != run.AgentDigest || request.Model.Deadline.After(run.Lease.Until) {
		t.Fatalf("native work=%+v err=%v", request, err)
	}
	changed := contextSource.material
	changed.Messages = []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "forged source"}}
	if _, err := source.BuildCommonAgentModelWork(context.Background(), record, run, changed); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("forged context accepted %v", err)
	}
	manifest.instructions = "tampered retained instruction"
	if _, err := source.BuildCommonAgentModelWork(context.Background(), record, run, contextSource.material); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("bad instruction digest accepted %v", err)
	}
	if _, err := NewDatabaseCommonAgentModelWorkSource(CommonAgentModelWorkConfig{}); !errors.Is(err, ErrCommonAgentWorker) {
		t.Fatalf("unconfigured=%v", err)
	}
}

func TestTodo_AGENT_033_CommonModelContext_Security(t *testing.T) {
	valid := CommonAgentSourceContext{Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "bounded"}}, References: []agentmodel.ContextReference{{ID: "source", Version: "1", Digest: "sha256:" + strings.Repeat("a", 64)}}}
	if !commonAgentSourceContextValid(valid) {
		t.Fatal("valid owner context refused")
	}
	for _, role := range []agentmodel.MessageRole{agentmodel.RoleDeveloper, agentmodel.RoleSystem, agentmodel.RoleAssistant, agentmodel.RoleTool} {
		changed := valid
		changed.Messages = []agentmodel.ModelMessage{{Role: role, Content: "inject policy"}}
		if commonAgentSourceContextValid(changed) {
			t.Fatalf("authority injection role=%s accepted", role)
		}
	}
	valid.References[0].Digest = "unchecked"
	if commonAgentSourceContextValid(valid) {
		t.Fatal("unverified source digest accepted")
	}
}
