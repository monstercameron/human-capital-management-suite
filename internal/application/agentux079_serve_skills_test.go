package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// agentUX079ServeRegistries builds empty served registries the way
// composition does, then binds the chat reply and the demo skills under
// test.
func agentUX079ServeRegistries(t *testing.T) (*capability.Registry, *agentskills.Registry) {
	t.Helper()
	catalog := capability.NewRegistry()
	skills := agentskills.NewRegistry(catalog)
	if _, err := bindPersonaChatReplySkill(catalog, skills); err != nil {
		t.Fatal(err)
	}
	if _, err := BindAgentUX079AmbientSkills(skills); err != nil {
		t.Fatal(err)
	}
	if _, err := BindAgentUX079SupportSkills(catalog, skills); err != nil {
		t.Fatal(err)
	}
	return catalog, skills
}

// TestAgentUX079_DemoSkillsPublished: the served registry publishes the
// ambient offer skills and Support Desk's effect skills with the exact pins
// the starters sealed, so the Agents page can list all four agents.
func TestAgentUX079_DemoSkillsPublished(t *testing.T) {
	catalog, skills := agentUX079ServeRegistries(t)
	starter, err := agentUXDemoSupportStarter()
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range starter.SkillPins {
		record, ok := skills.Lookup(pin.Key())
		if !ok || record.Digest != pin.Digest {
			t.Fatalf("served registry lacks the starter pin %+v", pin)
		}
	}
	for _, ambient := range AgentUXAmbientStarters() {
		for _, pin := range ambient.SkillPins {
			record, ok := skills.Lookup(pin.Key())
			if !ok || record.Digest != pin.Digest {
				t.Fatalf("served registry lacks the %s pin %+v", ambient.ID, pin)
			}
		}
	}
	for _, id := range []string{agentskills.SupportCreateTicket, agentskills.SupportAlertChannel} {
		record, ok := catalog.Lookup(capability.Key{ID: id, Version: 1})
		if !ok || !record.Definition.AgentEligible || record.Definition.EffectClass != capability.EffectInternalMutation {
			t.Fatalf("served capabilities lack an eligible %s: %+v", id, record.Definition)
		}
		if _, err := refuseAgentUX079SupportEffect(context.Background(), nil); err == nil {
			t.Fatalf("support capability %s executes outside the desk worker", id)
		}
	}
	if _, err := BindAgentUX079AmbientSkills(skills); err == nil {
		t.Fatal("ambient skills published twice")
	}
	if _, err := BindAgentUX079SupportSkills(catalog, skills); err == nil {
		t.Fatal("support skills published twice")
	}
	if _, err := BindAgentUX079AmbientSkills(nil); err == nil {
		t.Fatal("ambient skills bound without a registry")
	}
	if _, err := BindAgentUX079SupportSkills(nil, skills); err == nil {
		t.Fatal("support skills bound without capabilities")
	}
}

// TestAgentUX079_DemoSkillDescriptions: every demo skill states its work in
// plain words, and Support Desk's two skills do not share one line.
func TestAgentUX079_DemoSkillDescriptions(t *testing.T) {
	byID := map[string]string{}
	for _, definition := range agentskills.AmbientOfferSkills() {
		byID[definition.ID] = definition.Description
	}
	for _, effect := range agentskills.SupportEffectSkills() {
		byID[effect.Skill.ID] = effect.Skill.Description
	}
	ticket, alert := byID[agentskills.SupportCreateTicket], byID[agentskills.SupportAlertChannel]
	if ticket == "" || ticket == alert || !strings.Contains(ticket, "ticket") || !strings.Contains(alert, "alert") {
		t.Fatalf("support descriptions are shared or unclear: %q / %q", ticket, alert)
	}
	task, reminder := byID["hcmnext.ambient.task.offer"], byID["hcmnext.ambient.reminder.offer"]
	if !strings.Contains(task, "task") || !strings.Contains(reminder, "reminder") {
		t.Fatalf("ambient descriptions are unclear: %q / %q", task, reminder)
	}
	for id, description := range byID {
		if description == "" || strings.Contains(description, "source-bound") {
			t.Fatalf("skill %s is not in plain words: %q", id, description)
		}
	}
}

