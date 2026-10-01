package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaStarterManifestFake struct {
	manifest agentmanifest.Manifest
	err      error
	id       string
	calls    int
}

func (f *personaStarterManifestFake) ResolveCurrentPersonaManifest(_ context.Context, id string) (agentmanifest.Manifest, error) {
	f.calls++
	f.id = id
	return f.manifest, f.err
}

type personaStarterInstructionsFake struct {
	text    string
	err     error
	id      string
	version uint64
	digest  string
	calls   int
}

func (f *personaStarterInstructionsFake) ResolvePersonaInstructions(_ context.Context, id string, version uint64, digest string) (string, error) {
	f.calls++
	f.id, f.version, f.digest = id, version, digest
	return f.text, f.err
}

func TestTodo_AGENTP_023_DraftBuilderCreatesValidatedTenantDraft(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	instructions := "Answer policy questions only from approved policy material, and cite the supporting record."
	manifest := personaStarterManifest(instructions)
	store := &personaDraftStoreFake{}
	profiles := &personaStarterProfileBuilderSpy{}
	drafts := &PersonaAdminDraftService{Store: store, Authorizer: &personaCreateAuthorizerFake{allowedTenant: "tenant-a"}, Profiles: profiles, Clock: personaDraftClockFake{now}}
	manifests := &personaStarterManifestFake{manifest: manifest}
	texts := &personaStarterInstructionsFake{text: instructions}
	builder := &PersonaStarterDraftBuilder{Drafts: drafts, Manifests: manifests, Instructions: texts}
	ctx := trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, "tenant-a", "user:creator", now))
	receipt, err := builder.CreateDraft(ctx, PersonaStarterDraftRequest{
		StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1,
		PersonaID: "tenant-a.policy-helper", AvatarRef: "avatar:policy-helper", OrganizationScopes: []string{"org-a"},
		ManifestID: manifest.ID, BusinessOwnerID: "user:owner", TechnicalStewardID: "user:steward",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.PersonaID != "tenant-a.policy-helper" || receipt.Lifecycle != string(agentpersonastore.StateDraft) || store.calls != 1 {
		t.Fatalf("draft receipt/store calls = %+v / %d", receipt, store.calls)
	}
	profile := profiles.profile
	manifestDigest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if profile.Manifest != (agentpersona.AgentManifestRef{ID: manifest.ID, Version: uint32(manifest.Version), Digest: manifestDigest, SchemaVersion: manifest.SchemaVersion}) {
		t.Fatalf("manifest pin = %+v", profile.Manifest)
	}
	if texts.calls != 1 || texts.id != manifest.ID || texts.version != manifest.Version || texts.digest != manifest.InstructionsDigest || profile.Instructions != instructions {
		t.Fatalf("instructions were not resolved from the exact manifest: resolver=%+v profile=%q", texts, profile.Instructions)
	}
	starter, _ := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	if profile.TierCeiling != agentskills.TierT0 || len(profile.SkillPins) != len(starter.SkillPins) || profile.Owner != "user:owner" || profile.Steward != "user:steward" || profile.Audience.OrganizationScopes[0] != "org-a" {
		t.Fatalf("starter profile omitted governed bounds/owners: %+v", profile)
	}
	if profile.ChannelClasses[0] != agentpersona.ChannelPrivate || profile.ChannelClasses[1] != agentpersona.ChannelPublic || profile.Manifest.Digest == "" {
		t.Fatalf("policy starter reach or manifest digest = %+v", profile)
	}
}

func TestTodo_AGENTP_023_DraftBuilderSecurity(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	instructions := "Use approved records and ask for clarification when necessary."
	manifest := personaStarterManifest(instructions)
	for _, tc := range []struct {
		name        string
		request     func() PersonaStarterDraftRequest
		manifest    agentmanifest.Manifest
		text        string
		manifestErr error
		textErr     error
	}{
		{name: "unknown starter", request: func() PersonaStarterDraftRequest {
			req := validPersonaStarterRequest(manifest)
			req.StarterVersion = 99
			return req
		}, manifest: manifest, text: instructions},
		{name: "owner steward must differ", request: func() PersonaStarterDraftRequest {
			req := validPersonaStarterRequest(manifest)
			req.TechnicalStewardID = req.BusinessOwnerID
			return req
		}, manifest: manifest, text: instructions},
		{name: "caller cannot substitute instruction text", request: func() PersonaStarterDraftRequest { return validPersonaStarterRequest(manifest) }, manifest: manifest, text: "Forged text."},
		{name: "manifest resolution failure", request: func() PersonaStarterDraftRequest { return validPersonaStarterRequest(manifest) }, manifest: manifest, text: instructions, manifestErr: errors.New("store unavailable")},
		{name: "instruction resolution failure", request: func() PersonaStarterDraftRequest { return validPersonaStarterRequest(manifest) }, manifest: manifest, text: instructions, textErr: errors.New("instruction store unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &personaDraftStoreFake{}
			service := &PersonaAdminDraftService{Store: store, Authorizer: &personaCreateAuthorizerFake{allowedTenant: "tenant-a"}, Profiles: &personaStarterProfileBuilderSpy{}, Clock: personaDraftClockFake{now}}
			builder := &PersonaStarterDraftBuilder{Drafts: service, Manifests: &personaStarterManifestFake{manifest: tc.manifest, err: tc.manifestErr}, Instructions: &personaStarterInstructionsFake{text: tc.text, err: tc.textErr}}
			ctx := trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, "tenant-a", "user:creator", now))
			if _, err := builder.CreateDraft(ctx, tc.request()); !errors.Is(err, ErrPersonaDraftInvalid) {
				t.Fatalf("CreateDraft error = %v, want invalid draft", err)
			}
			if store.calls != 0 {
				t.Fatalf("invalid starter reached persistence %d times", store.calls)
			}
		})
	}
}

