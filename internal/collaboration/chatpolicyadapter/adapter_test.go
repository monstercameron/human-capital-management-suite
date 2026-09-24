package chatpolicyadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

type authorityStub struct {
	current chatpolicy.Principal
	err     error
}

type hostPolicyStub struct {
	current                  chatpolicy.Channel
	err                      error
	gotHost, gotConversation string
}

func (s *hostPolicyStub) Policy(_ context.Context, host, conversation string) (chatpolicy.Channel, error) {
	s.gotHost, s.gotConversation = host, conversation
	return s.current, s.err
}

func (s authorityStub) Resolve(context.Context, string, string, time.Time) (chatpolicy.Principal, error) {
	return s.current, s.err
}

var adapterAt = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func adapterRequest() Request {
	return Request{Action: chatpolicy.ActionRead, Now: adapterAt, Principal: chat.Principal{TenantID: "acme", SubjectID: "alice", Roles: []string{"manager"}}, Conversation: chat.Conversation{ID: "c1", TenantID: "acme", Kind: chat.PrivateChannel, Revision: 7}, Membership: &chat.Membership{ConversationID: "c1", HomeTenantID: "acme", SubjectID: "alice", JoinedAt: &adapterAt, Revision: 3}}
}

func TestAdapterUsesCurrentTypedAuthority(t *testing.T) {
	r := adapterRequest()
	a, err := New(authorityStub{current: chatpolicy.Principal{ID: "alice", Tenant: "acme", Active: true, Roles: []string{"manager"}, AuthorityRevision: 9}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Authorize(context.Background(), r)
	if err != nil || !got.Allowed {
		t.Fatalf("Authorize = %#v, %v", got, err)
	}
	r.Principal.Roles = nil
	got, err = a.Authorize(context.Background(), r)
	if err != nil || !got.Allowed {
		t.Fatalf("request profile changed decision: %#v, %v", got, err)
	}
}

func TestAdapterRejectsStaleOrForgedAuthority(t *testing.T) {
	r := adapterRequest()
	a, _ := New(authorityStub{current: chatpolicy.Principal{ID: "mallory", Tenant: "acme", Active: true, AuthorityRevision: 1}})
	if _, err := a.Authorize(context.Background(), r); !errors.Is(err, ErrStaleAuthority) {
		t.Fatalf("subject mismatch = %v", err)
	}
	a, _ = New(authorityStub{current: chatpolicy.Principal{ID: "alice", Tenant: "acme", Active: true}})
	if _, err := a.Authorize(context.Background(), r); !errors.Is(err, ErrStaleAuthority) {
		t.Fatalf("missing authority revision = %v", err)
	}
}

func TestAdapterPreservesHomeAndHostTenantBoundary(t *testing.T) {
	r := adapterRequest()
	r.Principal.TenantID = "vendor"
	r.Membership.HomeTenantID = "vendor"
	a, _ := New(authorityStub{current: chatpolicy.Principal{ID: "alice", Tenant: "vendor", Active: true, AuthorityRevision: 1}})
	r.Grant = adapterGrant()
	if _, err := a.Authorize(context.Background(), r); !errors.Is(err, ErrStaleHostFacts) {
		t.Fatalf("foreign tenant without trusted host facts = %v", err)
	}
	facts := &hostPolicyStub{current: adapterHostPolicy()}
	a, _ = New(authorityStub{current: chatpolicy.Principal{ID: "alice", Tenant: "vendor", Active: true, AuthorityRevision: 1}}, facts)
	if _, err := a.Authorize(context.Background(), r); err != nil {
		t.Fatalf("bilateral grant with matching home tenant denied: %v", err)
	}
	if facts.gotHost != "acme" || facts.gotConversation != "c1" {
		t.Fatalf("host facts requested for %q/%q", facts.gotHost, facts.gotConversation)
	}
}

func TestTodo_CHAT_012_AdapterFailsClosedWithoutExactHostTerms(t *testing.T) {
	r := adapterRequest()
	r.Principal.TenantID = "vendor"
	r.Membership.HomeTenantID = "vendor"
	r.Grant = adapterGrant()
	principal := authorityStub{current: chatpolicy.Principal{ID: "alice", Tenant: "vendor", Active: true, AuthorityRevision: 1}}

	for _, tc := range []struct {
		name  string
		facts chatpolicy.Channel
		err   error
	}{
		{name: "missing channel identity"},
		{name: "stale revision", facts: chatpolicy.Channel{ID: "c1", HostTenant: "acme", Revision: 0, Classification: "internal", Residency: "US"}},
		{name: "missing classification", facts: chatpolicy.Channel{ID: "c1", HostTenant: "acme", Revision: 4, Residency: "US"}},
		{name: "missing residency", facts: chatpolicy.Channel{ID: "c1", HostTenant: "acme", Revision: 4, Classification: "internal"}},
		{name: "wrong host", facts: chatpolicy.Channel{ID: "c1", HostTenant: "vendor", Revision: 4, Classification: "internal", Residency: "US"}},
		{name: "wrong conversation", facts: chatpolicy.Channel{ID: "other", HostTenant: "acme", Revision: 4, Classification: "internal", Residency: "US"}},
		{name: "unavailable facts", err: errors.New("policy store unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := &hostPolicyStub{current: tc.facts, err: tc.err}
			a, err := New(principal, facts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.Authorize(context.Background(), r); !errors.Is(err, ErrStaleHostFacts) {
				t.Fatalf("foreign grant without exact host facts = %v", err)
			}
		})
	}

	// A complete host snapshot is still not interchangeable with the terms the
	// consumer accepted: a changed classification or residency revokes access.
	for _, change := range []func(*Grant){
		func(g *Grant) { g.Classification = "restricted" },
		func(g *Grant) { g.Residency = "EU" },
	} {
		grant := adapterGrant()
		change(grant)
		r.Grant = grant
		a, _ := New(principal, &hostPolicyStub{current: adapterHostPolicy()})
		if _, err := a.Authorize(context.Background(), r); !errors.Is(err, chatpolicy.ErrNotAuthorized) {
			t.Fatalf("grant with changed host terms = %v", err)
		}
	}
}

func adapterGrant() *Grant {
	return &Grant{
		ConversationID: "c1", HostTenant: "acme", ConsumerTenant: "vendor", Version: 1,
		Scope: "conversation", Classification: "internal", Residency: "US",
		Proposed: true, AcceptedByHost: true, AcceptedByConsumer: true,
		ExpiresAt: adapterAt.Add(time.Hour),
	}
}

func adapterHostPolicy() chatpolicy.Channel {
	return chatpolicy.Channel{
		ID: "c1", HostTenant: "acme", Revision: 4,
		Classification: "internal", Residency: "US",
	}
}
