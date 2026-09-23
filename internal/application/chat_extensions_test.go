package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatmedia"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type extensionConversations struct{ chat.ConversationService }
type extensionAuditedRepo struct {
	*chatapps.MemoryRepository
	writes int
	fail   error
}

func (r *extensionAuditedRepo) PutAudited(ctx context.Context, v chatapps.Installation, actor chatapps.Actor, home, action string, policy uint64) error {
	if actor.Tenant != v.Tenant || actor.Conversation != v.Conversation || actor.Principal != "owner" || home != "tenant" || (action != "chat.app.install" && action != "chat.app.status") || policy != 1 {
		return chatapps.ErrDenied
	}
	if r.fail != nil {
		return r.fail
	}
	r.writes++
	return r.MemoryRepository.Put(ctx, v)
}

func TestTodo_CHAT_047_Application_DurableAppSkipsPostCommitAudit(t *testing.T) {
	repo := &extensionAuditedRepo{MemoryRepository: chatapps.NewMemoryRepository()}
	records := chatrecords.NewMemoryRepository()
	s := &ChatExtensions{Conversations: extensionConversations{}, Apps: &chatapps.Service{Repo: repo, Authority: ChatAppAuthority{Conversations: extensionConversations{}}}, Records: &chatrecords.Service{Repo: records, Auth: ChatRecordAuthority{}}}
	p := chat.Principal{TenantID: "tenant", SubjectID: "owner"}
	installed, err := s.Install(context.Background(), p, "conv", chatapps.Manifest{AppID: "app", Version: 1}, nil)
	if err != nil || repo.writes != 1 {
		t.Fatalf("durable audited install err=%v writes=%d", err, repo.writes)
	}
	if events, err := records.Events(context.Background(), "tenant"); err != nil || len(events) != 0 {
		t.Fatalf("legacy postcommit audit ran: events=%+v err=%v", events, err)
	}
	fault := errors.New("audit projection fault")
	repo.fail = fault
	if _, err := s.ChangeStatus(context.Background(), p, "conv", installed.ID, chatapps.Suspended); !errors.Is(err, fault) {
		t.Fatalf("atomic write failure: %v", err)
	}
	got, err := repo.Get(context.Background(), installed.ID)
	if err != nil || got.Status != chatapps.Active || repo.writes != 1 {
		t.Fatalf("failed status committed: %+v err=%v writes=%d", got, err, repo.writes)
	}
	repo.fail = nil
	if _, err := s.ChangeStatus(context.Background(), p, "conv", installed.ID, chatapps.Suspended); err != nil || repo.writes != 2 {
		t.Fatalf("status retry: %v writes=%d", err, repo.writes)
	}
	if events, err := records.Events(context.Background(), "tenant"); err != nil || len(events) != 0 {
		t.Fatalf("legacy status audit ran: events=%+v err=%v", events, err)
	}
}

type captureConversation struct {
	chat.ConversationService
	got chat.GetConversationRequest
}

func (c *captureConversation) GetConversation(_ context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	c.got = r
	return chat.Conversation{ID: r.ConversationID, TenantID: r.TenantID, Revision: 1}, nil
}

func (extensionConversations) GetConversation(_ context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	if r.Principal.SubjectID == "outsider" {
		return chat.Conversation{}, chat.ErrPermissionDenied
	}
	return chat.Conversation{ID: r.ConversationID, TenantID: r.TenantID, OwnerID: "owner", Revision: 1}, nil
}

func (extensionConversations) ListMemberships(_ context.Context, r chat.ListMembershipsRequest) (chat.ListMembershipsResponse, error) {
	return chat.ListMembershipsResponse{Memberships: []chat.Membership{{TenantID: r.TenantID, ConversationID: r.ConversationID, HomeTenantID: r.TenantID, SubjectID: r.Principal.SubjectID, Role: chat.Member}}}, nil
}

func TestTodo_CHAT_040_RuntimeAuthorization(t *testing.T) {
	s := &ChatExtensions{Conversations: extensionConversations{}, Apps: &chatapps.Service{Repo: chatapps.NewMemoryRepository(), Authority: ChatAppAuthority{Conversations: extensionConversations{}}}, Records: &chatrecords.Service{Repo: chatrecords.NewMemoryRepository(), Auth: ChatRecordAuthority{}}}
	p := chat.Principal{TenantID: "tenant", SubjectID: "member"}
	m := chatapps.Manifest{AppID: "app", Version: 1}
	if _, err := s.Install(context.Background(), p, "conv", m, nil); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("member installed app: %v", err)
	}
	p.SubjectID = "owner"
	v, err := s.Install(context.Background(), p, "conv", m, nil)
	if err != nil || v.Approver != "owner" {
		t.Fatalf("owner install: %#v %v", v, err)
	}
	p.SubjectID = "outsider"
	if _, err := s.ListInstallations(context.Background(), p, "conv"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("outsider read: %v", err)
	}
}

func TestTodo_CHAT_041_RuntimeCallbackUnavailable(t *testing.T) {
	s := &ChatExtensions{Conversations: extensionConversations{}, Apps: &chatapps.Service{Repo: chatapps.NewMemoryRepository(), Authority: ChatAppAuthority{Conversations: extensionConversations{}}}, Records: &chatrecords.Service{Repo: chatrecords.NewMemoryRepository(), Auth: ChatRecordAuthority{}}}
	p := chat.Principal{TenantID: "tenant", SubjectID: "owner"}
	m := chatapps.Manifest{AppID: "app", Version: 1, Scopes: []string{"invoke"}, Commands: []chatapps.Command{{Name: "run", Scope: "invoke"}}}
	v, err := s.Install(context.Background(), p, "conv", m, []string{"invoke"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Invoke(context.Background(), p, "conv", v.ID, chatapps.Callback{Command: "run", IdempotencyKey: "once"})
	if !errors.Is(err, chatapps.ErrDenied) {
		t.Fatalf("missing callback accepted: %v", err)
	}
}
func TestTodo_CHAT_036_MediaAuthorizationUsesTrustedHomeTenant(t *testing.T) {
	c := &captureConversation{}
	s := &ChatExtensions{Conversations: c}
	req := chatmedia.AccessRequest{TenantID: "host", ConversationID: "conv", PrincipalID: "member"}
	if e := s.AuthorizeMedia(context.Background(), req); !errors.Is(e, chat.ErrPermissionDenied) {
		t.Fatalf("untrusted media accepted: %v", e)
	}
	now := time.Now()
	p, e := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("home"), Subject: "member", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if e != nil {
		t.Fatal(e)
	}
	ctx := transport.WithInvocation(trust.WithPrincipal(context.Background(), p), &transport.Invocation{})
	req.PrincipalID = "forged"
	if e := s.AuthorizeMedia(ctx, req); !errors.Is(e, chat.ErrPermissionDenied) {
		t.Fatalf("forged principal accepted: %v", e)
	}
	req.PrincipalID = "member"
	if e := s.AuthorizeMedia(ctx, req); e != nil {
		t.Fatal(e)
	}
	if c.got.Principal.TenantID != "home" || c.got.TenantID != "host" {
		t.Fatalf("home/host collapsed: %#v", c.got)
	}
}
