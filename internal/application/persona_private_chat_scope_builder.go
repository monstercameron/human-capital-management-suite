package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaPrivateChatScopeBuilder = errors.New("application: private persona chat request unavailable")

type personaPrivateChatVersionReader interface {
	GetVersion(context.Context, string, int64) (agentpersonastore.PersonaVersion, error)
}

type personaPrivateChatVersionFactory interface {
	ForTenant(context.Context, values.TenantId) (personaPrivateChatVersionReader, error)
}

type personaPrivateChatSkillCatalog interface {
	ResolvePin(agentskills.SkillPin) (agentskills.SkillRecord, error)
}

// PersonaPrivateChatScopeUserResolver obtains current directory-backed
// population, roles and organization scopes for the verified principal.
type PersonaPrivateChatScopeUserResolver interface {
	Resolve(context.Context, *trust.Principal, string) (agentgate.UserContext, error)
}

type personaPrivateChatDiscoveryUserResolver struct{ current AgentDiscoveryContext }

func (r personaPrivateChatDiscoveryUserResolver) Resolve(ctx context.Context, principal *trust.Principal, purpose string) (agentgate.UserContext, error) {
	if r.current == nil {
		return agentgate.UserContext{}, errPersonaPrivateChatScopeBuilder
	}
	user, _, _, err := r.current.Resolve(ctx, principal, purpose)
	return user, err
}

// PersonaPrivateChatScopeRequestBuilder reconstructs the exact Gate request
// from the current authenticated principal and immutable admitted persona.
type PersonaPrivateChatScopeRequestBuilderAdapter struct {
	personas personaPrivateChatVersionFactory
	skills   personaPrivateChatSkillCatalog
	users    PersonaPrivateChatScopeUserResolver
	now      func() time.Time
}

var _ PersonaPrivateChatScopeRequestBuilder = (*PersonaPrivateChatScopeRequestBuilderAdapter)(nil)

// NewPersonaPrivateChatScopeRequestBuilder requires tenant-scoped immutable
// persona reads, the current pinned-skill catalog and current user facts.
func NewPersonaPrivateChatScopeRequestBuilder(personas personaPrivateChatVersionFactory, skills personaPrivateChatSkillCatalog, users PersonaPrivateChatScopeUserResolver, now func() time.Time) (*PersonaPrivateChatScopeRequestBuilderAdapter, error) {
	if personas == nil || skills == nil || users == nil || now == nil {
		return nil, errPersonaPrivateChatScopeBuilder
	}
	return &PersonaPrivateChatScopeRequestBuilderAdapter{personas: personas, skills: skills, users: users, now: now}, nil
}

// NewDatabasePersonaPrivateChatScopeRequestBuilder binds immutable persona
// reads to the existing tenant store and current directory resolver.
func NewDatabasePersonaPrivateChatScopeRequestBuilder(store *agentpersonastore.Store, skills personaPrivateChatSkillCatalog, current AgentDiscoveryContext, now func() time.Time) (*PersonaPrivateChatScopeRequestBuilderAdapter, error) {
	if store == nil || current == nil {
		return nil, errPersonaPrivateChatScopeBuilder
	}
	return NewPersonaPrivateChatScopeRequestBuilder(privateChatPersonaStoreFactory{store: store}, skills, personaPrivateChatDiscoveryUserResolver{current: current}, now)
}

type privateChatPersonaStoreFactory struct{ store *agentpersonastore.Store }

func (f privateChatPersonaStoreFactory) ForTenant(ctx context.Context, tenant values.TenantId) (personaPrivateChatVersionReader, error) {
	if f.store == nil || ctx == nil {
		return nil, errPersonaPrivateChatScopeBuilder
	}
	return f.store.Scoped(tenant)
}

