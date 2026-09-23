package chatextensions

import (
	"context"
	"testing"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// grantCall is one recorded call into the grant and policy port.
type grantCall struct {
	method            string
	principal         chatcore.Principal
	host, consumer    string
	conversation      string
	grantID           string
	classification    string
	residency         string
	expectedRevision  uint64
	sawPrincipalPort  bool
	expiresAtUnixUTC  int64
	requireAllAsMode  chatpolicy.RoleMode
	requiredRoleCount int
}

// recordingGrantService records what the transport handed it and nothing else.
//
// The previous stub re-implemented the consent check itself, so the test it
// backed proved only that the stub worked: the transport could have forwarded
// anything at all and the assertion would still have passed. This one decides
// nothing, which is what makes the assertions below assertions about the
// transport.
type recordingGrantService struct {
	Service
	calls []grantCall
	// principalPort reports whether this stub implements
	// [PrincipalGrantService]; the false case is the legacy fallback.
	principalPort bool
}

func (r *recordingGrantService) last() grantCall {
	if len(r.calls) == 0 {
		return grantCall{}
	}
	return r.calls[len(r.calls)-1]
}

func (r *recordingGrantService) ProposeGrant(_ context.Context, host, consumer, conversation, classification, residency string, expires time.Time) (string, error) {
	r.calls = append(r.calls, grantCall{method: "ProposeGrant", host: host, consumer: consumer, conversation: conversation, classification: classification, residency: residency, expiresAtUnixUTC: expires.Unix()})
	return "grant", nil
}
func (r *recordingGrantService) AcceptGrant(_ context.Context, host, consumer, conversation, id string) error {
	r.calls = append(r.calls, grantCall{method: "AcceptGrant", host: host, consumer: consumer, conversation: conversation, grantID: id})
	return nil
}
func (r *recordingGrantService) RevokeGrant(_ context.Context, host, consumer, conversation, id string) error {
	r.calls = append(r.calls, grantCall{method: "RevokeGrant", host: host, consumer: consumer, conversation: conversation, grantID: id})
	return nil
}
func (r *recordingGrantService) SetChannelPolicy(_ context.Context, host, conversation string, roles, _, _, _ []string, mode chatpolicy.RoleMode, classification, residency string, expected uint64) error {
	r.calls = append(r.calls, grantCall{method: "SetChannelPolicy", host: host, conversation: conversation, classification: classification, residency: residency, expectedRevision: expected, requireAllAsMode: mode, requiredRoleCount: len(roles)})
	return nil
}

// principalGrantService is [recordingGrantService] plus the principal-carrying
// port, so the same assertions can be run against both compositions.
type principalGrantService struct{ *recordingGrantService }

func (p principalGrantService) ProposeGrantAs(_ context.Context, actor chatcore.Principal, host, consumer, conversation, classification, residency string, expires time.Time) (string, error) {
	p.recordingGrantService.calls = append(p.recordingGrantService.calls, grantCall{method: "ProposeGrant", principal: actor, host: host, consumer: consumer, conversation: conversation, classification: classification, residency: residency, expiresAtUnixUTC: expires.Unix(), sawPrincipalPort: true})
	return "grant", nil
}
func (p principalGrantService) AcceptGrantAs(_ context.Context, actor chatcore.Principal, host, consumer, conversation, id string) error {
	p.recordingGrantService.calls = append(p.recordingGrantService.calls, grantCall{method: "AcceptGrant", principal: actor, host: host, consumer: consumer, conversation: conversation, grantID: id, sawPrincipalPort: true})
	return nil
}
func (p principalGrantService) RevokeGrantAs(_ context.Context, actor chatcore.Principal, host, consumer, conversation, id string) error {
	p.recordingGrantService.calls = append(p.recordingGrantService.calls, grantCall{method: "RevokeGrant", principal: actor, host: host, consumer: consumer, conversation: conversation, grantID: id, sawPrincipalPort: true})
	return nil
}
func (p principalGrantService) SetChannelPolicyAs(_ context.Context, actor chatcore.Principal, host, conversation string, roles, _, _, _ []string, mode chatpolicy.RoleMode, classification, residency string, expected uint64) error {
	p.recordingGrantService.calls = append(p.recordingGrantService.calls, grantCall{method: "SetChannelPolicy", principal: actor, host: host, conversation: conversation, classification: classification, residency: residency, expectedRevision: expected, requireAllAsMode: mode, requiredRoleCount: len(roles), sawPrincipalPort: true})
	return nil
}

func refusedWith(t *testing.T, err error, want envelope.Code) {
	t.Helper()
	e, ok := envelope.As(err)
	if !ok {
		t.Fatalf("error is not owned: %v", err)
	}
	if e.Code() != want {
		t.Fatalf("code = %v, want %v", e.Code(), want)
	}
}

// TestTodo_CHAT_012_GrantRPCsBindTheAuthenticatedCompany is the transport-side
// assertion the old test could not make: the four grant and policy RPCs refuse
// a caller whose tenant is not the side of the grant the RPC acts for, and the
// values they forward are the ones the request named for the side that was
// allowed.
func TestTodo_CHAT_012_GrantRPCsBindTheAuthenticatedCompany(t *testing.T) {
	type call func(*server, context.Context) error
	propose := func(s *server, ctx context.Context) error {
		_, e := s.ProposeCompanyGrant(ctx, &chatv1.ProposeCompanyGrantRequest{HostTenantId: "host", ConsumerTenantId: "consumer", ConversationId: "conv", Classification: "confidential", Residency: "eu", ExpiresAtUnix: 1800000000})
		return e
	}
	accept := func(s *server, ctx context.Context) error {
		_, e := s.AcceptCompanyGrant(ctx, &chatv1.AcceptCompanyGrantRequest{HostTenantId: "host", ConsumerTenantId: "consumer", ConversationId: "conv", GrantId: "g1"})
		return e
	}
	revoke := func(s *server, ctx context.Context) error {
		_, e := s.RevokeCompanyGrant(ctx, &chatv1.RevokeCompanyGrantRequest{HostTenantId: "host", ConsumerTenantId: "consumer", ConversationId: "conv", GrantId: "g1"})
		return e
	}
	policy := func(s *server, ctx context.Context) error {
		_, e := s.SetChannelPolicy(ctx, &chatv1.SetChannelPolicyRequest{HostTenantId: "host", ConversationId: "conv", RequiredRoles: []string{"manager"}, RequireAllRoles: true, Classification: "confidential", Residency: "eu", ExpectedRevision: 4})
		return e
	}
	for _, tc := range []struct {
		name         string
		call         call
		allowed      []string
		refused      []string
		wantMethod   string
		wantHost     string
		wantConsumer string
	}{
		{"propose is the host's", propose, []string{"host"}, []string{"consumer", "bystander"}, "ProposeGrant", "host", "consumer"},
		{"consent is the consumer's", accept, []string{"consumer"}, []string{"host", "bystander"}, "AcceptGrant", "host", "consumer"},
		{"either side may revoke", revoke, []string{"host", "consumer"}, []string{"bystander"}, "RevokeGrant", "host", "consumer"},
		{"policy is the host's", policy, []string{"host"}, []string{"consumer", "bystander"}, "SetChannelPolicy", "host", ""},
	} {
		for _, tenant := range tc.refused {
			rec := &recordingGrantService{}
			s := &server{deps: Dependencies{Service: rec}}
			err := tc.call(s, grantContext(t, tenant))
			if err == nil {
				t.Fatalf("%s: %q was allowed", tc.name, tenant)
			}
			refusedWith(t, err, envelope.CodePermissionDenied)
			if len(rec.calls) != 0 {
				t.Fatalf("%s: %q reached the service: %+v", tc.name, tenant, rec.calls)
			}
		}
		for _, tenant := range tc.allowed {
			rec := &recordingGrantService{}
			s := &server{deps: Dependencies{Service: rec}}
			if err := tc.call(s, grantContext(t, tenant)); err != nil {
				t.Fatalf("%s: %q was refused: %v", tc.name, tenant, err)
			}
			got := rec.last()
			if got.method != tc.wantMethod || got.host != tc.wantHost || got.consumer != tc.wantConsumer || got.conversation != "conv" {
				t.Fatalf("%s: forwarded %+v", tc.name, got)
			}
			if got.sawPrincipalPort {
				t.Fatalf("%s: legacy service reached through the principal port", tc.name)
			}
		}
	}
}

// TestTodo_CHAT_012_GrantRPCsThreadThePrincipal proves the authenticated
// principal now reaches the port, for a service that offers the
// principal-carrying form.
func TestTodo_CHAT_012_GrantRPCsThreadThePrincipal(t *testing.T) {
	rec := &recordingGrantService{principalPort: true}
	s := &server{deps: Dependencies{Service: principalGrantService{recordingGrantService: rec}}}

	if _, err := s.ProposeCompanyGrant(grantContext(t, "host"), &chatv1.ProposeCompanyGrantRequest{HostTenantId: "host", ConsumerTenantId: "consumer", ConversationId: "conv", ExpiresAtUnix: 1800000000}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptCompanyGrant(grantContext(t, "consumer"), &chatv1.AcceptCompanyGrantRequest{HostTenantId: "host", ConsumerTenantId: "consumer", ConversationId: "conv", GrantId: "g1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeCompanyGrant(grantContext(t, "consumer"), &chatv1.RevokeCompanyGrantRequest{HostTenantId: "host", ConsumerTenantId: "consumer", ConversationId: "conv", GrantId: "g1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetChannelPolicy(grantContext(t, "host"), &chatv1.SetChannelPolicyRequest{HostTenantId: "host", ConversationId: "conv", RequiredRoles: []string{"manager"}, RequireAllRoles: true, ExpectedRevision: 4}); err != nil {
		t.Fatal(err)
	}
	if len(rec.calls) != 4 {
		t.Fatalf("calls = %d, want 4", len(rec.calls))
	}
	wantActor := map[string]string{"ProposeGrant": "host", "AcceptGrant": "consumer", "RevokeGrant": "consumer", "SetChannelPolicy": "host"}
	for _, c := range rec.calls {
		if !c.sawPrincipalPort {
			t.Fatalf("%s did not use the principal port", c.method)
		}
		if c.principal.SubjectID != "admin" || c.principal.TenantID != wantActor[c.method] {
			t.Fatalf("%s actor = %+v, want tenant %q subject admin", c.method, c.principal, wantActor[c.method])
		}
	}
	if last := rec.last(); last.requireAllAsMode != chatpolicy.RolesAll || last.requiredRoleCount != 1 || last.expectedRevision != 4 {
		t.Fatalf("policy forwarded %+v", last)
	}
}

// TestTodo_CHAT_012_GrantRPCsRefuseMissingTenants keeps the empty-string case
// from authorizing itself: a request naming no host matched no side, and an
// unauthenticated caller never reaches the tenant check at all.
func TestTodo_CHAT_012_GrantRPCsRefuseMissingTenants(t *testing.T) {
	rec := &recordingGrantService{}
	s := &server{deps: Dependencies{Service: rec}}
	if _, err := s.ProposeCompanyGrant(grantContext(t, "host"), &chatv1.ProposeCompanyGrantRequest{ConversationId: "conv"}); err == nil {
		t.Fatal("grant with no host accepted")
	} else {
		refusedWith(t, err, envelope.CodeInvalidArgument)
	}
	if _, err := s.AcceptCompanyGrant(grantContext(t, "consumer"), &chatv1.AcceptCompanyGrantRequest{HostTenantId: "host", ConversationId: "conv"}); err == nil {
		t.Fatal("consent with no consumer accepted")
	} else {
		refusedWith(t, err, envelope.CodeInvalidArgument)
	}
	if _, err := s.RevokeCompanyGrant(context.Background(), &chatv1.RevokeCompanyGrantRequest{HostTenantId: "host", ConsumerTenantId: "consumer"}); err == nil {
		t.Fatal("anonymous revoke accepted")
	} else {
		refusedWith(t, err, envelope.CodeUnauthenticated)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("service reached: %+v", rec.calls)
	}
}

// TestTodo_CHAT_024_CallerSuppliedRecipientIdentityRefused covers the request
// fields the handlers used to read past in silence. A caller that thinks it is
// reading somebody else's unread count was being served its own.
func TestTodo_CHAT_024_CallerSuppliedRecipientIdentityRefused(t *testing.T) {
	svc := &recipientService{}
	s := &server{deps: Dependencies{Service: svc}}
	ctx := grantContext(t, "home")
	for name, call := range map[string]func() error{
		"GetCounts.subject_id": func() error {
			_, e := s.GetCounts(ctx, &chatv1.GetCountsRequest{TenantId: "host", ConversationId: "conv", SubjectId: "somebody"})
			return e
		},
		"GetThreadFollow.subject_id": func() error {
			_, e := s.GetThreadFollow(ctx, &chatv1.GetThreadFollowRequest{TenantId: "host", ConversationId: "conv", RootPostId: "root", SubjectId: "somebody"})
			return e
		},
		"GetSidebar.tenant_id": func() error {
			_, e := s.GetSidebar(ctx, &chatv1.GetSidebarRequest{TenantId: "other"})
			return e
		},
		"GetSidebar.subject_id": func() error {
			_, e := s.GetSidebar(ctx, &chatv1.GetSidebarRequest{SubjectId: "somebody"})
			return e
		},
		"GetQuietHours.tenant_id": func() error {
			_, e := s.GetQuietHours(ctx, &chatv1.GetQuietHoursRequest{TenantId: "other"})
			return e
		},
		"GetQuietHours.subject_id": func() error {
			_, e := s.GetQuietHours(ctx, &chatv1.GetQuietHoursRequest{SubjectId: "somebody"})
			return e
		},
	} {
		err := call()
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		refusedWith(t, err, envelope.CodeInvalidArgument)
	}
	// The same reads without the forged fields still answer.
	if v, err := s.GetCounts(ctx, &chatv1.GetCountsRequest{TenantId: "host", ConversationId: "conv"}); err != nil || v.GetCounts().GetSubjectId() != "admin" {
		t.Fatalf("clean counts = %#v %v", v, err)
	}
	if _, err := s.GetSidebar(ctx, &chatv1.GetSidebarRequest{}); err != nil {
		t.Fatalf("clean sidebar: %v", err)
	}
	if _, err := s.GetQuietHours(ctx, &chatv1.GetQuietHoursRequest{}); err != nil {
		t.Fatalf("clean quiet hours: %v", err)
	}
}