func TestTodo_AGENTP_023_DraftBuilderStarterBounds(t *testing.T) {
	manifest := personaStarterManifest("Answer only from approved records.")
	for _, tc := range []struct {
		id       string
		tier     agentskills.SideEffectTier
		kinds    []agentpersona.ConversationKind
		channels []agentpersona.ChannelClass
	}{
		{id: "hcmnext.persona_template.onboarding_coordinator", tier: agentskills.TierT2, kinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect, agentpersona.ConversationGroup, agentpersona.ConversationChannel, agentpersona.ConversationThread}, channels: []agentpersona.ChannelClass{agentpersona.ChannelPrivate, agentpersona.ChannelPublic}},
		{id: "hcmnext.persona_template.comp_analyst", tier: agentskills.TierT1, kinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect, agentpersona.ConversationGroup, agentpersona.ConversationChannel, agentpersona.ConversationThread}, channels: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}},
		{id: "hcmnext.persona_template.policy_helper", tier: agentskills.TierT0, kinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect, agentpersona.ConversationGroup, agentpersona.ConversationChannel, agentpersona.ConversationThread}, channels: []agentpersona.ChannelClass{agentpersona.ChannelPrivate, agentpersona.ChannelPublic}},
		{id: "hcmnext.persona_template.schedule_fixer", tier: agentskills.TierT3, kinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect, agentpersona.ConversationGroup, agentpersona.ConversationChannel, agentpersona.ConversationThread}, channels: []agentpersona.ChannelClass{agentpersona.ChannelPrivate, agentpersona.ChannelPublic}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			starter, ok := agenttemplate.PersonaStarterFor(tc.id, 1)
			if !ok {
				t.Fatal("starter missing")
			}
			profile := personaStarterProfile(starter, validPersonaStarterRequest(manifest), manifest, "Answer only from approved records.")
			pin := agenttemplate.PersonaStarterPin(starter)
			if profile.Template == nil || profile.Template.ID != pin.ID || profile.Template.Version != pin.Version || profile.Template.Digest != pin.Digest || profile.AlwaysPrivate != starter.AlwaysPrivate || len(profile.AllowedPlacementClasses) != len(starter.AllowedChannelClasses) {
				t.Fatalf("starter provenance/privacy/placement dropped: %+v", profile)
			}
			if starter.ID == "hcmnext.persona_template.schedule_fixer" {
				if profile.TierForConversation(agentpersona.ConversationDirect) != agentskills.TierT3 || profile.TierForConversation(agentpersona.ConversationChannel) != agentskills.TierT1 || profile.TierForConversation(agentpersona.ConversationGroup) != agentskills.TierT1 {
					t.Fatalf("Schedule Fixer allowed a shared T3 step: %+v", profile.ConversationTierCeilings)
				}
			}
			if profile.TierCeiling != tc.tier || !equalConversationKinds(profile.ConversationKinds, tc.kinds) || !equalPersonaChannelClasses(profile.ChannelClasses, tc.channels) {
				t.Fatalf("starter authority bounds = tier %s, kinds %v, channels %v", profile.TierCeiling, profile.ConversationKinds, profile.ChannelClasses)
			}
			for _, channel := range profile.ChannelClasses {
				if channel == agentpersona.ChannelExternal {
					t.Fatal("starter permits external channels")
				}
			}
		})
	}
}

func equalConversationKinds(left, right []agentpersona.ConversationKind) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalPersonaChannelClasses(left, right []agentpersona.ChannelClass) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func validPersonaStarterRequest(manifest agentmanifest.Manifest) PersonaStarterDraftRequest {
	return PersonaStarterDraftRequest{StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1, PersonaID: "tenant-a.policy-helper", AvatarRef: "avatar:policy", OrganizationScopes: []string{"org-a"}, ManifestID: manifest.ID, BusinessOwnerID: "user:owner", TechnicalStewardID: "user:steward"}
}

func personaStarterManifest(instructions string) agentmanifest.Manifest {
	hash := sha256.Sum256([]byte(instructions))
	ref := func(id string, char byte) agentmanifest.Reference {
		return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: "sha256:" + strings.Repeat(string(char), 64)}
	}
	return agentmanifest.Manifest{SchemaVersion: 1, ID: "agent.policy-helper", Version: 3, OwnerID: "user:agent-owner", Purpose: "Answer tenant policy questions", InstructionsDigest: "sha256:" + hex.EncodeToString(hash[:]), SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{}, ModelPolicy: ref("model.default", 'a'), AutonomyCeiling: "private_answer", Budget: agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 1000, MaxOutputTokens: 500, MaxConcurrentRuns: 1}, OutputSchema: ref("output.answer", 'b'), ContextGrants: []agentmanifest.Reference{}, EvaluationRefs: []agentmanifest.Reference{ref("eval.persona", 'c')}}
}

type personaStarterProfileBuilderSpy struct{ profile agentpersona.PersonaProfile }

func (s *personaStarterProfileBuilderSpy) Build(profile agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	s.profile = profile
	return agentpersona.Seal(profile)
}
