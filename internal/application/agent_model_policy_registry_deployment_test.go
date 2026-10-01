package application

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentmodelpolicystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func TestTodo_AGENT_021_ImmutablePolicyCurrentDeployment(t *testing.T) {
	now := time.Now().UTC()
	tenantID := uuid.New()
	profile := localOpenAIProfileFixture(t)
	profile.ToolSchemaDigest = LocalPersonaOpenAIToolSchemaDigest()
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	cfg, err := NewLocalPersonaOpenAIModelDeployment("harborcare-demo", []agentmodel.ModelProfile{profile}, localOpenAIMaterialFixture())
	if err != nil {
		t.Fatal(err)
	}
	selection := agentmodel.ModelSelection{ProfileID: profile.ID, ProfileDigest: profile.ProfileDigest, Identity: profile.Identity}
	term := cfg.Terms[0]
	route := struct {
		PersonaRunModelRoute
		ModelDigest string `json:"model_digest"`
	}{PersonaRunModelRoute: PersonaRunModelRoute{
		Route:   agentmodel.RouteRequest{Pin: agentmodel.ModelPin{AgentVersionDigest: profile.Evaluation.AgentVersionDigest, TaskProfileID: profile.TaskProfileIDs[0], Primary: selection, SemanticsDigest: profile.SemanticsDigest, OutputSchemaDigest: profile.OutputSchemaDigest, ToolSchemaDigest: profile.ToolSchemaDigest}, Task: agentmodel.TaskProfile{ID: profile.TaskProfileIDs[0], AgentVersionDigest: profile.Evaluation.AgentVersionDigest, Region: LocalPersonaOpenAIRegion, DataClasses: profile.DataClasses, MaxLatency: profile.MaxLatency, MaxCostMicros: profile.MaxCostMicros, SemanticsDigest: profile.SemanticsDigest, OutputSchemaDigest: profile.OutputSchemaDigest, ToolSchemaDigest: profile.ToolSchemaDigest}},
		Purpose: LocalPersonaOpenAIPurpose, Processing: agentmodel.ProcessingPolicy{Residency: LocalPersonaOpenAIRegion, Retention: fmt.Sprintf("%s:%d", term.Retention.Mode, int64(term.Retention.MaxAge)), TrainingUse: term.TrainingUse, Logging: term.Logging},
		Egress: agentegress.Profile{ID: profile.ID, Kind: agentegress.TargetModel, AllowedRegions: term.AllowedRegions, AllowedClasses: term.AllowedClasses, Retention: term.Retention}, ProfileClass: trustdlp.ClassPublic, InvokerClass: trustdlp.ClassInternal, ThreadClass: trustdlp.ClassInternal,
	}, ModelDigest: profile.ProfileDigest}
	raw, _ := json.Marshal(route)
	record := LocalPersonaOpenAIPolicyRecords()[0]
	ref := record.Reference
	reader := &agentPolicyRouteFixture{value: agentstore.PersonaModelRoutePolicy{TenantID: tenantID, LegalEntityID: "entity", PolicyID: ref.ID, PolicyVersion: int64(ref.Version), PolicySchemaVersion: int64(ref.SchemaVersion), PolicyDigest: ref.Digest, PolicyPayload: record.Content, RoutePayload: raw}}
	a := PersonaModelDeploymentPolicyAuthority{Routes: reader, Deployment: func(context.Context, values.TenantId) (PersonaModelDeployment, error) { return cfg, nil }, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }, Now: func() time.Time { return now }}
	request := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "harborcare-demo"}, LegalEntity: "entity", Agent: agentrun.VersionRef{Digest: profile.Evaluation.AgentVersionDigest}, Deadline: now.Add(time.Minute), Budget: agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 1000, MaxOutputTokens: 500}}
	current := agentmodelpolicystore.Current{Record: record}
	if err := a.CheckAgentPolicyDeployment(context.Background(), request, current); err != nil {
		t.Fatalf("qualified current deployment=%v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*agentrun.Request)
	}{{"other tenant", func(r *agentrun.Request) { r.Source.TenantID = "production" }}, {"other agent", func(r *agentrun.Request) { r.Agent.Digest = "substituted" }}, {"entity", func(r *agentrun.Request) { r.LegalEntity = "other" }}, {"cost", func(r *agentrun.Request) { r.Budget.MaxCostMicros = 10001 }}, {"tokens", func(r *agentrun.Request) { r.Budget.MaxInputTokens = 8193 }}} {
		changed := request
		tc.mutate(&changed)
		if err := a.CheckAgentPolicyDeployment(context.Background(), changed, current); err == nil {
			t.Fatalf("%s accepted", tc.name)
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*PersonaModelDeployment)
	}{{"withdrawn credential", func(c *PersonaModelDeployment) { c.Credential.Scopes = nil }}, {"unsigned pricing", func(c *PersonaModelDeployment) { c.Pricing.Signature = "invalid" }}, {"evaluation", func(c *PersonaModelDeployment) { c.Profiles[0].Evaluation.Passed = false }}, {"retention", func(c *PersonaModelDeployment) { c.Terms[0].Retention.MaxAge = 31 * 24 * time.Hour }}, {"class", func(c *PersonaModelDeployment) {
		c.Terms[0].AllowedClasses = []trustdlp.DataClass{trustdlp.ClassPublic}
	}}} {
		original := cfg
		raw, _ := json.Marshal(original)
		_ = json.Unmarshal(raw, &cfg)
		tc.mutate(&cfg)
		if err := a.CheckAgentPolicyDeployment(context.Background(), request, current); err == nil {
			t.Fatalf("%s deployment accepted", tc.name)
		}
		cfg = original
	}
	changed := route
	changed.Processing.Logging = agentmodel.UseDenied
	reader.value.RoutePayload, _ = json.Marshal(changed)
	if err := a.CheckAgentPolicyDeployment(context.Background(), request, current); err == nil {
		t.Fatal("processing substitution accepted")
	}
	reader.value.RoutePayload = raw
	reader.value.PolicyPayload = []byte(`{}`)
	if err := a.CheckAgentPolicyDeployment(context.Background(), request, current); err == nil {
		t.Fatal("semantic policy substitution accepted")
	}
	if err := (PersonaModelDeploymentPolicyAuthority{}).CheckAgentPolicyDeployment(context.Background(), request, current); err == nil {
		t.Fatal("missing current owner accepted")
	}
}

type agentPolicyRouteFixture struct {
	value agentstore.PersonaModelRoutePolicy
}

func (s *agentPolicyRouteFixture) CurrentPersonaModelRoutePolicy(context.Context, uuid.UUID, string, string, int64, int64, string, time.Time) (agentstore.PersonaModelRoutePolicy, error) {
	return s.value, nil
}
