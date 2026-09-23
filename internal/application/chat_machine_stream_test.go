package application

import (
	"context"
	"errors"
	"testing"
	"time"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type machineEpochResolver struct {
	membershipResolverStub
	epoch uint64
}

func (r machineEpochResolver) MachineWatchEpoch(_ context.Context, tenant, conversation string, p chatcore.Principal) (uint64, error) {
	if tenant != "host" || conversation != "room" || p.TenantID != "host" || p.SubjectID != "agent" {
		return 0, chatcore.ErrPermissionDenied
	}
	return r.epoch, nil
}

func TestTodo_CHAT_043_MachineStreamEpochRequiresInstallationResolver(t *testing.T) {
	at := time.Now().UTC()
	identity, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "host", Subject: "agent", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "watch", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), identity)
	p := chatcore.Principal{TenantID: "host", SubjectID: "agent"}
	watch, _ := watchService(t)
	if _, _, err := watch.WatchConversationWithErrors(ctx, chatcore.WatchConversationRequest{Principal: p, TenantID: "host", ConversationID: "room", AfterSequence: 7}); !errors.Is(err, chatcore.ErrInvalidArgument) {
		t.Fatalf("machine plain after_sequence accepted: %v", err)
	}
	resolver := machineEpochResolver{membershipResolverStub: membershipResolverStub{member: chatcore.Membership{Revision: 999}}, epoch: 7}
	epoch, err := chatMembershipEpoch(ctx, nil, resolver, "host", "room", p)
	if err != nil || epoch != 7 {
		t.Fatalf("machine epoch=%d err=%v", epoch, err)
	}
	if _, err := chatMembershipEpoch(ctx, nil, resolver.membershipResolverStub, "host", "room", p); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("machine fell back to human membership: %v", err)
	}
	if _, err := chatMembershipEpoch(ctx, nil, resolver, "other", "room", p); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("machine crossed tenant: %v", err)
	}
	if _, err := chatMembershipEpoch(ctx, nil, resolver, "host", "room", chatcore.Principal{TenantID: "host", SubjectID: "other"}); !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("machine changed subject: %v", err)
	}
}

func TestTodo_CHAT_043_WatchCancellationClosesWithoutPermissionError(t *testing.T) {
	service, _ := watchService(t)
	ctx, cancel := context.WithCancel(context.Background())
	events, failures, err := service.WatchConversationWithErrors(ctx, watchRequest())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	select {
	case err, ok := <-failures:
		if ok {
			t.Fatalf("cancelled watch reported failure: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled watch failure channel stayed open")
	}
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("cancelled watch delivered event")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled watch event channel stayed open")
	}
}
