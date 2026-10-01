package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona/handle"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrPersonaIdentityRegistrationUnavailable identifies a missing or
	// unavailable server-owned identity, publication, installation, or durable
	// registration port.
	ErrPersonaIdentityRegistrationUnavailable = errors.New("application: persona identity registration unavailable")
	// ErrPersonaIdentityRegistrationInvalid identifies a malformed request or a
	// source result that cannot be trusted as an exact identity binding.
	ErrPersonaIdentityRegistrationInvalid = errors.New("application: invalid persona identity registration")
	// ErrPersonaIdentityNotPublished identifies a persona version that has not
	// crossed the independent publication boundary.
	ErrPersonaIdentityNotPublished = errors.New("application: persona version is not published")
)

// PersonaIdentityPublicationSource reads one exact, independently reviewed
// published persona version. Implementations must not derive a version from a
// handle or display name.
type PersonaIdentityPublicationSource interface {
	PublishedPersona(context.Context, values.TenantId, string, uint32) (agentpersona.PersonaVersion, error)
}

// PersonaIdentityAgentSource reads the canonical chat Agent.ID from the chat
// installation boundary. It must return only an active, tenant-bound agent.
type PersonaIdentityAgentSource interface {
	ActiveChatAgent(context.Context, values.TenantId, string) (chatapps.Agent, error)
}

// PersonaIdentityDurableStore permanently binds an exact chat Agent.ID to one
// persona. Lookup is required so retries can prove an existing binding is the
// same binding rather than treating a conflict as success.
type PersonaIdentityDurableStore interface {
	RegisterPersonaChatIdentity(context.Context, values.TenantId, string, string, time.Time) error
	LookupPersonaChatIdentity(context.Context, values.TenantId, string) (agentpersonastore.PersonaChatIdentity, error)
}

// PersonaIdentityClock supplies trusted server time for durable registration.
type PersonaIdentityClock interface {
	Now() time.Time
}

// PersonaIdentityRegistrationCoordinator crosses the persona publication,
// chat installation, reserved-handle, and durable identity boundaries. It
// never accepts handle or display values from a caller.
type PersonaIdentityRegistrationCoordinator struct {
	Handles      *handle.Registry
	Publications PersonaIdentityPublicationSource
	Agents       PersonaIdentityAgentSource
	Identities   PersonaIdentityDurableStore
	Clock        PersonaIdentityClock
}

// PersonaIdentityRegistrationService is retained as a descriptive alias for
// compositions that name application services rather than coordinators.
type PersonaIdentityRegistrationService = PersonaIdentityRegistrationCoordinator

// PersonaIdentityRegistrationRequest selects a server-owned persona version
// and canonical chat Agent.ID. Presentation names are intentionally absent.
type PersonaIdentityRegistrationRequest struct {
	Tenant         values.TenantId
	PersonaID      string
	PersonaVersion uint32
	AgentID        string
}

// PersonaIdentityRegistration is the durable identity plus the permanent
// agent badge metadata. Invoker-specific "acting for" text is added only when
// a surface projects the identity.
type PersonaIdentityRegistration struct {
	Tenant         values.TenantId
	PersonaID      string
	PersonaVersion uint32
	AgentID        string
	Handle         string
	DisplayName    string
	Identity       handle.PersonaIdentity
	Badge          handle.AgentBadge
}

// NewPersonaIdentityRegistrationCoordinator validates the required source
// ports up front. Publication and active-installation evidence are mandatory.
func NewPersonaIdentityRegistrationCoordinator(handles *handle.Registry, publications PersonaIdentityPublicationSource, agents PersonaIdentityAgentSource, identities PersonaIdentityDurableStore, clock PersonaIdentityClock) (*PersonaIdentityRegistrationCoordinator, error) {
	if handles == nil || publications == nil || agents == nil || identities == nil || clock == nil {
		return nil, ErrPersonaIdentityRegistrationUnavailable
	}
	return &PersonaIdentityRegistrationCoordinator{Handles: handles, Publications: publications, Agents: agents, Identities: identities, Clock: clock}, nil
}

