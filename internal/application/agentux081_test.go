package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentUX081Persona seals a persona whose template provenance names the given
// starter, the way a tenant-owned copy of a starter keeps its template pin.
func agentUX081Persona(t *testing.T, id, name, starterID string, pin agentskills.SkillPin) agentpersona.PersonaVersion {
	t.Helper()
	profile := agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: "agent." + id, Version: 1, Digest: "manifest-" + id, SchemaVersion: 1},
		PersonaID: "persona." + id, Version: 1, Handle: id, DisplayName: name, AvatarRef: "avatar:" + id,
		Purpose: "agent.self_service", Audience: agentpersona.Audience{Roles: []string{"employee"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-a"}},
		SkillPins: []agentskills.SkillPin{pin}, TierCeiling: agentskills.TierRead,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect},
		ChannelClasses:    []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		Instructions:      "Answer within the declared skill set.", Owner: "owner", Steward: "steward", EvalSuiteRef: "eval:" + id,
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1000},
	}
	if starterID != "" {
		profile.Template = &agentpersona.TemplateProvenance{ID: starterID, Version: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
	}
	version, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatalf("seal persona: %v", err)
	}
	return version
}

func agentUX081Discovery(pin agentskills.SkillPin) *agentUserCatalogDiscovery {
	allowed := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Reply"}, Digest: pin.Digest, Status: agentskills.StatusActive}
	return &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{personaChatReplyPurpose: {allowed}}}
}

// TestTodo_AGENTUX_081 is the Ask-side half of the kind filter: the Agents
// page offers only agents of a kind that answers questions. An agent copied
// from Birthday Buddy stays out of "Who should answer?" while a Policy Helper
// copy, a tenant-authored agent with no template, and an agent from an unknown
// starter stay offered.
func TestTodo_AGENTUX_081(t *testing.T) {
	pin := agentskills.SkillPin{ID: "persona.chat_reply", Version: 1, Digest: "digest-v1"}
	personas := agentUserCatalogPersonas{
		agentUX081Persona(t, "policy-helper", "Policy Helper", "hcmnext.persona_template.policy_helper", pin),
		agentUX081Persona(t, "birthday-buddy", "Birthday Buddy", "hcmnext.persona_template.birthday_buddy", pin),
		agentUX081Persona(t, "task-catcher", "Task Catcher", "hcmnext.persona_template.task_catcher", pin),
		agentUX081Persona(t, "support-desk", "Support Desk", "hcmnext.persona_template.support_desk", pin),
		agentUX081Persona(t, "tenant-custom", "Tenant Custom", "", pin),
		agentUX081Persona(t, "unknown-starter", "Unknown Starter", "hcmnext.persona_template.something_new", pin),
	}
	catalog := &AgentUserCatalog{Personas: personas, Skills: agentUX081Discovery(pin)}
	principal := agentUserCatalogPrincipal(t)
	got, err := catalog.List(trust.WithPrincipal(context.Background(), principal), principal)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := map[string]bool{}
	for _, agent := range got {
		names[agent.Name] = true
	}
	for _, want := range []string{"Policy Helper", "Tenant Custom", "Unknown Starter"} {
		if !names[want] {
			t.Fatalf("Ask lost %q: offered = %v", want, names)
		}
	}
	for _, excluded := range []string{"Birthday Buddy", "Task Catcher", "Support Desk"} {
		if names[excluded] {
			t.Fatalf("Ask offers %q, which never answers questions: offered = %v", excluded, names)
		}
	}
}

