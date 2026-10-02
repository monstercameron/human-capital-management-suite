package agentaccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// Registry is the part of the agent connection registry the user's page
// reads and changes. *agentconnect.Registry satisfies it.
type Registry interface {
	ConnectionIDs(tenant string) []string
	Revision(tenant, connectionID string) (agentconnect.ConnectionRevision, error)
	GrantedSkills(user agentconnect.UserContext, connectionID string) ([]agentconnect.SkillExposure, error)
	LinkedAccount(user agentconnect.UserContext, connectionID string) (agentconnect.AccountLink, error)
	UnlinkAccount(user agentconnect.UserContext, connectionID, reason string) error
}

// DelegationGrant is one run-bound grant that lets an agent act for the user.
type DelegationGrant struct {
	ID        string
	TaskID    string
	TaskLabel string
	Scope     string
	ExpiresAt time.Time
}

// Delegations lists and revokes the user's active run-bound grants. A revoke
// must take effect before the agent's next step: the implementation bumps the
// grant's revocation state in the store the runner re-reads for every step.
type Delegations interface {
	ActiveGrants(tenant, userID string, at time.Time) ([]DelegationGrant, error)
	RevokeTaskGrant(tenant, userID, taskID, reason string) error
}

// UserAccess serves the user's own agent access page.
type UserAccess struct {
	registry    Registry
	linker      *Linker
	delegations Delegations
	now         func() time.Time
}

// NewUserAccess composes the page's server side. delegations and linker may be
// nil: the page then shows no task grants and offers no Link button.
func NewUserAccess(registry Registry, linker *Linker, delegations Delegations, now func() time.Time) (*UserAccess, error) {
	if registry == nil {
		return nil, fmt.Errorf("%w: registry is required", ErrInvalid)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &UserAccess{registry: registry, linker: linker, delegations: delegations, now: now}, nil
}

// For binds the service to the signed-in user the transport resolved. The
// returned session is the only thing the page can act through, so it can only
// ever act for that user.
func (a *UserAccess) For(user agentconnect.UserContext) *UserSession {
	return &UserSession{access: a, user: user}
}

// UserSession implements productui.AgentAccessClient for one user.
type UserSession struct {
	access *UserAccess
	user   agentconnect.UserContext
}

var _ productui.AgentAccessClient = (*UserSession)(nil)

// Snapshot lists the connections granted to the user with their link state,
// the skills the grants give them and each skill's tier, then their active
// task grants. A connection that grants the user nothing is not listed, and a
// connection that is revoked or paused is listed as such rather than hidden.
func (s *UserSession) Snapshot() (productui.AgentAccessSnapshot, error) {
	var snapshot productui.AgentAccessSnapshot
	if ready, ok := s.access.registry.(interface{ Ready(string) error }); ok {
		if err := ready.Ready(s.user.TenantID); err != nil {
			return productui.AgentAccessSnapshot{}, err
		}
	}
	for _, id := range s.access.registry.ConnectionIDs(s.user.TenantID) {
		revision, err := s.access.registry.Revision(s.user.TenantID, id)
		if err != nil {
			return productui.AgentAccessSnapshot{}, err
		}
		granted, err := s.access.registry.GrantedSkills(s.user, id)
		if err != nil {
			return productui.AgentAccessSnapshot{}, err
		}
		if len(granted) == 0 {
			continue
		}
		connection := productui.AgentAccessConnection{ID: id, Provider: providerName(revision), CredentialMode: string(revision.CredentialMode), LinkState: "not_linked"}
		for _, skill := range granted {
			connection.Skills = append(connection.Skills, productui.AgentAccessSkill{ID: skill.ID, Name: skill.ID, Tier: string(skill.Tier), Description: skill.Tool.Capability})
		}
		usable := revision.Connection != nil && revision.Connection.Usable()
		switch revision.CredentialMode {
		case agentconnect.Brokered:
			// A brokered connection uses an administrator's credential: there is
			// nothing for the user to link or unlink.
			connection.LinkState = "linked"
			connection.AccountLabel = ""
		default:
			if account, err := s.access.registry.LinkedAccount(s.user, id); err == nil {
				connection.LinkState = "linked"
				connection.AccountLabel = account.ExternalAccountID
				connection.CanUnlink = true
			} else if !errors.Is(err, agentconnect.ErrAccountNotLinked) {
				return productui.AgentAccessSnapshot{}, err
			}
			if _, providerKnown := s.providerFor(id); providerKnown && usable && s.access.linker != nil {
				connection.CanLink = true
			}
		}
		snapshot.Connections = append(snapshot.Connections, connection)
	}
	if s.access.delegations != nil {
		grants, err := s.access.delegations.ActiveGrants(s.user.TenantID, s.user.UserID, s.access.now())
		if err != nil {
			return productui.AgentAccessSnapshot{}, err
		}
		for _, grant := range grants {
			snapshot.Delegations = append(snapshot.Delegations, productui.AgentDelegationGrant{ID: grant.ID, TaskID: grant.TaskID, TaskLabel: grant.TaskLabel, Scope: grant.Scope, ExpiresAt: grant.ExpiresAt.UTC().Format(time.RFC3339), Revocable: true})
		}
	}
	return snapshot, nil
}

func (s *UserSession) providerFor(connectionID string) (Provider, bool) {
	if s.access.linker == nil {
		return Provider{}, false
	}
	return s.access.linker.providers.Provider(s.user.TenantID, connectionID)
}

func providerName(revision agentconnect.ConnectionRevision) string {
	if revision.Connection != nil && strings.TrimSpace(revision.Connection.ConnectorID()) != "" {
		return revision.Connection.ConnectorID()
	}
	return revision.ID
}

// StartProviderAuthorization starts linking from this page only: the linker
// holds the verifier and the state, and the page receives the provider URL.
func (s *UserSession) StartProviderAuthorization(connectionID string) (productui.AgentAuthorizationStart, error) {
	if s.access.linker == nil {
		return productui.AgentAuthorizationStart{}, ErrNotLinkable
	}
	authorization, err := s.access.linker.Start(s.user, connectionID)
	if err != nil {
		return productui.AgentAuthorizationStart{}, err
	}
	return productui.AgentAuthorizationStart{AuthorizationURL: authorization.URL, State: authorization.State, CodeChallenge: authorization.CodeChallenge, CodeChallengeMethod: authorization.CodeChallengeMethod}, nil
}

// UnlinkConnection removes the user's own account and fences every lease
// minted from it, so the agent's next step fails closed.
func (s *UserSession) UnlinkConnection(connectionID string) error {
	return s.access.registry.UnlinkAccount(s.user, connectionID, "user unlinked account")
}

// RevokeDelegation revokes the grant of one of the user's own tasks. Another user's
// grant id is indistinguishable from an unknown one.
func (s *UserSession) RevokeDelegation(taskID string) error {
	if s.access.delegations == nil {
		return ErrInvalid
	}
	return s.access.delegations.RevokeTaskGrant(s.user.TenantID, s.user.UserID, taskID, "user revoked grant")
}

// CompleteLink finishes a link from the provider's redirect for the signed-in
// user: the state must be one this user's own Start minted. It returns the
// connection that was linked.
func (a *UserAccess) CompleteLink(ctx context.Context, user agentconnect.UserContext, state, code string) (string, error) {
	if a.linker == nil {
		return "", ErrNotLinkable
	}
	return a.linker.Complete(ctx, user, state, code)
}
