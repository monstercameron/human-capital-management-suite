package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

func TestLocalPersonaFixtureInstallBindsExactIronridgeTuple(t *testing.T) {
	fixture, err := InstallLocalPersonaFixture(LocalPersonaFixtureProfile, LocalPersonaFixtureTenant, LocalPersonaFixtureUser, LocalPersonaFixtureConversation)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.PersonaID == "" || fixture.Version != 1 || fixture.ContentDigest == "" || fixture.OwnerID == "" || fixture.ModelProfile == "" {
		t.Fatalf("fixture provenance = %+v", fixture)
	}
	if fixture.TierCeiling != "T0" || len(fixture.DataClassesRead) != 0 || fixture.Published || fixture.Production {
		t.Fatalf("fixture grants = %+v", fixture)
	}
	id, digest, owner, model := LocalPersonaFixtureConstants()
	if fixture.PersonaID != id || fixture.ContentDigest != digest || fixture.OwnerID != owner || fixture.ModelProfile != model {
		t.Fatalf("fixture constants do not match composition fields: %+v", fixture)
	}
}

func TestLocalPersonaFixtureInstallFailsClosedOutsideApprovedTuple(t *testing.T) {
	cases := []struct {
		name                                string
		profile, tenant, user, conversation string
	}{
		{"standard", ServeProfileStandard, LocalPersonaFixtureTenant, LocalPersonaFixtureUser, LocalPersonaFixtureConversation},
		{"foreign tenant", LocalPersonaFixtureProfile, "harborcare-demo", LocalPersonaFixtureUser, LocalPersonaFixtureConversation},
		{"foreign user", LocalPersonaFixtureProfile, LocalPersonaFixtureTenant, "ir-013-ana-flores", LocalPersonaFixtureConversation},
		{"foreign channel", LocalPersonaFixtureProfile, LocalPersonaFixtureTenant, LocalPersonaFixtureUser, "general"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := InstallLocalPersonaFixture(tc.profile, tc.tenant, tc.user, tc.conversation); !errors.Is(err, ErrLocalPersonaFixtureUnavailable) {
				t.Fatalf("error = %v, want ErrLocalPersonaFixtureUnavailable", err)
			}
		})
	}
}

func TestLocalPersonaFixtureModelReturnsNoDataResponse(t *testing.T) {
	model := LocalPersonaFixtureModel{}
	_, _, _, profile := LocalPersonaFixtureConstants()
	result, err := model.Invoke(context.Background(), agentmodel.ModelRequest{
		ContractVersion: agentmodel.ContractVersion, TaskProfile: "persona.answer", ModelProfile: profile,
		Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "What can you do?"}},
		Output:   agentmodel.OutputConstraint{Mode: agentmodel.OutputText}, Deadline: time.Now().Add(time.Minute),
		Limits: agentmodel.ModelLimits{MaxOutputTokens: 80}, TraceID: "fixture-trace",
		Processing: agentmodel.ProcessingPolicy{Residency: "local", Retention: "NONE:0", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied},
	})
	if err != nil || !LocalPersonaFixtureReplyIsSafe(result.Text) || result.Finish != agentmodel.FinishComplete || result.Usage.TotalTokens != 0 {
		t.Fatalf("result = %+v, err=%v", result, err)
	}
}

func TestLocalPersonaFixtureModelRejectsCompanyDataAndTools(t *testing.T) {
	model := LocalPersonaFixtureModel{}
	_, _, _, profile := LocalPersonaFixtureConstants()
	base := agentmodel.ModelRequest{
		ContractVersion: agentmodel.ContractVersion, TaskProfile: "persona.answer", ModelProfile: profile,
		Messages: []agentmodel.ModelMessage{{Role: agentmodel.RoleUser, Content: "hello"}}, Output: agentmodel.OutputConstraint{Mode: agentmodel.OutputText},
		Deadline: time.Now().Add(time.Minute), TraceID: "fixture-trace",
		Processing: agentmodel.ProcessingPolicy{Residency: "local", Retention: "NONE:0", TrainingUse: agentmodel.UseDenied, Logging: agentmodel.UseDenied},
	}
	for name, mutate := range map[string]func(*agentmodel.ModelRequest){
		"context": func(r *agentmodel.ModelRequest) {
			r.ContextRefs = []agentmodel.ContextReference{{ID: "company-record", Version: "1", Digest: "sha256:x"}}
		},
		"tools": func(r *agentmodel.ModelRequest) {
			r.Tools = []agentmodel.ToolSchema{{Name: "read_company", InputSchema: []byte(`{"type":"object"}`)}}
		},
		"wrong model": func(r *agentmodel.ModelRequest) { r.ModelProfile = "production-model" },
	} {
		t.Run(name, func(t *testing.T) {
			req := base
			mutate(&req)
			if _, err := model.Invoke(context.Background(), req); !errors.Is(err, ErrLocalPersonaFixtureData) {
				t.Fatalf("error = %v, want ErrLocalPersonaFixtureData", err)
			}
		})
	}
}
