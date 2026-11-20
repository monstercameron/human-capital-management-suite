package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

func TestIssue3836_TranslationModelDefaultsToLocalModelOnlyInLocalDev(t *testing.T) {
	none := func(string) string { return "" }
	set := func(k string) string {
		if k == EnvChatTranslationModel {
			return " gpt-6-sol "
		}
		return ""
	}
	task := ChatlangTaskProfile().ID
	localDep := PersonaModelDeployment{Profiles: []agentmodel.ModelProfile{{ID: "p", TaskProfileIDs: []string{task}, Identity: agentmodel.ModelIdentity{ModelID: LocalPersonaOpenAIModelID}}}}
	otherDep := PersonaModelDeployment{Profiles: []agentmodel.ModelProfile{{ID: "p", TaskProfileIDs: []string{task}, Identity: agentmodel.ModelIdentity{ModelID: agentmodel.ChatlangDefaultModel}}}}
	if got := chatlangModelForProfile(none, true, localDep); got != LocalPersonaOpenAIModelID {
		t.Fatalf("local default = %q, want %q", got, LocalPersonaOpenAIModelID)
	}
	if got := chatlangModelForProfile(none, true, otherDep); got != agentmodel.ChatlangDefaultModel {
		t.Fatalf("a local deployment qualifying another model must keep the global default, got %q", got)
	}
	if got := chatlangModelForProfile(set, true, localDep); got != "gpt-6-sol" {
		t.Fatalf("explicit model lost in local-dev: %q", got)
	}
	if got := chatlangModelForProfile(none, false, localDep); got != agentmodel.ChatlangDefaultModel {
		t.Fatalf("non-local default changed: %q", got)
	}
	if got := chatlangModelForProfile(set, false, localDep); got != "gpt-6-sol" {
		t.Fatalf("explicit model lost outside local-dev: %q", got)
	}
	if agentmodel.ChatlangDefaultModel != "gpt-6-luna" {
		t.Fatal("the global default must stay gpt-6-luna")
	}
}

func TestIssue3836_QualifyChattoneEmptyModelWorksOnLocalBase_Integration(t *testing.T) {
	provider := newChattoneProvider(t)
	provider.rewrite = chattoneGoodRewrite
	profile := localOpenAIProfileFixture(t)
	profile.ToolSchemaDigest = LocalPersonaOpenAIToolSchemaDigest()
	profile.ProfileDigest = agentmodel.ModelProfileDigest(profile)
	base, err := NewLocalPersonaOpenAIModelDeployment("harborcare-demo", []agentmodel.ModelProfile{profile}, localOpenAIMaterialFixture())
	if err != nil {
		t.Fatal(err)
	}
	if !isLocalPersonaOpenAIBase(base) {
		t.Fatal("the local base is not recognised")
	}
	base.BaseURL = provider.server.URL
	cfg := ChattoneQualifyConfig{Base: base, APIKey: "test-only-provider-key", MaxLatency: 20 * time.Second, MaxCostMicros: 20000, Now: time.Now}
	_, identity, err := chattoneLiveModelFor(context.Background(), cfg)
	if err != nil {
		t.Fatalf("empty Model on the local base: %v", err)
	}
	if identity.ModelID != LocalPersonaOpenAIModelID {
		t.Fatalf("model = %q", identity.ModelID)
	}
	// An explicit model still wins and still needs a price.
	cfg.Model = "gpt-6-luna"
	if _, _, err := chattoneLiveModelFor(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "gpt-6-luna") {
		t.Fatalf("explicit unpriced model on the local base: %v", err)
	}
}

func TestIssue3836_NonLocalBaseKeepsGlobalDefault_Security(t *testing.T) {
	base := modelDeploymentFixture(t)
	if isLocalPersonaOpenAIBase(base) {
		t.Fatal("a non-local base was taken for the local one")
	}
	if got := (ChattoneQualifyConfig{Base: base}).configuredModel(); got != "" {
		t.Fatalf("non-local base model substituted: %q", got)
	}
}
