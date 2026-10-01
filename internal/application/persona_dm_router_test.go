package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type personaDMRouterResolver struct{ id string }

func (r personaDMRouterResolver) ResolvePersonaDM(context.Context, chatcore.Principal, string) (string, error) {
	return r.id, nil
}

func personaDMRouterInvocation() agentinvoke.RunRequest {
	return agentinvoke.RunRequest{
		InvocationID: "inv-1", TenantID: "tenant-a", ConversationID: "room-1", ThreadID: "thread-1", InvokingPostID: "post-1", InvokerID: "human-1", PersonaID: "persona-a", PersonaVersion: "v1", InstallationID: "install-a", Mode: agentinvoke.OnBehalfOf,
		Grant: agentinvoke.DelegationGrant{ID: "grant-1", UserID: "human-1", TenantID: "tenant-a"},
		Actor: agentinvoke.ActorChain{UserID: "human-1", PersonaID: "persona-a", PersonaVersion: "v1", InstallationID: "install-a", ConversationID: "room-1", InvokingPostID: "post-1", InvocationID: "inv-1"},
	}
}

func TestTodo_AGENTP_011_PersonaDMRouterSelectsByValidatedInvocation(t *testing.T) {
	router, err := NewPersonaDMRouter(map[string]chatcore.PersonaDMResolver{"persona-a": personaDMRouterResolver{id: "dm-a"}, "persona-b": personaDMRouterResolver{id: "dm-b"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithPersonaDMInvocation(context.Background(), personaDMRouterInvocation())
	got, err := router.ResolvePersonaDM(ctx, chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, "tenant-a")
	if err != nil || got != "dm-a" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestTodo_AGENTP_011_PersonaDMRouterRejectsForgedOrMissingInvocation(t *testing.T) {
	router, err := NewPersonaDMRouter(map[string]chatcore.PersonaDMResolver{"persona-a": personaDMRouterResolver{id: "dm-a"}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		ctx  context.Context
		p    chatcore.Principal
		want error
	}{
		{name: "missing context", ctx: context.Background(), p: chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, want: errPersonaDMInvocation},
		{name: "wrong principal", ctx: WithPersonaDMInvocation(context.Background(), personaDMRouterInvocation()), p: chatcore.Principal{TenantID: "tenant-a", SubjectID: "attacker"}, want: errPersonaDMInvocation},
		{name: "unknown persona", ctx: WithPersonaDMInvocation(context.Background(), func() agentinvoke.RunRequest {
			v := personaDMRouterInvocation()
			v.PersonaID = "persona-z"
			v.Actor.PersonaID = "persona-z"
			return v
		}()), p: chatcore.Principal{TenantID: "tenant-a", SubjectID: "human-1"}, want: chatcore.ErrPermissionDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, callErr := router.ResolvePersonaDM(tc.ctx, tc.p, "tenant-a")
			if got != "" || !errors.Is(callErr, tc.want) {
				t.Fatalf("got=%q err=%v", got, callErr)
			}
		})
	}
}
