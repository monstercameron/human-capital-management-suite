// Package chatpolicyadapter translates the transport contracts into the pure
// chat policy inputs. It deliberately resolves authority from a caller-owned
// current authority source; roles and qualifications carried in a request are
// never used as authorization facts.
package chatpolicyadapter

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

var ErrStaleAuthority = errors.New("chat policy: stale authority")

// AuthoritySource supplies a current, typed snapshot. Implementations must
// obtain this from the identity/governance boundary, rather than copying
// request profile fields.
type AuthoritySource interface {
	Resolve(context.Context, string, string, time.Time) (chatpolicy.Principal, error)
}

type Grant struct {
	ConversationID     string
	HostTenant         string
	ConsumerTenant     string
	Version            uint64
	Scope              string
	Classification     string
	Residency          string
	Proposed           bool
	AcceptedByHost     bool
	AcceptedByConsumer bool
	ExpiresAt          time.Time
	RevokedAt          time.Time
}

type Request struct {
	Action       chatpolicy.Action
	Principal    chat.Principal
	Conversation chat.Conversation
	Membership   *chat.Membership
	Grant        *Grant
	Now          time.Time
}

type Decision struct {
	Allowed  bool
	Revision uint64
}

type Adapter struct{ authority AuthoritySource }

func New(authority AuthoritySource) (*Adapter, error) {
	if authority == nil {
		return nil, ErrStaleAuthority
	}
	return &Adapter{authority: authority}, nil
}

func (a *Adapter) Authorize(ctx context.Context, req Request) (Decision, error) {
	if a == nil || a.authority == nil || req.Now.IsZero() || strings.TrimSpace(req.Principal.SubjectID) == "" || strings.TrimSpace(req.Principal.TenantID) == "" || strings.TrimSpace(req.Conversation.ID) == "" || strings.TrimSpace(req.Conversation.TenantID) == "" {
		return Decision{}, chatpolicy.ErrInvalidInput
	}
	current, err := a.authority.Resolve(ctx, req.Principal.TenantID, req.Principal.SubjectID, req.Now)
	if err != nil || current.ID != req.Principal.SubjectID || current.Tenant != req.Principal.TenantID || current.AuthorityRevision == 0 {
		return Decision{}, ErrStaleAuthority
	}
	in := chatpolicy.Input{Principal: current, Channel: chatpolicy.Channel{ID: req.Conversation.ID, HostTenant: req.Conversation.TenantID, Enabled: !req.Conversation.Archived, Private: req.Conversation.Kind != chat.PublicChannel, Revision: req.Conversation.Revision}, Now: req.Now}
	if req.Membership != nil {
		m := req.Membership
		if m.ConversationID != req.Conversation.ID || m.SubjectID != req.Principal.SubjectID || m.HomeTenantID != req.Principal.TenantID || m.Revision == 0 {
			return Decision{}, chatpolicy.ErrNotAuthorized
		}
		state := chatpolicy.MembershipCurrent
		if m.LeftAt != nil {
			state = chatpolicy.MembershipLeft
		}
		in.Membership = chatpolicy.Membership{ConversationID: m.ConversationID, PrincipalID: m.SubjectID, Tenant: m.HomeTenantID, State: state, Revision: m.Revision, JoinedAt: valueTime(m.JoinedAt), LeftAt: valueTime(m.LeftAt)}
		in.HasMembership = true
	}
	if req.Grant != nil {
		g := req.Grant
		in.Grant = chatpolicy.Grant{ConversationID: g.ConversationID, HostTenant: g.HostTenant, ConsumerTenant: g.ConsumerTenant, Version: g.Version, Scope: g.Scope, Classification: g.Classification, Residency: g.Residency, Proposed: g.Proposed, AcceptedByHost: g.AcceptedByHost, AcceptedByConsumer: g.AcceptedByConsumer, ExpiresAt: g.ExpiresAt, RevokedAt: g.RevokedAt}
		in.HasGrant = true
	}
	decision, err := chatpolicy.Evaluate(req.Action, in)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Allowed: decision.Allowed, Revision: decision.Revision}, nil
}

func valueTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return v.UTC()
}
