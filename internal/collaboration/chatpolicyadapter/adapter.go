// Package chatpolicyadapter translates the transport contracts into the pure
// chat policy inputs. It deliberately resolves authority from a caller-owned
// current authority source; roles and qualifications carried in a request are
// never used as authorization facts.
package chatpolicyadapter

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

var (
	ErrStaleAuthority = errors.New("chat policy: stale authority")
	ErrStaleHostFacts = errors.New("chat policy: stale host channel facts")
)

// AuthoritySource supplies a current, typed snapshot. Implementations must
// obtain this from the identity/governance boundary, rather than copying
// request profile fields.
type AuthoritySource interface {
	Resolve(context.Context, string, string, time.Time) (chatpolicy.Principal, error)
}

// HostChannelFactsSource supplies the current host-owned channel policy.
// Implementations must resolve from the host's durable governance state; they
// must not copy classification, residency, or allowlists from a request.
// Its Policy method shape is compatible with the chatauthority store.
type HostChannelFactsSource interface {
	Policy(context.Context, string, string) (chatpolicy.Channel, error)
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

type Adapter struct {
	authority AuthoritySource
	hostFacts HostChannelFactsSource
}

// New accepts an optional trusted host-policy source. Same-tenant decisions do
// not need cross-company terms, but foreign decisions fail closed unless the
// source is present and returns current, complete facts.
func New(authority AuthoritySource, hostFacts ...HostChannelFactsSource) (*Adapter, error) {
	if authority == nil {
		return nil, ErrStaleAuthority
	}
	if len(hostFacts) > 1 || len(hostFacts) == 1 && hostFacts[0] == nil {
		return nil, ErrStaleHostFacts
	}
	a := &Adapter{authority: authority}
	if len(hostFacts) == 1 {
		a.hostFacts = hostFacts[0]
	}
	return a, nil
}

func (a *Adapter) Authorize(ctx context.Context, req Request) (Decision, error) {
	if a == nil || a.authority == nil || req.Now.IsZero() || strings.TrimSpace(req.Principal.SubjectID) == "" || strings.TrimSpace(req.Principal.TenantID) == "" || strings.TrimSpace(req.Conversation.ID) == "" || strings.TrimSpace(req.Conversation.TenantID) == "" || req.Conversation.Revision == 0 {
		return Decision{}, chatpolicy.ErrInvalidInput
	}
	current, err := a.authority.Resolve(ctx, req.Principal.TenantID, req.Principal.SubjectID, req.Now)
	if err != nil || current.ID != req.Principal.SubjectID || current.Tenant != req.Principal.TenantID || current.AuthorityRevision == 0 {
		return Decision{}, ErrStaleAuthority
	}
	channel := chatpolicy.Channel{ID: req.Conversation.ID, HostTenant: req.Conversation.TenantID, Enabled: !req.Conversation.Archived, Private: req.Conversation.Kind != chat.PublicChannel, Revision: req.Conversation.Revision}
	if req.Principal.TenantID != req.Conversation.TenantID {
		if a.hostFacts == nil {
			return Decision{}, ErrStaleHostFacts
		}
		policy, err := a.hostFacts.Policy(ctx, req.Conversation.TenantID, req.Conversation.ID)
		if err != nil || policy.ID != req.Conversation.ID || policy.HostTenant != req.Conversation.TenantID || policy.Revision == 0 ||
			!completeHostTerm(policy.Classification) || !completeHostTerm(policy.Residency) {
			return Decision{}, ErrStaleHostFacts
		}
		channel.RequiredRoles = append([]string(nil), policy.RequiredRoles...)
		channel.RoleMode = policy.RoleMode
		channel.RequiredQualifications = append([]string(nil), policy.RequiredQualifications...)
		channel.AllowedPrincipals = append([]string(nil), policy.AllowedPrincipals...)
		channel.AllowedTenants = append([]string(nil), policy.AllowedTenants...)
		channel.DeniedPrincipals = append([]string(nil), policy.DeniedPrincipals...)
		channel.DeniedTenants = append([]string(nil), policy.DeniedTenants...)
		channel.Classification = policy.Classification
		channel.Residency = policy.Residency
		channel.Revision = hostPolicyRevision(req.Conversation.Revision, policy.Revision)
	}
	in := chatpolicy.Input{Principal: current, Channel: channel, Now: req.Now}
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

func completeHostTerm(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

// hostPolicyRevision binds conversation metadata and the independently
// revisioned host policy without the collisions possible with XOR.
func hostPolicyRevision(conversation, policy uint64) uint64 {
	var versions [16]byte
	binary.LittleEndian.PutUint64(versions[:8], conversation)
	binary.LittleEndian.PutUint64(versions[8:], policy)
	digest := sha256.Sum256(versions[:])
	revision := binary.LittleEndian.Uint64(digest[:8])
	if revision == 0 {
		return 1
	}
	return revision
}

func valueTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return v.UTC()
}
