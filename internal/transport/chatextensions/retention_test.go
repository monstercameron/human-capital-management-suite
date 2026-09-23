package chatextensions

import (
	"context"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

type retentionService struct {
	fullService
	principal chat.Principal
	policy    chatrecords.RetentionPolicy
	expected  uint64
}

func (s *retentionService) RetentionPolicy(_ context.Context, p chat.Principal, kind string) (chatrecords.RetentionPolicy, bool, error) {
	s.principal = p
	if kind != s.policy.Kind {
		return chatrecords.RetentionPolicy{}, false, nil
	}
	return s.policy, true, nil
}
func (s *retentionService) PutRetentionPolicy(_ context.Context, p chat.Principal, policy chatrecords.RetentionPolicy, expected uint64) (chatrecords.RetentionPolicy, error) {
	s.principal, s.policy, s.expected = p, policy, expected
	policy.Revision = expected + 1
	return policy, nil
}

func TestTodo_CHAT_048_TransportRetention(t *testing.T) {
	svc := &retentionService{}
	s := &server{deps: Dependencies{Service: svc}}
	ctx := grantContext(t, "tenant-a")
	put, err := s.PutRetentionPolicy(ctx, &chatv1.PutRetentionPolicyRequest{Policy: &chatv1.ChatRetentionPolicy{Mode: "SIZE_BUDGET", BudgetBytes: 1024}, ExpectedRevision: 0})
	if err != nil || put.GetPolicy().GetRevision() != 1 || svc.principal.TenantID != "tenant-a" || svc.policy.TenantID != "tenant-a" || svc.expected != 0 {
		t.Fatalf("put=%+v principal=%+v policy=%+v err=%v", put, svc.principal, svc.policy, err)
	}
	get, err := s.GetRetentionPolicy(ctx, &chatv1.GetRetentionPolicyRequest{})
	if err != nil || !get.GetConfigured() || get.GetPolicy().GetBudgetBytes() != 1024 {
		t.Fatalf("get=%+v err=%v", get, err)
	}
}

func TestTodo_CHAT_048_Security_TransportRetention(t *testing.T) {
	s := &server{deps: Dependencies{Service: &retentionService{}}}
	_, err := s.PutRetentionPolicy(context.Background(), &chatv1.PutRetentionPolicyRequest{Policy: &chatv1.ChatRetentionPolicy{Mode: "AGE", AgeDays: 1}})
	if e, ok := envelope.As(err); !ok || e.Code() != envelope.CodeUnauthenticated {
		t.Fatalf("unauthenticated write err=%v", err)
	}
	_, err = s.PutRetentionPolicy(grantContext(t, "tenant-a"), &chatv1.PutRetentionPolicyRequest{})
	if e, ok := envelope.As(err); !ok || e.Code() != envelope.CodeInvalidArgument {
		t.Fatalf("empty policy err=%v", err)
	}
}
