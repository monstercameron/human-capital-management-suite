package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// agentux037Policy is a conversation's current agent policy that a test
// changes between two reads, as an administrator would change it mid-run.
type agentux037Policy struct {
	authorityAudienceFake
	current chatstore.PersonaChannelPolicy
	err     error
	reads   int
}

func (p *agentux037Policy) CapturePersonaChannelPolicy(_ context.Context, tenant, conversation, _ string) (chatstore.PersonaChannelPolicySnapshot, error) {
	p.reads++
	if p.err != nil {
		return chatstore.PersonaChannelPolicySnapshot{}, p.err
	}
	return chatstore.PersonaChannelPolicySnapshot{TenantID: tenant, ConversationID: conversation, PolicyRevision: int64(p.reads), Policy: p.current}, nil
}

type agentux037Scopes struct {
	policy, installation agentpersonastore.ChannelPolicy
}

func (r *agentux037Scopes) ResolvePersonaScopes(_ context.Context, _ values.TenantId, _ agentpersona.PersonaProfile, installation agentpersonastore.ActiveInstallation, policy agentpersonastore.ChannelPolicy) (agentinvoke.SkillScopes, agentinvoke.SkillScopes, agentinvoke.SkillScopes, error) {
	r.policy, r.installation = policy, installation.ChannelPolicy
	scopes := agentinvoke.SkillScopes{"read": {"worker"}}
	return scopes, scopes.Clone(), scopes.Clone(), nil
}

// agentux037Accepted is the copy an installation took when the agent was
// added: every field at its widest, so each can be narrowed on its own.
func agentux037Accepted() agentpersonastore.ChannelPolicy {
	return agentpersonastore.ChannelPolicy{
		PlacementClass: "ANY_INTERNAL", MaxTier: "T2", AllowedDataClasses: []string{"PUBLIC", "INTERNAL", "POLICY_DOCUMENT"},
		AlwaysPrivate: false, ConversationSearchAllowed: true,
		AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationOneToOne, agentpersonastore.ConversationPrivate},
		AllowExternalMembers:  true, AllowCrossCompanyMembers: true,
	}
}

func agentux037Chat(policy agentpersonastore.ChannelPolicy) chatstore.PersonaChannelPolicy {
	classes := make([]string, 0, len(policy.AllowedChannelClasses))
	for _, class := range policy.AllowedChannelClasses {
		classes = append(classes, string(class))
	}
	return chatstore.PersonaChannelPolicy{
		PlacementClass: policy.PlacementClass, MaxTier: policy.MaxTier, AllowedDataClasses: append([]string(nil), policy.AllowedDataClasses...),
		AlwaysPrivate: policy.AlwaysPrivate, ConversationSearchAllowed: policy.ConversationSearchAllowed, AllowedChannelClasses: classes,
		AllowExternalMembers: policy.AllowExternalMembers, AllowCrossCompanyMembers: policy.AllowCrossCompanyMembers,
	}
}

// agentux037Fields narrows one policy field at a time and says how to tell
// that the narrowing took effect.
var agentux037Fields = []struct {
	name     string
	narrow   func(*agentpersonastore.ChannelPolicy)
	narrowed func(agentpersonastore.ChannelPolicy) bool
}{
	{"highest tier", func(p *agentpersonastore.ChannelPolicy) { p.MaxTier = "T0" }, func(p agentpersonastore.ChannelPolicy) bool { return p.MaxTier == "T0" }},
	{"data classes", func(p *agentpersonastore.ChannelPolicy) { p.AllowedDataClasses = []string{"PUBLIC"} }, func(p agentpersonastore.ChannelPolicy) bool {
		return len(p.AllowedDataClasses) == 1 && p.AllowedDataClasses[0] == "PUBLIC"
	}},
	{"always private", func(p *agentpersonastore.ChannelPolicy) { p.AlwaysPrivate = true }, func(p agentpersonastore.ChannelPolicy) bool { return p.AlwaysPrivate }},
	{"document search", func(p *agentpersonastore.ChannelPolicy) { p.ConversationSearchAllowed = false }, func(p agentpersonastore.ChannelPolicy) bool { return !p.ConversationSearchAllowed }},
	{"conversation kinds", func(p *agentpersonastore.ChannelPolicy) {
		p.AllowedChannelClasses = []agentpersonastore.ConversationClass{agentpersonastore.ConversationOneToOne}
	}, func(p agentpersonastore.ChannelPolicy) bool {
		return len(p.AllowedChannelClasses) == 1 && p.AllowedChannelClasses[0] == agentpersonastore.ConversationOneToOne
	}},
	{"external members", func(p *agentpersonastore.ChannelPolicy) { p.AllowExternalMembers = false }, func(p agentpersonastore.ChannelPolicy) bool { return !p.AllowExternalMembers }},
	{"cross-company members", func(p *agentpersonastore.ChannelPolicy) { p.AllowCrossCompanyMembers = false }, func(p agentpersonastore.ChannelPolicy) bool { return !p.AllowCrossCompanyMembers }},
	{"placement", func(p *agentpersonastore.ChannelPolicy) { p.PlacementClass = "ONE_TO_ONE_DM" }, func(p agentpersonastore.ChannelPolicy) bool { return p.PlacementClass == "ONE_TO_ONE_DM" }},
}

