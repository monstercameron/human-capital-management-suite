package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func machineContext(t *testing.T, kind trust.SubjectKind, method string, at time.Time) context.Context {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "agent-a", SubjectKind: kind, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, _, admitted := transport.Admit(context.Background(), transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return p, nil })}, transport.AdmissionRequest{Metadata: transport.MapMetadata{"authorization": {"Bearer machine-token"}}, Method: method, Kind: transport.KindHTTPEdge})
	if admitted != nil {
		t.Fatal(admitted)
	}
	return ctx
}

func TestTodo_CHAT_043_Security_MachineInstallationAdmission(t *testing.T) {
	at := time.Now().UTC()
	repo := chatapps.NewMemoryRepository()
	install := chatapps.Installation{ID: "tenant-a:channel-a:agent-a", Tenant: "tenant-a", Conversation: "channel-a", AppID: "agent-a", Version: 1, Revision: 2, Status: chatapps.Active, CreatedAt: at.Add(-time.Hour), Manifest: chatapps.Manifest{AppID: "agent-a", Agent: &chatapps.AgentManifest{DisplayName: "Agent A"}}, GrantedScopes: []string{"chat.posts.write", "chat.posts.read"}}
	if err := repo.Put(context.Background(), install); err != nil {
		t.Fatal(err)
	}
	a := chatCurrentAuthority{apps: repo}
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "agent-a"}
	c := chat.Conversation{ID: "channel-a", TenantID: "tenant-a"}
	postCtx := machineContext(t, trust.SubjectKindAgent, "/hcmnext.chat.v1.ConversationService/SendPost", at)
	got, membership, err := a.machineAdmission(postCtx, p, c, chatpolicy.ActionPost, at)
	if err != nil || got.ID != "agent-a" || got.AuthorityRevision != 2 || len(got.Roles) != 0 || membership.Revision != 2 || !membership.CurrentAt("channel-a", "agent-a", "tenant-a", at) {
		t.Fatalf("admission principal=%+v membership=%+v err=%v", got, membership, err)
	}
	readCtx := machineContext(t, trust.SubjectKindAgent, "/hcmnext.chat.v1.ConversationService/ListPosts", at)
	if _, _, err := a.machineAdmission(readCtx, p, c, chatpolicy.ActionRead, at); err != nil {
		t.Fatalf("read denied: %v", err)
	}
	install.GrantedScopes = []string{"chat.posts.read"}
	if err := repo.Put(context.Background(), install); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.machineAdmission(postCtx, p, c, chatpolicy.ActionPost, at); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("write without scope: %v", err)
	}
	install.Status = chatapps.Revoked
	if err := repo.Put(context.Background(), install); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.machineAdmission(readCtx, p, c, chatpolicy.ActionRead, at); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("read after revoke: %v", err)
	}
}

func TestTodo_CHAT_043_Security_MachineCannotImpersonateOrManage(t *testing.T) {
	at := time.Now().UTC()
	repo := chatapps.NewMemoryRepository()
	install := chatapps.Installation{ID: "tenant-a:channel-a:agent-a", Tenant: "tenant-a", Conversation: "channel-a", AppID: "agent-a", Version: 1, Revision: 1, Status: chatapps.Active, CreatedAt: at.Add(-time.Hour), Manifest: chatapps.Manifest{AppID: "agent-a", Agent: &chatapps.AgentManifest{DisplayName: "Agent A"}}, GrantedScopes: []string{"chat.posts.write", "chat.posts.read"}}
	if err := repo.Put(context.Background(), install); err != nil {
		t.Fatal(err)
	}
	a := chatCurrentAuthority{apps: repo}
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "agent-a"}
	c := chat.Conversation{ID: "channel-a", TenantID: "tenant-a"}
	for name, tc := range map[string]struct {
		kind         trust.SubjectKind
		method       string
		action       chatpolicy.Action
		principal    chat.Principal
		conversation chat.Conversation
	}{
		"different subject": {trust.SubjectKindAgent, "/hcmnext.chat.v1.ConversationService/SendPost", chatpolicy.ActionPost, chat.Principal{TenantID: "tenant-a", SubjectID: "person"}, c},
		"different channel": {trust.SubjectKindAgent, "/hcmnext.chat.v1.ConversationService/SendPost", chatpolicy.ActionPost, p, chat.Conversation{ID: "channel-b", TenantID: "tenant-a"}},
		"agent kind on app": {trust.SubjectKindIntegration, "/hcmnext.chat.v1.ConversationService/SendPost", chatpolicy.ActionPost, p, c},
		"manage method":     {trust.SubjectKindAgent, "/hcmnext.chat.v1.ConversationService/UpdateConversation", chatpolicy.ActionRead, p, c},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := machineContext(t, tc.kind, tc.method, at)
			if _, _, err := a.machineAdmission(ctx, tc.principal, tc.conversation, tc.action, at); err == nil {
				t.Fatal("unauthorized machine admitted")
			}
		})
	}
}

type failingMachineInstallationLookup struct{ err error }

func (f failingMachineInstallationLookup) Get(context.Context, string) (chatapps.Installation, error) {
	return chatapps.Installation{}, f.err
}

func TestTodo_CHAT_043_Security_MachineInstallationLookupFailure(t *testing.T) {
	at := time.Now().UTC()
	ctx := machineContext(t, trust.SubjectKindAgent, "/hcmnext.chat.v1.ConversationService/GetConversation", at)
	p := chat.Principal{TenantID: "tenant-a", SubjectID: "agent-a"}
	c := chat.Conversation{ID: "channel-a", TenantID: "tenant-a"}
	for _, tc := range []struct {
		name string
		get  error
		want error
	}{
		{"missing installation", chatapps.ErrNotFound, chat.ErrPermissionDenied},
		{"denied installation", chatapps.ErrDenied, chat.ErrPermissionDenied},
		{"store unavailable", errors.New("database unavailable"), chat.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := chatCurrentAuthority{apps: failingMachineInstallationLookup{err: tc.get}}
			if _, _, err := a.machineAdmission(ctx, p, c, chatpolicy.ActionRead, at); !errors.Is(err, tc.want) {
				t.Fatalf("lookup error %v: got %v, want %v", tc.get, err, tc.want)
			}
		})
	}
}