// BuildPrivateChatScopeRequest uses trust context for the invoker, reloads the
// exact immutable persona image, and returns only its unique pinned T0 reply skill.
func (b *PersonaPrivateChatScopeRequestBuilderAdapter) BuildPrivateChatScopeRequest(ctx context.Context, record agentrun.Record, run runstate.Run) (agentgate.PrivateChatScopeRequest, error) {
	if b == nil || b.personas == nil || b.skills == nil || b.users == nil || b.now == nil || ctx == nil || ctx.Err() != nil ||
		!personaPrivateChatRunTupleMatches(record, run) || record.Decision != agentrun.DecisionAccepted || record.Request.Persona == nil ||
		record.Request.Source.Kind != agentrun.SourcePersonaMention || record.Request.Principal.Mode != agentrun.ModeOnBehalfOf || record.Request.Purpose != "persona-mention" {
		return agentgate.PrivateChatScopeRequest{}, errPersonaPrivateChatScopeBuilder
	}
	r := record.Request
	wantID, idErr := agentrun.AdmissionRequestID(r.Source)
	wantDigest, digestErr := agentrun.AdmissionRequestDigest(r)
	if idErr != nil || digestErr != nil || wantID != record.ID || wantDigest != record.RequestDigest {
		return agentgate.PrivateChatScopeRequest{}, errPersonaPrivateChatScopeBuilder
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant().String() != r.Source.TenantID ||
		verified.Subject() != r.Principal.InvokerID || !verified.AuthorizesPurpose(r.Purpose) {
		return agentgate.PrivateChatScopeRequest{}, errPersonaPrivateChatScopeBuilder
	}
	at := b.now().UTC()
	if at.IsZero() || at.Before(verified.IssuedAt()) || !at.Before(verified.ExpiresAt()) {
		return agentgate.PrivateChatScopeRequest{}, errPersonaPrivateChatScopeBuilder
	}
	tenant := values.TenantId(r.Source.TenantID)
	if tenant.Validate() != nil {
		return agentgate.PrivateChatScopeRequest{}, errPersonaPrivateChatScopeBuilder
	}
	versionNumber, err := personaRunVersionNumber(r.Persona.Version)
	if err != nil {
		return agentgate.PrivateChatScopeRequest{}, errPersonaPrivateChatScopeBuilder
	}
	store, err := b.personas.ForTenant(ctx, tenant)
	if err != nil || store == nil {
		return agentgate.PrivateChatScopeRequest{}, fmt.Errorf("%w: tenant persona store: %v", errPersonaPrivateChatScopeBuilder, err)
	}
	version, err := store.GetVersion(ctx, r.Persona.ID, versionNumber)
	if err != nil || version.TenantID != tenant || version.PersonaID != r.Persona.ID || version.Version != versionNumber || version.ContentDigest != r.Persona.Digest {
		return agentgate.PrivateChatScopeRequest{}, errPersonaPrivateChatScopeBuilder
	}
	var profile agentpersona.PersonaProfile
	if json.Unmarshal(version.Profile, &profile) != nil || versionNumber > int64(^uint32(0)) || profile.PersonaID != r.Persona.ID || profile.Version != uint32(versionNumber) {
		return agentgate.PrivateChatScopeRequest{}, errPersonaPrivateChatScopeBuilder
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != r.Persona.Digest {
		return agentgate.PrivateChatScopeRequest{}, errPersonaPrivateChatScopeBuilder
	}
	pin, err := b.uniquePinnedReplySkill(profile.SkillPins)
	if err != nil {
		return agentgate.PrivateChatScopeRequest{}, err
	}
	user, err := b.users.Resolve(ctx, verified, r.Purpose)
	if err != nil || user.Principal == nil || user.Principal.Subject() != verified.Subject() || user.Principal.Tenant() != verified.Tenant() ||
		user.Principal.SubjectKind() != trust.SubjectKindHuman || user.Population == "" || len(user.Roles) == 0 || len(user.OrganizationScopes) == 0 {
		return agentgate.PrivateChatScopeRequest{}, errPersonaPrivateChatScopeBuilder
	}
	user.Principal = verified
	return agentgate.PrivateChatScopeRequest{User: user, Skill: pin, Purpose: r.Purpose, Tenant: tenant,
		ConversationID: r.Audience.ID, ThreadID: r.Context.ID, InvokingPostID: r.Source.Ref, At: at}, nil
}

func (b *PersonaPrivateChatScopeRequestBuilderAdapter) uniquePinnedReplySkill(pins []agentskills.SkillPin) (agentskills.SkillPin, error) {
	var selected agentskills.SkillPin
	for _, pin := range pins {
		record, err := b.skills.ResolvePin(pin)
		if err != nil || record.Definition.Key() != pin.Key() || record.Digest != pin.Digest || record.Status != agentskills.StatusActive {
			continue
		}
		if !personaPrivateChatReplySkill(record) || record.Definition.SideEffectTier != agentskills.TierT0 {
			continue
		}
		if selected.ID != "" {
			return agentskills.SkillPin{}, errPersonaPrivateChatScopeBuilder
		}
		selected = pin
	}
	if selected.ID == "" {
		return agentskills.SkillPin{}, errPersonaPrivateChatScopeBuilder
	}
	return selected, nil
}

func personaPrivateChatReplySkill(record agentskills.SkillRecord) bool {
	if len(record.ResolvedOperations) != 1 || !record.ResolvedOperations[0].HasCapability {
		return false
	}
	capabilityRecord := record.ResolvedOperations[0].Capability
	definition := capabilityRecord.Definition
	return capabilityRecord.Status == capability.StatusActive && definition.ID == agentgate.PrivateChatReplyCapability &&
		definition.Version == 1 && definition.AuthZScopeRef == agentgate.PrivateChatReplyScope &&
		definition.EffectClass == capability.EffectPure && definition.AgentEligible
}
