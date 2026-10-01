package application

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type personaPrivateRouteResolver struct {
	conversation string
	seen         bool
	order        *[]string
	err          error
}

func (r *personaPrivateRouteResolver) ResolvePersonaDM(context.Context, chatcore.Principal, string) (string, error) {
	r.seen = true
	*r.order = append(*r.order, "resolve")
	return r.conversation, r.err
}

type personaPrivateRouteService struct {
	chatcore.ConversationService
	order   *[]string
	request chatcore.SendEphemeralPostRequest
}

func (s *personaPrivateRouteService) SendEphemeralPost(_ context.Context, request chatcore.SendEphemeralPostRequest) (chatcore.EphemeralPost, error) {
	*s.order = append(*s.order, "lease-and-send")
	s.request = request
	return chatcore.EphemeralPost{DurableCopyConversationID: request.DurableCopyConversationID}, nil
}

func TestTodo_AGENTP_011_PrivateRouteResolvesBeforeLeaseAndIgnoresHint(t *testing.T) {
	order := []string{}
	base := &personaPrivateRouteService{order: &order}
	resolver := &personaPrivateRouteResolver{conversation: "canonical-dm", order: &order}
	routed, err := newPersonaChatPrivateRoute(base, resolver)
	if err != nil {
		t.Fatalf("new private route: %v", err)
	}
	got, err := routed.SendEphemeralPost(context.Background(), chatcore.SendEphemeralPostRequest{
		Principal: chatcore.Principal{TenantID: "tenant-a", SubjectID: "invoker"},
		TenantID:  "tenant-a", DurableCopyConversationID: "attacker-controlled-dm",
	})
	if err != nil {
		t.Fatalf("send ephemeral: %v", err)
	}
	if !resolver.seen || got.DurableCopyConversationID != "canonical-dm" || base.request.DurableCopyConversationID != "canonical-dm" {
		t.Fatalf("canonical route was not enforced: got=%+v request=%+v resolver=%v", got, base.request, resolver.seen)
	}
	if len(order) != 2 || order[0] != "resolve" || order[1] != "lease-and-send" {
		t.Fatalf("route lease acquired before identity resolution: %v", order)
	}
}

func TestTodo_AGENTP_011_PrivateRoutePreservesTenantAuthorization(t *testing.T) {
	order := []string{}
	base := &personaPrivateRouteService{order: &order}
	resolver := &personaPrivateRouteResolver{conversation: "canonical-dm", order: &order}
	routed, err := newPersonaChatPrivateRoute(base, resolver)
	if err != nil {
		t.Fatalf("new private route: %v", err)
	}
	_, err = routed.SendEphemeralPost(context.Background(), chatcore.SendEphemeralPostRequest{
		Principal: chatcore.Principal{TenantID: "tenant-a", SubjectID: "invoker"},
		TenantID:  "tenant-b", DurableCopyConversationID: "canonical-dm",
	})
	if !errors.Is(err, chatcore.ErrPermissionDenied) {
		t.Fatalf("tenant mismatch error=%v, want permission denied", err)
	}
	if resolver.seen || len(order) != 0 {
		t.Fatalf("tenant mismatch reached resolver/route: seen=%v order=%v", resolver.seen, order)
	}
}

func TestTodo_AGENTP_011_PrivateRouteRefusesResolverFailureBeforeLease(t *testing.T) {
	order := []string{}
	base := &personaPrivateRouteService{order: &order}
	want := errors.New("current authorization changed")
	resolver := &personaPrivateRouteResolver{order: &order, err: want}
	routed, err := newPersonaChatPrivateRoute(base, resolver)
	if err != nil {
		t.Fatalf("new private route: %v", err)
	}
	_, err = routed.SendEphemeralPost(context.Background(), chatcore.SendEphemeralPostRequest{
		Principal: chatcore.Principal{TenantID: "tenant-a", SubjectID: "invoker"}, TenantID: "tenant-a",
	})
	if !errors.Is(err, want) {
		t.Fatalf("resolver error=%v, want %v", err, want)
	}
	if len(order) != 1 || order[0] != "resolve" {
		t.Fatalf("route was reached after resolver denial: %v", order)
	}
}
