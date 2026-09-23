package chat

import (
	"context"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_CHAT_043_NativeMachineWatchRequiresOpaqueCursor(t *testing.T) {
	at := time.Now().UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "server", Subject: "agent", SubjectKind: trust.SubjectKindAgent, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "native-watch", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := trust.WithPrincipal(context.Background(), p)
	rec := &watchRecorder{transportChatFake: &transportChatFake{}}
	s := &server{deps: Dependencies{Service: rec}}
	_, _, err = s.watch(ctx, &chatv1.WatchConversationRequest{TenantId: "server", ConversationId: "c", AfterSequence: 7})
	owned, ok := envelope.As(err)
	if !ok || owned.Code() != envelope.CodeInvalidArgument || rec.got.ConversationID != "" {
		t.Fatalf("plain machine offset admitted: error=%v core=%+v", err, rec.got)
	}
	if _, _, err := s.watch(ctx, &chatv1.WatchConversationRequest{TenantId: "server", ConversationId: "c", ResumeCursor: "signed-cursor"}); err != nil {
		t.Fatalf("opaque machine cursor rejected: %v", err)
	}
	if rec.got.AfterSequence != 0 || rec.got.ResumeCursor != "signed-cursor" {
		t.Fatalf("machine core request=%+v", rec.got)
	}
}