// NewPersonaIdentityRegistrationService constructs the same fail-closed
// coordinator under the service-oriented name used by application wiring.
func NewPersonaIdentityRegistrationService(handles *handle.Registry, publications PersonaIdentityPublicationSource, agents PersonaIdentityAgentSource, identities PersonaIdentityDurableStore, clock PersonaIdentityClock) (*PersonaIdentityRegistrationCoordinator, error) {
	return NewPersonaIdentityRegistrationCoordinator(handles, publications, agents, identities, clock)
}

// Register publishes the exact canonical chat identity after verifying that
// the requested persona version is published and the chat agent installation
// is active. The durable binding is never inferred from display metadata.
func (c *PersonaIdentityRegistrationCoordinator) Register(ctx context.Context, req PersonaIdentityRegistrationRequest) (PersonaIdentityRegistration, error) {
	if c == nil || c.Handles == nil || c.Publications == nil || c.Agents == nil || c.Identities == nil || c.Clock == nil {
		return PersonaIdentityRegistration{}, ErrPersonaIdentityRegistrationUnavailable
	}
	if ctx == nil || req.Tenant.Validate() != nil || strings.TrimSpace(string(req.Tenant)) != string(req.Tenant) || strings.TrimSpace(req.PersonaID) == "" || req.PersonaVersion == 0 || strings.TrimSpace(req.AgentID) == "" {
		return PersonaIdentityRegistration{}, ErrPersonaIdentityRegistrationInvalid
	}
	version, err := c.Publications.PublishedPersona(ctx, req.Tenant, req.PersonaID, req.PersonaVersion)
	if err != nil {
		return PersonaIdentityRegistration{}, fmt.Errorf("%w: published persona: %v", ErrPersonaIdentityRegistrationUnavailable, err)
	}
	profile := version.Profile
	if version.Verify() != nil || profile.PersonaID != req.PersonaID || profile.Version != req.PersonaVersion || strings.TrimSpace(profile.Handle) == "" || strings.TrimSpace(profile.DisplayName) == "" {
		return PersonaIdentityRegistration{}, ErrPersonaIdentityNotPublished
	}
	canonicalHandle, err := handle.CanonicalHandle(profile.Handle)
	if err != nil {
		return PersonaIdentityRegistration{}, fmt.Errorf("%w: persona handle: %v", ErrPersonaIdentityRegistrationInvalid, err)
	}
	agent, err := c.Agents.ActiveChatAgent(ctx, req.Tenant, req.AgentID)
	if err != nil {
		return PersonaIdentityRegistration{}, fmt.Errorf("%w: active chat agent: %v", ErrPersonaIdentityRegistrationUnavailable, err)
	}
	if agent.ID != req.AgentID || strings.TrimSpace(agent.InstallationID) == "" || agent.Status != chatapps.Active {
		return PersonaIdentityRegistration{}, ErrPersonaIdentityRegistrationInvalid
	}
	now := c.Clock.Now().UTC()
	if now.IsZero() {
		return PersonaIdentityRegistration{}, ErrPersonaIdentityRegistrationUnavailable
	}
	registered, err := c.Handles.RegisterPersona(handle.PersonaRegistration{Tenant: string(req.Tenant), PersonaID: req.PersonaID, Handle: profile.Handle, DisplayName: profile.DisplayName, Agent: agent})
	if err != nil {
		if !errors.Is(err, handle.ErrAlreadyRegistered) {
			return PersonaIdentityRegistration{}, fmt.Errorf("%w: reserved handle: %v", ErrPersonaIdentityRegistrationInvalid, err)
		}
		registered, err = c.Handles.Persona(string(req.Tenant), req.PersonaID)
		if err != nil || registered.Agent.ID != agent.ID || registered.Handle != canonicalHandle || registered.Status != handle.Active {
			return PersonaIdentityRegistration{}, fmt.Errorf("%w: existing handle is not the exact agent identity", ErrPersonaIdentityRegistrationInvalid)
		}
	}
	if err := c.Identities.RegisterPersonaChatIdentity(ctx, req.Tenant, agent.ID, req.PersonaID, now); err != nil {
		existing, lookupErr := c.Identities.LookupPersonaChatIdentity(ctx, req.Tenant, agent.ID)
		if lookupErr != nil || !existing.Active || existing.AgentID != agent.ID || existing.PersonaID != req.PersonaID {
			return PersonaIdentityRegistration{}, fmt.Errorf("%w: durable identity: %v", ErrPersonaIdentityRegistrationUnavailable, err)
		}
	}
	return PersonaIdentityRegistration{Tenant: req.Tenant, PersonaID: req.PersonaID, PersonaVersion: req.PersonaVersion, AgentID: agent.ID, Handle: registered.Handle, DisplayName: registered.DisplayName, Identity: registered, Badge: handle.AgentBadge{IsAgent: true, PersonaID: req.PersonaID, AgentID: agent.ID, Label: "Agent"}}, nil
}

