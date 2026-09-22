// Package chatpolicy contains the pure authorization rules for collaboration.
// Callers provide the current identity and conversation state; this package
// does not read a database, process-wide cache, or clock singleton.
package chatpolicy

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNotAuthorized = errors.New("chat: not authorized")
	ErrInvalidInput  = errors.New("chat: invalid authorization input")
)

type Action uint8

const (
	ActionDiscover Action = iota + 1
	ActionJoin
	ActionRead
	ActionPost
)

type RoleMode uint8

const (
	RolesAny RoleMode = iota + 1
	RolesAll
)

type Qualification struct {
	Name      string
	Verified  bool
	ValidFrom time.Time
	ValidTo   time.Time
}

func (q Qualification) Current(at time.Time) bool {
	if !q.Verified || strings.TrimSpace(q.Name) == "" {
		return false
	}
	return !at.Before(q.ValidFrom) && (q.ValidTo.IsZero() || at.Before(q.ValidTo))
}

// Principal is the current, verified authority supplied by the identity
// boundary. Display profile fields are intentionally absent: labels never
// authorize access.
type Principal struct {
	ID                string
	Tenant            string
	Active            bool
	Roles             []string
	Qualifications    []Qualification
	Allowlist         map[string]bool
	AuthorityRevision uint64
	RevokedAt         time.Time
}

func (p Principal) Current(at time.Time) bool {
	return strings.TrimSpace(p.ID) != "" && strings.TrimSpace(p.Tenant) != "" && p.Active && (p.RevokedAt.IsZero() || at.Before(p.RevokedAt))
}

type Channel struct {
	ID                     string
	HostTenant             string
	Private                bool
	Enabled                bool
	Revision               uint64
	RequiredRoles          []string
	RoleMode               RoleMode
	RequiredQualifications []string
	AllowedPrincipals      []string
	AllowedTenants         []string
	Classification         string
	Residency              string
}