// TestTodo_AGENTUX_037 covers the rule itself: what a run reads is the
// accepted copy narrowed by the conversation's current policy, field by field,
// and never widened by it. A current policy that cannot be read, or that
// answers for another conversation, is refused, not ignored.
func TestTodo_AGENTUX_037(t *testing.T) {
	accepted := agentux037Accepted()
	for _, field := range agentux037Fields {
		t.Run(field.name, func(t *testing.T) {
			narrower := agentux037Accepted()
			field.narrow(&narrower)
			got := effectivePersonaChannelPolicy(accepted, narrower)
			if !field.narrowed(got) {
				t.Fatalf("narrowing %s in the conversation did not narrow the installation: %+v", field.name, got)
			}
			// Every other field is exactly the accepted copy.
			restored := got
			switch field.name {
			case "highest tier":
				restored.MaxTier = accepted.MaxTier
			case "data classes":
				restored.AllowedDataClasses = accepted.AllowedDataClasses
			case "always private":
				restored.AlwaysPrivate = accepted.AlwaysPrivate
			case "document search":
				restored.ConversationSearchAllowed = accepted.ConversationSearchAllowed
			case "conversation kinds":
				restored.AllowedChannelClasses = accepted.AllowedChannelClasses
			case "external members":
				restored.AllowExternalMembers = accepted.AllowExternalMembers
			case "cross-company members":
				restored.AllowCrossCompanyMembers = accepted.AllowCrossCompanyMembers
			case "placement":
				restored.PlacementClass = accepted.PlacementClass
			}
			if !agentPersonaPolicyEqual(restored, accepted) {
				t.Fatalf("narrowing %s changed another field: %+v", field.name, got)
			}
			// The reverse never widens: an installation accepted with the narrow
			// value keeps it when the conversation's policy is the wide one.
			if kept := effectivePersonaChannelPolicy(narrower, accepted); !field.narrowed(kept) {
				t.Fatalf("a wider conversation policy widened %s on the installation: %+v", field.name, kept)
			}
		})
	}

	ctx := context.Background()
	source := &agentux037Policy{current: agentux037Chat(accepted)}
	source.current.ConversationSearchAllowed = false
	got, supported, err := currentPersonaChannelPolicy(ctx, source, "tenant-a", "room-a", accepted)
	if err != nil || !supported || got.ConversationSearchAllowed {
		t.Fatalf("run-path read = %+v supported=%t err=%v", got, supported, err)
	}
	// No current-policy reader composed: the accepted copy is all there is, and
	// the caller is told so.
	if got, supported, err := currentPersonaChannelPolicy(ctx, nil, "tenant-a", "room-a", accepted); err != nil || supported || !agentPersonaPolicyEqual(got, accepted) {
		t.Fatalf("read without a current-policy reader = %+v supported=%t err=%v", got, supported, err)
	}
	// A policy that cannot be read is never replaced by the wider accepted copy.
	source.err = errors.New("chat store unavailable")
	if got, supported, err := currentPersonaChannelPolicy(ctx, source, "tenant-a", "room-a", accepted); err == nil || !supported || got.ConversationSearchAllowed || got.MaxTier != "" {
		t.Fatalf("unreadable current policy = %+v supported=%t err=%v; want a refusal carrying no policy", got, supported, err)
	}
	for name, snapshot := range map[string]chatstore.PersonaChannelPolicySnapshot{
		"another conversation": {TenantID: "tenant-a", ConversationID: "room-b", PolicyRevision: 3, Policy: agentux037Chat(accepted)},
		"another tenant":       {TenantID: "tenant-b", ConversationID: "room-a", PolicyRevision: 3, Policy: agentux037Chat(accepted)},
		"no revision":          {TenantID: "tenant-a", ConversationID: "room-a", Policy: agentux037Chat(accepted)},
	} {
		if _, _, err := currentPersonaChannelPolicy(ctx, agentux037Snapshot{snapshot}, "tenant-a", "room-a", accepted); err == nil {
			t.Errorf("a current policy answering for %s was accepted", name)
		}
	}
}