// TestTodo_AGENTUX_081_MentionCandidates is the mention-side half: the
// @-mention candidates (which also feed the mention menu and /ask) skip an
// agent whose declared kind never answers questions.
func TestTodo_AGENTUX_081_MentionCandidates(t *testing.T) {
	pin := agentskills.SkillPin{ID: "persona.chat_reply", Version: 1, Digest: "digest-v1"}
	profiles := []agentpersona.PersonaVersion{
		agentUX081Persona(t, "policy-helper", "Policy Helper", "hcmnext.persona_template.policy_helper", pin),
		agentUX081Persona(t, "birthday-buddy", "Birthday Buddy", "hcmnext.persona_template.birthday_buddy", pin),
	}
	var identities []personaRegisteredChatIdentity
	for _, profile := range profiles {
		identities = append(identities, personaRegisteredChatIdentity{TenantID: "tenant-a", ReferenceID: "agent:" + profile.Profile.Handle, PersonaID: profile.Profile.PersonaID, Active: true})
	}
	p, principal := candidatePrincipal(t)
	lookup := &personaCandidateLookupFunc{facts: map[string]personaReferenceFacts{}}
	for _, profile := range profiles {
		facts := currentPersonaReferenceFacts("agent:" + profile.Profile.Handle)
		facts.PersonaID = profile.Profile.PersonaID
		facts.ConversationID = "room-a"
		facts.PersonaVersion = uint64(profile.Profile.Version)
		facts.CurrentVersion = facts.PersonaVersion
		lookup.facts["tenant-a/room-a/agent:"+profile.Profile.Handle] = facts
	}
	source, err := newPersonaChatReferenceSource(personaCandidateProfiles{profiles: profiles}, personaCandidateIdentities{identities: identities}, lookup, func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.ListPersonaReferenceCandidates(trust.WithPrincipal(context.Background(), p), principal, "tenant-a", "room-a", "")
	if err != nil {
		t.Fatalf("list candidates: %v", err)
	}
	if len(got) != 1 || got[0].Display != "Policy Helper" {
		t.Fatalf("candidates = %+v, want only Policy Helper", got)
	}
}

type personaCandidateLookupFunc struct {
	facts map[string]personaReferenceFacts
}

func (l *personaCandidateLookupFunc) LookupPersonaReference(_ context.Context, tenant, conversation, reference string) (personaReferenceFacts, error) {
	return l.facts[tenant+"/"+conversation+"/"+reference], nil
}

type agentUX081StaticTasks struct{}

func (agentUX081StaticTasks) Snapshot(context.Context, productui.AgentSnapshotRequest) (productui.AgentSnapshot, error) {
	return productui.AgentSnapshot{Availability: productui.AgentsAvailable, StartAvailable: true}, nil
}

type agentUX081StaticPage struct {
	snapshot productui.AgentSnapshot
}

func (c agentUX081StaticPage) Snapshot(context.Context, productui.AgentSnapshotRequest) (productui.AgentSnapshot, error) {
	return c.snapshot, nil
}

// TestTodo_AGENTUX_081_Integration drives the whole Ask chain: the catalog
// binding filters sealed personas by declared kind, and the Agents page
// renders "Who should answer?" from the filtered list, without Birthday Buddy.
func TestTodo_AGENTUX_081_Integration(t *testing.T) {
	pin := agentskills.SkillPin{ID: "persona.chat_reply", Version: 1, Digest: "digest-v1"}
	binding, err := NewAgentPageCatalogBinding(agentUX081StaticTasks{},
		agentUserCatalogPersonas{
			agentUX081Persona(t, "policy-helper", "Policy Helper", "hcmnext.persona_template.policy_helper", pin),
			agentUX081Persona(t, "birthday-buddy", "Birthday Buddy", "hcmnext.persona_template.birthday_buddy", pin),
		},
		agentUX081Discovery(pin))
	if err != nil {
		t.Fatal(err)
	}
	principal := agentUserCatalogPrincipal(t)
	snapshot, err := binding.Snapshot(trust.WithPrincipal(context.Background(), principal),
		productui.AgentSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil {
		t.Fatal(err)
	}
	view := productui.View{Tenant: "tenant-a", Principal: "user-a"}
	markup, err := ui.RenderToString(productui.BuildAgentsPage(view, agentUX081StaticPage{snapshot: snapshot}))
	if err != nil {
		t.Fatal(err)
	}
	choices := markup[strings.Index(markup, "Who should answer?"):]
	choices = choices[:strings.Index(choices, "</fieldset>")]
	if !strings.Contains(choices, "Policy Helper") {
		t.Fatalf("Ask lost Policy Helper: %s", choices)
	}
	if strings.Contains(choices, "Birthday Buddy") {
		t.Fatalf("Ask offers Birthday Buddy: %s", choices)
	}
}

// TestTodo_AGENTUX_081_StarterKindsDeclared keeps the declaration honest: every
// starter in every catalog declares a kind, the non-answering kinds are
// exactly the announce, ambient and inbox families, and the resolver answers
// from the declaration.
func TestTodo_AGENTUX_081_StarterKindsDeclared(t *testing.T) {
	catalog := agentskills.NewRegistry(agentuxDemoSupportCapabilities{})
	for _, def := range agentskills.SupportEffectSkills() {
		if err := catalog.Publish(def.Skill); err != nil {
			t.Fatal(err)
		}
	}
	support, err := AgentUXDemoSupportStarter(catalog)
	if err != nil {
		t.Fatal(err)
	}
	starters := append(append([]agenttemplate.PersonaStarter{AgentUXDemoBirthdayStarter(), support}, AgentUXAmbientStarters()...), agenttemplate.PersonaStarters()...)
	seen := map[string]agenttemplate.PersonaStarterKind{}
	for _, starter := range starters {
		if starter.Kind == "" {
			t.Fatalf("starter %q declares no kind", starter.ID)
		}
		seen[starter.ID] = starter.Kind
		if got := agentUX081StarterKind(starter.ID); got != starter.Kind {
			t.Fatalf("resolver says %q is %q, declared %q", starter.ID, got, starter.Kind)
		}
	}
	for id, kind := range map[string]agenttemplate.PersonaStarterKind{
		"hcmnext.persona_template.birthday_buddy": agenttemplate.PersonaStarterKindAnnounce,
		"hcmnext.persona_template.task_catcher":   agenttemplate.PersonaStarterKindAmbient,
		"hcmnext.persona_template.reminder":       agenttemplate.PersonaStarterKindAmbient,
		"hcmnext.persona_template.support_desk":   agenttemplate.PersonaStarterKindInbox,
	} {
		if seen[id] != kind {
			t.Fatalf("starter %q kind = %q, want %q", id, seen[id], kind)
		}
		if agentUX081StarterKind(id).AnswersQuestions() {
			t.Fatalf("starter %q answers questions", id)
		}
	}
	for _, id := range []string{"hcmnext.persona_template.policy_helper", "hcmnext.persona_template.assistant"} {
		if !agentUX081StarterKind(id).AnswersQuestions() {
			t.Fatalf("starter %q does not answer questions", id)
		}
	}
	if agentUX081StarterKind("hcmnext.persona_template.something_new") != "" {
		t.Fatal("unknown starter resolved to a kind")
	}
	if !agentUX081StarterKind("hcmnext.persona_template.something_new").AnswersQuestions() {
		t.Fatal("unknown starter is not offered")
	}
}