type Grant struct {
	ID                 string
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

func (g Grant) Current(at time.Time, conversation, host, consumer string) bool {
	return g.Version > 0 && g.Proposed && g.AcceptedByHost && g.AcceptedByConsumer &&
		g.ConversationID == conversation && g.HostTenant == host && g.ConsumerTenant == consumer &&
		(g.ExpiresAt.IsZero() || at.Before(g.ExpiresAt)) && (g.RevokedAt.IsZero() || at.Before(g.RevokedAt))
}

// ProposeGrant creates the host half of a bilateral grant. Consumer consent
// is a separate transition and is never implied by an invitation or URL.
func ProposeGrant(id, conversation, host, consumer, scope, classification, residency string, version uint64, expiresAt time.Time) (Grant, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(conversation) == "" || strings.TrimSpace(host) == "" || strings.TrimSpace(consumer) == "" || host == consumer || version == 0 {
		return Grant{}, ErrInvalidInput
	}
	return Grant{ID: id, ConversationID: conversation, HostTenant: host, ConsumerTenant: consumer, Version: version, Scope: scope, Classification: classification, Residency: residency, Proposed: true, AcceptedByHost: true, ExpiresAt: expiresAt}, nil
}

// AcceptGrant records consumer consent while preserving the proposal's
// version and scope. A caller must provide the current time explicitly.
func AcceptGrant(g Grant, consumerTenant string, at time.Time) (Grant, error) {
	if !g.Proposed || g.RevokedAt.IsZero() == false || !g.ExpiresAt.IsZero() && !at.Before(g.ExpiresAt) || consumerTenant != g.ConsumerTenant || at.IsZero() {
		return Grant{}, ErrInvalidInput
	}
	g.AcceptedByConsumer = true
	return g, nil
}

func RevokeGrant(g Grant, at time.Time) (Grant, error) {
	if g.Version == 0 || at.IsZero() {
		return Grant{}, ErrInvalidInput
	}
	g.RevokedAt = at
	g.Version++
	return g, nil
}

type MembershipState uint8

const (
	MembershipCurrent MembershipState = iota + 1
	MembershipLeft
	MembershipSuspended
	MembershipRemoved
)

type Membership struct {
	ConversationID string
	PrincipalID    string
	Tenant         string
	State          MembershipState
	Revision       uint64
	GrantVersion   uint64
	JoinedAt       time.Time
	LeftAt         time.Time
	SuspendedAt    time.Time
}

func (m Membership) Current(conversation, principal, tenant string) bool {
	return m.Revision > 0 && m.State == MembershipCurrent && m.ConversationID == conversation && m.PrincipalID == principal && m.Tenant == tenant
}

func (m Membership) CurrentAt(conversation, principal, tenant string, at time.Time) bool {
	return m.Current(conversation, principal, tenant) && !at.Before(m.JoinedAt) && (m.LeftAt.IsZero() || at.Before(m.LeftAt)) && (m.SuspendedAt.IsZero() || at.Before(m.SuspendedAt))
}

type Input struct {
	Principal     Principal
	Channel       Channel
	Membership    Membership
	HasMembership bool
	Grant         Grant
	HasGrant      bool
	Now           time.Time
}

type Decision struct {
	Allowed  bool
	Revision uint64
}

// Evaluator supplies the service's clock without making time a process-wide
// dependency. State remains an argument to Evaluate, so replay and fault
// tests can use an exact snapshot.
type Evaluator struct{ clock Clock }

func New(clock Clock) Evaluator { return Evaluator{clock: clock} }

func (e Evaluator) Evaluate(action Action, in Input) (Decision, error) {
	if in.Now.IsZero() {
		in.Now = Now(e.clock)
	}
	return Evaluate(action, in)
}

// Evaluate applies mandatory denies first, then composes the declared role,
// qualification, and allowlist requirements. A denied result never indicates
// whether the channel exists or which rule failed.
func Evaluate(action Action, in Input) (Decision, error) {
	if action < ActionDiscover || action > ActionPost || in.Now.IsZero() || strings.TrimSpace(in.Channel.ID) == "" || strings.TrimSpace(in.Channel.HostTenant) == "" || in.Channel.Revision == 0 {
		return Decision{}, ErrInvalidInput
	}
	if !in.Principal.Current(in.Now) {
		return Decision{Revision: revision(in)}, ErrNotAuthorized
	}
	if !in.Channel.Enabled {
		return Decision{Revision: revision(in)}, ErrNotAuthorized
	}

	foreign := in.Principal.Tenant != in.Channel.HostTenant
	if foreign {
		if !in.HasGrant || !in.Grant.Current(in.Now, in.Channel.ID, in.Channel.HostTenant, in.Principal.Tenant) || !grantMatches(in.Channel, in.Grant) {
			return Decision{Revision: revision(in)}, ErrNotAuthorized
		}
	}
	if action != ActionDiscover || in.Channel.Private {
		if in.Channel.Private || len(in.Channel.RequiredRoles) > 0 || len(in.Channel.RequiredQualifications) > 0 || len(in.Channel.AllowedPrincipals) > 0 || len(in.Channel.AllowedTenants) > 0 {
			if !in.HasMembership || !in.Membership.CurrentAt(in.Channel.ID, in.Principal.ID, in.Principal.Tenant, in.Now) {
				return Decision{Revision: revision(in)}, ErrNotAuthorized
			}
		}
	}
	if !contains(in.Channel.AllowedTenants, in.Principal.Tenant) && len(in.Channel.AllowedTenants) > 0 {
		return Decision{Revision: revision(in)}, ErrNotAuthorized
	}
	if !contains(in.Channel.AllowedPrincipals, in.Principal.ID) && len(in.Channel.AllowedPrincipals) > 0 {
		return Decision{Revision: revision(in)}, ErrNotAuthorized
	}
	if !rolesMatch(in.Channel, in.Principal.Roles) || !qualificationsMatch(in.Channel, in.Principal.Qualifications, in.Now) {
		return Decision{Revision: revision(in)}, ErrNotAuthorized
	}
	return Decision{Allowed: true, Revision: revision(in)}, nil
}

func grantMatches(c Channel, g Grant) bool {
	return (g.Classification == "" || g.Classification == c.Classification) && (g.Residency == "" || g.Residency == c.Residency) && (g.Scope == "" || g.Scope == "conversation")
}

func rolesMatch(c Channel, held []string) bool {
	if len(c.RequiredRoles) == 0 {
		return true
	}
	have := make(map[string]bool, len(held))
	for _, role := range held {
		have[role] = true
	}
	matched := 0
	for _, role := range c.RequiredRoles {
		if have[role] {
			matched++
		}
	}
	if c.RoleMode == RolesAll {
		return matched == len(c.RequiredRoles)
	}
	return matched > 0
}

func qualificationsMatch(c Channel, held []Qualification, at time.Time) bool {
	if len(c.RequiredQualifications) == 0 {
		return true
	}
	current := make(map[string]bool, len(held))
	for _, q := range held {
		if q.Current(at) {
			current[q.Name] = true
		}
	}
	for _, required := range c.RequiredQualifications {
		if !current[required] {
			return false
		}
	}
	return true
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func revision(in Input) uint64 {
	r := in.Channel.Revision ^ in.Principal.AuthorityRevision
	if in.HasMembership {
		r ^= in.Membership.Revision
	}
	if in.HasGrant {
		r ^= in.Grant.Version
	}
	return r
}

type StreamBinding struct {
	PrincipalID        string
	Tenant             string
	ConversationID     string
	ChannelRevision    uint64
	PolicyRevision     uint64
	AuthorityRevision  uint64
	MembershipRevision uint64
	GrantVersion       uint64
}

func BindStream(in Input) (StreamBinding, error) {
	decision, err := Evaluate(ActionRead, in)
	if err != nil {
		return StreamBinding{}, ErrNotAuthorized
	}
	return StreamBinding{PrincipalID: in.Principal.ID, Tenant: in.Principal.Tenant, ConversationID: in.Channel.ID, ChannelRevision: in.Channel.Revision, PolicyRevision: decision.Revision, AuthorityRevision: in.Principal.AuthorityRevision, MembershipRevision: in.Membership.Revision, GrantVersion: in.Grant.Version}, nil
}

// StreamValid reauthorizes using current state. Any authority, membership,
// grant, channel, or policy revision change invalidates the old stream.
func StreamValid(binding StreamBinding, in Input) bool {
	if binding.PrincipalID != in.Principal.ID || binding.Tenant != in.Principal.Tenant || binding.ConversationID != in.Channel.ID {
		return false
	}
	decision, err := Evaluate(ActionRead, in)
	return err == nil && in.Channel.Revision == binding.ChannelRevision && decision.Revision == binding.PolicyRevision && in.Principal.AuthorityRevision == binding.AuthorityRevision && in.Membership.Revision == binding.MembershipRevision && in.Grant.Version == binding.GrantVersion
}

// Clock is injectable by service and test callers. The policy itself never
// reads time.Now; Evaluate receives an explicit instant for replayability.
type Clock func() time.Time

func Now(clock Clock) time.Time {
	if clock == nil {
		return time.Time{}
	}
	return clock()
}
