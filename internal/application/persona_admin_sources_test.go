package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaCatalogChatFake struct {
	rooms        []chat.Conversation
	discoverable []chat.Conversation
	members      []chat.Membership
	failMembers  bool
}

func (f personaCatalogChatFake) ListConversations(_ context.Context, req chat.ListConversationsRequest) (chat.ListConversationsResponse, error) {
	rooms := append([]chat.Conversation(nil), f.rooms...)
	if req.IncludeDiscoverable {
		rooms = append(rooms, f.discoverable...)
	}
	return chat.ListConversationsResponse{Conversations: rooms}, nil
}
func (f personaCatalogChatFake) ListMemberships(_ context.Context, req chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	if f.failMembers {
		return chat.ListMembershipsResponse{}, chat.ErrPermissionDenied
	}
	for _, room := range f.discoverable {
		if room.ID == req.ConversationID {
			return chat.ListMembershipsResponse{}, chat.ErrPermissionDenied
		}
	}
	var out []chat.Membership
	for _, member := range f.members {
		if member.ConversationID == req.ConversationID {
			out = append(out, member)
		}
	}
	return chat.ListMembershipsResponse{Memberships: out}, nil
}

func TestTodo_AGENTP_018_SourcesClassifiesMembershipFailure(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	source := ChatDirectoryPersonaCatalogTargets{
		Chat:      personaCatalogChatFake{rooms: []chat.Conversation{{ID: "room-1", TenantID: "tenant-a", Name: "Room"}}, failMembers: true},
		Directory: personaCatalogDirectoryFake{},
	}
	users, rooms, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-a")
	if err != nil || len(users) != 0 || len(rooms) != 0 {
		t.Fatalf("targets=%v rooms=%v err=%v; want the unreadable room omitted", users, rooms, err)
	}
}

func TestTodo_AGENTP_018_SourcesPreservesBoundedDirectoryFailureStage(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	room := chat.Conversation{ID: "room-1", TenantID: "tenant-a", Name: "Room"}
	member := chat.Membership{ConversationID: room.ID, TenantID: room.TenantID, HomeTenantID: room.TenantID, SubjectID: "opaque-subject"}
	directory := personaCatalogDirectoryFailure{stage: "member_facts_absent"}
	source := ChatDirectoryPersonaCatalogTargets{Chat: personaCatalogChatFake{rooms: []chat.Conversation{room}, members: []chat.Membership{member}}, Directory: directory}
	users, rooms, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-a")
	if err != nil || len(users) != 0 || len(rooms) != 1 {
		t.Fatalf("targets=%v rooms=%v err=%v; want only the unreadable member omitted", users, rooms, err)
	}
}

func TestAgentUXSetup3_F1DirectPlacementKeepsHumanName(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	public := chat.Conversation{ID: "general", TenantID: "tenant-a", Name: "General", Kind: chat.PublicChannel}
	direct := chat.Conversation{ID: "policy-dm", TenantID: "tenant-a", Name: "Policy Helper", Kind: chat.Direct}
	members := []chat.Membership{
		{ConversationID: public.ID, TenantID: public.TenantID, HomeTenantID: public.TenantID, SubjectID: "admin"},
		{ConversationID: direct.ID, TenantID: direct.TenantID, HomeTenantID: direct.TenantID, SubjectID: "admin"},
		{ConversationID: direct.ID, TenantID: direct.TenantID, HomeTenantID: direct.TenantID, SubjectID: "policy-helper"},
	}
	source := ChatDirectoryPersonaCatalogTargets{Chat: personaCatalogChatFake{rooms: []chat.Conversation{direct, public}, members: members}, Directory: agentUXSetup3Directory{}, Roles: personaCatalogRoleDirectoryFake{roles: []string{"hcm_admin"}}}
	users, rooms, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-a")
	if err != nil || len(rooms) != 2 || rooms[0].ID != direct.ID || rooms[0].Label != "Policy Helper" || rooms[0].PlacementLabel != "Walt Brennan" || !rooms[0].ViewerDirect || rooms[1].ID != public.ID || len(users) != 1 || users[0].ID != "admin" || users[0].Label != "Walt Brennan" {
		t.Fatalf("users=%#v rooms=%#v err=%v", users, rooms, err)
	}
}

type agentUXSetup3Directory struct{}

func (agentUXSetup3Directory) ResolvePersonaCatalogTarget(_ context.Context, _ values.TenantId, id string) (productui.PersonaAdminTarget, error) {
	if id == "policy-helper" {
		return productui.PersonaAdminTarget{}, errors.New("agent identity is not a human directory entry")
	}
	return productui.PersonaAdminTarget{ID: id, Label: "Walt Brennan"}, nil
}

type personaCatalogDirectoryFake struct{ fail bool }

func (f personaCatalogDirectoryFake) ResolvePersonaCatalogTarget(_ context.Context, _ values.TenantId, id string) (productui.PersonaAdminTarget, error) {
	if f.fail {
		return productui.PersonaAdminTarget{}, errors.New("directory unavailable")
	}
	return productui.PersonaAdminTarget{ID: id, Label: "Member " + id}, nil
}

type personaCatalogRoleDirectoryFake struct{ roles []string }

func (f personaCatalogRoleDirectoryFake) CurrentRoles(context.Context, values.TenantId, string) ([]string, error) {
	return append([]string(nil), f.roles...), nil
}