type agentux037Snapshot struct {
	snapshot chatstore.PersonaChannelPolicySnapshot
}

func (s agentux037Snapshot) CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error) {
	return s.snapshot, nil
}

// TestTodo_AGENTUX_037_Security narrows each policy field between two steps
// of one run and asserts the next authority resolution, document search and
// delivery obey the narrowed policy; a widening in between gives nothing back.
func TestTodo_AGENTUX_037_Security(t *testing.T) {
	version, _ := authorityProfile(t)
	accepted := agentux037Accepted()
	resolve := func(t *testing.T, audience *agentux037Policy) (agentpersonastore.ChannelPolicy, agentpersonastore.ChannelPolicy, error) {
		t.Helper()
		scopes := &agentux037Scopes{}
		source := &DatabasePersonaAuthoritySource{
			Installations: authorityInstallStoreFake{reader: authorityInstallReaderFake{version: version, current: agentpersonastore.ActiveInstallation{InstallationID: "install-a", PersonaID: "persona-a", PersonaVersion: 2, ConversationID: "room-a", ChannelPolicy: accepted}}},
			Audience:      audience, Scopes: scopes,
		}
		_, err := source.ResolvePersonaAuthority(authorityContext(t), agentinvoke.AdmissionRequest{TenantID: "tenant-a", ConversationID: "room-a", InvokerID: "user-a", PersonaID: "persona-a"}, TrustedInvoker{ID: "user-a", TenantID: "tenant-a"})
		return scopes.policy, scopes.installation, err
	}
	audience := func() *agentux037Policy {
		return &agentux037Policy{
			authorityAudienceFake: authorityAudienceFake{
				conversations: []PersonaAudienceConversation{{TenantID: "tenant-a", ConversationID: "room-a", Members: []PersonaAudienceMember{{SubjectID: "user-a", Roles: []string{"manager"}, Populations: []string{"staff"}, OrganizationScope: "org-a"}}}},
				installations: []PersonaAudienceInstallation{{Tuple: agentpersonastore.AvailableInstallation{PersonaID: "persona-a", PersonaVersion: 2, InstallationID: "install-a", ConversationID: "room-a"}, Active: true, CurrentVersion: true}},
			},
			current: agentux037Chat(accepted),
		}
	}
	for _, field := range agentux037Fields {
		t.Run("authority after narrowing "+field.name, func(t *testing.T) {
			policy := audience()
			before, _, err := resolve(t, policy)
			if err != nil || !agentPersonaPolicyEqual(before, accepted) {
				t.Fatalf("first step policy = %+v err=%v", before, err)
			}
			narrowed := agentux037Accepted()
			field.narrow(&narrowed)
			policy.current = agentux037Chat(narrowed)
			after, installation, err := resolve(t, policy)
			if err != nil || !field.narrowed(after) || !agentPersonaPolicyEqual(after, installation) {
				t.Fatalf("the step after narrowing %s ran with policy=%+v installation=%+v err=%v", field.name, after, installation, err)
			}
		})
	}
	t.Run("a widening between steps gives nothing back", func(t *testing.T) {
		policy := audience()
		wider := agentux037Accepted()
		wider.MaxTier, wider.AllowedDataClasses = "T4", append(wider.AllowedDataClasses, "COMPENSATION", "WORKFORCE")
		wider.AllowedChannelClasses = append(wider.AllowedChannelClasses, agentpersonastore.ConversationPublic)
		policy.current = agentux037Chat(wider)
		got, _, err := resolve(t, policy)
		if err != nil || !agentPersonaPolicyEqual(got, accepted) {
			t.Fatalf("a wider conversation policy changed the installation's ceiling: %+v err=%v", got, err)
		}
	})
	t.Run("an unreadable current policy stops the next step", func(t *testing.T) {
		policy := audience()
		policy.err = errors.New("chat store unavailable")
		if _, _, err := resolve(t, policy); !errors.Is(err, errPersonaAuthoritySourceUnavailable) {
			t.Fatalf("authority resolved without the current policy: %v", err)
		}
	})
	t.Run("the next delivery is private once the conversation is", func(t *testing.T) {
		policy := audience()
		_, installation, err := resolve(t, policy)
		if err != nil {
			t.Fatal(err)
		}
		active := agentpersonastore.ActiveInstallation{InstallationID: "install-a", PersonaID: "persona-a", PersonaVersion: 2, ConversationID: "room-a", ChannelPolicy: installation}
		identity := agentsecurity.FinalOutputIdentity{TenantID: version.TenantID.String(), PersonaID: version.PersonaID, PersonaVersion: "2", InstallationID: "install-a", ConversationID: "room-a"}
		if _, err := personaRuntimePublicProfile(version, active, identity); err != nil {
			t.Fatalf("a public answer was refused before the conversation was made private: %v", err)
		}
		policy.current.AlwaysPrivate = true
		if _, installation, err = resolve(t, policy); err != nil {
			t.Fatal(err)
		}
		active.ChannelPolicy = installation
		var private personaPrivateReasonError
		if _, err := personaRuntimePublicProfile(version, active, identity); !errors.As(err, &private) || private.reason != chat.PrivateReasonAgent {
			t.Fatalf("the delivery after the conversation was made private = %v, want a private answer", err)
		}
	})
	t.Run("the next document search is refused once search is turned off", func(t *testing.T) {
		ctx, tools, _, personas, _, _, record, run := runtimeToolFixture(t)
		ctx = WithPersonaBackgroundAdmission(ctx, record)
		identity, err := personaRunT0ToolInvocation(record, run)
		if err != nil {
			t.Fatal(err)
		}
		personas.reader.install.ChannelPolicy = accepted
		current := &agentux037Authority{AuthorityResolver: tools.cfg.Policy.authority, policy: agentux037Policy{current: agentux037Chat(accepted)}}
		tools.cfg.Policy.authority = current
		if scope, err := tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity); err != nil || scope.ScopeID != identity.ConversationID {
			t.Fatalf("search before the change: scope=%+v err=%v", scope, err)
		}
		current.policy.current.ConversationSearchAllowed = false
		if _, err := tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity); !errors.Is(err, errPersonaRuntimeTools) {
			t.Fatalf("the search after search was turned off was allowed: %v", err)
		}
		// Turning it back on restores it: the accepted copy still allows search.
		current.policy.current.ConversationSearchAllowed = true
		if _, err := tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity); err != nil {
			t.Fatalf("search after it was turned back on: %v", err)
		}
		current.policy.err = errors.New("chat store unavailable")
		if _, err := tools.cfg.Policy.ResolvePersonaDocumentSearchScope(ctx, identity); !errors.Is(err, errPersonaRuntimeTools) {
			t.Fatalf("search ran without the current policy: %v", err)
		}
	})
}