// RegisterPersonaChatIdentity is the explicit operation name used by
// composition roots that distinguish identity registration from other persona
// registrations.
func (c *PersonaIdentityRegistrationCoordinator) RegisterPersonaChatIdentity(ctx context.Context, req PersonaIdentityRegistrationRequest) (PersonaIdentityRegistration, error) {
	return c.Register(ctx, req)
}

// AgentPersonaPublishedSource adapts the tenant-scoped persona store to the
// exact publication source required by the coordinator.
type AgentPersonaPublishedSource struct{ Store *agentpersonastore.Store }

// PublishedPersona returns only an exact version whose latest lifecycle state
// is PUBLISHED; it does not search by handle or display name.
func (s AgentPersonaPublishedSource) PublishedPersona(ctx context.Context, tenant values.TenantId, personaID string, version uint32) (agentpersona.PersonaVersion, error) {
	if s.Store == nil || ctx == nil || tenant.Validate() != nil || strings.TrimSpace(personaID) == "" || version == 0 {
		return agentpersona.PersonaVersion{}, ErrPersonaIdentityRegistrationInvalid
	}
	scoped, err := s.Store.ForTenant(ctx, tenant)
	if err != nil {
		return agentpersona.PersonaVersion{}, err
	}
	items, err := scoped.ListPublished(ctx)
	if err != nil {
		return agentpersona.PersonaVersion{}, err
	}
	for _, item := range items {
		if item.PersonaID == personaID && item.Version == int64(version) {
			return decodeAvailableProfile(item)
		}
	}
	return agentpersona.PersonaVersion{}, ErrPersonaIdentityNotPublished
}

// AgentPersonaIdentityStore adapts the durable tenant-scoped identity store
// to the application registration port.
type AgentPersonaIdentityStore struct{ Store *agentpersonastore.Store }

// RegisterPersonaChatIdentity writes the tenant-bound immutable binding.
func (s AgentPersonaIdentityStore) RegisterPersonaChatIdentity(ctx context.Context, tenant values.TenantId, agentID, personaID string, at time.Time) error {
	if s.Store == nil {
		return ErrPersonaIdentityRegistrationUnavailable
	}
	scoped, err := s.Store.ForTenant(ctx, tenant)
	if err != nil {
		return err
	}
	return scoped.RegisterPersonaChatIdentity(ctx, agentID, personaID, at)
}

// LookupPersonaChatIdentity reads the exact tenant-bound binding for retry
// verification.
func (s AgentPersonaIdentityStore) LookupPersonaChatIdentity(ctx context.Context, tenant values.TenantId, agentID string) (agentpersonastore.PersonaChatIdentity, error) {
	if s.Store == nil {
		return agentpersonastore.PersonaChatIdentity{}, ErrPersonaIdentityRegistrationUnavailable
	}
	scoped, err := s.Store.ForTenant(ctx, tenant)
	if err != nil {
		return agentpersonastore.PersonaChatIdentity{}, err
	}
	return scoped.LookupPersonaChatIdentity(ctx, agentID)
}
