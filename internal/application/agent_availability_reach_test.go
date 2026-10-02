package application

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	agentReachTenant = "tenant-a"
	agentReachAgent  = "agent:coach"
)

// agentReachBackend holds published persona versions and returns one only for
// an installation tuple the audience resolver allowed. It records what it was
// asked, so a test can see whether an outsider's request named any agent.
type agentReachBackend struct {
	versions []agentpersonastore.PersonaVersion
	asked    [][]agentpersonastore.AvailableInstallation
}

func (b *agentReachBackend) ListAvailable(_ context.Context, _ values.TenantId, allowed []agentpersonastore.AvailableInstallation) ([]agentpersonastore.PersonaVersion, error) {
	b.asked = append(b.asked, slices.Clone(allowed))
	out := make([]agentpersonastore.PersonaVersion, 0, len(b.versions))
	for _, version := range b.versions {
		for _, tuple := range allowed {
			if tuple.PersonaID == version.PersonaID && tuple.PersonaVersion == version.Version {
				out = append(out, version)
				break
			}
		}
	}
	return out, nil
}

// agentReachLookup answers the exact-installation recheck for the
// conversations the agent is placed in and for no other.
type agentReachLookup struct {
	placed  map[string]string
	persona string
}

func (l agentReachLookup) LookupPersonaReference(_ context.Context, tenant, conversation, reference string) (personaReferenceFacts, error) {
	installation, ok := l.placed[conversation]
	if !ok || reference != agentReachAgent {
		return personaReferenceFacts{}, errPersonaReferenceInactive
	}
	return personaReferenceFacts{ReferenceID: reference, TenantID: tenant, ConversationID: conversation, PersonaID: l.persona, InstallationID: installation,
		PersonaVersion: 1, CurrentVersion: 1, InstallationState: personaReferenceActive, PersonaLifecycle: personaReferencePublished}, nil
}

// agentReachChat is the chat side of the fixture: #general holds two people
// and the agent's own chat identity, "dm" is the first person's direct
// conversation with the agent, and "other" has no agent placed in it.
func agentReachChat() agentReachChatFake {
	member := func(room, subject string) chat.Membership {
		return chat.Membership{ConversationID: room, TenantID: agentReachTenant, HomeTenantID: agentReachTenant, SubjectID: subject}
	}
	return agentReachChatFake{
		order: []string{"general", "dm", "other"},
		members: map[string][]chat.Membership{
			"general": {member("general", "user-a"), member("general", "user-b"), member("general", agentReachAgent)},
			"dm":      {member("dm", "user-a"), member("dm", agentReachAgent)},
			"other":   {member("other", "user-a"), member("other", "user-b")},
		},
	}
}

// agentReachChatFake lists, as Chat does, only the conversations the asking
// principal is a member of.
type agentReachChatFake struct {
	order   []string
	members map[string][]chat.Membership
}

func (f agentReachChatFake) ListConversations(_ context.Context, request chat.ListConversationsRequest) (chat.ListConversationsResponse, error) {
	var rooms []chat.Conversation
	for _, room := range f.order {
		for _, member := range f.members[room] {
			if member.SubjectID == request.Principal.SubjectID && request.Page.Cursor == "" {
				rooms = append(rooms, chat.Conversation{ID: room, TenantID: agentReachTenant})
			}
		}
	}
	return chat.ListConversationsResponse{Conversations: rooms}, nil
}

func (f agentReachChatFake) ListMemberships(_ context.Context, request chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	if request.Page.Cursor != "" {
		return chat.ListMembershipsResponse{}, nil
	}
	return chat.ListMembershipsResponse{Memberships: f.members[request.ConversationID]}, nil
}

func agentReachDirectory() *audienceDirectoryFake {
	return &audienceDirectoryFake{facts: map[string]PersonaAudienceMember{
		agentReachTenant + "/user-a": {SubjectID: "user-a", Roles: []string{"employee"}, Populations: []string{"employees"}, OrganizationScope: "org-a"},
		agentReachTenant + "/user-b": {SubjectID: "user-b", Roles: []string{"contractor"}, Populations: []string{"contractors"}, OrganizationScope: "org-a"},
	}}
}

type agentReachFixture struct {
	persona   agentpersona.PersonaVersion
	backend   *agentReachBackend
	directory *audienceDirectoryFake
	source    *DatabasePersonaAudienceSource
	reader    *TenantAvailablePersonaReader
	catalog   *AgentUserCatalog
	mentions  *productionPersonaChatReferenceSource
}

