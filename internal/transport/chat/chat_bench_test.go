package chat

import (
	"context"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// benchContext admits the same fixture principal the contract tests use,
// without the testing.T helpers benchmarks cannot call.
func benchContext() (context.Context, error) {
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("server"), Subject: "u", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest"})
	if err != nil {
		return nil, err
	}
	c, _, e := transport.Admit(context.Background(), transport.Config{Verifier: trust.VerifierFunc(func(context.Context, trust.Credential) (*trust.Principal, error) { return p, nil })}, transport.AdmissionRequest{Metadata: transport.MapMetadata{"authorization": {"Bearer [REDACTED]"}}, Method: "/hcmnext.chat.v1.ConversationService/GetConversation", Kind: transport.KindGRPC})
	if e != nil {
		return nil, e
	}
	return c, nil
}

// BenchmarkChatRPCs measures the in-process serving cost of the hot chat
// RPCs against the contract fake: the transport's own projection and bounds,
// with no store behind them.
func BenchmarkChatRPCs(b *testing.B) {
	ctx, err := benchContext()
	if err != nil {
		b.Fatal(err)
	}
	s := &server{deps: Dependencies{Service: transportChatFake{}}}
	b.Run("SendPost", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.SendPost(ctx, &chatv1.SendPostRequest{ConversationId: "c", Body: "hello", IdempotencyKey: "k"}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("ListPosts", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.ListPosts(ctx, &chatv1.ListPostsRequest{ConversationId: "c", PageSize: 50}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("EditPost", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.EditPost(ctx, &chatv1.EditPostRequest{Post: &chatv1.Post{Id: "p", ConversationId: "c"}, Body: "edited", ExpectedRevision: 1}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("ListConversations", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.ListConversations(ctx, &chatv1.ListConversationsRequest{PageSize: 50}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