type agentux037Authority struct {
	agentinvoke.AuthorityResolver
	policy agentux037Policy
}

func (a *agentux037Authority) CapturePersonaChannelPolicy(ctx context.Context, tenant, conversation, post string) (chatstore.PersonaChannelPolicySnapshot, error) {
	return a.policy.CapturePersonaChannelPolicy(ctx, tenant, conversation, post)
}

type agentux037Authorizer struct{ refuse bool }

func (a agentux037Authorizer) AuthorizePersonaAdminCommand(context.Context, PersonaAdminCommandActor, PersonaAdminCommandAction, string) error {
	if a.refuse {
		return ErrPersonaAdminCommandUnavailable
	}
	return nil
}

// agentux037Placement stands for the placement authority: it refuses, or it
// admits the placement and supplies the conversation's class and the policy
// copy the installation takes, as the real one does from Chat.
type agentux037Placement struct {
	refuse bool
	class  agentpersonastore.ConversationClass
	policy agentpersonastore.ChannelPolicy
}

func (a agentux037Placement) AuthorizePersonaInstallation(_ context.Context, _ PersonaAdminCommandActor, installation agentpersonastore.PersonaInstallation) (agentpersonastore.PersonaInstallation, error) {
	if a.refuse {
		return agentpersonastore.PersonaInstallation{}, ErrPersonaAdminCommandUnavailable
	}
	installation.ConversationClass, installation.ChannelPolicy = a.class, a.policy
	return installation, nil
}