// newAgentReachFixture composes the availability path as the served cell does
// (one reader behind both the Agents page catalog and the mention lookup),
// with fakes only at the chat, directory and store edges. placed controls
// whether the agent is added to #general and the direct conversation.
func newAgentReachFixture(t *testing.T, placed bool) agentReachFixture {
	t.Helper()
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	persona := agentUserCatalogPersona(t, "people-coach", "People Coach", "agent.self_service", pin)
	audience := agentpersonastore.InstallationAudience{Roles: persona.Profile.Audience.Roles, Populations: persona.Profile.Audience.Populations, OrganizationScope: persona.Profile.Audience.OrganizationScopes}
	installs := audienceInstallFake{
		published:  []agentpersonastore.PersonaVersion{{TenantID: agentReachTenant, PersonaID: persona.Profile.PersonaID, Version: 1}},
		identities: map[string]agentpersonastore.PersonaChatIdentity{agentReachAgent: {TenantID: agentReachTenant, AgentID: agentReachAgent, PersonaID: persona.Profile.PersonaID, Active: true}},
	}
	lookup := agentReachLookup{persona: persona.Profile.PersonaID, placed: map[string]string{}}
	if placed {
		for _, room := range []string{"general", "dm"} {
			installs.active = append(installs.active, agentpersonastore.ActiveInstallation{PersonaID: persona.Profile.PersonaID, PersonaVersion: 1, InstallationID: "install-" + room, ConversationID: room, Audience: audience})
			lookup.placed[room] = "install-" + room
		}
	}
	backend := &agentReachBackend{versions: []agentpersonastore.PersonaVersion{availablePersonaCandidate(t, persona)}}
	directory := agentReachDirectory()
	source := &DatabasePersonaAudienceSource{Chat: agentReachChat(), Installations: audienceInstallStoreFake{store: installs}, Directory: directory}
	discovery := &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{
		personaChatReplyPurpose: {{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Read my profile"}, Digest: pin.Digest, Status: agentskills.StatusActive}},
	}}
	reader := &TenantAvailablePersonaReader{Backend: backend, Audience: &CurrentPersonaAudience{Source: source}, Skills: discovery}
	identities := personaCandidateIdentities{identities: []personaRegisteredChatIdentity{{TenantID: agentReachTenant, ReferenceID: agentReachAgent, PersonaID: persona.Profile.PersonaID, Active: true}}}
	mentions, err := newPersonaChatReferenceSource(reader, identities, lookup, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return agentReachFixture{persona: persona, backend: backend, directory: directory, source: source, reader: reader, catalog: &AgentUserCatalog{Personas: reader, Skills: discovery}, mentions: mentions}
}

func agentReachMentions(t *testing.T, f agentReachFixture, ctx context.Context, subject, conversation string) []chat.ReferenceCandidate {
	t.Helper()
	got, err := f.mentions.ListPersonaReferenceCandidates(ctx, chat.Principal{TenantID: agentReachTenant, SubjectID: subject}, agentReachTenant, conversation, "")
	if err != nil {
		t.Fatalf("mention lookup for %s in %s: %v", subject, conversation, err)
	}
	return got
}

// TestTodo_AGENTUX_021 places a published agent in #general and in a direct
// conversation: the member in its audience is offered it on the Agents page
// and in the mention lookup of each conversation where it is placed, and the
// agent's own chat identity is never counted among the people.
func TestTodo_AGENTUX_021(t *testing.T) {
	f := newAgentReachFixture(t, true)
	ctx, principal := agentp026Context(t, agentReachTenant, "user-a", trust.SubjectKindHuman)

	agents, err := f.catalog.List(ctx, principal)
	if err != nil || len(agents) != 1 || agents[0].Name != "People Coach" || agents[0].ID != f.persona.Profile.PersonaID || agents[0].Status != "Ready" {
		t.Fatalf("Agents page choices = %+v, %v; want the placed agent by name", agents, err)
	}
	for _, conversation := range []string{"general", "dm"} {
		got := agentReachMentions(t, f, ctx, "user-a", conversation)
		if len(got) != 1 || got[0].Kind != chat.AgentMention || got[0].ID != agentReachAgent || got[0].Display != "People Coach" || got[0].ConversationID != conversation || !got[0].Eligible {
			t.Fatalf("mention candidates in %s = %+v; want the placed agent", conversation, got)
		}
	}
	if got := agentReachMentions(t, f, ctx, "user-a", "other"); len(got) != 0 {
		t.Fatalf("the agent is offered in a conversation it is not placed in: %+v", got)
	}

	// The agent is a member of two conversations. It must not be projected as
	// a person in either, nor looked up in the human directory.
	audience, err := f.source.ListCurrentPersonaAudience(ctx, agentReachTenant, "user-a")
	if err != nil || len(audience) != 3 {
		t.Fatalf("audience = %+v, %v", audience, err)
	}
	for _, conversation := range audience {
		for _, member := range conversation.Members {
			if member.SubjectID == agentReachAgent {
				t.Fatalf("the agent is listed among the people of %s", conversation.ConversationID)
			}
		}
	}
	if slices.Contains(f.directory.calls, agentReachTenant+"/"+agentReachAgent) {
		t.Fatalf("the agent identity was looked up as a person: %v", f.directory.calls)
	}
}

// TestTodo_AGENTUX_021_Security asks as a member outside the agent's audience.
// The answer has the same shape as for a tenant where no agent is placed at
// all, and the store is never asked for the agent's profile.
func TestTodo_AGENTUX_021_Security(t *testing.T) {
	ctx, outsider := agentp026Context(t, agentReachTenant, "user-b", trust.SubjectKindHuman)
	type answer struct {
		agents   []productui.AgentSummary
		mentions map[string][]chat.ReferenceCandidate
		asked    [][]agentpersonastore.AvailableInstallation
	}
	ask := func(placed bool) answer {
		f := newAgentReachFixture(t, placed)
		agents, err := f.catalog.List(ctx, outsider)
		if err != nil {
			t.Fatalf("outsider catalog (placed=%t): %v", placed, err)
		}
		out := answer{agents: agents, mentions: map[string][]chat.ReferenceCandidate{}}
		for _, conversation := range []string{"general", "other"} {
			out.mentions[conversation] = agentReachMentions(t, f, ctx, "user-b", conversation)
		}
		out.asked = f.backend.asked
		return out
	}
	withAgent, withoutAgent := ask(true), ask(false)
	if withAgent.agents == nil || len(withAgent.agents) != 0 || len(withAgent.mentions["general"]) != 0 || len(withAgent.mentions["other"]) != 0 {
		t.Fatalf("a member outside the audience was offered an agent: %+v", withAgent)
	}
	left, _ := json.Marshal(struct {
		Agents   []productui.AgentSummary
		Mentions map[string][]chat.ReferenceCandidate
	}{withAgent.agents, withAgent.mentions})
	right, _ := json.Marshal(struct {
		Agents   []productui.AgentSummary
		Mentions map[string][]chat.ReferenceCandidate
	}{withoutAgent.agents, withoutAgent.mentions})
	if string(left) != string(right) {
		t.Fatalf("the answer differs when an agent the member cannot use exists:\n with: %s\nwithout: %s", left, right)
	}
	// The same store reads happen in both cases and none of them names the agent.
	if len(withAgent.asked) != len(withoutAgent.asked) {
		t.Fatalf("store reads differ: %d with the agent, %d without", len(withAgent.asked), len(withoutAgent.asked))
	}
	for _, allowed := range withAgent.asked {
		if len(allowed) != 0 {
			t.Fatalf("the store was asked for an agent on behalf of an outsider: %+v", allowed)
		}
	}

	// A member cannot borrow another member's identity to see their agents.
	f := newAgentReachFixture(t, true)
	_, insider := agentp026Context(t, agentReachTenant, "user-a", trust.SubjectKindHuman)
	if _, err := f.reader.ListAvailable(ctx, insider); !errors.Is(err, errAgentAvailablePersonas) {
		t.Fatalf("a request signed in as user-b listed user-a's agents: %v", err)
	}
	if _, err := f.mentions.ListPersonaReferenceCandidates(ctx, chat.Principal{TenantID: agentReachTenant, SubjectID: "user-a"}, agentReachTenant, "general", ""); !errors.Is(err, errPersonaReferenceInvalid) {
		t.Fatalf("a request signed in as user-b looked up mentions as user-a: %v", err)
	}
}

// TestTodo_AGENTUX_021_Integration reads a published, placed agent from the
// real persona store through the adapters the served cell composes: the
// installation reader, the available-persona backend, the chat identity
// directory and the exact-installation lookup. Chat membership and the human
// directory are the test's own, since they live in other databases.
func TestTodo_AGENTUX_021_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, db.SQL); err != nil {
		t.Fatal(err)
	}
	tenantUUID := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1)`, tenantUUID)
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	personas, err := agentpersonastore.New(conn, func(tenant values.TenantId) uuid.UUID {
		if tenant == agentReachTenant {
			return tenantUUID
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := personas.Scoped(agentReachTenant)
	if err != nil {
		t.Fatal(err)
	}

	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	persona := agentUserCatalogPersona(t, "people-coach", "People Coach", "agent.self_service", pin)
	personaID := persona.Profile.PersonaID
	profile, err := json.Marshal(persona.Profile)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := json.Marshal(agentpersonastore.ChannelPolicy{PlacementClass: "PRIVATE", MaxTier: "T0", AllowedDataClasses: []string{"WORKFORCE"}, AlwaysPrivate: true, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}})
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO persona_versions (tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at) VALUES ($1,$2,1,'agent-v1','people-coach','People Coach',$3::jsonb,$4,now())`, tenantUUID, personaID, string(profile), persona.Digest)
	db.Exec(t, `INSERT INTO persona_owners (tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES ($1,$2,'BUSINESS_OWNER','owner','admin',now())`, tenantUUID, personaID)
	db.Exec(t, `INSERT INTO persona_lifecycle_events (tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES ($1,'publish-v1',$2,1,'IN_REVIEW','PUBLISHED','reviewed','reviewer',now(),$3,'sha256:review','reviewer','sha256:run',$3,'sha256:suite')`, tenantUUID, personaID, persona.Digest)
	for _, room := range []string{"general", "dm"} {
		db.Exec(t, `INSERT INTO persona_installations (tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES ($1,$2,$3,1,$4,'PRIVATE','admin',$5::jsonb,'ACTIVE',1,1,now(),now())`, tenantUUID, "install-"+room, personaID, room, string(policy))
	}
	if err := scoped.RegisterPersonaChatIdentity(ctx, agentReachAgent, personaID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	directory := agentReachDirectory()
	source := &DatabasePersonaAudienceSource{Chat: agentReachChat(), Installations: personaInstallationStore{store: personas}, Directory: directory}
	discovery := &agentUserCatalogDiscovery{byPurpose: map[string][]agentskills.SkillRecord{
		personaChatReplyPurpose: {{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Read my profile"}, Digest: pin.Digest, Status: agentskills.StatusActive}},
	}}
	reader := &TenantAvailablePersonaReader{Backend: &AgentPersonaStoreBackend{Store: personas}, Audience: &CurrentPersonaAudience{Source: source}, Skills: discovery}
	identities, err := newProductionPersonaChatIdentityDirectory(personas)
	if err != nil {
		t.Fatal(err)
	}
	lookup, err := newProductionPersonaReferenceLookup(identities, personas)
	if err != nil {
		t.Fatal(err)
	}
	mentions, err := newPersonaChatReferenceSource(reader, identities, lookup, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &AgentUserCatalog{Personas: reader, Skills: discovery}

	insiderCtx, insider := agentp026Context(t, agentReachTenant, "user-a", trust.SubjectKindHuman)
	agents, err := catalog.List(insiderCtx, insider)
	if err != nil || len(agents) != 1 || agents[0].Name != "People Coach" {
		t.Fatalf("Agents page choices from the store = %+v, %v", agents, err)
	}
	insiderChat := chat.Principal{TenantID: agentReachTenant, SubjectID: "user-a"}
	for _, conversation := range []string{"general", "dm"} {
		got, err := mentions.ListPersonaReferenceCandidates(insiderCtx, insiderChat, agentReachTenant, conversation, "")
		if err != nil || len(got) != 1 || got[0].ID != agentReachAgent || got[0].Display != "People Coach" || got[0].ConversationID != conversation {
			t.Fatalf("mention candidates in %s from the store = %+v, %v", conversation, got, err)
		}
	}
	if got, err := mentions.ListPersonaReferenceCandidates(insiderCtx, insiderChat, agentReachTenant, "other", ""); err != nil || len(got) != 0 {
		t.Fatalf("mention candidates in a conversation without the agent = %+v, %v", got, err)
	}
	if slices.Contains(directory.calls, agentReachTenant+"/"+agentReachAgent) {
		t.Fatalf("the stored agent identity was looked up as a person: %v", directory.calls)
	}

	outsiderCtx, outsider := agentp026Context(t, agentReachTenant, "user-b", trust.SubjectKindHuman)
	if agents, err := catalog.List(outsiderCtx, outsider); err != nil || len(agents) != 0 {
		t.Fatalf("outsider Agents page choices = %+v, %v; want none", agents, err)
	}
	if got, err := mentions.ListPersonaReferenceCandidates(outsiderCtx, chat.Principal{TenantID: agentReachTenant, SubjectID: "user-b"}, agentReachTenant, "general", ""); err != nil || len(got) != 0 {
		t.Fatalf("outsider mention candidates = %+v, %v; want none", got, err)
	}

	// Removing the agent from #general takes it out of that conversation's
	// mention lookup at once and leaves the direct conversation as it was.
	db.Exec(t, `UPDATE persona_installations SET state='RETIRED',revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND installation_id='install-general'`, tenantUUID)
	if got, err := mentions.ListPersonaReferenceCandidates(insiderCtx, insiderChat, agentReachTenant, "general", ""); err != nil || len(got) != 0 {
		t.Fatalf("mention candidates after removal = %+v, %v; want none", got, err)
	}
	if got, err := mentions.ListPersonaReferenceCandidates(insiderCtx, insiderChat, agentReachTenant, "dm", ""); err != nil || len(got) != 1 {
		t.Fatalf("mention candidates in the direct conversation after the other removal = %+v, %v", got, err)
	}
}
