package chatextensions

import (
	"context"
	"reflect"
	"testing"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

// sidebarService answers a whole sidebar in one call and records the call.
type sidebarService struct {
	recipientService
	calls int
	asked []string
}

func (s *sidebarService) SidebarCounts(_ context.Context, p chatcore.Principal, host string, conversations []string) (map[string]chatrecipient.Counts, error) {
	s.calls++
	s.got, s.host = p, host
	s.asked = append([]string(nil), conversations...)
	return map[string]chatrecipient.Counts{"conv": {Unread: 3, Mentions: 1}, "quiet": {}}, nil
}

// TestTodo_CHATBUG_014_SidebarCounts: a request that names several
// conversations is one call on the service, answered in the order asked, for
// the authenticated principal and nobody else.
func TestTodo_CHATBUG_014_SidebarCounts(t *testing.T) {
	ctx := grantContext(t, "home")
	svc := &sidebarService{}
	s := &server{deps: Dependencies{Service: svc}}
	request := &chatv1.GetCountsRequest{TenantId: "host", ConversationIds: []string{"quiet", "refused", "conv", "quiet"}}
	if _, err := s.GetCounts(context.Background(), request); err == nil {
		t.Fatal("an anonymous sidebar read was accepted")
	}
	out, err := s.GetCounts(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if svc.calls != 1 || !reflect.DeepEqual(svc.asked, request.GetConversationIds()) || svc.host != "host" || svc.got.SubjectID != "admin" || svc.got.TenantID != "home" {
		t.Fatalf("service calls=%d asked=%v host=%q principal=%+v", svc.calls, svc.asked, svc.host, svc.got)
	}
	if out.GetCounts() != nil {
		t.Fatalf("a batch answered the single-conversation field: %+v", out.GetCounts())
	}
	var order []string
	for _, counts := range out.GetAllCounts() {
		order = append(order, counts.GetConversationId())
		if counts.GetSubjectId() != "admin" || counts.GetHomeTenantId() != "home" || counts.GetTenantId() != "host" {
			t.Fatalf("counts addressed to %+v", counts)
		}
	}
	if !reflect.DeepEqual(order, []string{"quiet", "conv"}) {
		t.Fatalf("answered %v, want the readable conversations once each in the order asked", order)
	}
	if got := out.GetAllCounts()[1]; got.GetUnreadCount() != 3 || got.GetMentionCount() != 1 {
		t.Fatalf("conv counts=%+v", got)
	}

	// Refused before the service is asked.
	for name, bad := range map[string]*chatv1.GetCountsRequest{
		"a caller-chosen subject":     {TenantId: "host", SubjectId: "someone", ConversationIds: []string{"conv"}},
		"one conversation and a list": {TenantId: "host", ConversationId: "conv", ConversationIds: []string{"quiet"}},
		"more than the limit":         {TenantId: "host", ConversationIds: make([]string, chatrecipient.SidebarCountsLimit+1)},
	} {
		if _, err := s.GetCounts(ctx, bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if svc.calls != 1 {
		t.Fatalf("a refused request reached the service: %d calls", svc.calls)
	}

	// A service without the batch read answers the same request, one
	// conversation at a time, leaving out what it refuses.
	single := &recipientService{}
	s = &server{deps: Dependencies{Service: single}}
	out, err = s.GetCounts(ctx, &chatv1.GetCountsRequest{TenantId: "host", ConversationIds: []string{"conv", "unknown"}})
	if err != nil || len(out.GetAllCounts()) != 1 || out.GetAllCounts()[0].GetConversationId() != "conv" || out.GetAllCounts()[0].GetUnreadCount() != 3 {
		t.Fatalf("fallback: %+v %v", out, err)
	}
}