// TestTodo_AGENTUX_037_Integration uses the real persona and chat stores. The
// conversation's stored policy narrows a live installation at the next read;
// Remove retires exactly one installation and Add again replaces it with a
// fresh copy, through the administrator command executor.
func TestTodo_AGENTUX_037_Integration(t *testing.T) {
	ctx := context.Background()
	agents := pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, agents.SQL); err != nil {
		t.Fatal(err)
	}
	tenantUUID := uuid.New()
	agents.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1)`, tenantUUID)
	conn := agents.NewConn(t)
	if _, err := conn.Exec(ctx, "SET ROLE "+agentstore.AppRole); err != nil {
		t.Fatal(err)
	}
	personas, err := agentpersonastore.New(conn, func(tenant values.TenantId) uuid.UUID {
		if tenant == "tenant-a" {
			return tenantUUID
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := personas.Scoped("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	pin := agentskills.SkillPin{ID: "skill.people.read", Version: 2, Digest: "digest-v2"}
	persona := agentUserCatalogPersona(t, "policy-helper", "Policy Helper", "agent.self_service", pin)
	personaID := persona.Profile.PersonaID
	profile, err := json.Marshal(persona.Profile)
	if err != nil {
		t.Fatal(err)
	}
	accepted := agentux037Accepted()
	accepted.PlacementClass = "PRIVATE"
	accepted.AllowedChannelClasses = []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}
	stored, err := json.Marshal(accepted)
	if err != nil {
		t.Fatal(err)
	}
	agents.Exec(t, `INSERT INTO persona_versions (tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at) VALUES ($1,$2,1,'agent-v1','policy-helper','Policy Helper',$3::jsonb,$4,now())`, tenantUUID, personaID, string(profile), persona.Digest)
	agents.Exec(t, `INSERT INTO persona_owners (tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at) VALUES ($1,$2,'BUSINESS_OWNER','owner','admin',now())`, tenantUUID, personaID)
	agents.Exec(t, `INSERT INTO persona_lifecycle_events (tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest) VALUES ($1,'publish-v1',$2,1,'IN_REVIEW','PUBLISHED','reviewed','reviewer',now(),$3,'sha256:review','reviewer','sha256:run',$3,'sha256:suite')`, tenantUUID, personaID, persona.Digest)
	for _, room := range []string{"general", "benefits"} {
		agents.Exec(t, `INSERT INTO persona_installations (tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,revision,revocation_epoch,created_at,updated_at) VALUES ($1,$2,$3,1,$4,'PRIVATE','admin',$5::jsonb,'ACTIVE',1,1,now(),now())`, tenantUUID, "install-"+room, personaID, room, string(stored))
	}

	chats := pgtest.NewEmpty(t)
	applyPersonaChatMigrations(t, chats)
	chatStore, err := chatstore.New(ctx, chatstore.Config{DSN: personaChatSchemaDSN(t, chats.URL, chats.Schema)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chatStore.Close)
	if _, err := chatstore.NewAdapter(chatStore).CreateConversation(ctx, chat.Conversation{ID: "general", TenantID: "tenant-a", Kind: chat.PrivateChannel, Name: "general", OwnerID: "user-a"}, []chat.Membership{
		{ConversationID: "general", TenantID: "tenant-a", HomeTenantID: "tenant-a", SubjectID: "user-a", Role: chat.Manager, HistoryVisibility: chat.FullHistory},
	}, "agentux-037-room"); err != nil {
		t.Fatal(err)
	}

	// What a run reads: the stored installation narrowed by the stored policy.
	read := func(t *testing.T) agentpersonastore.ChannelPolicy {
		t.Helper()
		_, installation, err := scoped.ReadCurrentPersonaAuthority(ctx, "general", personaID)
		if err != nil {
			t.Fatal(err)
		}
		got, supported, err := currentPersonaChannelPolicy(ctx, chatStore, "tenant-a", "general", installation.ChannelPolicy)
		if err != nil || !supported {
			t.Fatalf("current policy read supported=%t err=%v", supported, err)
		}
		return got
	}
	narrow := agentux037Chat(accepted)
	narrow.ConversationSearchAllowed, narrow.AlwaysPrivate, narrow.MaxTier, narrow.AllowedDataClasses = false, true, "T0", []string{"PUBLIC"}
	revision, err := chatStore.PutPersonaChannelPolicy(ctx, "tenant-a", "general", 0, narrow)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t); got.ConversationSearchAllowed || !got.AlwaysPrivate || got.MaxTier != "T0" || len(got.AllowedDataClasses) != 1 {
		t.Fatalf("the read after the conversation was narrowed = %+v", got)
	}
	wide := agentux037Chat(accepted)
	wide.MaxTier, wide.AllowedDataClasses = "T3", append(wide.AllowedDataClasses, "COMPENSATION")
	if _, err := chatStore.PutPersonaChannelPolicy(ctx, "tenant-a", "general", revision, wide); err != nil {
		t.Fatal(err)
	}
	if got := read(t); !agentPersonaPolicyEqual(got, accepted) {
		t.Fatalf("the read after the conversation was widened = %+v, want the accepted copy %+v", got, accepted)
	}

	// Remove and add again, as the administrator does on Agent setup.
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	adminCtx, principal := personaAdminCommandContext(t)
	if principal.Tenant() != "tenant-a" {
		t.Fatalf("the command fixture signs in to %q; this test's stores hold tenant-a", principal.Tenant())
	}
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	nextID := 0
	executor := func(authorizer agentux037Authorizer, placement agentux037Placement) *PersonaAdminLifecycleExecutor {
		return NewPersonaAdminLifecycleExecutor(personaAdminLifecycleStoreAdapter{store: personas}, authorizer, nil, nil, nil, nil, placement, nil,
			func() time.Time { return now }, func() string { nextID++; return "install-new-" + string(rune('0'+nextID)) })
	}
	// A command names the agent and the conversation only; the class and the
	// policy copy come from the placement authority.
	placement := func(conversation string) PersonaAdminCommand {
		return PersonaAdminCommand{PersonaID: personaID, Installation: agentpersonastore.PersonaInstallation{PersonaID: personaID, ConversationID: conversation}}
	}
	admitted := func(policy agentpersonastore.ChannelPolicy) agentux037Placement {
		return agentux037Placement{class: agentpersonastore.ConversationPrivate, policy: policy}
	}
	state := func(t *testing.T, installation string) (string, int64, int64) {
		t.Helper()
		var state string
		var revision, epoch int64
		if err := agents.QueryRow(ctx, `SELECT state,revision,revocation_epoch FROM persona_installations WHERE tenant_id=$1 AND installation_id=$2`, tenantUUID, installation).Scan(&state, &revision, &epoch); err != nil {
			t.Fatal(err)
		}
		return state, revision, epoch
	}
	active := func(t *testing.T, conversation string) []string {
		t.Helper()
		var ids []string
		rows, err := agents.SQL.QueryContext(ctx, `SELECT installation_id FROM persona_installations WHERE tenant_id=$1 AND conversation_id=$2 AND state='ACTIVE' ORDER BY installation_id`, tenantUUID, conversation)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		return ids
	}

	remove := placement("general")
	remove.Action = PersonaAdminUninstall
	// Removal is authorized like adding: a refused administrator, or a refused
	// placement, changes nothing.
	if err := executor(agentux037Authorizer{refuse: true}, admitted(accepted)).ExecutePersonaAdminCommand(adminCtx, actor, remove); err == nil {
		t.Fatal("an administrator without the permission removed an agent")
	}
	if err := executor(agentux037Authorizer{}, agentux037Placement{refuse: true}).ExecutePersonaAdminCommand(adminCtx, actor, remove); err == nil {
		t.Fatal("a refused placement was removed")
	}
	if got, revision, epoch := state(t, "install-general"); got != "ACTIVE" || revision != 1 || epoch != 1 {
		t.Fatalf("a refused removal changed the installation: %s revision %d epoch %d", got, revision, epoch)
	}
	if err := executor(agentux037Authorizer{}, admitted(accepted)).ExecutePersonaAdminCommand(adminCtx, actor, remove); err != nil {
		t.Fatal(err)
	}
	if got, revision, epoch := state(t, "install-general"); got != "RETIRED" || revision != 2 || epoch != 2 {
		t.Fatalf("removed installation = %s revision %d epoch %d; want RETIRED, 2, 2", got, revision, epoch)
	}
	if got, revision, epoch := state(t, "install-benefits"); got != "ACTIVE" || revision != 1 || epoch != 1 {
		t.Fatalf("removing the agent from one conversation changed another: %s revision %d epoch %d", got, revision, epoch)
	}
	// Removing again is a no-op.
	if err := executor(agentux037Authorizer{}, admitted(accepted)).ExecutePersonaAdminCommand(adminCtx, actor, remove); err != nil {
		t.Fatal(err)
	}
	if got, revision, epoch := state(t, "install-general"); got != "RETIRED" || revision != 2 || epoch != 2 {
		t.Fatalf("a repeated removal changed the installation: %s revision %d epoch %d", got, revision, epoch)
	}
	if _, _, err := scoped.ReadCurrentPersonaAuthority(ctx, "general", personaID); err == nil {
		t.Fatal("a removed agent still has run authority in the conversation")
	}

	// Add again in benefits with a fresh, narrower copy: the old installation
	// is retired and the new one is the only active one, in one step.
	fresh := accepted
	fresh.ConversationSearchAllowed, fresh.MaxTier = false, "T0"
	again := placement("benefits")
	again.Action = PersonaAdminReinstall
	if err := executor(agentux037Authorizer{}, agentux037Placement{refuse: true}).ExecutePersonaAdminCommand(adminCtx, actor, again); err == nil {
		t.Fatal("a refused placement was replaced")
	}
	if err := executor(agentux037Authorizer{}, admitted(fresh)).ExecutePersonaAdminCommand(adminCtx, actor, again); err != nil {
		t.Fatal(err)
	}
	ids := active(t, "benefits")
	if len(ids) != 1 || ids[0] == "install-benefits" {
		t.Fatalf("active installations in benefits after adding again = %v, want one new installation", ids)
	}
	if got, _, epoch := state(t, "install-benefits"); got != "RETIRED" || epoch != 2 {
		t.Fatalf("the replaced installation = %s epoch %d; want RETIRED with a bumped epoch", got, epoch)
	}
	_, installation, err := scoped.ReadCurrentPersonaAuthority(ctx, "benefits", personaID)
	if err != nil || installation.InstallationID != ids[0] || installation.ChannelPolicy.ConversationSearchAllowed || installation.ChannelPolicy.MaxTier != "T0" {
		t.Fatalf("run authority after adding again = %+v err=%v; want the fresh copy", installation, err)
	}
	// The same request again changes nothing and adds no second installation.
	if err := executor(agentux037Authorizer{}, admitted(fresh)).ExecutePersonaAdminCommand(adminCtx, actor, again); err != nil {
		t.Fatal(err)
	}
	if replay := active(t, "benefits"); len(replay) != 1 || replay[0] != ids[0] {
		t.Fatalf("active installations after the same request again = %v, want %v", replay, ids)
	}
}
