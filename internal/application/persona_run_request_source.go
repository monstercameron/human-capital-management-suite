package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var errPersonaRunRequestSource = errors.New("application: trusted persona run facts unavailable")

// PersonaRunOwnerFacts contains run facts that belong to services other than
// the persona and agent stores. Implementations must read current trusted state;
// invocation claims and display fields are not authoritative inputs.
type PersonaRunOwnerFacts struct {
	LegalEntityID  string
	AgentPrincipal string
	Audience       agentrun.AudienceScope
	Context        agentrun.ContextScope
	Deadline       time.Time
	Budget         agentrun.Budget
}

// PersonaRunOwnerFactsSource resolves admission facts owned outside the
// persona catalog, including current audience and context snapshot digests.
type PersonaRunOwnerFactsSource interface {
	ResolvePersonaRunOwnerFacts(context.Context, agentinvoke.RunRequest) (PersonaRunOwnerFacts, error)
}

type personaRunAuthorityReader interface {
	ReadCurrentPersonaAuthority(context.Context, string, string) (agentpersonastore.PersonaVersion, agentpersonastore.ActiveInstallation, error)
}

type personaRunAuthorityFactory interface {
	ForTenant(context.Context, string) (personaRunAuthorityReader, error)
}

// PersonaRunAuthorityStore adapts the isolated persona store to the run-fact
// source. It scopes every read from the invocation tenant before querying.
type PersonaRunAuthorityStore struct{ Store *agentpersonastore.Store }

func (s PersonaRunAuthorityStore) ForTenant(ctx context.Context, tenant string) (personaRunAuthorityReader, error) {
	if s.Store == nil || ctx == nil || strings.TrimSpace(tenant) != tenant || tenant == "" {
		return nil, errPersonaRunRequestSource
	}
	scoped, err := s.Store.Scoped(values.TenantId(tenant))
	if err != nil {
		return nil, err
	}
	return scoped, nil
}

type personaRunManifestResolver interface {
	ResolveAgentManifestContext(context.Context, agentpersona.AgentManifestRef) (agentmanifest.Manifest, error)
	TenantID() string
}

type personaRunManifestResolverFactory interface {
	ForTenant(context.Context, string) (personaRunManifestResolver, error)
}

// TenantPersonaRunManifestResolver composes a tenant-specific exact-version
// manifest resolver. The returned resolver must be bound to the requested
// tenant by its implementation.
type TenantPersonaRunManifestResolver func(context.Context, string) (personaRunManifestResolver, error)

func (f TenantPersonaRunManifestResolver) ForTenant(ctx context.Context, tenant string) (personaRunManifestResolver, error) {
	if f == nil || ctx == nil || tenant == "" || strings.TrimSpace(tenant) != tenant {
		return nil, errPersonaRunRequestSource
	}
	return f(ctx, tenant)
}

// DatabasePersonaRunRequestSource combines tenant-scoped persona and agent
// records with a required owner-facts port. It does not derive missing
// audience, context, legal-entity, principal, budget, or deadline values.
type DatabasePersonaRunRequestSource struct {
	Personas   personaRunAuthorityFactory
	Manifests  personaRunManifestResolverFactory
	OwnerFacts PersonaRunOwnerFactsSource
}

var _ PersonaRunRequestSource = (*DatabasePersonaRunRequestSource)(nil)