type personaCatalogRoleStoreFake struct {
	roleaccess.Store
	snapshot roleaccess.Snapshot
}

func (f personaCatalogRoleStoreFake) Load(context.Context, values.TenantId, string) (roleaccess.Snapshot, error) {
	return f.snapshot, nil
}

type personaCatalogDirectoryFailureStage string

func (e personaCatalogDirectoryFailureStage) Error() string                      { return "directory failure" }
func (e personaCatalogDirectoryFailureStage) PersonaCatalogFailureStage() string { return string(e) }

type personaCatalogDirectoryFailure struct{ stage string }

func (f personaCatalogDirectoryFailure) ResolvePersonaCatalogTarget(context.Context, values.TenantId, string) (productui.PersonaAdminTarget, error) {
	return productui.PersonaAdminTarget{}, personaCatalogDirectoryFailureStage(f.stage)
}

func TestTodo_AGENTP_018_Sources_TargetsUseAuthorizedChatMemberships(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	room := chat.Conversation{ID: "room-1", TenantID: "tenant-a", Name: "Payroll", Kind: chat.PrivateChannel}
	member := chat.Membership{ConversationID: room.ID, TenantID: room.TenantID, HomeTenantID: room.TenantID, SubjectID: "user-a"}
	source := ChatDirectoryPersonaCatalogTargets{Chat: personaCatalogChatFake{rooms: []chat.Conversation{room}, members: []chat.Membership{member}}, Directory: personaCatalogDirectoryFake{}, Roles: personaCatalogRoleDirectoryFake{roles: []string{"worker_self", "hcm_admin"}}}
	users, rooms, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != "user-a" || users[0].Role != "hcm_admin" || len(rooms) != 1 || rooms[0].ID != room.ID || rooms[0].Kind != string(chat.PrivateChannel) {
		t.Fatalf("targets = %#v, rooms = %#v", users, rooms)
	}
	if _, _, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-b"); !errors.Is(err, errPersonaCatalogSource) {
		t.Fatalf("cross-tenant error = %v", err)
	}
}

func TestTodo_AGENTUX_003_SourcesProjectReadableRoleNames(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	source := ChatDirectoryPersonaCatalogTargets{RoleNames: personaCatalogRoleStoreFake{snapshot: roleaccess.Snapshot{Roles: []roleaccess.Role{{ID: "custom_people_partner", Name: "Custom people partner", Active: true}}}}}
	targets := source.ResolvePersonaCatalogRoleTargets(context.Background(), *principal, "tenant-a", []string{"worker_self", "custom_people_partner"}, []string{"org-a"})
	if len(targets) != 2 || targets[0].ID != "custom_people_partner" || targets[0].Label != "Custom people partner" || targets[1].ID != "worker_self" || targets[1].Label != "Employee self-service" {
		t.Fatalf("role targets = %#v", targets)
	}
}

func TestTodo_AGENTP_018_Sources_TargetsSkipUnjoinedDiscoverableRooms(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	joined := chat.Conversation{ID: "joined-room", TenantID: "tenant-a", Name: "My team", Kind: chat.PrivateChannel, Joined: true}
	discoverable := chat.Conversation{ID: "discoverable-room", TenantID: "tenant-a", Name: "Open channel", Kind: chat.PublicChannel, Joined: false}
	chatSource := personaCatalogChatFake{rooms: []chat.Conversation{joined}, discoverable: []chat.Conversation{discoverable}, members: []chat.Membership{{ConversationID: joined.ID, TenantID: joined.TenantID, HomeTenantID: joined.TenantID, SubjectID: "user-a"}}}
	source := ChatDirectoryPersonaCatalogTargets{Chat: chatSource, Directory: personaCatalogDirectoryFake{}}
	users, rooms, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-a")
	if err != nil {
		t.Fatalf("snapshot targets: %v", err)
	}
	if len(users) != 1 || users[0].ID != "user-a" || len(rooms) != 1 || rooms[0].ID != joined.ID {
		t.Fatalf("targets included rooms outside current membership: users=%#v rooms=%#v", users, rooms)
	}
}

func TestTodo_AGENTP_018_Sources_TargetsFailClosedWithoutDirectory(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	source := ChatDirectoryPersonaCatalogTargets{Chat: personaCatalogChatFake{rooms: []chat.Conversation{{ID: "room-1", TenantID: "tenant-a", Name: "Room"}}}}
	if _, _, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-a"); !errors.Is(err, errPersonaCatalogSource) {
		t.Fatalf("missing directory error = %v", err)
	}
}

func TestTodo_AGENTP_018_Sources_GrantAndAuthorizerFailClosedWithoutAuthority(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	grants := &CurrentPersonaCatalogGrants{}
	if _, err := grants.ResolvePersonaSkillGrant(context.Background(), *principal, "tenant-a", "subject", "room", agentskills.SkillPin{ID: "skill", Version: 1, Digest: "digest"}); !errors.Is(err, errPersonaCatalogSource) {
		t.Fatalf("missing grant authority error = %v", err)
	}
	if err := (TrustedPersonaCatalogAuthorizer{}).AuthorizePersonaCatalog(context.Background(), principal, "tenant-a"); !errors.Is(err, errPersonaCatalogSource) {
		t.Fatalf("missing page authority error = %v", err)
	}
}

func personaCatalogSourcePrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "admin", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", CredentialDigest: "digest", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