// TestAgentUX079_SkillDiscoveryPurposes: the page derives discovery purposes
// from the served registry, so a skill is listed under the purposes it
// requires; without a registry the chat purpose stands alone as before.
func TestAgentUX079_SkillDiscoveryPurposes(t *testing.T) {
	_, skills := agentUX079ServeRegistries(t)
	purposes := agentSkillRegistryPurposes(skills)
	for _, want := range []string{personaChatReplyPurpose, "customer-support", "chat.ambient.offer"} {
		found := false
		for _, purpose := range purposes {
			found = found || purpose == want
		}
		if !found {
			t.Fatalf("purposes %v lack %q", purposes, want)
		}
	}
	if alone := agentSkillRegistryPurposes(nil); len(alone) != 1 || alone[0] != personaChatReplyPurpose {
		t.Fatalf("nil registry purposes = %v, want only the chat purpose", alone)
	}
}

// agentUX079AvailableBackend is the persona store behind the page tests: one
// placed persona and the published set.
type agentUX079AvailableBackend struct {
	placed    []agentpersonastore.PersonaVersion
	published []agentpersonastore.PersonaVersion
}

func (b agentUX079AvailableBackend) ListAvailable(context.Context, values.TenantId, []agentpersonastore.AvailableInstallation) ([]agentpersonastore.PersonaVersion, error) {
	return b.placed, nil
}

func (b agentUX079AvailableBackend) ListPublished(context.Context, values.TenantId) ([]agentpersonastore.PersonaVersion, error) {
	return b.published, nil
}

type agentUX079AvailableAudience struct{}

func (agentUX079AvailableAudience) ResolveAvailablePersonaInstallations(context.Context, *trust.Principal) ([]agentpersonastore.AvailableInstallation, error) {
	return nil, nil
}

type agentUX079AvailableDiscovery struct {
	byPurpose map[string][]agentskills.SkillRecord
}

func (d agentUX079AvailableDiscovery) Discover(_ context.Context, _ *trust.Principal, purpose string) ([]agentskills.SkillRecord, error) {
	return d.byPurpose[purpose], nil
}

func agentUX079PagePrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(localAgentDemoTenant), Subject: localAgentDemoAdmin, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodMutualTLS, Assurance: trust.AssuranceSubstantial, SessionRef: "page", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "page"})
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

// agentUX079SealedPageVersion seals a persona profile the way the store
// holds it: raw JSON plus the seal digest.
func agentUX079SealedPageVersion(t *testing.T, profile agentpersona.PersonaProfile) agentpersonastore.PersonaVersion {
	t.Helper()
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	return agentpersonastore.PersonaVersion{PersonaID: profile.PersonaID, Version: int64(profile.Version), Profile: raw, ContentDigest: sealed.Digest}
}

func agentUX079PageProfile(personaID string, templateID string, pins ...agentskills.SkillPin) agentpersona.PersonaProfile {
	profile := agentpersona.PersonaProfile{PersonaID: personaID, Version: 1, Handle: "agent", DisplayName: "Agent", AvatarRef: "avatar", Purpose: "test", Owner: "owner", Steward: "steward", EvalSuiteRef: "suite",
		Audience:   agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"workspace"}, OrganizationScopes: []string{"tenant"}},
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1}, Instructions: "Be helpful.",
		SkillPins: append([]agentskills.SkillPin(nil), pins...), TierCeiling: agentskills.TierT1,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPublic}}
	sum := sha256.Sum256([]byte("agentux-079 page profile " + personaID))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	profile.Manifest = agentpersona.AgentManifestRef{ID: "page-manifest", Version: 1, SchemaVersion: 1, Digest: digest}
	if templateID != "" {
		profile.Template = &agentpersona.TemplateProvenance{ID: templateID, Version: 1, Digest: digest}
	}
	return profile
}

