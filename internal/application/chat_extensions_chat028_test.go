package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
)

type chat028CommandAuthorizer struct {
	err                    error
	principal              chat.Principal
	conversation, app, cap string
}

func (a *chat028CommandAuthorizer) AuthorizeChatAppCommand(_ context.Context, p chat.Principal, conversation, app, capability string) error {
	a.principal, a.conversation, a.app, a.cap = p, conversation, app, capability
	return a.err
}

type chat028Callback struct{ calls int }

func (c *chat028Callback) Call(context.Context, chatapps.Installation, chatapps.Callback) (chatapps.CallbackResult, error) {
	c.calls++
	return chatapps.CallbackResult{Accepted: true}, nil
}

func TestTodo_CHAT_028_Integration(t *testing.T) {
	repo := chatapps.NewMemoryRepository()
	callback := &chat028Callback{}
	apps := &chatapps.Service{Repo: repo, Authority: ChatAppAuthority{Conversations: extensionConversations{}}, Callback: callback}
	install := chatapps.Installation{
		ID: "tenant:conv:payroll", Tenant: "tenant", Conversation: "conv", AppID: "payroll", Version: 1,
		Manifest:      chatapps.Manifest{AppID: "payroll", Version: 1, Scopes: []string{"chat:invoke", "hcm:write"}, Commands: []chatapps.Command{{Name: "change-pay", Scope: "hcm:write"}}},
		GrantedScopes: []string{"hcm:write"}, Status: chatapps.Active,
	}
	if err := repo.Put(context.Background(), install); err != nil {
		t.Fatal(err)
	}
	service := &ChatExtensions{Conversations: extensionConversations{}, Apps: apps}
	principal := chat.Principal{TenantID: "tenant", SubjectID: "member"}
	cb := chatapps.Callback{Command: "change-pay", IdempotencyKey: "cmd-1"}
	if _, err := service.Invoke(context.Background(), principal, "conv", install.ID, cb); !errors.Is(err, chat.ErrPermissionDenied) || callback.calls != 0 {
		t.Fatalf("installation grant alone invoked HCM command: err=%v callbacks=%d", err, callback.calls)
	}

	authorizer := &chat028CommandAuthorizer{}
	service.AppCommandAuthorization = authorizer
	if _, err := service.Invoke(context.Background(), principal, "conv", install.ID, cb); err != nil || callback.calls != 1 {
		t.Fatalf("authorized command err=%v callbacks=%d", err, callback.calls)
	}
	if authorizer.principal.TenantID != principal.TenantID || authorizer.principal.SubjectID != principal.SubjectID || authorizer.conversation != "conv" || authorizer.app != "payroll" || authorizer.cap != "hcm:write" {
		t.Fatalf("authorization did not bind current command: %+v", authorizer)
	}

	authorizer.err = chat.ErrPermissionDenied
	if _, err := service.Invoke(context.Background(), principal, "conv", install.ID, chatapps.Callback{Command: "change-pay", IdempotencyKey: "cmd-2"}); !errors.Is(err, chat.ErrPermissionDenied) || callback.calls != 1 {
		t.Fatalf("revoked invoker authority reached callback: err=%v callbacks=%d", err, callback.calls)
	}
	authorizer.err = nil
	apps.Callback = nil
	if _, err := service.Invoke(context.Background(), principal, "conv", install.ID, chatapps.Callback{Command: "change-pay", IdempotencyKey: "cmd-3"}); !errors.Is(err, chatapps.ErrDenied) || callback.calls != 1 {
		t.Fatalf("missing served callback changed behavior: err=%v callbacks=%d", err, callback.calls)
	}
}
