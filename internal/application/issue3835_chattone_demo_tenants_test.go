package application

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentaudit"
	"github.com/monstercameron/human-capital-management-suite/internal/agentbudget"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// composeChattoneForTenants composes the served writing-style service the way
// the composition root does, for the given profile and served workspaces.
func composeChattoneForTenants(t *testing.T, profile string, primary, others string) *ChattoneService {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(EnvLocalAgentDevDir, dir)
	provider := newChattoneProvider(t)
	dep := chattoneTestDeployment(t, provider)
	raw, err := json.Marshal(dep)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "deployment.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	ledger, err := agentbudget.New(agentBudgetPolicy())
	if err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		switch k {
		case EnvChatWritingStyleModelConfigFile:
			return path
		case "MODEL_API_KEY":
			return "test-only-provider-key"
		}
		return ""
	}
	material, err := LoadOrCreateLocalPersonaModelSigningMaterial(filepath.Join(t.TempDir(), "signing.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := ServeConfig{Profile: profile, CellID: dep.Worker.Cell, Tenant: primary, Tenants: others,
		PersonaOutputSigningSeed: material.OutputSeed, PersonaWorkloadSigningSeed: material.WorkloadSeed}
	chatOK := composedChat{service: (*chat.Service)(nil), renderings: &ChatRenderingSurface{}, filters: &chatfilter.Service{}}
	svc, reason, err := composeServedChattone(context.Background(), chattoneServeInput{Config: cfg,
		Runtime: &agentRuntime{Budget: ledger, Audit: agentaudit.NewMemoryStore()}, Chat: chatOK, Env: env})
	if err != nil || svc == nil {
		t.Fatalf("composition: %v %q", err, reason)
	}
	return svc
}

// Local development turns writing styles on for every served demo workspace,
// and for nothing else: any other workspace still opts in through its
// administrator.
func TestIssue3835_LocalDevEnablesEveryServedDemoTenant(t *testing.T) {
	svc := composeChattoneForTenants(t, ServeProfileLocalDev, "harborcare-demo", "ironridge-demo,acme-demo,tenant-a")
	for tenant, want := range map[string]bool{"harborcare-demo": true, "ironridge-demo": true, "acme-demo": false, "tenant-a": false, "unserved-demo": false} {
		if _, on := svc.Rewrite.Registry.Styles(tenant); on != want {
			t.Errorf("tenant %s writing styles on = %v, want %v", tenant, on, want)
		}
	}
}

func TestIssue3835_HarborcareAloneIsEnabled(t *testing.T) {
	svc := composeChattoneForTenants(t, ServeProfileLocalDev, "harborcare-demo", "")
	if _, on := svc.Rewrite.Registry.Styles("harborcare-demo"); !on {
		t.Fatal("a local run serving only harborcare-demo has writing styles off")
	}
	if _, on := svc.Rewrite.Registry.Styles("ironridge-demo"); on {
		t.Fatal("an unserved demo workspace was turned on")
	}
}

// Outside the local development profile nothing is turned on, demo names
// included.
func TestIssue3835_OtherProfilesEnableNothing_Security(t *testing.T) {
	svc := composeChattoneForTenants(t, ServeProfileStandard, "harborcare-demo", "ironridge-demo")
	for _, tenant := range []string{"harborcare-demo", "ironridge-demo"} {
		if _, on := svc.Rewrite.Registry.Styles(tenant); on {
			t.Errorf("tenant %s was turned on outside the local development profile", tenant)
		}
	}
}
