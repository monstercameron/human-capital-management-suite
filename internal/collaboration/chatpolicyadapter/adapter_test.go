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
	if _, err := a.Authorize(context.Background(), r); !errors.Is(err, chatpolicy.ErrNotAuthorized) {
		t.Fatalf("foreign tenant without grant = %v", err)
	}
	r.Grant = &Grant{ConversationID: "c1", HostTenant: "acme", ConsumerTenant: "vendor", Version: 1, Scope: "conversation", Proposed: true, AcceptedByHost: true, AcceptedByConsumer: true}
	if _, err := a.Authorize(context.Background(), r); err != nil {
		t.Fatalf("bilateral grant with matching home tenant denied: %v", err)
	}
}