func agentUX079PageRecords(t *testing.T, skills *agentskills.Registry, ids ...string) []agentskills.SkillRecord {
	t.Helper()
	records := make([]agentskills.SkillRecord, 0, len(ids))
	for _, id := range ids {
		record, ok := skills.Lookup(agentskills.SkillKey{ID: id, Version: 1})
		if !ok {
			t.Fatalf("served registry lacks %s", id)
		}
		records = append(records, record)
	}
	return records
}

func agentUX079PagePins(t *testing.T, skills *agentskills.Registry, ids ...string) []agentskills.SkillPin {
	t.Helper()
	pins := make([]agentskills.SkillPin, 0, len(ids))
	for _, id := range ids {
		pin, err := skills.Pin(agentskills.SkillKey{ID: id, Version: 1})
		if err != nil {
			t.Fatal(err)
		}
		pins = append(pins, pin)
	}
	return pins
}

// TestAgentUX079_ReaderDiscoversAcrossPurposes: the page reader discovers
// pinned skills under every purpose the served registry requires, so a
// persona pinned on customer-support skills is listed; under the chat purpose
// alone it stays hidden, as before.
func TestAgentUX079_ReaderDiscoversAcrossPurposes(t *testing.T) {
	_, skills := agentUX079ServeRegistries(t)
	pins := agentUX079PagePins(t, skills, agentskills.SupportCreateTicket, agentskills.SupportAlertChannel)
	version := agentUX079SealedPageVersion(t, agentUX079PageProfile(AgentUXDemoSupportPersonaID, "", pins...))
	discovery := &agentUX079AvailableDiscovery{byPurpose: map[string][]agentskills.SkillRecord{
		"customer-support": agentUX079PageRecords(t, skills, agentskills.SupportCreateTicket, agentskills.SupportAlertChannel),
	}}
	principal := agentUX079PagePrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	reader := &TenantAvailablePersonaReader{Backend: agentUX079AvailableBackend{placed: []agentpersonastore.PersonaVersion{version}}, Audience: agentUX079AvailableAudience{}, Skills: discovery,
		Purposes: []string{personaChatReplyPurpose, "customer-support", "chat.ambient.offer"}}
	listed, err := reader.ListAvailable(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Profile.PersonaID != AgentUXDemoSupportPersonaID {
		t.Fatalf("listed = %v, want Support Desk", listed)
	}
	legacy := &TenantAvailablePersonaReader{Backend: agentUX079AvailableBackend{placed: []agentpersonastore.PersonaVersion{version}}, Audience: agentUX079AvailableAudience{}, Skills: discovery}
	hidden, err := legacy.ListAvailable(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 0 {
		t.Fatalf("chat-purpose discovery listed %+v, want nothing", hidden)
	}
}

// TestAgentUX079_ReaderListsUnplaced: published personas without an
// installation are listed once their pinned skills are all discoverable, at
// their latest published version; undiscoverable ones stay hidden, and
// without the published backend nothing unplaced is listed.
func TestAgentUX079_ReaderListsUnplaced(t *testing.T) {
	_, skills := agentUX079ServeRegistries(t)
	supportPins := agentUX079PagePins(t, skills, agentskills.SupportCreateTicket, agentskills.SupportAlertChannel)
	placed := agentUX079SealedPageVersion(t, agentUX079PageProfile("hcmnext.local.persona.policy_helper", "", agentUX079PagePins(t, skills, personaChatReplySkillID)...))
	old := agentUX079SealedPageVersion(t, agentUX079PageProfile(AgentUXDemoSupportPersonaID, agentUX081SupportDeskStarterID, supportPins...))
	profile := agentUX079PageProfile(AgentUXDemoSupportPersonaID, agentUX081SupportDeskStarterID, supportPins...)
	profile.Version = 3
	profile.DisplayName = "Support Desk"
	current := agentUX079SealedPageVersion(t, profile)
	ambientPins := agentUX079PagePins(t, skills, "hcmnext.ambient.task.offer")
	ambient := agentUX079SealedPageVersion(t, agentUX079PageProfile("hcmnext.local.persona.task_catcher", "hcmnext.persona_template.task_catcher", ambientPins...))
	backend := agentUX079AvailableBackend{placed: []agentpersonastore.PersonaVersion{placed}, published: []agentpersonastore.PersonaVersion{placed, old, current, ambient}}
	discovery := &agentUX079AvailableDiscovery{byPurpose: map[string][]agentskills.SkillRecord{
		personaChatReplyPurpose: agentUX079PageRecords(t, skills, personaChatReplySkillID),
		"customer-support":      agentUX079PageRecords(t, skills, agentskills.SupportCreateTicket, agentskills.SupportAlertChannel),
	}}
	principal := agentUX079PagePrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), principal)
	reader := &TenantAvailablePersonaReader{Backend: backend, Audience: agentUX079AvailableAudience{}, Skills: discovery,
		Purposes: []string{personaChatReplyPurpose, "customer-support", "chat.ambient.offer"}, Published: backend}
	listed, err := reader.ListAvailable(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed %d personas, want the placed one and Support Desk: %v", len(listed), listed)
	}
	var support *agentpersona.PersonaVersion
	for i := range listed {
		if listed[i].Profile.PersonaID == AgentUXDemoSupportPersonaID {
			support = &listed[i]
		}
	}
	if support == nil || support.Profile.Version != 3 {
		t.Fatalf("Support Desk listed = %+v, want the latest published version", support)
	}
	placedOnly := &TenantAvailablePersonaReader{Backend: backend, Audience: agentUX079AvailableAudience{}, Skills: discovery,
		Purposes: []string{personaChatReplyPurpose, "customer-support", "chat.ambient.offer"}}
	without, err := placedOnly.ListAvailable(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(without) != 1 {
		t.Fatalf("listed %d personas without the published backend, want only the placed one", len(without))
	}
}

// TestAgentUX079_CatalogListPage: the Agents page roster lists every
// skill-discoverable persona with its skills in plain words, while Ask keeps
// offering only the kinds that answer questions.
func TestAgentUX079_CatalogListPage(t *testing.T) {
	_, skills := agentUX079ServeRegistries(t)
	supportPins := agentUX079PagePins(t, skills, agentskills.SupportCreateTicket, agentskills.SupportAlertChannel)
	support := agentUX079PageProfile(AgentUXDemoSupportPersonaID, agentUX081SupportDeskStarterID, supportPins...)
	support.DisplayName = "Support Desk"
	sealedSupport, err := agentpersona.Seal(support)
	if err != nil {
		t.Fatal(err)
	}
	answerPins := agentUX079PagePins(t, skills, personaChatReplySkillID)
	answer := agentUX079PageProfile("hcmnext.local.persona.policy_helper", "hcmnext.persona_template.policy_helper", answerPins...)
	answer.DisplayName = "Policy Helper"
	sealedAnswer, err := agentpersona.Seal(answer)
	if err != nil {
		t.Fatal(err)
	}
	discovery := &agentUX079AvailableDiscovery{byPurpose: map[string][]agentskills.SkillRecord{
		personaChatReplyPurpose: agentUX079PageRecords(t, skills, personaChatReplySkillID),
		"customer-support":      agentUX079PageRecords(t, skills, agentskills.SupportCreateTicket, agentskills.SupportAlertChannel),
	}}
	catalog := &AgentUserCatalog{Personas: agentUserCatalogPersonas{sealedSupport, sealedAnswer}, Skills: discovery,
		Purposes: []string{personaChatReplyPurpose, "customer-support", "chat.ambient.offer"}}
	principal := agentUX079PagePrincipal(t)
	roster, err := catalog.ListPage(trust.WithPrincipal(context.Background(), principal), principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(roster) != 2 {
		t.Fatalf("roster has %d agents, want both: %v", len(roster), roster)
	}
	if len(roster[0].Skills) != 2 || roster[0].Skills[0] == roster[0].Skills[1] {
		t.Fatalf("Support Desk skills = %v, want two distinct plain-words lines", roster[0].Skills)
	}
	ask, err := catalog.List(trust.WithPrincipal(context.Background(), principal), principal)
	if err != nil {
		t.Fatal(err)
	}
	if len(ask) != 1 || ask[0].ID != "hcmnext.local.persona.policy_helper" {
		t.Fatalf("Ask offers %+v, want only Policy Helper", ask)
	}
}