// ResolvePersonaRun reads the current published profile and active placement,
// validates the profile's exact agent manifest pin, and then obtains the
// remaining admission facts from their owning service.
func (s *DatabasePersonaRunRequestSource) ResolvePersonaRun(ctx context.Context, invocation agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
	if s == nil || s.Personas == nil || s.Manifests == nil || s.OwnerFacts == nil || ctx == nil || !validPersonaRunInvocation(invocation) {
		return PersonaRunRequestFacts{}, errPersonaRunRequestSource
	}
	store, err := s.Personas.ForTenant(ctx, invocation.TenantID)
	if err != nil || store == nil {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: scope persona store: %v", errPersonaRunRequestSource, err)
	}
	version, installation, err := store.ReadCurrentPersonaAuthority(ctx, invocation.ConversationID, invocation.PersonaID)
	if err != nil {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: read current persona authority: %v", errPersonaRunRequestSource, err)
	}
	if version.TenantID.String() != invocation.TenantID || version.PersonaID != invocation.PersonaID ||
		version.Version <= 0 || uint64(version.Version) > math.MaxUint32 || !personaRunVersionMatches(version.Version, invocation.PersonaVersion) ||
		installation.InstallationID != invocation.InstallationID || installation.PersonaID != invocation.PersonaID ||
		installation.PersonaVersion != version.Version || installation.ConversationID != invocation.ConversationID ||
		!validPersonaRunDigest(version.ContentDigest) {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: persona or installation is stale or mismatched", errPersonaRunRequestSource)
	}
	var profile agentpersona.PersonaProfile
	if err := json.Unmarshal(version.Profile, &profile); err != nil || profile.PersonaID != version.PersonaID || profile.Version != uint32(version.Version) ||
		profile.Manifest.ID == "" || profile.Manifest.Version == 0 || profile.Manifest.SchemaVersion == 0 || !validPersonaRunDigest(profile.Manifest.Digest) {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: stored persona profile or agent pin is invalid", errPersonaRunRequestSource)
	}
	if version.AgentVersion != personaRunAgentVersion(profile.Manifest) || installation.AgentVersion != version.AgentVersion {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: persisted agent version labels differ from persona manifest pin", errPersonaRunRequestSource)
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil || sealed.Digest != version.ContentDigest {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: stored persona profile digest does not match its immutable image", errPersonaRunRequestSource)
	}
	manifestResolver, err := s.Manifests.ForTenant(ctx, invocation.TenantID)
	if err != nil || manifestResolver == nil {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: create tenant manifest resolver: %v", errPersonaRunRequestSource, err)
	}
	if manifestResolver.TenantID() != invocation.TenantID {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: manifest resolver is bound to another tenant", errPersonaRunRequestSource)
	}
	manifest, err := manifestResolver.ResolveAgentManifestContext(ctx, profile.Manifest)
	if err != nil {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: resolve pinned agent manifest: %v", errPersonaRunRequestSource, err)
	}
	if manifest.ID != profile.Manifest.ID || manifest.Version != uint64(profile.Manifest.Version) || manifest.SchemaVersion != profile.Manifest.SchemaVersion {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: resolved agent manifest identity differs from persona pin", errPersonaRunRequestSource)
	}
	digest, err := manifest.Digest()
	if err != nil || digest != profile.Manifest.Digest {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: resolved agent manifest digest differs from persona pin", errPersonaRunRequestSource)
	}
	owner, err := s.OwnerFacts.ResolvePersonaRunOwnerFacts(ctx, invocation)
	if err != nil {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: resolve owner facts: %v", errPersonaRunRequestSource, err)
	}
	if owner.Audience.ID != invocation.ConversationID || owner.Context.ID != invocation.ThreadID ||
		!required(owner.LegalEntityID) || !required(owner.AgentPrincipal) || !required(owner.Audience.SnapshotID) || !required(owner.Context.SnapshotID) ||
		!validPersonaRunDigest(owner.Audience.Digest) || !validPersonaRunDigest(owner.Context.Digest) || owner.Deadline.IsZero() ||
		owner.Budget.MaxCostMicros == 0 || owner.Budget.MaxInputTokens == 0 || owner.Budget.MaxOutputTokens == 0 {
		return PersonaRunRequestFacts{}, fmt.Errorf("%w: owner facts are incomplete or out of invocation scope", errPersonaRunRequestSource)
	}
	return PersonaRunRequestFacts{
		TenantID: invocation.TenantID, LegalEntityID: owner.LegalEntityID,
		Agent:          agentrun.VersionRef{AgentID: manifest.ID, Version: strconv.FormatUint(manifest.Version, 10), Digest: digest},
		AgentPrincipal: owner.AgentPrincipal, PersonaDigest: version.ContentDigest,
		Audience: owner.Audience, Context: owner.Context, Deadline: owner.Deadline,
		Budget: owner.Budget, TriggerID: invocation.InvocationID,
	}, nil
}

func validPersonaRunInvocation(i agentinvoke.RunRequest) bool {
	if i.Mode != agentinvoke.OnBehalfOf || values.TenantId(i.TenantID).Validate() != nil {
		return false
	}
	for _, value := range []string{i.ConversationID, i.ThreadID, i.InvokingPostID, i.InvokerID, i.PersonaID, i.PersonaVersion, i.InstallationID, i.InvocationID} {
		if !required(value) {
			return false
		}
	}
	actor := i.Actor
	for _, value := range []string{actor.UserID, actor.PersonaID, actor.PersonaVersion, actor.InstallationID, actor.ConversationID, actor.InvokingPostID, actor.InvocationID} {
		if !required(value) {
			return false
		}
	}
	return actor.InvocationID == i.InvocationID && actor.UserID == i.InvokerID && actor.PersonaID == i.PersonaID &&
		actor.PersonaVersion == i.PersonaVersion && actor.InstallationID == i.InstallationID &&
		actor.ConversationID == i.ConversationID && actor.InvokingPostID == i.InvokingPostID
}

func validPersonaRunDigest(value string) bool { return personaRequestDigest(value) }

func personaRunVersionMatches(version int64, label string) bool {
	label = strings.TrimPrefix(label, "v")
	parsed, err := strconv.ParseInt(label, 10, 64)
	return err == nil && parsed == version && strconv.FormatInt(parsed, 10) == label
}

func personaRunAgentVersion(ref agentpersona.AgentManifestRef) string {
	return fmt.Sprintf("%s@%d", ref.ID, ref.Version)
}
