package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type studioDraftSourceFake struct {
	snapshot AgentStudioDraftSnapshot
	err      error
}

func (f studioDraftSourceFake) CurrentAgentStudioDraft(context.Context, string) (AgentStudioDraftSnapshot, error) {
	return f.snapshot, f.err
}

type studioExecutorFake struct {
	calls   int
	command PersonaAdminCommand
	err     error
}

func (f *studioExecutorFake) ExecutePersonaAdminCommand(_ context.Context, _ PersonaAdminCommandActor, command PersonaAdminCommand) error {
	f.calls++
	f.command = command
	return f.err
}

func studioProfile(t *testing.T) agentpersona.PersonaProfile {
	t.Helper()
	return agentpersona.PersonaProfile{Manifest: agentpersona.AgentManifestRef{ID: "helper", Version: 1, Digest: "manifest", SchemaVersion: 1}, PersonaID: "persona-a", Version: 1, Handle: "helper", DisplayName: "Helper", AvatarRef: "avatar:helper", Purpose: "Answer policy questions", Audience: agentpersona.Audience{Roles: []string{"employee"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-a"}}, Owner: "user:owner", Steward: "user:steward", SkillPins: []agentskills.SkillPin{{ID: "policy.read", Version: 1, Digest: "skill"}}, TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate, agentpersona.ChannelPublic}, Instructions: "Use approved sources.", EvalSuiteRef: "eval", EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1000}}
}

func studioContext(t *testing.T) context.Context {
	t.Helper()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	return trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, "tenant-a", "user:owner", now))
}

func studioProfileJSON(t *testing.T, profile agentpersona.PersonaProfile) []byte {
	t.Helper()
	b, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTodo_AGENT_051(t *testing.T) {
	profile := studioProfile(t)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	current := AgentStudioDraftSnapshot{PersonaID: "persona-a", Revision: 7, Version: agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: "persona-a", Version: 1, ContentDigest: sealed.Digest, Profile: studioProfileJSON(t, sealed.Profile)}, State: agentpersonastore.StateDraft}
	executor := &studioExecutorFake{}
	service := &AgentStudioSuggestionService{Drafts: studioDraftSourceFake{snapshot: current}, Profiles: personaProfileBuilderFake{}, Executor: executor, Authorizer: &personaAdminCommandAuthFake{}}
	receipt, err := service.ApplySelected(studioContext(t), AgentStudioSuggestion{PersonaID: "persona-a", Revision: 7, Digest: sealed.Digest, Changes: []AgentStudioSuggestionChange{{Field: AgentStudioFieldPurpose, Value: "Answer policy and leave questions", Reason: "Clarifies the job", Uncertainty: .1}}})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Draft.Version != 2 || receipt.Draft.Lifecycle != string(agentpersonastore.StateDraft) || receipt.Draft.Digest == "" || len(receipt.Applied) != 1 {
		t.Fatalf("receipt = %+v", receipt)
	}
	if executor.calls != 1 || executor.command.Action != PersonaAdminCreateVersion || executor.command.Version.Profile.Purpose != "Answer policy and leave questions" || executor.command.Version.Profile.SkillPins[0] != profile.SkillPins[0] {
		t.Fatalf("command = %+v", executor.command)
	}
}

func TestTodo_AGENT_051_Security(t *testing.T) {
	profile := studioProfile(t)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	current := AgentStudioDraftSnapshot{PersonaID: "persona-a", Revision: 2, Version: agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: "persona-a", Version: 1, ContentDigest: sealed.Digest, Profile: studioProfileJSON(t, sealed.Profile)}, State: agentpersonastore.StateDraft}
	executor := &studioExecutorFake{}
	service := &AgentStudioSuggestionService{Drafts: studioDraftSourceFake{snapshot: current}, Profiles: personaProfileBuilderFake{}, Executor: executor, Authorizer: &personaAdminCommandAuthFake{}}
	ctx := studioContext(t)
	for _, field := range []AgentStudioSuggestionField{"instructions", "skill_pins", "tier_ceiling", "audience", "data_classes_read", "eval_suite_ref"} {
		_, err := service.ApplySelected(ctx, AgentStudioSuggestion{PersonaID: "persona-a", Revision: 2, Digest: sealed.Digest, Changes: []AgentStudioSuggestionChange{{Field: field, Value: "expand", Reason: "model request"}}})
		if !errors.Is(err, ErrAgentStudioSuggestionInvalid) {
			t.Fatalf("field %q error = %v", field, err)
		}
	}
	if executor.calls != 0 {
		t.Fatal("authority-bearing suggestion reached executor")
	}
	_, err = service.ApplySelected(ctx, AgentStudioSuggestion{PersonaID: "persona-a", Revision: 1, Digest: sealed.Digest, Changes: []AgentStudioSuggestionChange{{Field: AgentStudioFieldPurpose, Value: "stale", Reason: "old view"}}})
	if !errors.Is(err, ErrAgentStudioSuggestionStale) {
		t.Fatalf("stale error = %v", err)
	}
}

func TestTodo_AGENT_051_Conformance(t *testing.T) {
	profile := studioProfile(t)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	current := AgentStudioDraftSnapshot{PersonaID: "persona-a", Revision: 4, Version: agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: "persona-a", Version: 1, ContentDigest: sealed.Digest, Profile: studioProfileJSON(t, sealed.Profile)}, State: agentpersonastore.StateDraft}
	executor := &studioExecutorFake{}
	service := &AgentStudioSuggestionService{Drafts: studioDraftSourceFake{snapshot: current}, Profiles: personaProfileBuilderFake{}, Executor: executor, Authorizer: &personaAdminCommandAuthFake{}}
	_, err = service.ApplySelected(studioContext(t), AgentStudioSuggestion{PersonaID: "persona-a", Revision: 4, Digest: sealed.Digest, Changes: []AgentStudioSuggestionChange{{Field: AgentStudioFieldChannelClasses, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelExternal}, Reason: "wider reach"}}})
	if !errors.Is(err, ErrAgentStudioSuggestionInvalid) {
		t.Fatalf("external channel error = %v", err)
	}
	if executor.calls != 0 {
		t.Fatal("invalid scope change reached executor")
	}
}
