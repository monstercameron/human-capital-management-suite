package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
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
	_, _, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-a")
	var staged interface{ PersonaCatalogFailureStage() string }
	if !errors.As(err, &staged) || staged.PersonaCatalogFailureStage() != "target_memberships" {
		t.Fatalf("failure = %v, stage = %v; want target_memberships", err, staged)
	}
}

func TestTodo_AGENTP_018_SourcesPreservesBoundedDirectoryFailureStage(t *testing.T) {
	principal := personaCatalogSourcePrincipal(t)
	room := chat.Conversation{ID: "room-1", TenantID: "tenant-a", Name: "Room"}
	member := chat.Membership{ConversationID: room.ID, TenantID: room.TenantID, HomeTenantID: room.TenantID, SubjectID: "opaque-subject"}
	directory := personaCatalogDirectoryFailure{stage: "member_facts_absent"}
	source := ChatDirectoryPersonaCatalogTargets{Chat: personaCatalogChatFake{rooms: []chat.Conversation{room}, members: []chat.Membership{member}}, Directory: directory}
	_, _, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-a")
	var staged interface{ PersonaCatalogFailureStage() string }
	if !errors.As(err, &staged) || staged.PersonaCatalogFailureStage() != "member_facts_absent" {
		t.Fatalf("failure = %v, stage = %v; want bounded member_facts_absent", err, staged)
	}
	if strings.Contains(err.Error(), "opaque-subject") {
		t.Fatalf("failure exposed member identity: %v", err)
	}
}

type personaCatalogDirectoryFake struct{ fail bool }

func (f personaCatalogDirectoryFake) ResolvePersonaCatalogTarget(_ context.Context, _ values.TenantId, id string) (productui.PersonaAdminTarget, error) {
	if f.fail {
		return productui.PersonaAdminTarget{}, errors.New("directory unavailable")
	}
	return productui.PersonaAdminTarget{ID: id, Label: "Member " + id}, nil
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
	source := ChatDirectoryPersonaCatalogTargets{Chat: personaCatalogChatFake{rooms: []chat.Conversation{room}, members: []chat.Membership{member}}, Directory: personaCatalogDirectoryFake{}}
	users, rooms, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != "user-a" || len(rooms) != 1 || rooms[0].ID != room.ID {
		t.Fatalf("targets = %#v, rooms = %#v", users, rooms)
	}
	if _, _, err := source.ListPersonaCatalogTargets(context.Background(), *principal, "tenant-b"); !errors.Is(err, errPersonaCatalogSource) {
		t.Fatalf("cross-tenant error = %v", err)
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
